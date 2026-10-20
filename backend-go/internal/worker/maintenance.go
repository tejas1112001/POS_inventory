package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Maintenance runs the periodic jobs. Each walks the active tenants and
// works in one tenant session at a time.
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

// eachTenant runs fn per tenant; one tenant's failure does not stop the
// others, and the errors are joined.
func (m *Maintenance) eachTenant(ctx context.Context, job string, fn func(tenancy.Tenant) error) error {
	ts, err := m.tenants(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, t := range ts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := fn(t); err != nil {
			m.Log.WarnContext(ctx, "maintenance failed for tenant", slog.String("job", job), slog.String("tenant_id", t.ID), slog.Any("err", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SweepPOSLocks deletes POS locks that expired more than a minute ago
// (they are already ignored; this keeps the table small).
func (m *Maintenance) SweepPOSLocks(ctx context.Context) (int64, error) {
	var total int64
	err := m.eachTenant(ctx, "pos_lock_sweep", func(t tenancy.Tenant) error {
		return m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			tag, err := tx.Exec(ctx, `DELETE FROM pos_locks WHERE expires_at < now() - interval '1 minute'`)
			total += tag.RowsAffected()
			return err
		})
	})
	return total, err
}

// CleanupIdempotencyKeys deletes keys older than 24 hours (S2).
func (m *Maintenance) CleanupIdempotencyKeys(ctx context.Context) (int64, error) {
	var total int64
	err := m.eachTenant(ctx, "idempotency_cleanup", func(t tenancy.Tenant) error {
		return m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			tag, err := tx.Exec(ctx, `DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'`)
			total += tag.RowsAffected()
			return err
		})
	})
	return total, err
}

// maxReported caps the mismatches written to one audit entry.
const maxReported = 20

// ReconcileLedgers compares every inventory row with its stock ledger
// (S1). Mismatches are logged, counted and written to that tenant's audit
// log as a system entry; nothing is changed automatically.
func (m *Maintenance) ReconcileLedgers(ctx context.Context) (int, error) {
	var total int
	err := m.eachTenant(ctx, "ledger_reconcile", func(t tenancy.Tenant) error {
		var found []inventory.Mismatch
		if err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly, StatementTimeout: 2 * time.Minute}, func(tx tenancy.TenantTx) error {
			var err error
			found, err = inventory.Reconcile(ctx, tx)
			return err
		}); err != nil {
			return err
		}
		if len(found) == 0 {
			return nil
		}
		total += len(found)
		m.Metrics.LedgerMismatch.Add(float64(len(found)))
		m.Log.WarnContext(ctx, "stock ledger mismatch", slog.String("tenant_id", t.ID), slog.Int("rows", len(found)))
		items := make([]map[string]any, 0, min(len(found), maxReported))
		for _, f := range found[:min(len(found), maxReported)] {
			items = append(items, map[string]any{"store_id": f.StoreID, "variant_id": f.VariantID, "qty": f.Qty, "ledger_sum": f.LedgerSum})
		}
		return m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			return audit.Write(ctx, tx, audit.Entry{ActorType: audit.ActorSystem, Action: "stock.ledger_mismatch",
				Entity: "inventory", After: map[string]any{"rows": len(found), "items": items}})
		})
	})
	return total, err
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
