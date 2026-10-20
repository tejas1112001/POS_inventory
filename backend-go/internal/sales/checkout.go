package sales

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/pricing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const (
	maxLines    = 500
	maxQty      = 1_000_000
	maxNoteLen  = 1000
	maxFutureBy = 24 * time.Hour
)

// ----------------------------------------------------------------- input

type itemIn struct {
	ProductID money.Int `json:"productId"`
	VariantID money.Int `json:"variantId"`
	Quantity  money.Int `json:"quantity"`
	UnitPrice money.Dec `json:"unitPrice"`
	Discount  money.Dec `json:"discount"`

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
	qty                  int64
	price                decimal.Decimal
	priceSet             bool
}

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
}

type fieldErrs map[string]string

func (f fieldErrs) err() error {
	if len(f) == 0 {
		return nil
	}
	if len(f) == 1 {
		for k, v := range f {
			return apperr.Invalid(k, v)
		}
	}
	return apperr.Validation(f)
}

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
		l := line{productID: it.ProductID.V, variantID: it.VariantID.V, qty: it.Quantity.V}
		if !it.Quantity.Valid || it.Quantity.V <= 0 || it.Quantity.V > maxQty {
			f[k+".quantity"] = "Quantity must be a whole number above 0."
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
	inclusiveDefault        bool
	maxDiscountPct          decimal.Decimal
	defaultCustomer         *int64
	loc                     *time.Location
}

func loadSettings(ctx context.Context, tx tenancy.TenantTx) (settings, error) {
	var s settings
	var tz string
	err := tx.QueryRow(ctx, `SELECT currency, tax_split, invoice_prefix, prices_include_tax_default,
			max_discount_pct_without_override, default_customer_id, timezone FROM settings WHERE id = 1`).
		Scan(&s.currency, &s.split, &s.prefix, &s.inclusiveDefault, &s.maxDiscountPct, &s.defaultCustomer, &tz)
	if err != nil {
		return s, err
	}
	if s.loc, err = time.LoadLocation(tz); err != nil {
		s.loc = time.UTC
	}
	return s, nil
}

// parseDate reads a sale date: RFC 3339, or YYYY-MM-DD in the tenant's
// time zone (today means now). Empty means now. Far-future dates are
// refused.
func parseDate(v string, loc *time.Location, now time.Time) (time.Time, error) {
	if v == "" {
		return now, nil
	}
	var t time.Time
	if d, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		if d.Format("2006-01-02") == now.In(loc).Format("2006-01-02") {
			return now, nil
		}
		t = d
	} else if d, err := time.Parse(time.RFC3339, v); err == nil {
		t = d
	} else {
		return time.Time{}, apperr.Invalid("date", "Date must be YYYY-MM-DD or an RFC 3339 timestamp.")
	}
	if t.After(now.Add(maxFutureBy)) {
		return time.Time{}, apperr.Invalid("date", "Date cannot be in the future.")
	}
	return t, nil
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
}

const (
	qSingleVariant = `SELECT p.id, min(v.id), count(v.id) FROM products p
		LEFT JOIN product_variants v ON v.product_id = p.id AND v.deleted_at IS NULL
		WHERE p.id = ANY($1) AND p.deleted_at IS NULL GROUP BY p.id`
	qVariants = `SELECT v.id, v.product_id, v.sku::text, v.price, p.name, p.status, p.tax_type, p.tax_rate
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
		if err := rows.Scan(&v.id, &v.productID, &v.sku, &v.price, &v.productName, &v.status, &v.taxType, &v.taxRate); err != nil {
			rows.Close()
			return nil, err
		}
		byID[v.id] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, l := range lines {
		if l.variantID <= 0 {
			continue
		}
		k := fmt.Sprintf("orderItems[%d]", i)
		v, ok := byID[l.variantID]
		switch {
		case !ok:
			f[k+".variantId"] = "Product variant not found."
		case l.productID > 0 && v.productID != l.productID:
			f[k+".variantId"] = "The variant does not belong to this product."
		case v.status != "Active":
			f[k+".productId"] = fmt.Sprintf("%s is inactive and cannot be sold.", v.productName)
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

// ------------------------------------------------------------------ stock

type stockKey struct{ store, variant int64 }

// StockShortage is one entry of the INSUFFICIENT_STOCK details.
type StockShortage struct {
	VariantID int64  `json:"variantId"`
	SKU       string `json:"sku"`
	Available int    `json:"available"`
	Requested int    `json:"requested"`
}

// lockStock locks every touched inventory row in (store, variant) order
// and checks that no decrement goes below zero. Deterministic lock order
// keeps concurrent checkouts and edits deadlock-free (PHASE1_DESIGN §4.4).
// It returns the keys to move, in lock order.
func lockStock(ctx context.Context, tx tenancy.TenantTx, deltas map[stockKey]int, skus map[int64]string) ([]stockKey, error) {
	keys := make([]stockKey, 0, len(deltas))
	for k, d := range deltas {
		if d != 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].store != keys[b].store {
			return keys[a].store < keys[b].store
		}
		return keys[a].variant < keys[b].variant
	})
	have := map[stockKey]int{}
	for i := 0; i < len(keys); {
		j := i
		var vids []int64
		for ; j < len(keys) && keys[j].store == keys[i].store; j++ {
			vids = append(vids, keys[j].variant)
		}
		q, err := inventory.LockRows(ctx, tx, keys[i].store, vids)
		if err != nil {
			return nil, err
		}
		for v, n := range q {
			have[stockKey{keys[i].store, v}] = n
		}
		i = j
	}
	var short []StockShortage
	for _, k := range keys {
		if d := deltas[k]; d < 0 && have[k]+d < 0 {
			short = append(short, StockShortage{VariantID: k.variant, SKU: skus[k.variant], Available: have[k], Requested: -d})
		}
	}
	if len(short) > 0 {
		msg := fmt.Sprintf("Insufficient stock for %s. Available: %d, requested: %d.", short[0].SKU, short[0].Available, short[0].Requested)
		return nil, apperr.Conflict(apperr.CodeInsufficientStock, msg).WithDetails(map[string]any{"items": short})
	}
	return keys, nil
}

// moveStock applies locked deltas and writes one ledger row per key.
func moveStock(ctx context.Context, tx tenancy.TenantTx, keys []stockKey, deltas map[stockKey]int, reason string, ref inventory.Ref, userID int64) error {
	for _, k := range keys {
		if _, err := inventory.Move(ctx, tx, k.store, k.variant, deltas[k], reason, ref, userID); err != nil {
			if errors.Is(err, inventory.ErrInsufficient) {
				return apperr.Conflict(apperr.CodeInsufficientStock, "Insufficient stock.")
			}
			return err
		}
	}
	return nil
}

// applyStock is lockStock followed by moveStock.
func applyStock(ctx context.Context, tx tenancy.TenantTx, deltas map[stockKey]int, skus map[int64]string, reason string, ref inventory.Ref, userID int64) error {
	keys, err := lockStock(ctx, tx, deltas, skus)
	if err != nil {
		return err
	}
	return moveStock(ctx, tx, keys, deltas, reason, ref, userID)
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

const qInsertItem = `INSERT INTO order_items (sale_id, product_id, variant_id, product_name, sku, quantity, unit_price,
		catalog_price, discount, tax_type, tax_rate, tax_amount, cgst, sgst, igst, subtotal)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

func insertItems(ctx context.Context, tx tenancy.TenantTx, saleID int64, p priced) error {
	for _, l := range p.lines {
		if _, err := tx.Exec(ctx, qInsertItem, saleID, l.v.productID, l.v.id, l.v.productName, l.v.sku, l.qty, l.unit,
			l.catalog, l.q.Discount, l.taxType, l.taxRate, l.q.Tax, l.q.CGST, l.q.SGST, l.q.IGST, l.q.Total); err != nil {
			return err
		}
	}
	return nil
}

func skuMap(p priced) map[int64]string {
	m := map[int64]string{}
	for _, l := range p.lines {
		m[l.v.id] = l.v.sku
	}
	return m
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
