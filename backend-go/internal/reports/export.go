package reports

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Export limits: an export never silently drops rows; above the limit the
// request is refused and asks for a narrower filter.
const (
	MaxXLSXRows = 50_000
	MaxPDFRows  = 5_000
)

const (
	contentXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	contentPDF  = "application/pdf"
)

func exportFormat(r *http.Request) (string, int, error) {
	switch f := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))); f {
	case "xlsx", "excel":
		return "xlsx", MaxXLSXRows, nil
	case "pdf":
		return "pdf", MaxPDFRows, nil
	default:
		return "", 0, apperr.Invalid("format", "format must be xlsx or pdf.")
	}
}

func tooMany(n, max int, format string) error {
	return apperr.Invalid("startDate", fmt.Sprintf("The report has %d rows; a %s export holds at most %d. Choose a shorter date range or more filters.",
		n, strings.ToUpper(format), max))
}

func period(f receipts.Formatter, from, to *time.Time) string {
	switch {
	case from != nil && to != nil:
		return f.Date(*from) + " to " + f.Date(to.Add(-time.Microsecond))
	case from != nil:
		return "From " + f.Date(*from)
	case to != nil:
		return "Up to " + f.Date(to.Add(-time.Microsecond))
	}
	return "All dates"
}

// export runs build in a read-only transaction, renders the table, writes
// an audit row (export requests are audited, PHASE1_DESIGN §8) and sends
// the file.
func (a *API) export(w http.ResponseWriter, r *http.Request, name string,
	build func(ctx context.Context, tx tenancy.TenantTx, b settings.Business, max int) (table, map[string]any, error)) {
	format, max, err := exportFormat(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t := tenant(r)
	if err := a.d.Plans.RequireFeature(r.Context(), t.PlanID, plans.FeatureReportsExport, "report exports"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var (
		file    []byte
		filters map[string]any
		rows    int
	)
	err = a.ro(r.Context(), t, func(tx tenancy.TenantTx) error {
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		tbl, fl, err := build(r.Context(), tx, b, max)
		if err != nil {
			return err
		}
		filters, rows = fl, len(tbl.rows)
		loc := location(b)
		if format == "xlsx" {
			file, err = writeXLSX(tbl, b.Currency, loc)
		} else {
			file, err = writePDF(tbl, receipts.NewFormatter(b.Locale, b.Currency, loc))
		}
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	err = a.d.Sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: p.UserID, Action: "report.export", Entity: "report", EntityID: name,
			After: map[string]any{"format": format, "rows": rows, "filters": filters}, RequestID: httpx.RequestID(r.Context()),
			UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ct := contentXLSX
	if format == "pdf" {
		ct = contentPDF
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.%s"`, name, a.d.Now().UTC().Format("2006-01-02"), format))
	h.Set("Content-Length", strconv.Itoa(len(file)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file)
}

var salesColumns = []column{
	{header: "Date", kind: kindDate, width: 3, xlsx: 17},
	{header: "Reference", kind: kindText, width: 3, xlsx: 18},
	{header: "Product", kind: kindText, width: 4, xlsx: 32},
	{header: "SKU", kind: kindText, width: 2, xlsx: 16},
	{header: "Customer", kind: kindText, width: 3, xlsx: 22},
	{header: "Store", kind: kindText, width: 2, xlsx: 18},
	{header: "Qty", kind: kindInt, width: 1, xlsx: 7, total: true},
	{header: "Amount", kind: kindMoney, width: 2, xlsx: 14, total: true},
	{header: "Returned", kind: kindMoney, width: 2, xlsx: 14, total: true},
	{header: "Net", kind: kindMoney, width: 2, xlsx: 14, total: true},
}

// salesExport is GET /api/sales/report/export?format=xlsx|pdf with the
// sales report's filters and scope.
func (a *API) salesExport(w http.ResponseWriter, r *http.Request) {
	a.export(w, r, "sales-report", func(ctx context.Context, tx tenancy.TenantTx, b settings.Business, max int) (table, map[string]any, error) {
		f, err := a.salesFilter(r, b)
		if err != nil {
			return table{}, nil, err
		}
		rows, total, err := SalesRows(ctx, tx, f, max, 0)
		if err != nil {
			return table{}, nil, err
		}
		if total > max {
			return table{}, nil, tooMany(total, max, r.URL.Query().Get("format"))
		}
		fm := receipts.NewFormatter(b.Locale, b.Currency, location(b))
		t := table{title: "Sales report", business: b.Name, columns: salesColumns,
			subtitle: period(fm, f.From, f.To) + " · generated " + fm.DateTime(a.d.Now())}
		for _, x := range rows {
			t.rows = append(t.rows, []any{x.Date, x.Reference, x.Product.Name, str(x.ItemSKU), x.CustomerName, str(&x.Store.Name),
				x.SoldQty, x.SoldAmount, x.ReturnedAmount, x.NetAmount})
		}
		return t, map[string]any{"startDate": r.URL.Query().Get("startDate"), "endDate": r.URL.Query().Get("endDate"),
			"own_only": f.Scope != 0, "store_id": f.StoreID, "product_id": f.ProductID, "customer_id": f.CustomerID}, nil
	})
}

var purchaseColumns = []column{
	{header: "Date", kind: kindDate, width: 3, xlsx: 17},
	{header: "Reference", kind: kindText, width: 3, xlsx: 18},
	{header: "Supplier", kind: kindText, width: 3, xlsx: 24},
	{header: "Product", kind: kindText, width: 4, xlsx: 32},
	{header: "SKU", kind: kindText, width: 2, xlsx: 16},
	{header: "Store", kind: kindText, width: 2, xlsx: 18},
	{header: "Status", kind: kindText, width: 2, xlsx: 12},
	{header: "Qty", kind: kindInt, width: 1, xlsx: 7, total: true},
	{header: "Unit price", kind: kindMoney, width: 2, xlsx: 14},
	{header: "Amount", kind: kindMoney, width: 2, xlsx: 14, total: true},
}

// purchaseExport is GET /api/purchase/report/export?format=xlsx|pdf with
// the purchase report's filters (needs purchases:view too).
func (a *API) purchaseExport(w http.ResponseWriter, r *http.Request) {
	if !principal(r).Has("purchases:view") {
		httpx.WriteError(w, r, errNoPurchases)
		return
	}
	a.export(w, r, "purchase-report", func(ctx context.Context, tx tenancy.TenantTx, b settings.Business, max int) (table, map[string]any, error) {
		f, err := a.purchaseFilter(r, b)
		if err != nil {
			return table{}, nil, err
		}
		rows, total, err := PurchaseRows(ctx, tx, f, max, 0)
		if err != nil {
			return table{}, nil, err
		}
		if total > max {
			return table{}, nil, tooMany(total, max, r.URL.Query().Get("format"))
		}
		fm := receipts.NewFormatter(b.Locale, b.Currency, location(b))
		t := table{title: "Purchase report", business: b.Name, columns: purchaseColumns,
			subtitle: period(fm, f.From, f.To) + " · generated " + fm.DateTime(a.d.Now())}
		for _, x := range rows {
			t.rows = append(t.rows, []any{x.Date, x.Reference, x.SupplierName, x.Product.Name, str(x.ItemSKU), str(&x.Store.Name),
				x.Status, x.PurchaseQty, x.UnitPrice, x.PurchaseAmount})
		}
		return t, map[string]any{"startDate": r.URL.Query().Get("startDate"), "endDate": r.URL.Query().Get("endDate"),
			"supplier_id": f.SupplierID, "store_id": f.StoreID, "product_id": f.ProductID}, nil
	})
}
