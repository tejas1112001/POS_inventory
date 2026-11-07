//go:build integration

package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// TENANCY §5.1 "re-run at scale in Phase 3": 50 tenants, pool sizes 1, 2
// and 8, every transaction kind at once (reads, writes, errors, panics,
// cancellations, statement timeouts). Every successful transaction must
// run as its own tenant and see only its own rows; every failed one must
// commit nothing; every released connection must be clean.
func TestSearchPathAtScaleWithMixedFailures(t *testing.T) {
	ctx := context.Background()
	const nTenants, perSize = 50, 1200
	tenants := make([]tenancy.Tenant, nTenants)
	for i := range tenants {
		tenants[i] = newTenant(t, "", "")
	}
	seed := sessions(tdb.AppPool(t, 4), nil)
	for _, tn := range tenants {
		require.NoError(t, seed.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
			_, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ($1)`, "only-"+tn.ID)
			return err
		}))
	}

	var writes [nTenants]atomic.Int64
	boom := errors.New("boom")
	for _, size := range []int32{1, 2, 8} {
		t.Run(fmt.Sprintf("pool=%d", size), func(t *testing.T) {
			p := tdb.AppPool(t, size)
			s := sessions(p, nil)
			errs := make(chan error, perSize)
			var wg sync.WaitGroup
			for i := range perSize {
				idx := (i * 7) % nTenants // neighbours in time hit different tenants
				tn := tenants[idx]
				kind := i % 6
				wg.Go(func() {
					switch kind {
					case 0: // read: own role, own path, own rows only
						errs <- s.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
							var user, path, tid string
							if err := tx.QueryRow(ctx, `SELECT current_user::text, current_setting('search_path'), current_setting('app.tenant_id')`).Scan(&user, &path, &tid); err != nil {
								return err
							}
							if user != tn.Role.String() || path != tn.Schema.SearchPath() || tid != tn.ID {
								return fmt.Errorf("tenant %s ran as %s path %q tid %s", tn.ID, user, path, tid)
							}
							rows, err := tx.Query(ctx, `SELECT DISTINCT name FROM products`)
							if err != nil {
								return err
							}
							names, err := pgx.CollectRows(rows, pgx.RowTo[string])
							if err != nil {
								return err
							}
							for _, n := range names {
								if n != "only-"+tn.ID && n != "w-"+tn.ID {
									return fmt.Errorf("tenant %s saw %q", tn.ID, n)
								}
							}
							return nil
						})
					case 1: // write, unqualified table name
						err := s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
							_, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ($1)`, "w-"+tn.ID)
							return err
						})
						if err == nil {
							writes[idx].Add(1)
						}
						errs <- err
					case 2: // handler error after a write
						err := s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
							if _, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ($1)`, "err-"+tn.ID); err != nil {
								return err
							}
							return boom
						})
						if !errors.Is(err, boom) {
							err = fmt.Errorf("want boom, got %w", err)
						} else {
							err = nil
						}
						errs <- err
					case 3: // panic after a write
						func() {
							defer func() {
								if recover() == nil {
									errs <- errors.New("panic swallowed")
									return
								}
								errs <- nil
							}()
							_ = s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
								if _, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ($1)`, "panic-"+tn.ID); err != nil {
									return err
								}
								panic("handler panic")
							})
						}()
					case 4: // context cancelled mid-query (or while waiting for the pool)
						cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
						err := s.InTenantTx(cctx, tn, rw, func(tx tenancy.TenantTx) error {
							if _, err := tx.Exec(cctx, `INSERT INTO products (name) VALUES ($1)`, "cancel-"+tn.ID); err != nil {
								return err
							}
							_, err := tx.Exec(cctx, `SELECT pg_sleep(2)`)
							return err
						})
						cancel()
						if err == nil {
							err = errors.New("cancelled transaction committed")
						} else {
							err = nil
						}
						errs <- err
					case 5: // statement timeout
						err := s.InTenantTx(ctx, tn, tenancy.TxOptions{Access: tenancy.ReadWrite, StatementTimeout: 30 * time.Millisecond}, func(tx tenancy.TenantTx) error {
							if _, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ($1)`, "slow-"+tn.ID); err != nil {
								return err
							}
							_, err := tx.Exec(ctx, `SELECT pg_sleep(2)`)
							return err
						})
						if err == nil {
							err = errors.New("timed-out transaction committed")
						} else {
							err = nil
						}
						errs <- err
					}
				})
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			assertCleanReleased(t, p)
		})
	}

	// Each schema holds its seed row, exactly its own committed writes and
	// nothing from a failed transaction or another tenant.
	var total int64
	for i := range writes {
		total += writes[i].Load()
	}
	require.EqualValues(t, 3*perSize/6, total, "every write committed")
	for i, tn := range tenants {
		require.NoError(t, seed.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
			var only, w, other int64
			err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE name = $1), count(*) FILTER (WHERE name = $2),
				count(*) FILTER (WHERE name NOT IN ($1, $2)) FROM products`, "only-"+tn.ID, "w-"+tn.ID).Scan(&only, &w, &other)
			if err != nil {
				return err
			}
			require.EqualValues(t, 1, only, tn.ID)
			require.Equal(t, writes[i].Load(), w, "committed writes for %s", tn.ID)
			require.Zero(t, other, "rows from failed transactions or other tenants in %s", tn.ID)
			return nil
		}))
	}
}
