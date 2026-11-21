package sales

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/pricing"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

const (
	maxLines   = 500
	maxQty     = 1_000_000
	maxNoteLen = 1000
)

// ----------------------------------------------------------------- input

type itemIn struct {
	ProductID money.Int `json:"productId"`
	VariantID money.Int `json:"variantId"`
	Quantity  qty.In    `json:"quantity"`
	UnitPrice money.Dec `json:"unitPrice"`
	Discount  money.Dec `json:"discount"`
	// UnitID sells the line in another of the product's units (H3): the
	// quantity is in that unit, the unit price stays per base unit.
	UnitID money.Int `json:"unitId"`
	// Batches the cashier chose (industry P3 item 3, A27, Q12); the rest
	// of the line is filled earliest expiry first.
	Batches []batchPickIn `json:"batches"`

	// Accepted and ignored: the server computes line totals (C-04).
	Total    json.RawMessage `json:"total"`
	Subtotal json.RawMessage `json:"subtotal"`
}

// saleIn is the POST/PUT body. Decoding is strict, so every key the POS
// and the back-office form send is listed: either used, an alias, or on the
// ignore list (C-04).
type saleIn struct {
	OrderItems    []itemIn  `json:"orderItems"`
	CustomerID    money.Int `json:"customerId"`
	StoreID       money.Int `json:"storeId"`
	Discount      money.Dec `json:"discount"`
	Shipping      money.Dec `json:"shipping"`
	Paid          money.Dec `json:"paid"`
	Status        *string   `json:"status"`
	Date          *string   `json:"date"`
	PaymentMethod *string   `json:"payment_method"`
	ReceiptEmail  *string   `json:"receiptEmail"`
	Note          *string   `json:"note"`
	PaymentNote   *string   `json:"payment_note"`
	Tendered      money.Dec `json:"tendered"`
	// Split and part payment (S10 item 5): the payments, and the due date
	// (YYYY-MM-DD) of what is left on account.
	Payments []paymentIn `json:"payments"`
	DueDate  *string     `json:"dueDate"`
	// PlaceOfSupply is the GST state the goods are supplied to (M3 A4);
	// empty or missing: the store's state.
	PlaceOfSupply *string `json:"placeOfSupply"`

	// POS aliases.
	Method         *string   `json:"method"`
	PaymentNoteC   *string   `json:"paymentNote"`
	SaleNote       *string   `json:"saleNote"`
	ReceivedAmount money.Dec `json:"receivedAmount"`
	AmountTendered money.Dec `json:"amountTendered"`

	// Accepted and ignored: totals are computed here, the reference is
	// generated, the biller is the signed-in user, and the PayForm keys are
	// UI state.
	Subtotal      json.RawMessage `json:"subtotal"`
	Tax           json.RawMessage `json:"tax"`
	Total         json.RawMessage `json:"total"`
	Due           json.RawMessage `json:"due"`
	PaymentStatus json.RawMessage `json:"payment_status"`
	Reference     json.RawMessage `json:"reference"`
	UserID        json.RawMessage `json:"userId"`
	PayingAmount  json.RawMessage `json:"payingAmount"`
	Change        json.RawMessage `json:"change"`
	PaymentType   json.RawMessage `json:"paymentType"`
	QuickCash     json.RawMessage `json:"quickCash"`
}

type line struct {
	productID, variantID int64 // variantID 0: the product's only variant
	qty                  qty.Q // in base units (converted from unit when set)
	unitID               int64
	unit                 *lineUnit
	price                decimal.Decimal
	priceSet             bool
	picks                []batches.Pick // industry P3 item 3
}

// batchPickIn is a batch the cashier chose for a line (A27).
type batchPickIn struct {
	BatchID  money.Int `json:"batchId"`
	Quantity qty.In    `json:"quantity"`
}

// maxPicks bounds the batches named on one line.
const maxPicks = 50

type cleanSale struct {
	lines              []line
	customerID         int64 // 0: the default (walk-in) customer
	storeID            int64
	discount, shipping decimal.Decimal
	paid, tendered     *decimal.Decimal
	status             string // "" keeps the default/current value
	date               string
	paymentMethod      *string
	paymentNote, note  *string
	receiptEmail       *string
	payments           []payLine // nil: the old single-payment fields
	dueDate            string
	place              string // M3: the place of supply asked for ("" the store's state)
}

type fieldErrs map[string]string

func (f fieldErrs) err() error { return apperr.Fields(f) }

func firstStr(vals ...*string) *string {
	for _, v := range vals {
		if v != nil {
			s := strings.TrimSpace(*v)
			return &s
		}
	}
	return nil
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && len(s) <= 254
}

func validate(in saleIn) (cleanSale, error) {
	f := fieldErrs{}
	c := cleanSale{}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		f["storeId"] = "storeId is required."
	}
	c.storeID = in.StoreID.V
	if in.CustomerID.Valid {
		if in.CustomerID.V <= 0 {
			f["customerId"] = "customerId is invalid."
		}
		c.customerID = in.CustomerID.V
	}
	switch {
	case len(in.OrderItems) == 0:
		f["orderItems"] = "At least one order item is required."
	case len(in.OrderItems) > maxLines:
		f["orderItems"] = fmt.Sprintf("A sale can have at most %d lines.", maxLines)
	}
	for i, it := range in.OrderItems {
		k := fmt.Sprintf("orderItems[%d]", i)
		l := line{productID: it.ProductID.V, variantID: it.VariantID.V, qty: it.Quantity.V, unitID: it.UnitID.V}
		if it.UnitID.Valid && it.UnitID.V <= 0 {
			f[k+".unitId"] = "unitId is invalid."
		}
		if !it.Quantity.Valid || !it.Quantity.V.IsPositive() || it.Quantity.V.Greater(qty.Int(maxQty)) {
			f[k+".quantity"] = "Quantity must be above 0."
		}
		if (!it.VariantID.Valid || it.VariantID.V <= 0) && (!it.ProductID.Valid || it.ProductID.V <= 0) {
			f[k+".variantId"] = "variantId is required."
		}
		if it.UnitPrice.Valid {
			if it.UnitPrice.D.IsNegative() {
				f[k+".unitPrice"] = "Unit price cannot be negative."
			}
			l.price, l.priceSet = it.UnitPrice.D.Round(4), true
		}
		if it.Discount.Valid && !it.Discount.D.IsZero() {
			f[k+".discount"] = "Line discounts are not supported; use the sale discount."
		}
		if len(it.Batches) > maxPicks {
			f[k+".batches"] = fmt.Sprintf("At most %d batches per line.", maxPicks)
		}
		for j, b := range it.Batches {
			bk := fmt.Sprintf("%s.batches[%d]", k, j)
			if !b.BatchID.Valid || b.BatchID.V <= 0 {
				f[bk+".batchId"] = "batchId is required."
			}
			if !b.Quantity.Valid || !b.Quantity.V.IsPositive() || b.Quantity.V.Greater(qty.Int(maxQty)) {
				f[bk+".quantity"] = "Quantity must be above 0."
			}
			l.picks = append(l.picks, batches.Pick{BatchID: b.BatchID.V, Qty: b.Quantity.V})
		}
		c.lines = append(c.lines, l)
	}
	for name, v := range map[string]money.Dec{"discount": in.Discount, "shipping": in.Shipping, "paid": in.Paid} {
		if v.Valid && v.D.IsNegative() {
			f[name] = "Must not be negative."
		}
	}
	if in.Discount.Valid {
		c.discount = in.Discount.D
	}
	if in.Shipping.Valid {
		c.shipping = in.Shipping.D
	}
	if in.Paid.Valid {
		p := in.Paid.D
		c.paid = &p
	}
	// The PayForm sends amountTendered 0 for card payments: 0 means none.
	for _, t := range []money.Dec{in.AmountTendered, in.ReceivedAmount, in.Tendered} {
		if t.Valid && t.D.IsPositive() {
			v := t.D
			c.tendered = &v
			break
		}
	}
	if in.Status != nil {
		switch s := strings.TrimSpace(*in.Status); s {
		case "", "Completed", "Pending":
			c.status = s
		default:
			f["status"] = "Status must be Completed or Pending. Use DELETE to void a sale."
		}
	}
	if in.Date != nil {
		c.date = strings.TrimSpace(*in.Date)
	}
	c.paymentMethod = firstStr(in.PaymentMethod, in.Method)
	if c.paymentMethod != nil && len(*c.paymentMethod) > 40 {
		f["payment_method"] = "Payment method is too long."
	}
	c.paymentNote = firstStr(in.PaymentNote, in.PaymentNoteC)
	c.note = firstStr(in.Note, in.SaleNote)
	for k, v := range map[string]*string{"payment_note": c.paymentNote, "note": c.note} {
		if v != nil && len([]rune(*v)) > maxNoteLen {
			f[k] = fmt.Sprintf("At most %d characters.", maxNoteLen)
		}
	}
	if in.Payments != nil {
		c.payments = validatePayments(in.Payments, f)
	}
	if in.DueDate != nil {
		c.dueDate = strings.TrimSpace(*in.DueDate)
	}
	if in.PlaceOfSupply != nil {
		c.place = strings.TrimSpace(*in.PlaceOfSupply) // checked with the store (taxContext)
	}
	if in.ReceiptEmail != nil {
		e := strings.TrimSpace(*in.ReceiptEmail)
		if e != "" && !validEmail(e) {
			f["receiptEmail"] = "Enter a valid email address."
		}
		c.receiptEmail = &e
	}
	return c, f.err()
}

// --------------------------------------------------------------- settings

type settings struct {
	currency, split, prefix string
	taxReg, stateCode       *string // M3: the shop's GSTIN (when GST) and state
	inclusiveDefault        bool
	maxDiscountPct          decimal.Decimal
	defaultCustomer         *int64
	loc                     *time.Location
}

func loadSettings(ctx context.Context, tx tenancy.TenantTx) (settings, error) {
	var s settings
	var tz string
	err := tx.QueryRow(ctx, `SELECT currency, tax_split, invoice_prefix, prices_include_tax_default,
			max_discount_pct_without_override, default_customer_id, timezone,
			CASE WHEN upper(tax_label) = 'GST' AND length(tax_registration_no) = 15 THEN tax_registration_no END, state_code
		FROM settings WHERE id = 1`).
		Scan(&s.currency, &s.split, &s.prefix, &s.inclusiveDefault, &s.maxDiscountPct, &s.defaultCustomer, &tz, &s.taxReg, &s.stateCode)
	if err != nil {
		return s, err
	}
	if s.loc, err = time.LoadLocation(tz); err != nil {
		s.loc = time.UTC
	}
	return s, nil
}

// --------------------------------------------------------------- variants

type variantRow struct {
	id, productID int64
	sku           string
	price         decimal.Decimal
	productName   string
	status        string
	taxType       *string
	taxRate       *decimal.Decimal
	mrp           *decimal.Decimal // the variant's MRP; a batch's MRP wins (lineMRP)
	fractional    bool             // H3: sold in part units
}

const (
	qSingleVariant = `SELECT p.id, min(v.id), count(v.id) FROM products p
		LEFT JOIN product_variants v ON v.product_id = p.id AND v.deleted_at IS NULL
		WHERE p.id = ANY($1) AND p.deleted_at IS NULL GROUP BY p.id`
	qVariants = `SELECT v.id, v.product_id, v.sku::text, v.price, p.name, p.status, p.tax_type, p.tax_rate, v.mrp, p.allow_fraction
		FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = ANY($1) AND v.deleted_at IS NULL AND p.deleted_at IS NULL`
)

// resolve fills each line's variant (a line with only productId must name a
// single-variant product) and returns the catalog rows by variant ID.
func resolve(ctx context.Context, tx tenancy.TenantTx, lines []line) (map[int64]variantRow, error) {
	f := fieldErrs{}
	var productOnly, ids []int64
	for _, l := range lines {
		if l.variantID > 0 {
			ids = append(ids, l.variantID)
		} else {
			productOnly = append(productOnly, l.productID)
		}
	}
	if len(productOnly) > 0 {
		rows, err := tx.Query(ctx, qSingleVariant, productOnly)
		if err != nil {
			return nil, err
		}
		only := map[int64]int64{}
		for rows.Next() {
			var pid int64
			var vid *int64
			var n int
			if err := rows.Scan(&pid, &vid, &n); err != nil {
				rows.Close()
				return nil, err
			}
			if n == 1 && vid != nil {
				only[pid] = *vid
			} else {
				only[pid] = -1
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i := range lines {
			if lines[i].variantID > 0 {
				continue
			}
			k := fmt.Sprintf("orderItems[%d].variantId", i)
			switch v := only[lines[i].productID]; {
			case v == 0:
				f[k] = "Product not found."
			case v < 0:
				f[k] = "Choose a variant for this product."
			default:
				lines[i].variantID = v
				ids = append(ids, v)
			}
		}
	}
	rows, err := tx.Query(ctx, qVariants, ids)
	if err != nil {
		return nil, err
	}
	byID := map[int64]variantRow{}
	for rows.Next() {
		var v variantRow
		if err := rows.Scan(&v.id, &v.productID, &v.sku, &v.price, &v.productName, &v.status, &v.taxType, &v.taxRate, &v.mrp, &v.fractional); err != nil {
			rows.Close()
			return nil, err
		}
		byID[v.id] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	units, err := saleUnits(ctx, tx, lines, byID)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		l := &lines[i]
		if l.variantID <= 0 {
			continue
		}
		k := fmt.Sprintf("orderItems[%d]", i)
		v, ok := byID[l.variantID]
		if ok && l.unitID > 0 {
			u, found := units[unitKey{v.productID, l.unitID}]
			if !found {
				f[k+".unitId"] = "This product is not sold in that unit."
				continue
			}
			base, err := l.qty.Times(u.factor)
			if err != nil {
				f[k+".quantity"] = "That quantity of this unit is not a whole thousandth of the base unit."
				continue
			}
			u.qty, l.qty = l.qty, base
			l.unit = &u
		}
		switch {
		case !ok:
			f[k+".variantId"] = "Product variant not found."
		case l.productID > 0 && v.productID != l.productID:
			f[k+".variantId"] = "The variant does not belong to this product."
		case v.status != "Active":
			f[k+".productId"] = fmt.Sprintf("%s is inactive and cannot be sold.", v.productName)
		case !inventory.AllowedQty(l.qty, v.fractional):
			f[k+".quantity"] = inventory.WholeOnlyMessage
		default:
			for j, pk := range l.picks {
				if !inventory.AllowedQty(pk.Qty, v.fractional) {
					f[fmt.Sprintf("%s.batches[%d].quantity", k, j)] = inventory.WholeOnlyMessage
				}
			}
		}
	}
	return byID, f.err()
}

// ---------------------------------------------------------------- pricing

// priorPrice is a unit price already on the sale being edited; keeping it
// is not an override even if the catalog price changed since.
type priorPrice struct {
	variantID int64
	unit      string // decimal.String(): decimal.Decimal is not comparable as a map key
}

type pricedLine struct {
	line
	v        variantRow
	unit     decimal.Decimal
	catalog  decimal.Decimal
	override bool
	q        pricing.LineQuote
	taxRate  decimal.Decimal
	taxType  *string
}

type priced struct {
	lines     []pricedLine
	quote     pricing.Quote
	overrides []map[string]any
}

// PriceMismatch is one entry of the PRICE_CHANGED details.
type PriceMismatch struct {
	Index     int             `json:"index"`
	ProductID int64           `json:"productId"`
	VariantID int64           `json:"variantId"`
	SKU       string          `json:"sku"`
	UnitPrice decimal.Decimal `json:"unitPrice"`
	Sent      decimal.Decimal `json:"sent"`
}

func price(c cleanSale, byID map[int64]variantRow, s settings, p auth.Principal, prior map[priorPrice]decimal.Decimal) (priced, error) {
	canOverride := p.Has("sales:price_override")
	var out priced
	var mismatches []PriceMismatch
	var pl []pricing.Line
	for i, l := range c.lines {
		v := byID[l.variantID]
		x := pricedLine{line: l, v: v, unit: v.price, catalog: v.price, taxType: v.taxType}
		if v.taxRate != nil {
			x.taxRate = *v.taxRate
		}
		if l.priceSet && !l.price.Equal(v.price) {
			if cat, ok := prior[priorPrice{v.id, l.price.String()}]; ok {
				x.unit, x.catalog = l.price, cat
			} else if canOverride {
				x.unit, x.override = l.price, true
				out.overrides = append(out.overrides, map[string]any{"variantId": v.id, "sku": v.sku,
					"catalogPrice": v.price, "unitPrice": l.price, "quantity": l.qty})
			} else {
				mismatches = append(mismatches, PriceMismatch{Index: i, ProductID: v.productID, VariantID: v.id,
					SKU: v.sku, UnitPrice: v.price, Sent: l.price})
			}
		}
		inclusive := s.inclusiveDefault
		if v.taxType != nil {
			inclusive = *v.taxType == "Inclusive"
		}
		pl = append(pl, pricing.Line{UnitPrice: x.unit, Qty: l.qty, TaxRate: x.taxRate, Inclusive: inclusive})
		out.lines = append(out.lines, x)
	}
	if len(mismatches) > 0 {
		return out, apperr.Conflict(apperr.CodePriceChanged, "Prices have changed. Review the cart and try again.").
			WithDetails(map[string]any{"items": mismatches})
	}
	q, err := pricing.Compute(pl, c.discount, c.shipping, pricing.Settings{Currency: s.currency, Split: s.split})
	if errors.Is(err, pricing.ErrDiscountTooLarge) {
		return out, apperr.Invalid("discount", "Discount cannot be larger than the subtotal.")
	}
	if err != nil {
		return out, apperr.Invalid("orderItems", "Invalid amounts.")
	}
	for i := range out.lines {
		out.lines[i].q = q.Lines[i]
	}
	out.quote = q
	return out, checkDiscount(q, s, p)
}

// checkDiscount: any discount needs sales:discount; above the settings cap
// it also needs sales:price_override.
func checkDiscount(q pricing.Quote, s settings, p auth.Principal) error {
	if q.Discount.IsZero() {
		return nil
	}
	if !p.Has("sales:discount") {
		return apperr.Forbidden(apperr.CodeForbidden, "You do not have permission to give discounts.")
	}
	if q.Subtotal.IsPositive() && !p.Has("sales:price_override") {
		pct := q.Discount.Mul(decimal.NewFromInt(100)).Div(q.Subtotal)
		if pct.GreaterThan(s.maxDiscountPct) {
			return apperr.Forbidden(apperr.CodeForbidden,
				fmt.Sprintf("Discounts above %s%% need the price override permission.", s.maxDiscountPct.String()))
		}
	}
	return nil
}

// payment derives paid, due, change and payment_status. A missing paid
// means paid in full; paid: 0 is respected (C-04).
type payment struct {
	paid, due     decimal.Decimal
	tendered, chg *decimal.Decimal
	status        string
}

func pay(total decimal.Decimal, paid, tendered *decimal.Decimal, currency string) payment {
	p := payment{paid: total}
	if paid != nil {
		p.paid = decimal.Min(money.Round(*paid, currency), total)
	}
	p.due = total.Sub(p.paid)
	switch {
	case p.paid.GreaterThanOrEqual(total):
		p.status = "Paid"
	case p.paid.IsPositive():
		p.status = "Partial"
	default:
		p.status = "Unpaid"
	}
	if tendered != nil {
		t := money.Round(*tendered, currency)
		chg := decimal.Max(t.Sub(p.paid), decimal.Zero)
		p.tendered, p.chg = &t, &chg
	}
	return p
}

// --------------------------------------------------------------- helpers

// checkParties validates the customer (or the default walk-in customer)
// and the store, and returns the customer ID to store.
func checkParties(ctx context.Context, tx tenancy.TenantTx, c cleanSale, s settings) (int64, error) {
	cust := c.customerID
	if cust == 0 {
		if s.defaultCustomer == nil {
			return 0, apperr.Invalid("customerId", "customerId is required.")
		}
		cust = *s.defaultCustomer
	}
	var custOK, storeOK bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1 AND deleted_at IS NULL),
			EXISTS (SELECT 1 FROM stores WHERE id = $2 AND deleted_at IS NULL AND status = 'Active')`, cust, c.storeID).
		Scan(&custOK, &storeOK); err != nil {
		return 0, err
	}
	f := fieldErrs{}
	if !custOK {
		f["customerId"] = "Customer not found."
	}
	if !storeOK {
		f["storeId"] = "Store not found or inactive."
	}
	return cust, f.err()
}

// nextReference takes the next gapless sale number. The counter row stays
// locked until commit, so call it after the inventory locks.
func nextReference(ctx context.Context, tx tenancy.TenantTx, prefix string) (string, error) {
	var n int64
	if err := tx.QueryRow(ctx, `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'sale' RETURNING next_value - 1`).Scan(&n); err != nil {
		return "", err
	}
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "-")
	if prefix == "" {
		return fmt.Sprintf("%06d", n), nil
	}
	return fmt.Sprintf("%s-%06d", prefix, n), nil
}

// qInsertItem snapshots the product's HSN code (industry P1 item 10,
// DB24) and the variant label ("M / Blue", "UK 8 /
// Black": attributes.Label in SQL) when the product has variant
// dimensions (industry P1 item 4, DB8); NULL otherwise.
const qInsertItem = `INSERT INTO order_items (sale_id, product_id, variant_id, product_name, sku, quantity, unit_price,
		catalog_price, discount, tax_type, tax_rate, tax_amount, cgst, sgst, igst, subtotal, mrp, mrp_amount, unit_name, unit_factor, unit_qty,
		unit_id, hsn_code, variant_label)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22,
		(SELECT hsn_code FROM products WHERE id = $2),
		(SELECT string_agg(CASE WHEN d.k = 'size' AND coalesce(p.attributes ->> 'size_system', '') <> ''
		          THEN (p.attributes ->> 'size_system') || ' ' ELSE '' END || (v.attributes ->> d.k), ' / ' ORDER BY d.ord)
		   FROM product_variants v JOIN products p ON p.id = v.product_id
		   CROSS JOIN LATERAL unnest(p.variant_dims) WITH ORDINALITY AS d(k, ord)
		  WHERE v.id = $3 AND coalesce(v.attributes ->> d.k, '') <> ''))
	RETURNING id`

// insertItems writes the order lines in one batch (one round trip) and
// returns their IDs in line order. Each line keeps its MRP at sale time,
// from the batches it is sold from (sb) or the variant.
func insertItems(ctx context.Context, tx tenancy.TenantTx, saleID int64, p priced, sb saleBatches) ([]int64, error) {
	b := &pgx.Batch{}
	for i, l := range p.lines {
		unitMRP, mrpAmount := lineMRP(l, sb.sold[i])
		un, uf, uq := l.line.unit.columns()
		b.Queue(qInsertItem, saleID, l.v.productID, l.v.id, l.v.productName, l.v.sku, l.qty, l.unit,
			l.catalog, l.q.Discount, l.taxType, l.taxRate, l.q.Tax, l.q.CGST, l.q.SGST, l.q.IGST, l.q.Total, unitMRP, mrpAmount, un, uf, uq,
			l.line.unit.unitID())
	}
	br := tx.SendBatch(ctx, b)
	ids := make([]int64, len(p.lines))
	for i := range p.lines {
		if err := br.QueryRow().Scan(&ids[i]); err != nil {
			_ = br.Close()
			return nil, err
		}
	}
	return ids, br.Close()
}

// lineMRP is a line's MRP: one unit's (nil when unknown or when its
// batches carry different MRPs) and the whole line's, summed over its
// batches (nil when any unit has none). A batch's MRP wins over the
// variant's.
func lineMRP(l pricedLine, sold []batches.Sold) (unit, amount *decimal.Decimal) {
	if len(sold) == 0 {
		if l.v.mrp == nil {
			return nil, nil
		}
		a := l.v.mrp.Mul(l.qty.Decimal())
		return l.v.mrp, &a
	}
	total := decimal.Zero
	same := true
	for _, s := range sold {
		m := s.MRP
		if m == nil {
			m = l.v.mrp
		}
		if m == nil {
			return nil, nil
		}
		if unit == nil {
			unit = m
		} else if !unit.Equal(*m) {
			same = false
		}
		total = total.Add(m.Mul(s.Qty.Decimal()))
	}
	if !same {
		unit = nil
	}
	return unit, &total
}

// aboveMRP reports whether a line costs the customer more than its MRP
// (tax and the discount share included). Warned about, never refused.
func aboveMRP(total decimal.Decimal, mrpAmount *decimal.Decimal) bool {
	return mrpAmount != nil && total.GreaterThan(*mrpAmount)
}

// mrpWarning is the line warning for a line above its MRP.
func mrpWarning(total, mrpAmount decimal.Decimal) string {
	return fmt.Sprintf("Charged %s, above the MRP of %s.", total.StringFixed(2), mrpAmount.StringFixed(2))
}

func skuMap(p priced) map[int64]string {
	m := map[int64]string{}
	for _, l := range p.lines {
		m[l.v.id] = l.v.sku
	}
	return m
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
