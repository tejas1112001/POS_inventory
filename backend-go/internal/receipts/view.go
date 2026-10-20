// Package receipts renders sale receipts as PDF (EMAIL_AND_RECEIPTS §5),
// stores them per sale version and serves the download and resend
// endpoints. Everything a receipt shows is read in the tenant's own
// transaction; Load takes a TenantTx and a receipt ID and nothing else.
package receipts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Errors.
var (
	ErrNotFound   = errors.New("receipts: not found")
	ErrSuperseded = errors.New("receipts: the sale has a newer version")
)

// Line is one item, already formatted.
type Line struct {
	Name, Attributes, SKU                          string
	Qty                                            int
	UnitPrice, Discount, TaxRate, TaxAmount, Total string
	HasDiscount                                    bool
}

// TaxRow is one line of the tax summary ("CGST 9%").
type TaxRow struct {
	Label, Taxable, Amount string
}

// View is everything a receipt (PDF or email) shows, formatted for the
// tenant's currency, number format and time zone.
type View struct {
	ReceiptID, SaleID int64
	Version           int
	Title             string

	BusinessName, LegalName, Phone, Email, TaxID string
	Address                                      []string
	LogoKey                                      string // storage key, read outside the transaction

	StoreName, StoreAddress, StorePhone string

	Reference, Date, Cashier, Customer, CustomerEmail string

	Lines   []Line
	TaxRows []TaxRow

	Subtotal, Discount, Tax, Shipping, Total, Paid, Tendered, Change, Due string
	HasDiscount, HasShipping, HasTendered, HasDue                         bool
	PaymentMethod, PaymentStatus                                          string

	Header, Footer []string

	Void       bool
	VoidedAt   string
	VoidReason string

	// Raw values for templates that need them.
	TotalAmount decimal.Decimal
	Currency    string
	ReceiptURL  string
}

const (
	qReceipt = `SELECT sale_id, sale_version FROM receipts WHERE id = $1`
	qSale    = `SELECT s.reference, s.date, s.status, s.payment_status, s.payment_method, s.subtotal, s.discount, s.tax,
			s.shipping, s.total, s.paid, s.due, s.tendered, s.change_due, s.currency, s.version, s.receipt_email::text,
			s.voided_at, s.void_reason, st.name, st.address, st.phone, c.name, c.email::text, u.name
		FROM sales s
		JOIN stores st ON st.id = s.store_id
		JOIN customers c ON c.id = s.customer_id
		JOIN users u ON u.id = s.user_id
		WHERE s.id = $1`
	qItems = `SELECT i.product_name, i.sku, i.quantity, i.unit_price, i.discount, i.tax_rate, i.tax_amount,
			i.cgst, i.sgst, i.igst, i.subtotal, v.attributes
		FROM order_items i LEFT JOIN product_variants v ON v.id = i.variant_id
		WHERE i.sale_id = $1 ORDER BY i.id`
	qLogo = `SELECT storage_key FROM files WHERE id = $1`
)

type taxAcc struct {
	rate                          decimal.Decimal
	taxable, cgst, sgst, igst, tx decimal.Decimal
}

func lines(s *string) []string {
	if s == nil {
		return nil
	}
	var out []string
	for l := range strings.SplitSeq(*s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func attributes(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// Load reads one receipt's data in the tenant's transaction.
func Load(ctx context.Context, tx tenancy.TenantTx, receiptID int64) (View, error) {
	v := View{ReceiptID: receiptID}
	if err := tx.QueryRow(ctx, qReceipt, receiptID).Scan(&v.SaleID, &v.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return v, ErrNotFound
		}
		return v, err
	}
	b, err := settings.LoadBusiness(ctx, tx)
	if err != nil {
		return v, err
	}
	var (
		date                                   time.Time
		status, payStatus, currency            string
		method, receiptEmail, voidReason       *string
		storeAddr, storePhone, custEmail       *string
		sub, disc, tax, ship, total, paid, due decimal.Decimal
		tendered, change                       *decimal.Decimal
		version                                int
		voidedAt                               *time.Time
	)
	if err := tx.QueryRow(ctx, qSale, v.SaleID).Scan(&v.Reference, &date, &status, &payStatus, &method, &sub, &disc, &tax,
		&ship, &total, &paid, &due, &tendered, &change, &currency, &version, &receiptEmail, &voidedAt, &voidReason,
		&v.StoreName, &storeAddr, &storePhone, &v.Customer, &custEmail, &v.Cashier); err != nil {
		return v, err
	}
	if version != v.Version {
		return v, ErrSuperseded
	}
	f := NewFormatter(b.Locale, currency, b.Location())
	v.Currency, v.TotalAmount = currency, total
	v.BusinessName, v.LegalName, v.Phone, v.Email = b.Name, deref(b.LegalName), deref(b.Phone), deref(b.Email)
	v.Address, v.Header, v.Footer = lines(b.Address), lines(b.ReceiptHeader), lines(b.ReceiptFooter)
	v.Title = "Receipt"
	if b.TaxRegistrationNo != nil {
		v.TaxID = b.TaxLabel + " No: " + *b.TaxRegistrationNo
		if b.TaxLabel == "GST" {
			v.TaxID = "GSTIN: " + *b.TaxRegistrationNo
		}
		v.Title = "Tax Invoice"
	}
	v.StoreAddress, v.StorePhone = deref(storeAddr), deref(storePhone)
	v.Date = f.DateTime(date)
	v.CustomerEmail = deref(receiptEmail)
	if v.CustomerEmail == "" {
		v.CustomerEmail = deref(custEmail)
	}
	v.Subtotal, v.Discount, v.Tax, v.Shipping = f.Money(sub), f.Money(disc), f.Money(tax), f.Money(ship)
	v.Total, v.Paid, v.Due = f.Money(total), f.Money(paid), f.Money(due)
	v.HasDiscount, v.HasShipping, v.HasDue = disc.IsPositive(), ship.IsPositive(), due.IsPositive()
	if tendered != nil && tendered.IsPositive() {
		v.HasTendered, v.Tendered = true, f.Money(*tendered)
		if change != nil {
			v.Change = f.Money(*change)
		}
	}
	v.PaymentMethod, v.PaymentStatus = deref(method), payStatus
	if status == "Void" {
		v.Void, v.VoidReason = true, deref(voidReason)
		if voidedAt != nil {
			v.VoidedAt = f.DateTime(*voidedAt)
		}
	}
	if b.LogoFileID != nil {
		err := tx.QueryRow(ctx, qLogo, *b.LogoFileID).Scan(&v.LogoKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return v, err
		}
	}

	rows, err := tx.Query(ctx, qItems, v.SaleID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	accs := map[string]*taxAcc{}
	var order []string
	for rows.Next() {
		var (
			name                            string
			sku                             *string
			qty                             int
			unit, ldisc, rate, ltax, ltotal decimal.Decimal
			cgst, sgst, igst                *decimal.Decimal
			attrs                           []byte
		)
		if err := rows.Scan(&name, &sku, &qty, &unit, &ldisc, &rate, &ltax, &cgst, &sgst, &igst, &ltotal, &attrs); err != nil {
			return v, err
		}
		v.Lines = append(v.Lines, Line{Name: name, Attributes: attributes(attrs), SKU: deref(sku), Qty: qty,
			UnitPrice: f.Money(unit), Discount: f.Money(ldisc), HasDiscount: ldisc.IsPositive(),
			TaxRate: f.Percent(rate), TaxAmount: f.Money(ltax), Total: f.Money(ltotal)})
		if !rate.IsPositive() {
			continue
		}
		k := rate.String()
		a := accs[k]
		if a == nil {
			a = &taxAcc{rate: rate}
			accs[k] = a
			order = append(order, k)
		}
		a.taxable = a.taxable.Add(ltotal.Sub(ltax))
		a.tx = a.tx.Add(ltax)
		if cgst != nil {
			a.cgst = a.cgst.Add(*cgst)
		}
		if sgst != nil {
			a.sgst = a.sgst.Add(*sgst)
		}
		if igst != nil {
			a.igst = a.igst.Add(*igst)
		}
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	half := decimal.NewFromInt(2)
	for _, k := range order {
		a := accs[k]
		switch {
		case a.cgst.IsPositive() || a.sgst.IsPositive():
			hr := f.Percent(a.rate.Div(half))
			v.TaxRows = append(v.TaxRows,
				TaxRow{Label: "CGST " + hr, Taxable: f.Money(a.taxable), Amount: f.Money(a.cgst)},
				TaxRow{Label: "SGST " + hr, Taxable: f.Money(a.taxable), Amount: f.Money(a.sgst)})
		case a.igst.IsPositive():
			v.TaxRows = append(v.TaxRows, TaxRow{Label: "IGST " + f.Percent(a.rate), Taxable: f.Money(a.taxable), Amount: f.Money(a.igst)})
		default:
			v.TaxRows = append(v.TaxRows, TaxRow{Label: b.TaxLabel + " " + f.Percent(a.rate), Taxable: f.Money(a.taxable), Amount: f.Money(a.tx)})
		}
	}
	return v, nil
}
