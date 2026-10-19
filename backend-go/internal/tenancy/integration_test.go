//go:build integration

package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

var tdb *testdb.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	tdb, err = testdb.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = tdb.Stop(ctx)
	os.Exit(code)
}

var (
	tenantSeq int
	seqMu     sync.Mutex
)

func newTenant(t *testing.T, status, extraDDL string) tenancy.Tenant {
	t.Helper()
	seqMu.Lock()
	tenantSeq++
	slug := fmt.Sprintf("shop%d-%s", tenantSeq, strings.ToLower(t.Name()[len(t.Name())-3:]))
	seqMu.Unlock()
	slug = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return 'x'
	}, slug)
	tn, err := tdb.CreateTenant(context.Background(), testdb.TenantSpec{Slug: slug, Status: status, ExtraDDL: extraDDL})
	require.NoError(t, err)
	return tn
}

func sessions(p *pgxpool.Pool, tampered prometheus.Counter) *tenancy.Sessions {
	return tenancy.NewSessions(p, 5*time.Second, tampered, nil)
}

var rw = tenancy.TxOptions{Access: tenancy.ReadWrite}

// assertCleanReleased checks every idle pool connection: empty search_path,
// pos_app, no open transaction.
func assertCleanReleased(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	n := int(p.Stat().TotalConns())
	conns := make([]*pgxpool.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			c.Release()
		}
	}()
	for range n {
		c, err := p.Acquire(ctx)
		require.NoError(t, err)
		conns = append(conns, c)
		var user, path string
		require.NoError(t, c.QueryRow(ctx, `SELECT current_user::text, current_setting('search_path')`).Scan(&user, &path))
		require.Equal(t, "pos_app", user)
		require.Contains(t, []string{"", `""`}, path, "released connection must have the empty default search_path")
		require.Equal(t, byte('I'), c.Conn().PgConn().TxStatus())
	}
}

// --- TENANCY §5.4 extensions tests 1–5 ---------------------------------

func TestSearchPathExactInsideTxAndEmptyAfter(t *testing.T) { // §5.4 test 1
	for _, size := range []int32{1, 2} {
		t.Run(fmt.Sprintf("pool=%d", size), func(t *testing.T) {
			tn := newTenant(t, "", "")
			p := tdb.AppPool(t, size)
			s := sessions(p, nil)
			err := s.InTenantTx(context.Background(), tn, rw, func(tx tenancy.TenantTx) error {
				var path, user, tid string
				if err := tx.QueryRow(context.Background(), `SELECT current_setting('search_path'), current_user::text, current_setting('app.tenant_id')`).Scan(&path, &user, &tid); err != nil {
					return err
				}
				require.Equal(t, tn.Schema.SearchPath(), path)
				require.Equal(t, tn.Role.String(), user)
				require.Equal(t, tn.ID, tid)
				return nil
			})
			require.NoError(t, err)
			assertCleanReleased(t, p)
		})
	}
}

func TestCitextCaseInsensitive(t *testing.T) { // §5.4 test 2
	tn := newTenant(t, "", "")
	s := sessions(tdb.AppPool(t, 2), nil)
	ctx := context.Background()
	err := s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
		var eq bool
		require.NoError(t, tx.QueryRow(ctx, `SELECT 'A@x.io'::citext = 'a@x.io'::citext`).Scan(&eq))
		require.True(t, eq)
		_, err := tx.Exec(ctx, `INSERT INTO users (name, email) VALUES ('A', 'A@x.io')`)
		return err
	})
	require.NoError(t, err)
	err = s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
		_, err := tx.Exec(ctx, `INSERT INTO users (name, email) VALUES ('B', 'a@x.io')`)
		return err
	})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code)
	require.Equal(t, "users_email_uq", pgErr.ConstraintName)
}

func TestTrigramIndexUsed(t *testing.T) { // §5.4 test 3
	tn := newTenant(t, "", "")
	s := sessions(tdb.AppPool(t, 1), nil)
	ctx := context.Background()
	err := s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO products (name) SELECT 'product ' || g FROM generate_series(1, 2000) g`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('enable_seqscan', 'off', true)`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM products WHERE name % 'prodcut 42'`)
		if err != nil {
			return err
		}
		plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		require.Contains(t, strings.Join(plan, "\n"), "products_name_trgm")
		return nil
	})
	require.NoError(t, err)
}

func TestTenantRoleCannotCreateOrSeePlatform(t *testing.T) { // §5.4 test 4
	tn := newTenant(t, "", "")
	other := newTenant(t, "", "")
	s := sessions(tdb.AppPool(t, 1), nil)
	ctx := context.Background()
	for name, q := range map[string]string{
		"create in extensions": `CREATE TABLE extensions.evil (x int)`,
		"create in public":     `CREATE TABLE public.evil (x int)`,
		"read platform":        `SELECT count(*) FROM platform.tenants`,
	} {
		t.Run(name, func(t *testing.T) {
			err := s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
				_, err := tx.Exec(ctx, q)
				return err
			})
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "42501", pgErr.Code, "permission denied")
		})
	}
	t.Run("read other tenant", func(t *testing.T) {
		var priv bool
		err := s.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
			return tx.QueryRow(ctx, `SELECT has_schema_privilege($1, 'USAGE')`, other.Schema.String()).Scan(&priv)
		})
		require.NoError(t, err)
		require.False(t, priv)
	})
}

func TestWithoutExtensionsCitextBecomesCaseSensitive(t *testing.T) { // §5.4 test 5 (negative)
	tn := newTenant(t, "", "")
	p := tdb.AppPool(t, 1)
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, db.SetLocalRole(ctx, tx, tn.Role.String()))
	_, err = tx.Exec(ctx, `SELECT set_config('search_path', $1, true)`, `"`+tn.Schema.String()+`"`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO users (name, email) VALUES ('A', 'A@x.io')`)
	require.NoError(t, err)
	var n int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'a@x.io'`).Scan(&n))
	require.Equal(t, 0, n, "without extensions on the path the citext operator is not found and the match is case-sensitive")
}

// --- TENANCY §5.1 pool and context leaks ---------------------------------

func TestConcurrentTenantsSeeOnlyTheirRows(t *testing.T) {
	for _, size := range []int32{1, 2} {
		t.Run(fmt.Sprintf("pool=%d", size), func(t *testing.T) {
			p := tdb.AppPool(t, size)
			s := sessions(p, nil)
			ctx := context.Background()
			tenants := []tenancy.Tenant{newTenant(t, "", ""), newTenant(t, "", ""), newTenant(t, "", "")}
			for _, tn := range tenants {
				require.NoError(t, s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
					_, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ($1)`, "only-"+tn.ID)
					return err
				}))
			}
			var wg sync.WaitGroup
			errs := make(chan error, 300)
			for i := range 300 {
				tn := tenants[i%3]
				wg.Go(func() {
					errs <- s.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
						rows, err := tx.Query(ctx, `SELECT name FROM products`)
						if err != nil {
							return err
						}
						names, err := pgx.CollectRows(rows, pgx.RowTo[string])
						if err != nil {
							return err
						}
						if len(names) != 1 || names[0] != "only-"+tn.ID {
							return fmt.Errorf("tenant %s saw %v", tn.ID, names)
						}
						return nil
					})
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
}

func TestErrorPanicAndCancelLeaveConnectionsClean(t *testing.T) {
	tn := newTenant(t, "", "")
	p := tdb.AppPool(t, 1)
	s := sessions(p, nil)
	ctx := context.Background()

	err := s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
		_, _ = tx.Exec(ctx, `INSERT INTO products (name) VALUES ('rolled back')`)
		return errors.New("boom")
	})
	require.EqualError(t, err, "boom")
	assertCleanReleased(t, p)

	require.Panics(t, func() {
		_ = s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error { panic("handler panic") })
	})
	assertCleanReleased(t, p)

	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	err = s.InTenantTx(cctx, tn, rw, func(tx tenancy.TenantTx) error {
		_, err := tx.Exec(cctx, `SELECT pg_sleep(5)`)
		return err
	})
	require.Error(t, err)
	assertCleanReleased(t, p)

	var n int
	require.NoError(t, s.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&n)
	}))
	require.Zero(t, n, "failed transactions committed nothing")
}

func TestStatementTimeoutApplies(t *testing.T) {
	tn := newTenant(t, "", "")
	s := sessions(tdb.AppPool(t, 1), nil)
	ctx := context.Background()
	err := s.InTenantTx(ctx, tn, tenancy.TxOptions{Access: tenancy.ReadOnly, StatementTimeout: 100 * time.Millisecond}, func(tx tenancy.TenantTx) error {
		_, err := tx.Exec(ctx, `SELECT pg_sleep(2)`)
		return err
	})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "57014", pgErr.Code, "query_canceled by statement_timeout")
}

func TestReadOnlyTransactionsRejectWrites(t *testing.T) {
	tn := newTenant(t, "", "")
	s := sessions(tdb.AppPool(t, 1), nil)
	ctx := context.Background()
	err := s.InTenantTx(ctx, tn, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		_, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ('x')`)
		return err
	})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "25006", pgErr.Code)
}

func TestReleasedInTransactionIsDestroyed(t *testing.T) {
	p := tdb.AppPool(t, 1)
	ctx := context.Background()
	c, err := p.Acquire(ctx)
	require.NoError(t, err)
	pid := c.Conn().PgConn().PID()
	_, err = c.Exec(ctx, `BEGIN`)
	require.NoError(t, err)
	c.Release()

	c2, err := p.Acquire(ctx)
	require.NoError(t, err)
	defer c2.Release()
	require.NotEqual(t, pid, c2.Conn().PgConn().PID(), "a connection released inside a transaction must not be reused")
}

func TestAppRoleOutsideSessionHasNoAccess(t *testing.T) {
	tn := newTenant(t, "", "")
	p := tdb.AppPool(t, 1)
	ctx := context.Background()
	_, err := p.Exec(ctx, `SELECT count(*) FROM products`)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "42P01", pgErr.Code, "empty search_path: relation does not exist")

	_, err = p.Exec(ctx, `SELECT count(*) FROM "`+tn.Schema.String()+`".products`)
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "42501", pgErr.Code, "pos_app has no direct privileges")
}

// --- tripwire --------------------------------------------------------------

func TestTamperedSessionRollsBack(t *testing.T) {
	a := newTenant(t, "", "")
	b := newTenant(t, "", "")
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "t"})
	s := sessions(tdb.AppPool(t, 1), counter)
	ctx := context.Background()

	err := s.InTenantTx(ctx, a, rw, func(tx tenancy.TenantTx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ('should vanish')`); err != nil {
			return err
		}
		// Simulates injected SQL switching tenant inside one statement.
		_, err := tx.Exec(ctx, `SELECT set_config('role', $1, true), set_config('search_path', $2, true)`, b.Role.String(), b.Schema.SearchPath())
		return err
	})
	require.ErrorIs(t, err, tenancy.ErrSessionTampered)
	require.Equal(t, 1.0, testutil.ToFloat64(counter))

	var n int
	require.NoError(t, s.InTenantTx(ctx, a, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&n)
	}))
	require.Zero(t, n)
}

// --- TENANCY §5.2 statement cache decision ----------------------------------

const skewDDL = `ALTER TABLE products ADD COLUMN price numeric(18,4) NOT NULL DEFAULT 0;`

func TestVersionSkewSameSQLSameConnection(t *testing.T) { // §5.2 test (a) and (c)
	oldT := newTenant(t, "", "")
	newT := newTenant(t, "", skewDDL)
	s := sessions(tdb.AppPool(t, 1), nil)
	ctx := context.Background()
	for _, tn := range []tenancy.Tenant{oldT, newT} {
		require.NoError(t, s.InTenantTx(ctx, tn, rw, func(tx tenancy.TenantTx) error {
			_, err := tx.Exec(ctx, `INSERT INTO products (name) VALUES ('p')`)
			return err
		}))
	}
	for range 5 {
		for _, tn := range []tenancy.Tenant{oldT, newT, oldT} {
			err := s.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
				rows, err := tx.Query(ctx, `SELECT * FROM products`)
				if err != nil {
					return err
				}
				defer rows.Close()
				want := 2
				if tn.ID == newT.ID {
					want = 3
				}
				if got := len(rows.FieldDescriptions()); got != want {
					return fmt.Errorf("tenant %s: %d columns, want %d", tn.ID, got, want)
				}
				for rows.Next() {
				}
				return rows.Err()
			})
			require.NoError(t, err)
		}
	}
}

// TestCacheStatementModeBreaks documents why the pool uses Exec mode: with
// pgx's default statement cache, the same SQL text under another tenant's
// search_path reuses a stale description.
func TestCacheStatementModeBreaks(t *testing.T) { // §5.2 test (b)
	oldT := newTenant(t, "", "")
	newT := newTenant(t, "", skewDDL)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(tdb.AppDSN.Reveal())
	require.NoError(t, err)
	cfg.MaxConns = 1
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer p.Close()

	run := func(tn tenancy.Tenant) error {
		return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
			if err := db.SetLocalRole(ctx, tx, tn.Role.String()); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT set_config('search_path', $1, true)`, tn.Schema.SearchPath()); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT * FROM products`)
			if err != nil {
				return err
			}
			defer rows.Close()
			n := len(rows.FieldDescriptions())
			for rows.Next() {
			}
			if err := rows.Err(); err != nil {
				return err
			}
			want := 2
			if tn.ID == newT.ID {
				want = 3
			}
			if n != want {
				return fmt.Errorf("stale description: %d columns, want %d", n, want)
			}
			return nil
		})
	}
	require.NoError(t, run(oldT))
	require.Error(t, run(newT), "CacheStatement mode must fail here; if it ever passes, re-check TENANCY §5.2")
}

// --- registry and resolution ----------------------------------------------

func TestRegistryResolution(t *testing.T) {
	ctx := context.Background()
	s := sessions(tdb.AppPool(t, 2), nil)
	reg := tenancy.NewRegistry(s, nil)

	active := newTenant(t, "active", "")
	got, err := reg.BySlug(ctx, active.Slug)
	require.NoError(t, err)
	require.Equal(t, active.ID, got.ID)
	require.Equal(t, active.Schema, got.Schema)
	byID, err := reg.ByID(ctx, active.ID)
	require.NoError(t, err)
	require.Equal(t, active.Slug, byID.Slug)

	suspended := newTenant(t, "suspended", "")
	got, err = reg.BySlug(ctx, suspended.Slug)
	require.NoError(t, err, "suspended tenants resolve; the gate (step 5) blocks them")
	require.Equal(t, "suspended", got.Status)

	for _, status := range []string{"provisioning", "failed", "deleted"} {
		tn := newTenant(t, status, "")
		_, err := reg.BySlug(ctx, tn.Slug)
		e, ok := apperr.As(err)
		require.True(t, ok, status)
		require.Equal(t, apperr.CodeNotFound, e.Code, status)
	}
	for _, slug := range []string{"nosuchshop", "admin", "www", "a--b", "x", `t_x"; drop`} {
		_, err := reg.BySlug(ctx, slug)
		e, ok := apperr.As(err)
		require.True(t, ok, slug)
		require.Equal(t, apperr.CodeNotFound, e.Code, slug)
	}
}

func TestRegistryNegativeCacheAndFlush(t *testing.T) {
	ctx := context.Background()
	s := sessions(tdb.AppPool(t, 1), nil)
	reg := tenancy.NewRegistry(s, func() time.Duration { return time.Hour })

	_, err := reg.BySlug(ctx, "late-shop")
	require.Error(t, err)
	tn, err := tdb.CreateTenant(ctx, testdb.TenantSpec{Slug: "late-shop"})
	require.NoError(t, err)
	_, err = reg.BySlug(ctx, "late-shop")
	require.Error(t, err, "negative entry still cached")
	reg.Flush()
	got, err := reg.BySlug(ctx, "late-shop")
	require.NoError(t, err)
	require.Equal(t, tn.ID, got.ID)
}

// --- TENANCY §5.5 listener -------------------------------------------------

func TestListenerInvalidatesAndRecovers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := sessions(tdb.AppPool(t, 2), nil)
	l := tenancy.NewListener(tdb.AppDSN, nil, tenancy.ListenerMetrics{})
	reg := tenancy.NewRegistry(s, l.TTL(time.Hour, 300*time.Millisecond))
	l.On(tenancy.ChanTenantChanged, func(string) { reg.Flush() })
	l.OnReconnect(reg.Flush)
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	testdb.Eventually(t, 5*time.Second, l.Healthy, "listener connects")

	tn := newTenant(t, "active", "")
	_, err := reg.BySlug(ctx, tn.Slug)
	require.NoError(t, err)

	admin := tdb.Admin(t)
	setStatus := func(st string) {
		_, err := admin.Exec(ctx, `UPDATE platform.tenants SET status = $1 WHERE id = $2`, st, tn.ID)
		require.NoError(t, err)
	}

	// 1. NOTIFY path: a status change is visible without waiting for the TTL.
	setStatus("suspended")
	testdb.Eventually(t, 3*time.Second, func() bool {
		got, err := reg.BySlug(ctx, tn.Slug)
		return err == nil && got.Status == "suspended"
	}, "tenant_changed flushes the cache")

	// 2. Kill the listener's backend; change status while it is down.
	pid := l.BackendPID()
	_, err = admin.Exec(ctx, `SELECT pg_terminate_backend($1)`, pid)
	require.NoError(t, err)
	testdb.Eventually(t, 3*time.Second, func() bool { return !l.Healthy() }, "listener notices the drop")
	setStatus("deleted")
	// While down, the degraded TTL (300 ms here) bounds staleness.
	testdb.Eventually(t, 3*time.Second, func() bool {
		_, err := reg.BySlug(ctx, tn.Slug)
		return err != nil
	}, "change enforced within the reduced TTL while the listener is down")

	// 3. After reconnect the caches are flushed and notifications flow again.
	testdb.Eventually(t, 5*time.Second, func() bool { return l.Healthy() && l.BackendPID() != pid }, "listener reconnects")
	setStatus("active")
	testdb.Eventually(t, 3*time.Second, func() bool {
		_, err := reg.BySlug(ctx, tn.Slug)
		return err == nil
	}, "notifications work after reconnect")

	cancel()
	<-done
}

func TestResolveMiddlewareGeneric404(t *testing.T) {
	ctx := context.Background()
	s := sessions(tdb.AppPool(t, 2), nil)
	reg := tenancy.NewRegistry(s, nil)
	live := newTenant(t, "active", "")
	gone := newTenant(t, "deleted", "")
	prov := newTenant(t, "provisioning", "")

	h := httpx.NewRouter(httpx.RouterDeps{
		Logger: slog.New(slog.DiscardHandler), Network: httpx.Network{BaseDomain: "pos.test"},
		Health: httpx.NewHealth(time.Second), RequestTimeout: time.Second,
		TenantResolver: tenancy.Resolve(reg, httpx.Network{BaseDomain: "pos.test"}, "pos.test"),
		Tenant: func(r chi.Router) {
			r.Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
				tn, _ := tenancy.TenantFrom(r.Context())
				httpx.WriteJSON(w, 200, map[string]string{"id": tn.ID})
			})
		},
	})
	get := func(slug string) (int, string) {
		req := httptest.NewRequestWithContext(ctx, "GET", "http://"+slug+".pos.test/api/whoami", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	code, body := get(live.Slug)
	require.Equal(t, 200, code)
	require.JSONEq(t, `{"id":"`+live.ID+`"}`, body)

	_, ref := get("nosuchtenant")
	for _, slug := range []string{gone.Slug, prov.Slug, "nosuchtenant", "admin2x", "www"} {
		code, body := get(slug)
		require.Equal(t, 404, code, slug)
		require.Equal(t, ref, body, "identical body for %s", slug)
	}
}
