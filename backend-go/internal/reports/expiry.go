package reports

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// The expiry report and dashboard figures (industry P3 item 7, A33,
// design §7). A batch's stock in a store falls in one section:
//   - recalled: a recalled batch, all of it (sellable and quarantined);
//   - expired: expiry before today (shop time zone), sellable stock;
//   - expiring: expiry from today to today + window days, sellable stock;
//   - quarantined: quarantined units of a batch that is not recalled.
// Values: cost (the batch's cost, else the variant's) and MRP (the batch's;
// none counts as 0).

// Sections, in report order.
var expirySections = []string{"recalled", "expired", "expiring", "quarantined"}

const maxExpiryWindow = 730

// ExpiryRow is one batch in one store and section.
type ExpiryRow struct {
	Section      string          `json:"section"`
	BatchID      int64           `json:"batchId"`
	BatchNo      string          `json:"batchNo"`
	Unknown      bool            `json:"unknown"`
	Expiry       *string         `json:"expiry"`
	DaysToExpiry *int            `json:"daysToExpiry"`
	VariantID    int64           `json:"variantId"`
	ProductID    int64           `json:"productId"`
	ProductName  string          `json:"productName"`
	SKU          string          `json:"sku"`
	StoreID      int64           `json:"storeId"`
	StoreName    string          `json:"storeName"`
	Quantity     int             `json:"quantity"`
	CostValue    decimal.Decimal `json:"costValue"`
	MRPValue     decimal.Decimal `json:"mrpValue"`
}

// ExpirySummary totals one section.
type ExpirySummary struct {
	Batches   int             `json:"batches"`
	Units     int             `json:"units"`
	CostValue decimal.Decimal `json:"costValue"`
	MRPValue  decimal.Decimal `json:"mrpValue"`
}

// ExpiryReport is GET /api/reports/expiry.
type ExpiryReport struct {
	Today      string                   `json:"today"`
	WindowDays int                      `json:"windowDays"`
	StoreID    int64                    `json:"storeId"`
	Summary    map[string]ExpirySummary `json:"summary"`
	Items      []ExpiryRow              `json:"items"`
}

const qExpiryRows = `WITH t AS (SELECT (now() AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date AS today),
	base AS (
		SELECT s.store_id, st.name AS store_name, b.id AS batch_id, b.batch_no::text AS batch_no, b.is_unknown, b.expiry_date, b.status,
			v.id AS variant_id, p.id AS product_id, p.name AS product_name, coalesce(v.sku::text, '') AS sku,
			s.qty, s.quarantined_qty, coalesce(b.cost, v.cost) AS unit_cost, coalesce(b.mrp, 0) AS unit_mrp
		FROM batch_stock s JOIN batches b ON b.id = s.batch_id
		JOIN product_variants v ON v.id = b.variant_id JOIN products p ON p.id = v.product_id
		JOIN stores st ON st.id = s.store_id
		WHERE p.tracks_batches AND EXISTS (SELECT 1 FROM settings WHERE id = 1 AND 'batch_expiry' = ANY(modules))
		  AND ($1::bigint = 0 OR s.store_id = $1) AND (s.qty > 0 OR s.quarantined_qty > 0)),
	rows AS (
		SELECT 'recalled' AS section, 1 AS ord, base.*, qty + quarantined_qty AS n FROM base WHERE status = 'recalled'
		UNION ALL
		SELECT 'expired', 2, base.*, qty FROM base, t WHERE status = 'available' AND expiry_date < t.today AND qty > 0
		UNION ALL
		SELECT 'expiring', 3, base.*, qty FROM base, t WHERE status = 'available' AND expiry_date >= t.today
			AND expiry_date <= t.today + $2::int AND qty > 0
		UNION ALL
		SELECT 'quarantined', 4, base.*, quarantined_qty FROM base WHERE status = 'available' AND quarantined_qty > 0)
	SELECT r.section, r.batch_id, r.batch_no, r.is_unknown, to_char(r.expiry_date, 'YYYY-MM-DD'), (r.expiry_date - t.today)::int,
		r.variant_id, r.product_id, r.product_name, r.sku, r.store_id, r.store_name, r.n,
		round(r.n * r.unit_cost, 2), round(r.n * r.unit_mrp, 2), t.today::text
	FROM rows r, t
	WHERE ($3 = '' OR r.section = $3)
	ORDER BY r.ord, r.expiry_date NULLS LAST, r.product_name, r.sku, r.store_name, r.batch_id
	LIMIT $4`

type expiryFilter struct {
	store   int64
	window  int
	section string
}

func (a *API) expiryFilter(ctx context.Context, tx tenancy.TenantTx, r *http.Request) (expiryFilter, error) {
	f := expiryFilter{store: queryID(r, "storeId")}
	q := r.URL.Query()
	if s := strings.TrimSpace(q.Get("section")); s != "" {
		if !slices.Contains(expirySections, s) {
			return f, apperr.Invalid("section", "section must be recalled, expired, expiring or quarantined.")
		}
		f.section = s
	}
	if w := strings.TrimSpace(q.Get("windowDays")); w != "" {
		var n int
		if _, err := fmt.Sscan(w, &n); err != nil || n < 1 || n > maxExpiryWindow {
			return f, apperr.Invalid("windowDays", fmt.Sprintf("windowDays must be 1 to %d.", maxExpiryWindow))
		}
		f.window = n
		return f, nil
	}
	err := tx.QueryRow(ctx, `SELECT near_expiry_days FROM settings WHERE id = 1`).Scan(&f.window)
	return f, err
}

// ExpiryRows runs the report (at most limit rows; the summary covers them).
func ExpiryRows(ctx context.Context, tx tenancy.TenantTx, store int64, window int, section string, limit int) (ExpiryReport, error) {
	out := ExpiryReport{WindowDays: window, StoreID: store, Summary: map[string]ExpirySummary{}, Items: []ExpiryRow{}}
	for _, s := range expirySections {
		out.Summary[s] = ExpirySummary{CostValue: decimal.Zero, MRPValue: decimal.Zero}
	}
	if err := tx.QueryRow(ctx, `SELECT ((now() AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date)::text`).Scan(&out.Today); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, qExpiryRows, store, window, section, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var x ExpiryRow
		var today string
		if err := rows.Scan(&x.Section, &x.BatchID, &x.BatchNo, &x.Unknown, &x.Expiry, &x.DaysToExpiry, &x.VariantID, &x.ProductID,
			&x.ProductName, &x.SKU, &x.StoreID, &x.StoreName, &x.Quantity, &x.CostValue, &x.MRPValue, &today); err != nil {
			return out, err
		}
		s := out.Summary[x.Section]
		s.Batches++
		s.Units += x.Quantity
		s.CostValue = s.CostValue.Add(x.CostValue)
		s.MRPValue = s.MRPValue.Add(x.MRPValue)
		out.Summary[x.Section] = s
		out.Items = append(out.Items, x)
	}
	return out, rows.Err()
}

// expiryReport is GET /api/reports/expiry?storeId&windowDays&section.
func (a *API) expiryReport(w http.ResponseWriter, r *http.Request) {
	var out ExpiryReport
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		f, err := a.expiryFilter(r.Context(), tx, r)
		if err != nil {
			return err
		}
		out, err = ExpiryRows(r.Context(), tx, f.store, f.window, f.section, MaxXLSXRows)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

var expiryColumns = []column{
	{header: "Section", kind: kindText, width: 2, xlsx: 12},
	{header: "Product", kind: kindText, width: 4, xlsx: 32},
	{header: "SKU", kind: kindText, width: 2, xlsx: 16},
	{header: "Batch", kind: kindText, width: 2, xlsx: 16},
	{header: "Expiry", kind: kindText, width: 2, xlsx: 12},
	{header: "Store", kind: kindText, width: 2, xlsx: 18},
	{header: "Qty", kind: kindInt, width: 1, xlsx: 7, total: true},
	{header: "Cost value", kind: kindMoney, width: 2, xlsx: 14, total: true},
	{header: "MRP value", kind: kindMoney, width: 2, xlsx: 14, total: true},
}

// expiryExport is GET /api/reports/expiry/export?format=xlsx|pdf.
func (a *API) expiryExport(w http.ResponseWriter, r *http.Request) {
	a.export(w, r, "expiry-report", func(ctx context.Context, tx tenancy.TenantTx, b settings.Business, max int) (table, map[string]any, error) {
		f, err := a.expiryFilter(ctx, tx, r)
		if err != nil {
			return table{}, nil, err
		}
		rep, err := ExpiryRows(ctx, tx, f.store, f.window, f.section, max+1)
		if err != nil {
			return table{}, nil, err
		}
		if len(rep.Items) > max {
			return table{}, nil, apperr.Invalid("section", fmt.Sprintf("The report has more than %d rows; choose a store or a section.", max))
		}
		fm := receipts.NewFormatter(b.Locale, b.Currency, location(b))
		t := table{title: "Expiry report", business: b.Name, columns: expiryColumns,
			subtitle: fmt.Sprintf("Expiring within %d days · today %s · generated %s", f.window, rep.Today, fm.DateTime(a.d.Now()))}
		for _, x := range rep.Items {
			batch := x.BatchNo
			if x.Unknown {
				batch = "No batch"
			}
			t.rows = append(t.rows, []any{x.Section, x.ProductName, x.SKU, batch, str(x.Expiry), x.StoreName, x.Quantity, x.CostValue, x.MRPValue})
		}
		return t, map[string]any{"store_id": f.store, "window_days": f.window, "section": f.section}, nil
	})
}

// DashboardBatches are the pharmacy tile's figures (units), shown with the
// module on and batches:view.
type DashboardBatches struct {
	WindowDays  int `json:"windowDays"`
	Expired     int `json:"expired"`
	Expiring    int `json:"expiring"`
	Quarantined int `json:"quarantined"`
	Recalled    int `json:"recalled"`
	Unbatched   int `json:"unbatched"`
}

const qDashBatches = `WITH t AS (SELECT (now() AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date AS today,
		(SELECT near_expiry_days FROM settings WHERE id = 1) AS win)
	SELECT t.win,
		coalesce(sum(s.qty) FILTER (WHERE b.status = 'available' AND b.expiry_date < t.today), 0)::int,
		coalesce(sum(s.qty) FILTER (WHERE b.status = 'available' AND b.expiry_date >= t.today AND b.expiry_date <= t.today + t.win), 0)::int,
		coalesce(sum(s.quarantined_qty) FILTER (WHERE b.status = 'available'), 0)::int,
		coalesce(sum(s.qty + s.quarantined_qty) FILTER (WHERE b.status = 'recalled'), 0)::int,
		coalesce(sum(s.qty) FILTER (WHERE b.is_unknown), 0)::int
	FROM t
	LEFT JOIN batch_stock s ON ($1::bigint = 0 OR s.store_id = $1)
		AND EXISTS (SELECT 1 FROM batches bb JOIN product_variants v ON v.id = bb.variant_id JOIN products p ON p.id = v.product_id
			WHERE bb.id = s.batch_id AND p.tracks_batches)
	LEFT JOIN batches b ON b.id = s.batch_id
	GROUP BY t.win`

func dashBatches(ctx context.Context, tx tenancy.TenantTx, store int64) (*DashboardBatches, error) {
	var on bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM settings WHERE id = 1 AND 'batch_expiry' = ANY(modules))`).Scan(&on); err != nil || !on {
		return nil, err
	}
	var d DashboardBatches
	err := tx.QueryRow(ctx, qDashBatches, store).Scan(&d.WindowDays, &d.Expired, &d.Expiring, &d.Quarantined, &d.Recalled, &d.Unbatched)
	return &d, err
}
