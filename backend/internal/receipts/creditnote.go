package receipts

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Credit notes (M3 §5): a sale return printed as a document, with the
// seller, buyer and place of supply of its GST copy (the sale's for a
// return against one), the original invoice, and the tax split of the
// returned lines. Rendered on request from the saved return (a return is
// never edited, so nothing needs storing).

const (
	qReturnDoc = `SELECT r.reference, r.date, r.status, r.total, r.tax, r.paid, r.currency, r.note, r.settlement,
			s.reference, s.date, st.name, st.address, st.phone, c.name, c.email::text, u.name,
			r.seller_gstin, r.seller_state, r.buyer_gstin, r.buyer_name, r.buyer_address, r.buyer_state, r.place_of_supply,
			r.tax_split, r.invoice_kind
		FROM sale_returns r
		LEFT JOIN sales s ON s.id = r.sale_id
		JOIN stores st ON st.id = r.store_id
		JOIN customers c ON c.id = r.customer_id
		LEFT JOIN users u ON u.id = r.created_by
		WHERE r.id = $1`
	qReturnDocItems = `SELECT ri.product_name, ri.sku, ri.quantity, ri.unit_price, ri.discount, coalesce(oi.tax_rate, 0), ri.tax_amount,
			ri.cgst, ri.sgst, ri.igst, ri.subtotal, oi.hsn_code, coalesce(ri.reason, ''), ri.unit_name, ri.unit_qty
		FROM return_items ri LEFT JOIN order_items oi ON oi.id = ri.order_item_id
		WHERE ri.sale_return_id = $1 ORDER BY ri.id`
)

// SettlementLabels name how a return's value went back.
var SettlementLabels = map[string]string{"refund": "Refund", "store_credit": "Store credit", "exchange": "Exchange"}

// LoadCreditNote reads return id as a printable credit note.
func LoadCreditNote(ctx context.Context, tx tenancy.TenantTx, id int64) (View, error) {
	var v View
	b, err := settings.LoadBusiness(ctx, tx)
	if err != nil {
		return v, err
	}
	var (
		date                           time.Time
		saleDate                       *time.Time
		status, currency               string
		total, tax, paid               decimal.Decimal
		note, settlement, saleRef      *string
		storeAddr, storePhone, custEml *string
		cashier                        *string
		g                              saleGST
	)
	err = tx.QueryRow(ctx, qReturnDoc, id).Scan(&v.Reference, &date, &status, &total, &tax, &paid, &currency, &note, &settlement,
		&saleRef, &saleDate, &v.StoreName, &storeAddr, &storePhone, &v.Customer, &custEml, &cashier,
		&g.sellerGSTIN, &g.sellerState, &g.buyerGSTIN, &g.buyerName, &g.buyerAddress, &g.buyerState, &g.place, &g.split, &g.kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	f := NewFormatter(b.Locale, currency, b.Location())
	v.CreditNote, v.Title, v.SecondHalf = true, "Credit Note", "SGST"
	v.Currency, v.TotalAmount = currency, total
	v.BusinessName, v.LegalName, v.Phone, v.Email = b.Name, deref(b.LegalName), deref(b.Phone), deref(b.Email)
	v.Address, v.Footer = lines(b.Address), lines(b.ReceiptFooter)
	if g.sellerState != nil {
		g.apply(&v)
		v.Title = "Credit Note" // apply names a tax invoice; this is its credit note
	} else if b.TaxRegistrationNo != nil {
		v.TaxID = b.TaxLabel + " No: " + *b.TaxRegistrationNo
		if b.TaxLabel == "GST" {
			v.TaxID = "GSTIN: " + *b.TaxRegistrationNo
		}
	}
	v.StoreAddress, v.StorePhone = deref(storeAddr), deref(storePhone)
	v.Date, v.Cashier, v.CustomerEmail = f.DateTime(date), deref(cashier), deref(custEml)
	if saleRef != nil {
		against := "Against invoice " + *saleRef
		if saleDate != nil {
			against += " of " + f.Date(*saleDate)
		}
		v.Header = append(v.Header, against)
	} else {
		v.Header = append(v.Header, "Return without an invoice")
	}
	if s := SettlementLabels[deref(settlement)]; s != "" {
		v.Header = append(v.Header, "Given back as: "+s)
	}
	if n := deref(note); n != "" {
		v.Header = append(v.Header, n)
	}
	if b.LogoFileID != nil {
		if err := tx.QueryRow(ctx, qLogo, *b.LogoFileID).Scan(&v.LogoKey); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return v, err
		}
	}
	v.Tax, v.Total, v.Paid = f.Money(tax), f.Money(total), f.Money(paid)
	v.Subtotal = f.Money(total.Sub(tax))
	if status == "Void" {
		v.Void = true
	}

	rows, err := tx.Query(ctx, qReturnDocItems, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	accs := map[string]*taxAcc{}
	var order []string
	for rows.Next() {
		var (
			name, reason                    string
			sku, hsn, unitName              *string
			n                               qty.Q
			unitQty                         *qty.Q
			unit, ldisc, rate, ltax, ltotal decimal.Decimal
			cgst, sgst, igst                *decimal.Decimal
		)
		if err := rows.Scan(&name, &sku, &n, &unit, &ldisc, &rate, &ltax, &cgst, &sgst, &igst, &ltotal, &hsn, &reason, &unitName, &unitQty); err != nil {
			return v, err
		}
		ln := Line{Name: name, SKU: deref(sku), HSN: deref(hsn), Qty: n, UnitPrice: f.Money(unit), Discount: f.Money(ldisc),
			HasDiscount: ldisc.IsPositive(), TaxRate: f.Percent(rate), TaxAmount: f.Money(ltax), Total: f.Money(ltotal)}
		if reason != "" {
			ln.Attributes = "Reason: " + reason
		}
		if unitName != nil && unitQty != nil {
			ln.UnitName, ln.UnitQty = *unitName, *unitQty
		}
		v.Lines = append(v.Lines, ln)
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
		for _, x := range []struct{ p, to *decimal.Decimal }{{cgst, &a.cgst}, {sgst, &a.sgst}, {igst, &a.igst}} {
			if x.p != nil {
				*x.to = x.to.Add(*x.p)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	v.TaxRows = taxRows(accs, order, f, b.TaxLabel, v.SecondHalf)
	return v, nil
}

// CreditNotePDF renders return id's credit note and returns it with the
// return's reference.
func (s *Service) CreditNotePDF(ctx context.Context, t tenancy.Tenant, id int64) ([]byte, string, error) {
	var v View
	err := s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		var err error
		v, err = LoadCreditNote(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	var logo []byte
	if v.LogoKey != "" {
		if rc, _, err := s.store.Get(ctx, v.LogoKey); err == nil {
			logo, _ = io.ReadAll(io.LimitReader(rc, 4<<20))
			_ = rc.Close()
		}
	}
	pdf, err := RenderPDF(v, logo)
	return pdf, v.Reference, err
}
