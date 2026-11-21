package sales

import (
	"context"
	"fmt"
	"time"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Invoice and credit note numbers (M3 step 4, docs/M3_GST_DESIGN.md §6).
// settings.invoice_numbering:
//   - shop (the default): the shop-wide counters, INV-000042 and RET-000010.
//   - store_year: per store and financial year (1 April to 31 March),
//     MUM/2627/00042 and MUM/2627/C0003; at most 16 characters with a
//     store code of up to 5 (owner's choice, 2026-11-21).
const (
	NumberShop      = "shop"
	NumberStoreYear = "store_year"

	seriesSale       = "sale"
	seriesCreditNote = "credit_note"
)

// fiscalYear is the Indian financial year of t in loc: "2627" from
// 1 April 2026 to 31 March 2027.
func fiscalYear(t time.Time, loc *time.Location) string {
	d := t.In(loc)
	y := d.Year()
	if d.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%02d%02d", y%100, (y+1)%100)
}

// The next number of a store's series. The row stays locked until commit
// (gapless, as doc_counters); a new year starts at 1.
const qNextSeries = `INSERT INTO invoice_series (store_id, kind, fy, next_value) VALUES ($1, $2, $3, 2)
	ON CONFLICT (store_id, kind, fy) DO UPDATE SET next_value = invoice_series.next_value + 1
	RETURNING next_value - 1`

// storeYearNumber is the next per-store, per-year number of kind.
func storeYearNumber(ctx context.Context, tx tenancy.TenantTx, storeID int64, kind string, date time.Time, loc *time.Location) (string, error) {
	var code *string
	if err := tx.QueryRow(ctx, `SELECT invoice_code FROM stores WHERE id = $1`, storeID).Scan(&code); err != nil {
		return "", err
	}
	if code == nil || *code == "" {
		return "", apperr.Invalid("storeId", "This store has no invoice code. Give it one (Stores) to number its invoices per store and year.")
	}
	fy := fiscalYear(date, loc)
	var n int64
	if err := tx.QueryRow(ctx, qNextSeries, storeID, kind, fy).Scan(&n); err != nil {
		return "", err
	}
	if kind == seriesCreditNote {
		return fmt.Sprintf("%s/%s/C%04d", *code, fy, n), nil
	}
	return fmt.Sprintf("%s/%s/%05d", *code, fy, n), nil
}

// saleReference numbers a new sale. Call it after the inventory locks.
func saleReference(ctx context.Context, tx tenancy.TenantTx, s settings, storeID int64, date time.Time) (string, error) {
	if s.numbering == NumberStoreYear {
		return storeYearNumber(ctx, tx, storeID, seriesSale, date, s.loc)
	}
	return nextReference(ctx, tx, s.prefix)
}

// returnReference numbers a new credit note (return). Call it after the
// inventory locks.
func returnReference(ctx context.Context, tx tenancy.TenantTx, storeID int64, date time.Time) (string, error) {
	var numbering, tz string
	if err := tx.QueryRow(ctx, `SELECT invoice_numbering, timezone FROM settings WHERE id = 1`).Scan(&numbering, &tz); err != nil {
		return "", err
	}
	if numbering != NumberStoreYear {
		return nextReturnReference(ctx, tx)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return storeYearNumber(ctx, tx, storeID, seriesCreditNote, date, loc)
}
