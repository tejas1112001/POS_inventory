package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Maintenance runs the periodic jobs. Each walks the active tenants a few
// at a time (TenantParallelism), each tenant in its own session with its
// own time limit, so one slow or broken shop never holds up the rest
// (ARCHITECTURE_REVIEW M2). The stock reconcile, the heaviest, runs as
// one job per shop instead (ReconcileTenant).
type Maintenance struct {
	Jobs    *tenancy.Sessions
	Metrics *obs.Metrics
	Log     *slog.Logger
	// EnqueueMail queues delivery jobs (River in production).
	EnqueueMail func(ctx context.Context, tenantIDs []string, platform bool) error
}

const qActiveTenants = `SELECT id, slug, name, status, coalesce(plan_id, 0) FROM platform.tenants
	WHERE status NOT IN ('provisioning','failed','deleted') ORDER BY id`

// tenants lists the tenants whose schemas are live.
func (m *Maintenance) tenants(ctx context.Context) ([]tenancy.Tenant, error) {
	var out []tenancy.Tenant
	err := m.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qActiveTenants)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t tenancy.Tenant
			if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.PlanID); err != nil {
				return err
			}
			if t.Schema, t.Role, err = tenancy.SchemaFor(t.ID); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// TenantParallelism is how many tenants one maintenance job works on at
// once. The jobs pool has 10 connections shared with mail and receipts.
const TenantParallelism = 3

// TenantJobTimeout limits one tenant's share of a sweep.
const TenantJobTimeout = 2 * time.Minute

// eachTenant runs fn for every live tenant, TenantParallelism at a time,
// each under TenantJobTimeout, and returns the sum of what fn returned. One
// tenant's failure does not stop the others; the errors are joined.
func (m *Maintenance) eachTenant(ctx context.Context, job string, fn func(context.Context, tenancy.Tenant) (int64, error)) (int64, error) {
	ts, err := m.tenants(ctx)
	if err != nil {
		return 0, err
	}
	var (
		mu    sync.Mutex
		total int64
		errs  []error
		wg    sync.WaitGroup
		sem   = make(chan struct{}, TenantParallelism)
	)
loop:
	for _, t := range ts {
		select {
		case <-ctx.Done():
			break loop
		case sem <- struct{}{}:
		}
		wg.Go(func() {
			defer func() { <-sem }()
			tctx, cancel := context.WithTimeout(ctx, TenantJobTimeout)
			defer cancel()
			n, err := fn(tctx, t)
			mu.Lock()
			defer mu.Unlock()
			total += n
			if err != nil {
				m.Log.WarnContext(ctx, "maintenance failed for tenant", slog.String("job", job), slog.String("tenant_id", t.ID), slog.Any("err", err))
				errs = append(errs, err)
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		errs = append(errs, ctx.Err())
	}
	return total, errors.Join(errs...)
}

// SweepPOSLocks deletes POS locks that expired more than a minute ago
// (they are already ignored; this keeps the table small).
func (m *Maintenance) SweepPOSLocks(ctx context.Context) (int64, error) {
	return m.eachTenant(ctx, "pos_lock_sweep", func(ctx context.Context, t tenancy.Tenant) (int64, error) {
		var n int64
		err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			tag, err := tx.Exec(ctx, `DELETE FROM pos_locks WHERE expires_at < now() - interval '1 minute'`)
			n += tag.RowsAffected()
			if err != nil {
				return err
			}
			// Held carts past their 7 days (S10 item 7).
			tag, err = tx.Exec(ctx, `DELETE FROM held_carts WHERE expires_at < now()`)
			n += tag.RowsAffected()
			return err
		})
		return n, err
	})
}

// CleanupIdempotencyKeys deletes keys older than 24 hours (S2).
func (m *Maintenance) CleanupIdempotencyKeys(ctx context.Context) (int64, error) {
	return m.eachTenant(ctx, "idempotency_cleanup", func(ctx context.Context, t tenancy.Tenant) (int64, error) {
		var n int64
		err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			tag, err := tx.Exec(ctx, `DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'`)
			n = tag.RowsAffected()
			return err
		})
		return n, err
	})
}

// maxReported caps the mismatches written to one audit entry.
const maxReported = 20

// ReconcileTimeout limits one shop's stock reconcile (a full check adds
// up its whole ledgers).
const ReconcileTimeout = 10 * time.Minute

// ReconcileLedgers checks every live tenant's stock against its ledgers
// (S1), a few at a time. Production runs one job per shop instead
// (TenantIDs + ReconcileTenant); this is for posctl and tests.
func (m *Maintenance) ReconcileLedgers(ctx context.Context) (int, error) {
	n, err := m.eachTenant(ctx, "ledger_reconcile", func(ctx context.Context, t tenancy.Tenant) (int64, error) {
		found, err := m.ReconcileTenant(ctx, t)
		return int64(found), err
	})
	return int(n), err
}

// TenantIDs lists the live tenants (the per-shop job dispatcher).
func (m *Maintenance) TenantIDs(ctx context.Context) ([]string, error) {
	ts, err := m.tenants(ctx)
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	return ids, err
}

// ReconcileTenant compares one shop's stock with its ledgers, from
// checkpoints when recent (inventory.ReconcileCheckpointed). Mismatches
// are logged, counted and written to the shop's audit log as a system
// entry; nothing is changed automatically. It returns how many were found.
func (m *Maintenance) ReconcileTenant(ctx context.Context, t tenancy.Tenant) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, ReconcileTimeout)
	defer cancel()
	var res inventory.ReconcileResult
	if err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite, StatementTimeout: ReconcileTimeout}, func(tx tenancy.TenantTx) error {
		var err error
		res, err = inventory.ReconcileCheckpointed(ctx, tx, time.Now())
		return err
	}); err != nil {
		return 0, err
	}
	found := res.Mismatches
	if len(found) == 0 {
		return 0, nil
	}
	m.Metrics.LedgerMismatch.Add(float64(len(found)))
	m.Log.WarnContext(ctx, "stock ledger mismatch", slog.String("tenant_id", t.ID), slog.Int("rows", len(found)), slog.Bool("full", res.Full))
	items := make([]map[string]any, 0, min(len(found), maxReported))
	for _, f := range found[:min(len(found), maxReported)] {
		it := map[string]any{"store_id": f.StoreID, "variant_id": f.VariantID, "qty": f.Qty, "ledger_sum": f.LedgerSum}
		if f.Kind != "" { // industry P3: batch checks
			it["kind"], it["batch_id"] = f.Kind, f.BatchID
		}
		items = append(items, it)
	}
	return len(found), m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		return audit.Write(ctx, tx, audit.Entry{ActorType: audit.ActorSystem, Action: "stock.ledger_mismatch",
			Entity: "inventory", After: map[string]any{"rows": len(found), "items": items}})
	})
}

// SweepMail queues delivery for every tenant whose dispatch mark is due,
// and for the platform outbox, and refreshes the outbox depth gauges. It
// is the safety net behind the LISTEN wake-up (EMAIL_AND_RECEIPTS §4.3).
func (m *Maintenance) SweepMail(ctx context.Context) error {
	var (
		due           []string
		platformDue   bool
		platformDepth int
	)
	err := m.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id FROM platform.mail_dispatch WHERE next_attempt_at <= now() ORDER BY next_attempt_at LIMIT 1000`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			due = append(due, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT (count(*) FILTER (WHERE `+dueWhere+`)) > 0, count(*) FROM platform.email_outbox
			WHERE status IN ('pending','failed','sending')`).Scan(&platformDue, &platformDepth)
	})
	if err != nil {
		return err
	}
	m.Metrics.OutboxDepth.WithLabelValues("platform").Set(float64(platformDepth))
	m.Metrics.OutboxDepth.WithLabelValues("tenant").Set(float64(m.tenantDepth(ctx)))
	if len(due) == 0 && !platformDue {
		return nil
	}
	return m.EnqueueMail(ctx, due, platformDue)
}

// tenantDepth sums waiting mail over the tenants that have a dispatch mark
// (at most 200 per sweep; the gauge is a trend, not an exact count).
func (m *Maintenance) tenantDepth(ctx context.Context) int {
	var ids []string
	if err := m.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id FROM platform.mail_dispatch ORDER BY tenant_id LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		return 0
	}
	total := 0
	for _, id := range ids {
		s, r, err := tenancy.SchemaFor(id)
		if err != nil {
			continue
		}
		var n int
		if err := m.Jobs.InTenantTx(ctx, tenancy.Tenant{ID: id, Schema: s, Role: r}, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM email_outbox WHERE status IN ('pending','failed','sending')`).Scan(&n)
		}); err == nil {
			total += n
		}
	}
	return total
}

// EndDeletedSessions revokes every refresh token of tenants deleted in the
// last 3 days (S10 item 4). Their hosts no longer resolve, so no session
// works anyway; this makes it explicit. It returns how many were revoked.
func (m *Maintenance) EndDeletedSessions(ctx context.Context) (int64, error) {
	var ids []string
	err := m.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM platform.tenants WHERE status = 'deleted' AND deleted_at > now() - interval '3 days' ORDER BY id`)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, err
	}
	var total int64
	var errs []error
	for _, id := range ids {
		t := tenancy.Tenant{ID: id, Status: "deleted"}
		if t.Schema, t.Role, err = tenancy.SchemaFor(id); err != nil {
			errs = append(errs, err)
			continue
		}
		err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			tag, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now(), revoke_reason = 'tenant_deleted' WHERE revoked_at IS NULL`)
			total += tag.RowsAffected()
			return err
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}
