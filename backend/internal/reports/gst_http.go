package reports

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/gst"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend/internal/xlsx"
)

// GSTINChoice is a GSTIN the shop files for, and who uses it.
type GSTINChoice struct {
	GSTIN string `json:"gstin"`
	State string `json:"state"`
	Label string `json:"label"` // "Shop" or the stores' names
}

// The shop's GSTIN (when its tax is GST), the stores' own, and any seen on
// sales (a store may have changed its GSTIN since).
const qGSTINs = `SELECT g, string_agg(DISTINCT who, ', ') FROM (
		SELECT tax_registration_no AS g, 'Shop' AS who FROM settings
			WHERE id = 1 AND upper(tax_label) = 'GST' AND length(tax_registration_no) = 15
		UNION ALL SELECT gstin, name FROM stores WHERE gstin IS NOT NULL AND deleted_at IS NULL
		UNION ALL SELECT DISTINCT seller_gstin, 'Earlier sales' FROM sales WHERE seller_gstin IS NOT NULL
	) x GROUP BY g ORDER BY g`

func gstins(ctx context.Context, tx tenancy.TenantTx) ([]GSTINChoice, error) {
	rows, err := tx.Query(ctx, qGSTINs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GSTINChoice{}
	for rows.Next() {
		var c GSTINChoice
		if err := rows.Scan(&c.GSTIN, &c.Label); err != nil {
			return nil, err
		}
		c.State = placeLabel(c.GSTIN[:2])
		out = append(out, c)
	}
	return out, rows.Err()
}

// placeLabel is a place of supply as the GST tool writes it: "29-Karnataka".
func placeLabel(code string) string {
	if s, ok := gst.StateOf(code); ok {
		return code + "-" + s.Name
	}
	return code
}

var errGSTAll = apperr.Forbidden(apperr.CodeForbidden, "GST returns cover the whole shop: they need permission to see all sales.")

// gstInput reads ?gstin=&from=&to= and builds the report.
func (a *API) gstInput(ctx context.Context, tx tenancy.TenantTx, r *http.Request, b settings.Business) (GSTReport, error) {
	q := r.URL.Query()
	gstin := gst.NormGSTIN(q.Get("gstin"))
	p, err := ParseGSTPeriod(q.Get("from"), q.Get("to"), location(b))
	if err != nil {
		return GSTReport{}, err
	}
	choices, err := gstins(ctx, tx)
	if err != nil {
		return GSTReport{}, err
	}
	known := false
	for _, c := range choices {
		known = known || c.GSTIN == gstin
	}
	if !known {
		return GSTReport{}, apperr.Invalid("gstin", "Choose one of the shop's GSTINs.")
	}
	return BuildGSTReport(ctx, tx, gstin, p)
}

// gstGSTINs is GET /api/reports/gst/gstins: the GSTINs to report on.
func (a *API) gstGSTINs(w http.ResponseWriter, r *http.Request) {
	if ownerScope(principal(r)) != 0 {
		httpx.WriteError(w, r, errGSTAll)
		return
	}
	var out []GSTINChoice
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = gstins(r.Context(), tx)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// gstReport is GET /api/reports/gst?gstin=&from=YYYY-MM&to=YYYY-MM.
func (a *API) gstReport(w http.ResponseWriter, r *http.Request) {
	if ownerScope(principal(r)) != 0 {
		httpx.WriteError(w, r, errGSTAll)
		return
	}
	var out GSTReport
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		out, err = a.gstInput(r.Context(), tx, r, b)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// gstExport is GET /api/reports/gst/export?gstin=&from=&to=: the GSTR-1
// workbook (one sheet per section, a summary first). Audited.
func (a *API) gstExport(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if ownerScope(p) != 0 {
		httpx.WriteError(w, r, errGSTAll)
		return
	}
	t := tenant(r)
	if err := a.d.Plans.RequireFeatureFor(r.Context(), t, plans.FeatureReportsExport, "report exports"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var (
		file []byte
		rep  GSTReport
	)
	err := a.ro(r.Context(), t, func(tx tenancy.TenantTx) error {
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		if rep, err = a.gstInput(r.Context(), tx, r, b); err != nil {
			return err
		}
		file, err = writeGSTWorkbook(rep, b.Currency, location(b))
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	err = a.d.Sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: p.UserID, Action: "report.export", Entity: "report", EntityID: "gstr1",
			After: map[string]any{"format": "xlsx", "gstin": rep.GSTIN, "period": rep.Period}, RequestID: httpx.RequestID(r.Context()),
			UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	name := fmt.Sprintf("GSTR1-%s-%s.xlsx", rep.GSTIN, strings.ReplaceAll(strings.ReplaceAll(rep.Period, " to ", "_"), " ", ""))
	h := w.Header()
	h.Set("Content-Type", contentXLSX)
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	h.Set("Content-Length", strconv.Itoa(len(file)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file)
}

// writeGSTWorkbook lays the report out as the GST offline tool's sections.
func writeGSTWorkbook(rep GSTReport, currency string, loc *time.Location) ([]byte, error) {
	var buf bytes.Buffer
	w, err := xlsx.NewBook(&buf, xlsxMoneyFormat(currency))
	if err != nil {
		return nil, err
	}
	txt := func(s string) xlsx.Cell { return xlsx.Text(SafeText(s), xlsx.Plain) }
	hdr := func(s string) xlsx.Cell { return xlsx.Text(s, xlsx.Header) }
	money := func(d decimal.Decimal) xlsx.Cell { return xlsx.Num(d.InexactFloat64(), xlsx.Money) }
	num := func(d decimal.Decimal) xlsx.Cell { return xlsx.Num(d.InexactFloat64(), xlsx.Plain) }
	day := func(t time.Time) xlsx.Cell { return txt(t.In(loc).Format("02-Jan-2006")) }
	cols := func(widths ...float64) []xlsx.Col {
		out := make([]xlsx.Col, len(widths))
		for i, wd := range widths {
			out[i] = xlsx.Col{Width: wd}
		}
		return out
	}
	sheet := func(name string, widths []float64, headers ...string) error {
		if err := w.Sheet(name, cols(widths...)); err != nil {
			return err
		}
		cells := make([]xlsx.Cell, len(headers))
		for i, h := range headers {
			cells[i] = hdr(h)
		}
		return w.Row(cells...)
	}
	taxes := func(t GSTTax) []xlsx.Cell {
		return []xlsx.Cell{money(t.Taxable), money(t.IGST), money(t.CGST), money(t.SGST), money(decimal.Zero)}
	}
	row := func(cells ...[]xlsx.Cell) error {
		var all []xlsx.Cell
		for _, c := range cells {
			all = append(all, c...)
		}
		return w.Row(all...)
	}
	one := func(c ...xlsx.Cell) []xlsx.Cell { return c }

	// Summary: what this is, the warnings, GSTR-3B.
	if err := sheet("summary", []float64{44, 18, 16, 16, 16}, "GSTR-1 workbook", ""); err != nil {
		return nil, err
	}
	for _, r := range [][]string{{"GSTIN", rep.GSTIN}, {"Period", rep.Period}, {"Prepared figures; check them before filing.", ""}} {
		if err := w.Row(txt(r[0]), txt(r[1])); err != nil {
			return nil, err
		}
	}
	for _, m := range rep.Warnings {
		if err := w.Row(txt("Warning: " + m)); err != nil {
			return nil, err
		}
	}
	g := rep.GSTR3B
	for _, r := range [][]xlsx.Cell{
		{},
		{hdr("GSTR-3B"), hdr("Taxable value"), hdr("IGST"), hdr("CGST"), hdr("SGST/UTGST")},
		{txt("3.1(a) Outward taxable supplies (net of credit notes)"), money(g.Outward.Taxable), money(g.Outward.IGST), money(g.Outward.CGST), money(g.Outward.SGST)},
		{txt("3.1(c) Nil-rated supplies"), money(g.Nil)},
		{txt("Credit notes (already taken off 3.1(a))"), money(g.CreditNotes.Taxable), money(g.CreditNotes.IGST), money(g.CreditNotes.CGST), money(g.CreditNotes.SGST)},
		{txt("Invoices"), xlsx.Num(float64(g.InvoiceCount), xlsx.Plain)},
		{txt("Credit notes"), xlsx.Num(float64(g.CreditNoteCnt), xlsx.Plain)},
		{},
		{hdr("3.2 Inter-state supplies to unregistered persons"), hdr("Taxable value"), hdr("IGST")},
	} {
		if err := w.Row(r...); err != nil {
			return nil, err
		}
	}
	for _, u := range g.Unregistered {
		if err := w.Row(txt(placeLabel(u.PlaceOfSupply)), money(u.Taxable), money(u.IGST)); err != nil {
			return nil, err
		}
	}

	taxHeads := []string{"Taxable Value", "Integrated Tax Amount", "Central Tax Amount", "State/UT Tax Amount", "Cess Amount"}
	if err := sheet("b2b", []float64{18, 28, 22, 14, 14, 20, 10, 12, 8, 14, 14, 14, 14, 10},
		append([]string{"GSTIN/UIN of Recipient", "Receiver Name", "Invoice Number", "Invoice date", "Invoice Value", "Place Of Supply",
			"Reverse Charge", "Invoice Type", "Rate"}, taxHeads...)...); err != nil {
		return nil, err
	}
	for _, r := range rep.B2B {
		if err := row(one(txt(r.BuyerGSTIN), txt(r.BuyerName), txt(r.Invoice), day(r.Date), money(r.Value), txt(placeLabel(r.PlaceOfSupply)),
			txt("N"), txt("Regular B2B"), num(r.Rate)), taxes(r.GSTTax)); err != nil {
			return nil, err
		}
	}
	if err := sheet("b2cl", []float64{22, 14, 14, 20, 8, 14, 14, 10},
		"Invoice Number", "Invoice date", "Invoice Value", "Place Of Supply", "Rate", "Taxable Value", "Integrated Tax Amount", "Cess Amount"); err != nil {
		return nil, err
	}
	for _, r := range rep.B2CL {
		if err := w.Row(txt(r.Invoice), day(r.Date), money(r.Value), txt(placeLabel(r.PlaceOfSupply)), num(r.Rate), money(r.Taxable),
			money(r.IGST), money(decimal.Zero)); err != nil {
			return nil, err
		}
	}
	if err := sheet("b2cs", []float64{8, 22, 8, 14, 14, 14, 14, 10},
		append([]string{"Type", "Place Of Supply", "Rate"}, taxHeads...)...); err != nil {
		return nil, err
	}
	for _, r := range rep.B2CS {
		if err := row(one(txt("OE"), txt(placeLabel(r.PlaceOfSupply)), num(r.Rate)), taxes(r.GSTTax)); err != nil {
			return nil, err
		}
	}
	noteHeads := []string{"Note Number", "Note Date", "Original Invoice Number", "Original Invoice Date", "Note Value", "Place Of Supply", "Rate"}
	if err := sheet("cdnr", []float64{18, 28, 22, 14, 22, 14, 14, 20, 8, 14, 14, 14, 14, 10},
		append(append([]string{"GSTIN/UIN of Recipient", "Receiver Name"}, noteHeads...), taxHeads...)...); err != nil {
		return nil, err
	}
	noteRow := func(r GSTNoteRow, registered bool) error {
		inv, invDay := xlsx.Cell{}, xlsx.Cell{}
		if r.Invoice != "" {
			inv = txt(r.Invoice)
		}
		if r.InvoiceDate != nil {
			invDay = day(*r.InvoiceDate)
		}
		var head []xlsx.Cell
		if registered {
			head = one(txt(r.BuyerGSTIN), txt(r.BuyerName))
		} else {
			head = one(txt("B2CL"))
		}
		return row(head, one(txt(r.Note), day(r.Date), inv, invDay, money(r.Value), txt(placeLabel(r.PlaceOfSupply)), num(r.Rate)), taxes(r.GSTTax))
	}
	for _, r := range rep.CDNR {
		if err := noteRow(r, true); err != nil {
			return nil, err
		}
	}
	if err := sheet("cdnur", []float64{8, 22, 14, 22, 14, 14, 20, 8, 14, 14, 14, 14, 10},
		append(append([]string{"UR Type"}, noteHeads...), taxHeads...)...); err != nil {
		return nil, err
	}
	for _, r := range rep.CDNUR {
		if err := noteRow(r, false); err != nil {
			return nil, err
		}
	}
	if err := sheet("hsn", []float64{12, 32, 8, 14, 14, 14, 14, 14, 14, 10},
		append([]string{"HSN", "Description", "UQC", "Total Quantity", "Total Value"}, taxHeads...)...); err != nil {
		return nil, err
	}
	for _, r := range rep.HSN {
		h := r.HSN
		if h == "" {
			h = "(no HSN code)"
		}
		if err := row(one(txt(h), txt(r.Description), txt(r.UQC), xlsx.Num(r.Quantity.Float(), xlsx.Plain), money(r.Value)), taxes(r.GSTTax)); err != nil {
			return nil, err
		}
	}
	if err := sheet("nil", []float64{44, 18}, "Description", "Nil Rated Supplies"); err != nil {
		return nil, err
	}
	for _, r := range []struct {
		label string
		v     decimal.Decimal
	}{
		{"Inter-State supplies to registered persons", rep.Nil.InterB2B},
		{"Intra-State supplies to registered persons", rep.Nil.IntraB2B},
		{"Inter-State supplies to unregistered persons", rep.Nil.InterB2C},
		{"Intra-State supplies to unregistered persons", rep.Nil.IntraB2C},
	} {
		if err := w.Row(txt(r.label), money(r.v)); err != nil {
			return nil, err
		}
	}
	if err := sheet("docs", []float64{30, 22, 22, 12, 12}, "Nature of Document", "Sr. No. From", "Sr. No. To", "Total Number", "Cancelled"); err != nil {
		return nil, err
	}
	for _, d := range rep.Docs {
		nature := "Invoices for outward supply"
		if d.Kind == "credit_note" {
			nature = "Credit Note"
		}
		if err := w.Row(txt(nature), txt(d.From), txt(d.To), xlsx.Num(float64(d.Issued), xlsx.Plain), xlsx.Num(float64(d.Cancelled), xlsx.Plain)); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
