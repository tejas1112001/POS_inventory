package reports

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// GST returns (docs/GST_REPORTS_DESIGN.md): the figures of GSTR-1 for one
// seller GSTIN and a period of whole months, from the sales and credit
// notes made under a GST state (their M3 copies), plus a GSTR-3B summary.
// The app prepares the figures; it files nothing.

// B2CLLimit is the value above which a B2C invoice to another state is
// reported invoice by invoice (b2cl; design Q1, ₹1,00,000 since August 2024).
var B2CLLimit = decimal.NewFromInt(100_000)

// HSNMinDigits is the HSN code length below which a code is warned about
// (design Q4).
const HSNMinDigits = 4

// GSTPeriod is whole months [From, To) in the shop's time zone.
type GSTPeriod struct {
	From, To time.Time
	Label    string // "2026-10 to 2026-12" or "2026-11"
}

// ParseGSTPeriod reads from=YYYY-MM and to=YYYY-MM (to optional: one
// month), at most 12 months.
func ParseGSTPeriod(from, to string, loc *time.Location) (GSTPeriod, error) {
	f, err := time.ParseInLocation("2006-01", strings.TrimSpace(from), loc)
	if err != nil {
		return GSTPeriod{}, apperr.Invalid("from", "from is a month: YYYY-MM.")
	}
	t := f
	if strings.TrimSpace(to) != "" {
		if t, err = time.ParseInLocation("2006-01", strings.TrimSpace(to), loc); err != nil {
			return GSTPeriod{}, apperr.Invalid("to", "to is a month: YYYY-MM.")
		}
	}
	if t.Before(f) {
		return GSTPeriod{}, apperr.Invalid("to", "to is not before from.")
	}
	end := t.AddDate(0, 1, 0)
	if end.After(f.AddDate(1, 0, 0)) {
		return GSTPeriod{}, apperr.Invalid("to", "A GST report covers at most 12 months.")
	}
	label := f.Format("2006-01")
	if !t.Equal(f) {
		label += " to " + t.Format("2006-01")
	}
	return GSTPeriod{From: f, To: end, Label: label}, nil
}

// GSTTax is a taxable value and its taxes. SGST holds UTGST too (a UT's
// sales say so in their split).
type GSTTax struct {
	Taxable decimal.Decimal `json:"taxable"`
	IGST    decimal.Decimal `json:"igst"`
	CGST    decimal.Decimal `json:"cgst"`
	SGST    decimal.Decimal `json:"sgst"`
}

func (x *GSTTax) add(o GSTTax, sign int64) {
	k := decimal.NewFromInt(sign)
	x.Taxable = x.Taxable.Add(o.Taxable.Mul(k))
	x.IGST = x.IGST.Add(o.IGST.Mul(k))
	x.CGST = x.CGST.Add(o.CGST.Mul(k))
	x.SGST = x.SGST.Add(o.SGST.Mul(k))
}

// Total is the taxes together.
func (x GSTTax) Total() decimal.Decimal { return x.IGST.Add(x.CGST).Add(x.SGST) }

// GSTInvoiceRow is one invoice and tax rate (b2b, b2cl).
type GSTInvoiceRow struct {
	BuyerGSTIN    string          `json:"buyerGstin,omitempty"`
	BuyerName     string          `json:"buyerName,omitempty"`
	Invoice       string          `json:"invoice"`
	Date          time.Time       `json:"date"`
	Value         decimal.Decimal `json:"value"` // the invoice total
	PlaceOfSupply string          `json:"placeOfSupply"`
	Rate          decimal.Decimal `json:"rate"`
	GSTTax
}

// GSTB2CSRow is the other B2C supplies of one place of supply and rate,
// net of their credit notes.
type GSTB2CSRow struct {
	PlaceOfSupply string          `json:"placeOfSupply"`
	Rate          decimal.Decimal `json:"rate"`
	GSTTax
}

// GSTNoteRow is one credit note and rate (cdnr, cdnur).
type GSTNoteRow struct {
	BuyerGSTIN    string          `json:"buyerGstin,omitempty"`
	BuyerName     string          `json:"buyerName,omitempty"`
	Note          string          `json:"note"`
	Date          time.Time       `json:"date"`
	Invoice       string          `json:"invoice,omitempty"` // the original invoice
	InvoiceDate   *time.Time      `json:"invoiceDate,omitempty"`
	Value         decimal.Decimal `json:"value"`
	PlaceOfSupply string          `json:"placeOfSupply"`
	Rate          decimal.Decimal `json:"rate"`
	GSTTax
}

// GSTHSNRow is the HSN summary of one code and unit, net of credit notes.
type GSTHSNRow struct {
	HSN         string          `json:"hsn"`
	Description string          `json:"description"`
	UQC         string          `json:"uqc"`
	Quantity    qty.Q           `json:"quantity"`
	Value       decimal.Decimal `json:"value"`
	GSTTax
}

// GSTNil is the nil-rated (0 %) supplies, net of credit notes.
type GSTNil struct {
	InterB2B decimal.Decimal `json:"interB2b"`
	IntraB2B decimal.Decimal `json:"intraB2b"`
	InterB2C decimal.Decimal `json:"interB2c"`
	IntraB2C decimal.Decimal `json:"intraB2c"`
}

// Total is all nil-rated supplies.
func (n GSTNil) Total() decimal.Decimal {
	return n.InterB2B.Add(n.IntraB2B).Add(n.InterB2C).Add(n.IntraB2C)
}

// GSTDocRow is one number series of the period's documents.
type GSTDocRow struct {
	Kind      string `json:"kind"` // invoice or credit_note
	Series    string `json:"series"`
	From      string `json:"from"`
	To        string `json:"to"`
	Issued    int    `json:"issued"`
	Cancelled int    `json:"cancelled"`
}

// GST3BPlace is GSTR-3B table 3.2: inter-state supplies to unregistered
// persons, by place of supply.
type GST3BPlace struct {
	PlaceOfSupply string          `json:"placeOfSupply"`
	Taxable       decimal.Decimal `json:"taxable"`
	IGST          decimal.Decimal `json:"igst"`
}

// GST3B is the GSTR-3B summary of the period.
type GST3B struct {
	Outward       GSTTax          `json:"outward"`      // 3.1(a), net of credit notes
	Nil           decimal.Decimal `json:"nil"`          // 3.1(c)
	Unregistered  []GST3BPlace    `json:"unregistered"` // 3.2
	CreditNotes   GSTTax          `json:"creditNotes"`  // already taken off Outward
	InvoiceCount  int             `json:"invoiceCount"` // non-void invoices
	CreditNoteCnt int             `json:"creditNoteCount"`
}

// GSTReport is GSTR-1 and the GSTR-3B summary of a GSTIN and period.
type GSTReport struct {
	GSTIN    string          `json:"gstin"`
	Period   string          `json:"period"`
	B2B      []GSTInvoiceRow `json:"b2b"`
	B2CL     []GSTInvoiceRow `json:"b2cl"`
	B2CS     []GSTB2CSRow    `json:"b2cs"`
	CDNR     []GSTNoteRow    `json:"cdnr"`
	CDNUR    []GSTNoteRow    `json:"cdnur"`
	HSN      []GSTHSNRow     `json:"hsn"`
	Nil      GSTNil          `json:"nil"`
	Docs     []GSTDocRow     `json:"docs"`
	GSTR3B   GST3B           `json:"gstr3b"`
	Warnings []string        `json:"warnings"`
}

// The invoice lines of the GSTIN's sales in the period, by invoice and rate.
const qGSTInvoices = `SELECT s.id, s.reference, s.date, s.total, coalesce(s.place_of_supply, ''), coalesce(s.seller_state, ''),
		coalesce(s.invoice_kind, 'b2c'), coalesce(s.buyer_gstin, ''), coalesce(s.buyer_name, ''), coalesce(i.tax_rate, 0),
		sum(i.subtotal - i.tax_amount), coalesce(sum(i.igst), 0), coalesce(sum(i.cgst), 0), coalesce(sum(i.sgst), 0)
	FROM sales s JOIN order_items i ON i.sale_id = s.id
	WHERE s.seller_gstin = $1 AND s.tax_split IS NOT NULL AND s.status <> 'Void' AND s.date >= $2 AND s.date < $3
	GROUP BY s.id, coalesce(i.tax_rate, 0) ORDER BY s.date, s.id, 10`

// The credit note lines, by note and rate, with the original invoice.
const qGSTNotes = `SELECT r.id, r.reference, r.date, r.total, coalesce(r.place_of_supply, ''), coalesce(r.seller_state, ''),
		coalesce(r.invoice_kind, 'b2c'), coalesce(r.buyer_gstin, ''), coalesce(r.buyer_name, ''), coalesce(oi.tax_rate, 0),
		sum(ri.subtotal - ri.tax_amount), coalesce(sum(ri.igst), 0), coalesce(sum(ri.cgst), 0), coalesce(sum(ri.sgst), 0),
		s.reference, s.date, s.total, s.place_of_supply
	FROM sale_returns r JOIN return_items ri ON ri.sale_return_id = r.id
	LEFT JOIN order_items oi ON oi.id = ri.order_item_id
	LEFT JOIN sales s ON s.id = r.sale_id
	WHERE r.seller_gstin = $1 AND r.tax_split IS NOT NULL AND r.date >= $2 AND r.date < $3
	GROUP BY r.id, coalesce(oi.tax_rate, 0), s.id ORDER BY r.date, r.id, 10`

// The HSN summary of sales (sign 1) and credit notes (sign -1).
const qGSTHSN = `SELECT coalesce(i.hsn_code, ''), coalesce(u.uqc, 'OTH'), min(i.product_name), sum(i.quantity), sum(i.subtotal),
		sum(i.subtotal - i.tax_amount), coalesce(sum(i.igst), 0), coalesce(sum(i.cgst), 0), coalesce(sum(i.sgst), 0),
		count(*) FILTER (WHERE u.uqc IS NULL), bool_or(s.invoice_kind = 'b2b')
	FROM sales s JOIN order_items i ON i.sale_id = s.id
	LEFT JOIN products p ON p.id = i.product_id LEFT JOIN units u ON u.id = p.unit_id
	WHERE s.seller_gstin = $1 AND s.tax_split IS NOT NULL AND s.status <> 'Void' AND s.date >= $2 AND s.date < $3
	GROUP BY 1, 2`
const qGSTHSNNotes = `SELECT coalesce(oi.hsn_code, ''), coalesce(u.uqc, 'OTH'), min(ri.product_name), sum(ri.quantity), sum(ri.subtotal),
		sum(ri.subtotal - ri.tax_amount), coalesce(sum(ri.igst), 0), coalesce(sum(ri.cgst), 0), coalesce(sum(ri.sgst), 0),
		count(*) FILTER (WHERE u.uqc IS NULL), false
	FROM sale_returns r JOIN return_items ri ON ri.sale_return_id = r.id
	LEFT JOIN order_items oi ON oi.id = ri.order_item_id
	LEFT JOIN products p ON p.id = ri.product_id LEFT JOIN units u ON u.id = p.unit_id
	WHERE r.seller_gstin = $1 AND r.tax_split IS NOT NULL AND r.date >= $2 AND r.date < $3
	GROUP BY 1, 2`

// The documents of the period by number series (the reference without its
// last digits), void sales counted as cancelled.
const qGSTDocs = `SELECT 'invoice', regexp_replace(reference, '[0-9]+$', ''), min(reference), max(reference), count(*),
		count(*) FILTER (WHERE status = 'Void')
	FROM sales WHERE seller_gstin = $1 AND tax_split IS NOT NULL AND date >= $2 AND date < $3 GROUP BY 2
	UNION ALL
	SELECT 'credit_note', regexp_replace(reference, '[0-9]+$', ''), min(reference), max(reference), count(*), 0
	FROM sale_returns WHERE seller_gstin = $1 AND tax_split IS NOT NULL AND date >= $2 AND date < $3 GROUP BY 2
	ORDER BY 1 DESC, 2`

const qGSTOldSales = `SELECT count(*) FROM sales WHERE tax_split IS NULL AND status <> 'Void' AND date >= $1 AND date < $2`

type b2csKey struct {
	place string
	rate  string
}

// BuildGSTReport works out the report. The caller checks the GSTIN is one
// of the shop's.
func BuildGSTReport(ctx context.Context, tx tenancy.TenantTx, gstin string, p GSTPeriod) (GSTReport, error) {
	rep := GSTReport{GSTIN: gstin, Period: p.Label, B2B: []GSTInvoiceRow{}, B2CL: []GSTInvoiceRow{}, B2CS: []GSTB2CSRow{},
		CDNR: []GSTNoteRow{}, CDNUR: []GSTNoteRow{}, HSN: []GSTHSNRow{}, Docs: []GSTDocRow{}, Warnings: []string{}}
	b2cs := map[b2csKey]*GSTB2CSRow{}
	var b2csOrder []b2csKey
	b2csAdd := func(place string, rate decimal.Decimal, t GSTTax, sign int64) {
		k := b2csKey{place, rate.String()}
		row := b2cs[k]
		if row == nil {
			row = &GSTB2CSRow{PlaceOfSupply: place, Rate: rate}
			b2cs[k] = row
			b2csOrder = append(b2csOrder, k)
		}
		row.add(t, sign)
	}
	nilAdd := func(inter, b2b bool, v decimal.Decimal) {
		switch {
		case inter && b2b:
			rep.Nil.InterB2B = rep.Nil.InterB2B.Add(v)
		case b2b:
			rep.Nil.IntraB2B = rep.Nil.IntraB2B.Add(v)
		case inter:
			rep.Nil.InterB2C = rep.Nil.InterB2C.Add(v)
		default:
			rep.Nil.IntraB2C = rep.Nil.IntraB2C.Add(v)
		}
	}
	unreg := map[string]*GST3BPlace{}
	unregAdd := func(place string, t GSTTax, sign int64) {
		u := unreg[place]
		if u == nil {
			u = &GST3BPlace{PlaceOfSupply: place}
			unreg[place] = u
		}
		k := decimal.NewFromInt(sign)
		u.Taxable, u.IGST = u.Taxable.Add(t.Taxable.Mul(k)), u.IGST.Add(t.IGST.Mul(k))
	}

	// Invoices.
	rows, err := tx.Query(ctx, qGSTInvoices, gstin, p.From, p.To)
	if err != nil {
		return rep, err
	}
	invoices := map[int64]bool{}
	for rows.Next() {
		var (
			id                int64
			r                 GSTInvoiceRow
			sellerState, kind string
		)
		if err := rows.Scan(&id, &r.Invoice, &r.Date, &r.Value, &r.PlaceOfSupply, &sellerState, &kind, &r.BuyerGSTIN, &r.BuyerName,
			&r.Rate, &r.Taxable, &r.IGST, &r.CGST, &r.SGST); err != nil {
			rows.Close()
			return rep, err
		}
		invoices[id] = true
		inter := r.PlaceOfSupply != sellerState
		switch {
		case !r.Rate.IsPositive():
			nilAdd(inter, kind == "b2b", r.Taxable)
		case kind == "b2b":
			rep.B2B = append(rep.B2B, r)
			rep.GSTR3B.Outward.add(r.GSTTax, 1)
		case inter && r.Value.GreaterThan(B2CLLimit):
			r.BuyerGSTIN, r.BuyerName = "", ""
			rep.B2CL = append(rep.B2CL, r)
			rep.GSTR3B.Outward.add(r.GSTTax, 1)
			unregAdd(r.PlaceOfSupply, r.GSTTax, 1)
		default:
			b2csAdd(r.PlaceOfSupply, r.Rate, r.GSTTax, 1)
			rep.GSTR3B.Outward.add(r.GSTTax, 1)
			if inter {
				unregAdd(r.PlaceOfSupply, r.GSTTax, 1)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}
	rep.GSTR3B.InvoiceCount = len(invoices)

	// Credit notes.
	rows, err = tx.Query(ctx, qGSTNotes, gstin, p.From, p.To)
	if err != nil {
		return rep, err
	}
	notes := map[int64]bool{}
	for rows.Next() {
		var (
			id                int64
			r                 GSTNoteRow
			sellerState, kind string
			invRef, invPlace  *string
			invDate           *time.Time
			invTotal          *decimal.Decimal
		)
		if err := rows.Scan(&id, &r.Note, &r.Date, &r.Value, &r.PlaceOfSupply, &sellerState, &kind, &r.BuyerGSTIN, &r.BuyerName,
			&r.Rate, &r.Taxable, &r.IGST, &r.CGST, &r.SGST, &invRef, &invDate, &invTotal, &invPlace); err != nil {
			rows.Close()
			return rep, err
		}
		notes[id] = true
		if invRef != nil {
			r.Invoice, r.InvoiceDate = *invRef, invDate
		}
		inter := r.PlaceOfSupply != sellerState
		rep.GSTR3B.CreditNotes.add(r.GSTTax, 1)
		switch {
		case !r.Rate.IsPositive():
			nilAdd(inter, kind == "b2b", r.Taxable.Neg())
		case kind == "b2b":
			rep.CDNR = append(rep.CDNR, r)
			rep.GSTR3B.Outward.add(r.GSTTax, -1)
		case inter && invTotal != nil && invTotal.GreaterThan(B2CLLimit):
			// A credit note of a b2cl invoice.
			r.BuyerGSTIN, r.BuyerName = "", ""
			rep.CDNUR = append(rep.CDNUR, r)
			rep.GSTR3B.Outward.add(r.GSTTax, -1)
			unregAdd(r.PlaceOfSupply, r.GSTTax, -1)
		default:
			// Other B2C credit notes reduce b2cs.
			b2csAdd(r.PlaceOfSupply, r.Rate, r.GSTTax, -1)
			rep.GSTR3B.Outward.add(r.GSTTax, -1)
			if inter {
				unregAdd(r.PlaceOfSupply, r.GSTTax, -1)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}
	rep.GSTR3B.CreditNoteCnt = len(notes)
	for _, k := range b2csOrder {
		rep.B2CS = append(rep.B2CS, *b2cs[k])
	}
	sort.SliceStable(rep.B2CS, func(i, j int) bool {
		if rep.B2CS[i].PlaceOfSupply != rep.B2CS[j].PlaceOfSupply {
			return rep.B2CS[i].PlaceOfSupply < rep.B2CS[j].PlaceOfSupply
		}
		return rep.B2CS[i].Rate.LessThan(rep.B2CS[j].Rate)
	})
	rep.GSTR3B.Nil = rep.Nil.Total()
	for _, u := range unreg {
		rep.GSTR3B.Unregistered = append(rep.GSTR3B.Unregistered, *u)
	}
	sort.Slice(rep.GSTR3B.Unregistered, func(i, j int) bool {
		return rep.GSTR3B.Unregistered[i].PlaceOfSupply < rep.GSTR3B.Unregistered[j].PlaceOfSupply
	})
	if rep.GSTR3B.Unregistered == nil {
		rep.GSTR3B.Unregistered = []GST3BPlace{}
	}

	// HSN summary, net of credit notes.
	hsn := map[string]*GSTHSNRow{}
	var noUQC, b2bNoHSN int
	for _, q := range []struct {
		sql  string
		sign int64
	}{{qGSTHSN, 1}, {qGSTHSNNotes, -1}} {
		rows, err := tx.Query(ctx, q.sql, gstin, p.From, p.To)
		if err != nil {
			return rep, err
		}
		for rows.Next() {
			var (
				r       GSTHSNRow
				missing int
				b2b     bool
			)
			if err := rows.Scan(&r.HSN, &r.UQC, &r.Description, &r.Quantity, &r.Value, &r.Taxable, &r.IGST, &r.CGST, &r.SGST,
				&missing, &b2b); err != nil {
				rows.Close()
				return rep, err
			}
			if q.sign > 0 {
				noUQC += missing
				if b2b && r.HSN == "" {
					b2bNoHSN++
				}
			}
			k := r.HSN + "|" + r.UQC
			cur := hsn[k]
			if cur == nil {
				cur = &GSTHSNRow{HSN: r.HSN, UQC: r.UQC, Description: r.Description}
				hsn[k] = cur
			}
			if q.sign > 0 {
				cur.Quantity, cur.Value = cur.Quantity.Add(r.Quantity), cur.Value.Add(r.Value)
			} else {
				cur.Quantity, cur.Value = cur.Quantity.Sub(r.Quantity), cur.Value.Sub(r.Value)
			}
			cur.add(r.GSTTax, q.sign)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return rep, err
		}
	}
	short := 0
	for _, r := range hsn {
		rep.HSN = append(rep.HSN, *r)
		if r.HSN != "" && len(r.HSN) < HSNMinDigits {
			short++
		}
	}
	sort.Slice(rep.HSN, func(i, j int) bool {
		if rep.HSN[i].HSN != rep.HSN[j].HSN {
			return rep.HSN[i].HSN > rep.HSN[j].HSN // codes first, "" (no HSN) last
		}
		return rep.HSN[i].UQC < rep.HSN[j].UQC
	})

	// Documents issued.
	rows, err = tx.Query(ctx, qGSTDocs, gstin, p.From, p.To)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var d GSTDocRow
		if err := rows.Scan(&d.Kind, &d.Series, &d.From, &d.To, &d.Issued, &d.Cancelled); err != nil {
			rows.Close()
			return rep, err
		}
		rep.Docs = append(rep.Docs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	// Warnings.
	var old int
	if err := tx.QueryRow(ctx, qGSTOldSales, p.From, p.To).Scan(&old); err != nil {
		return rep, err
	}
	if old > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d sales in the period were made before GST details were recorded on sales; they are not in this report.", old))
	}
	if b2bNoHSN > 0 {
		rep.Warnings = append(rep.Warnings, "Some B2B invoice lines have no HSN code (see the HSN sheet's blank row). Add HSN codes to those products.")
	}
	if short > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d HSN codes are shorter than %d digits.", short, HSNMinDigits))
	}
	if noUQC > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d lines are in units without a GST unit code (shown as OTH). Set the code on the Units page.", noUQC))
	}
	return rep, nil
}
