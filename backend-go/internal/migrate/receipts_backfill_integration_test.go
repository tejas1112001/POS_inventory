//go:build integration

package migrate_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// TestReceiptsBackfill (industry P1 item 8, DB13-DB15): migration 00022
// gives every received purchase one receipt dated when it was received,
// holding each line's full quantity; other purchases get none. Report:
// purchases, receipts and lines created, mismatches (must be 0).
func TestReceiptsBackfill(t *testing.T) {
	ctx := context.Background()
	s, err := tenancy.ScratchFor("bkfill0022", time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NoError(t, migrate.CreateScratch(ctx, tdb.MigratorDSN, s))
	t.Cleanup(func() { _ = migrate.DropScratch(context.Background(), tdb.MigratorDSN, s) })
	v, err := migrate.UpScratch(ctx, tdb.MigratorDSN, s, 21)
	require.NoError(t, err)
	require.EqualValues(t, 21, v)

	admin := tdb.Admin(t)
	tx, err := admin.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SET LOCAL search_path TO "+s.String()+", extensions")
	require.NoError(t, err)
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := tx.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO roles (name, is_system, system_key) VALUES ('Owner', true, 'owner')`)
	exec(`INSERT INTO users (name, email, role_id, status) VALUES ('Owner', 'owner@shop.test', 1, 'Active')`)
	exec(`INSERT INTO stores (name, slug) VALUES ('Main', 'main')`)
	exec(`INSERT INTO suppliers (name, status) VALUES ('Supplier', 'Active')`)
	exec(`INSERT INTO products (name, product_type, status) VALUES ('Tee', 'variable', 'Active')`)
	exec(`INSERT INTO product_variants (product_id, sku, price) VALUES (1, 'T-M', 100), (1, 'T-L', 100)`)
	// Received (two lines, received later than ordered), received again
	// (one line), ordered, pending, cancelled.
	purchases := []struct {
		ref, status string
		received    any
		lines       []int
	}{
		{"INV-1", "Received", "2026-10-02 11:00:00+05:30", []int{10, 4}},
		{"INV-2", "Received", "2026-09-15 09:30:00+05:30", []int{7}},
		{"INV-3", "Ordered", nil, []int{3, 3}},
		{"INV-4", "Pending", nil, []int{1}},
		{"INV-5", "Cancelled", nil, []int{2}},
	}
	for i, p := range purchases {
		exec(`INSERT INTO purchases (reference, status, subtotal, total, due, currency, supplier_id, received_at, received_by, created_by)
			VALUES ($1, $2, 100, 100, 100, 'INR', 1, $3, CASE WHEN $3::timestamptz IS NOT NULL THEN 1 END, 1)`, p.ref, p.status, p.received)
		for j, q := range p.lines {
			exec(`INSERT INTO purchase_items (purchase_id, product_id, variant_id, store_id, product_name, sku, quantity, unit_price, subtotal)
				VALUES ($1, 1, $2, 1, 'Tee', 'T', $3, 10, $3 * 10)`, i+1, j+1, q)
		}
	}
	require.NoError(t, tx.Commit(ctx))

	_, err = migrate.UpScratch(ctx, tdb.MigratorDSN, s, 22)
	require.NoError(t, err)

	sc := s.String()
	var nPurchases, received, receipts, lines, mismatches int
	_, err = admin.Exec(ctx, `SET search_path TO `+sc+`, extensions`)
	require.NoError(t, err)
	require.NoError(t, admin.QueryRow(ctx, reportSQL).Scan(new(string), &nPurchases, &received, &receipts, &lines, &mismatches))
	_, _ = admin.Exec(ctx, `RESET search_path`)
	t.Logf("backfill report: purchases=%d received=%d receipts=%d receipt_lines=%d mismatches=%d", nPurchases, received, receipts, lines, mismatches)
	require.Equal(t, 5, nPurchases)
	require.Equal(t, 2, received)
	require.Equal(t, 2, receipts, "one receipt per received purchase")
	require.Equal(t, 3, lines)
	require.Zero(t, mismatches)

	var refs []string
	rows, err := admin.Query(ctx, `SELECT r.reference || ' ' || p.reference FROM `+sc+`.purchase_receipts r JOIN `+sc+`.purchases p ON p.id = r.purchase_id ORDER BY r.reference`)
	require.NoError(t, err)
	for rows.Next() {
		var x string
		require.NoError(t, rows.Scan(&x))
		refs = append(refs, x)
	}
	require.Equal(t, []string{"GRN-000001 INV-2", "GRN-000002 INV-1"}, refs, "numbered oldest first")
	var next int
	require.NoError(t, admin.QueryRow(ctx, `SELECT next_value FROM `+sc+`.doc_counters WHERE name = 'goods_receipt'`).Scan(&next))
	require.Equal(t, 3, next)
}

// reportSQL is the per-shop backfill report (scratchpad item8/report.sql).
const reportSQL = `SELECT current_schema(),
  (SELECT count(*) FROM purchases),
  (SELECT count(*) FROM purchases WHERE status = 'Received'),
  (SELECT count(*) FROM purchase_receipts),
  (SELECT count(*) FROM purchase_receipt_items),
  (SELECT count(*) FROM purchases p WHERE p.status = 'Received' AND
     (SELECT count(*) FROM purchase_receipts r WHERE r.purchase_id = p.id) <> 1)
  + (SELECT count(*) FROM purchase_items i JOIN purchases p ON p.id = i.purchase_id
     WHERE (p.status = 'Received') <> (i.received_qty = i.quantity)
        OR i.received_qty <> coalesce((SELECT sum(ri.quantity) FROM purchase_receipt_items ri WHERE ri.purchase_item_id = i.id), 0))
  + (SELECT count(*) FROM purchase_receipts r JOIN purchases p ON p.id = r.purchase_id WHERE r.received_at IS DISTINCT FROM p.received_at)`
