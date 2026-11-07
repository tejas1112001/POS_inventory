package worker

import (
	"context"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Usage snapshot (docs/S9_SECURITY_NOTE.md, approved): the worker counts
// each tenant's users, stores, products and receipt emails in the tenant's
// own session and writes the counts (no names, emails or amounts) to
// platform.tenant_usage. The console reads only that table.

const (
	qUsageCounts = `SELECT
		(SELECT count(*) FROM users WHERE deleted_at IS NULL),
		(SELECT count(*) FROM stores WHERE deleted_at IS NULL AND status = 'Active'),
		(SELECT count(*) FROM products WHERE deleted_at IS NULL),
		(SELECT count(*) FROM email_outbox WHERE status = 'sent' AND created_at > now() - interval '7 days'),
		(SELECT count(*) FROM email_outbox WHERE status IN ('failed','dead') AND created_at > now() - interval '7 days'),
		(SELECT count(*) FROM email_outbox WHERE status IN ('pending','sending'))`
	qUsageUpsert = `INSERT INTO platform.tenant_usage (tenant_id, users, stores, products, receipts_sent_7d, receipts_failed_7d, receipts_waiting, taken_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (tenant_id) DO UPDATE SET users = EXCLUDED.users, stores = EXCLUDED.stores, products = EXCLUDED.products,
			receipts_sent_7d = EXCLUDED.receipts_sent_7d, receipts_failed_7d = EXCLUDED.receipts_failed_7d,
			receipts_waiting = EXCLUDED.receipts_waiting, taken_at = now()`
)

// SnapshotUsage refreshes every live tenant's usage row; it returns how many
// tenants were written.
func (m *Maintenance) SnapshotUsage(ctx context.Context) (int, error) {
	n := 0
	err := m.eachTenant(ctx, JobUsageSnapshot, func(t tenancy.Tenant) error {
		var c [6]int64
		if err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
			return tx.QueryRow(ctx, qUsageCounts).Scan(&c[0], &c[1], &c[2], &c[3], &c[4], &c[5])
		}); err != nil {
			return err
		}
		if err := m.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			_, err := tx.Exec(ctx, qUsageUpsert, t.ID, c[0], c[1], c[2], c[3], c[4], c[5])
			return err
		}); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}
