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

// TestPaymentsBackfill (S10 item 5, D5): migration 00012 gives every
// existing sale payment rows that sum exactly to its paid amount (method
// mapped, cash when none), and none when nothing was paid; returns keep
// their amounts under the new check.
func TestPaymentsBackfill(t *testing.T) {
	ctx := context.Background()
	s, err := tenancy.ScratchFor("bkfill0001", time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NoError(t, migrate.CreateScratch(ctx, tdb.MigratorDSN, s))
	t.Cleanup(func() { _ = migrate.DropScratch(context.Background(), tdb.MigratorDSN, s) })
	v, err := migrate.UpScratch(ctx, tdb.MigratorDSN, s, 11)
	require.NoError(t, err)
	require.EqualValues(t, 11, v)

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
	exec(`INSERT INTO users (name, email, role_id, status) VALUES ('Old Biller', 'old@shop.test', 1, 'Active')`)
	exec(`INSERT INTO customers (name) VALUES ('Old Customer')`)
	exec(`INSERT INTO stores (name, slug) VALUES ('Old Store', 'old')`)
	type old struct {
		method   any
		total    string
		paid     string
		tendered any
		want     string
	}
	cases := []old{
		{"Cash", "100", "100", "150", "cash"}, {"UPI", "200", "50", nil, "upi"}, {"Credit Card", "300", "300", nil, "card"},
		{nil, "400", "400", nil, "cash"}, {"Bank Transfer", "500", "500", nil, "deposit"}, {"Cash", "600", "0", nil, ""},
		{"Something odd", "700", "10.5", nil, "cash"},
	}
	for i, c := range cases {
		exec(`INSERT INTO sales (reference, status, payment_status, payment_method, subtotal, total, paid, due, tendered, change_due,
				currency, customer_id, store_id, user_id)
			VALUES ($1, 'Completed', 'Paid', $2, $3, $3, $4, $3::numeric - $4::numeric, $5, CASE WHEN $5::numeric IS NOT NULL THEN $5::numeric - $4::numeric END,
				'INR', 1, 1, 1)`, "OLD-"+string(rune('A'+i)), c.method, c.total, c.paid, c.tendered)
	}
	exec(`INSERT INTO sale_returns (reference, status, payment_status, total, paid, due, currency, customer_id, store_id, created_by)
		VALUES ('RET-OLD', 'Completed', 'Partial', 50, 20, 30, 'INR', 1, 1, 1)`)
	require.NoError(t, tx.Commit(ctx))

	_, err = migrate.UpScratch(ctx, tdb.MigratorDSN, s, 0)
	require.NoError(t, err)

	sc := s.String()
	var sales, rows, differ int
	require.NoError(t, admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM `+sc+`.sales), (SELECT count(*) FROM `+sc+`.sale_payments),
		(SELECT count(*) FROM `+sc+`.sales s WHERE s.paid <> coalesce((SELECT sum(amount) FROM `+sc+`.sale_payments p WHERE p.sale_id = s.id), 0))`).
		Scan(&sales, &rows, &differ))
	require.Equal(t, 7, sales)
	require.Equal(t, 6, rows, "one row per sale with something paid")
	require.Zero(t, differ, "every sale's payments sum to its paid amount")
	for i, c := range cases {
		var method string
		var tendered *string
		err := admin.QueryRow(ctx, `SELECT p.method, p.tendered::text FROM `+s.String()+`.sale_payments p JOIN `+s.String()+`.sales x ON x.id = p.sale_id
			WHERE x.reference = $1`, "OLD-"+string(rune('A'+i))).Scan(&method, &tendered)
		if c.want == "" {
			require.Error(t, err, "nothing paid: no row")
			continue
		}
		require.NoError(t, err)
		require.Equal(t, c.want, method, c.method)
		if c.tendered != nil {
			require.NotNil(t, tendered, "cash tendered kept")
		}
	}
	var applied string
	require.NoError(t, admin.QueryRow(ctx, `SELECT applied_to_due::text FROM `+s.String()+`.sale_returns WHERE reference = 'RET-OLD'`).Scan(&applied))
	require.Equal(t, "0.0000", applied)
}
