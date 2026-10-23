package sales

// Sales returns (PHASE1_DESIGN step 10, API_CONTRACT C-13, PRD §3.6):
// quantities above 0; a return linked to a sale is checked cumulatively
// against each sold line under the sale's row lock; store and customer
// come from the sale; the refund is the sold amount (client prices are
// ignored); a return without a sale needs sales:price_override. Returned
// goods go back to stock at once, through the ledger.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const (
	maxReturnLines = 500
	maxReasonLen   = 500
	// returnHashPrefix keeps an Idempotency-Key used for a sale from
	// replaying as a return (both share the idempotency_keys table).
	returnHashPrefix = "POST /api/sales/returns\n"
)

// returnItemIn is one line of AddSalesReturn.jsx. orderItemId is new and
// optional: it names the sold line exactly.
type returnItemIn struct {
	OrderItemID money.Int `json:"orderItemId"`
	ProductID   money.Int `json:"productId"`
	VariantID   money.Int `json:"variantId"`
	Quantity    money.Int `json:"quantity"`
	UnitPrice   money.Dec `json:"unitPrice"`
	Reason      *string   `json:"reason"`

	// Accepted and ignored: the server computes line amounts.
	Total    json.RawMessage `json:"total"`
	Subtotal json.RawMessage `json:"subtotal"`
}

// returnIn is the POST body. Decoding is strict; every key the form sends
// is used or on the ignore list.
type returnIn struct {
	ReturnItems []returnItemIn `json:"returnItems"`
	SaleID      money.Int      `json:"saleId"`
	SaleIDAlias money.Int      `json:"sale_id"`
	CustomerID  money.Int      `json:"customerId"`
	StoreID     money.Int      `json:"storeId"`
	Date        *string        `json:"date"`
	Status      *string        `json:"status"`
	Paid        money.Dec      `json:"paid"`
	Note        *string        `json:"note"`

	// Accepted and ignored: the reference is generated and the totals,
	// due amount and payment status are computed here.
	Reference     json.RawMessage `json:"reference"`
	Total         json.RawMessage `json:"total"`
	Due           json.RawMessage `json:"due"`
	PaymentStatus json.RawMessage `json:"payment_status"`
}

type returnLine struct {
	orderItemID, productID, variantID int64
	qty                               int64
	price                             decimal.Decimal
	priceSet                          bool
	reason                            string
}

type cleanReturn struct {
	lines                       []returnLine
	saleID, customerID, storeID int64
	status, date                string
	paid                        *decimal.Decimal
	note                        string
}

func validateReturn(in returnIn) (cleanReturn, error) {
	f := fieldErrs{}
	c := cleanReturn{status: "Pending"}
	sale := in.SaleID
	if !sale.Valid {
		sale = in.SaleIDAlias
	}
	if sale.Valid {
		if sale.V <= 0 {
			f["saleId"] = "saleId is invalid."
		}
		c.saleID = sale.V
	}
	switch {
	case in.StoreID.Valid && in.StoreID.V <= 0:
		f["storeId"] = "storeId is invalid."
	case !in.StoreID.Valid && c.saleID == 0:
		f["storeId"] = "storeId is required."
	}
	c.storeID = in.StoreID.V
	if in.CustomerID.Valid && in.CustomerID.V <= 0 {
		f["customerId"] = "customerId is invalid."
	}
	c.customerID = in.CustomerID.V
	switch {
	case len(in.ReturnItems) == 0:
		f["returnItems"] = "At least one return item is required."
	case len(in.ReturnItems) > maxReturnLines:
		f["returnItems"] = fmt.Sprintf("A return can have at most %d lines.", maxReturnLines)
	}
	for i, it := range in.ReturnItems {
		k := fmt.Sprintf("returnItems[%d]", i)
		l := returnLine{orderItemID: it.OrderItemID.V, productID: it.ProductID.V, variantID: it.VariantID.V, qty: it.Quantity.V}
		if !it.Quantity.Valid || it.Quantity.V <= 0 || it.Quantity.V > maxQty {
			f[k+".quantity"] = "Quantity must be a whole number above 0."
		}
		for name, id := range map[string]money.Int{"orderItemId": it.OrderItemID, "productId": it.ProductID, "variantId": it.VariantID} {
			if id.Valid && id.V <= 0 {
				f[k+"."+name] = name + " is invalid."
			}
		}
		if l.orderItemID <= 0 && l.variantID <= 0 && l.productID <= 0 {
			f[k+".productId"] = "productId is required."
		}
		if l.orderItemID > 0 && c.saleID == 0 {
			f[k+".orderItemId"] = "orderItemId needs a saleId."
		}
		if it.UnitPrice.Valid {
			if it.UnitPrice.D.IsNegative() {
				f[k+".unitPrice"] = "Unit price cannot be negative."
			}
			l.price, l.priceSet = it.UnitPrice.D.Round(4), true
		}
		if it.Reason != nil {
			l.reason = strings.TrimSpace(*it.Reason)
			if len([]rune(l.reason)) > maxReasonLen {
				f[k+".reason"] = fmt.Sprintf("At most %d characters.", maxReasonLen)
			}
		}
		c.lines = append(c.lines, l)
	}
	if in.Status != nil {
		switch s := strings.TrimSpace(*in.Status); s {
		case "":
		case "Pending", "Completed":
			c.status = s
		default:
			f["status"] = "Status must be Pending or Completed."
		}
	}
	if in.Date != nil {
		c.date = strings.TrimSpace(*in.Date)
	}
	if in.Paid.Valid {
		if in.Paid.D.IsNegative() {
			f["paid"] = "Must not be negative."
		}
		v := in.Paid.D
		c.paid = &v
	}
	if in.Note != nil {
		c.note = strings.TrimSpace(*in.Note)
		if len([]rune(c.note)) > maxNoteLen {
			f["note"] = fmt.Sprintf("At most %d characters.", maxNoteLen)
		}
	}
	return c, f.err()
}

// ------------------------------------------------------------ sold lines

// soldLine is an order line of the sale being returned. returned counts
// earlier returns plus what this request has taken so far.
type soldLine struct {
	id                            int64
	productID, variantID          *int64
	name                          string
	sku                           *string
	qty, returned                 int64
	unit, discount, tax, subtotal decimal.Decimal
	cgst, sgst, igst              *decimal.Decimal
}

const qSoldLines = `SELECT i.id, i.product_id, i.variant_id, i.product_name, i.sku, i.quantity, i.unit_price, i.discount,
		i.tax_amount, i.cgst, i.sgst, i.igst, i.subtotal,
		coalesce((SELECT sum(r.quantity) FROM return_items r WHERE r.order_item_id = i.id), 0)
	FROM order_items i WHERE i.sale_id = $1 ORDER BY i.id`

func soldLines(ctx context.Context, tx tenancy.TenantTx, saleID int64) ([]soldLine, error) {
	rows, err := tx.Query(ctx, qSoldLines, saleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []soldLine
	for rows.Next() {
		var s soldLine
		if err := rows.Scan(&s.id, &s.productID, &s.variantID, &s.name, &s.sku, &s.qty, &s.unit, &s.discount,
			&s.tax, &s.cgst, &s.sgst, &s.igst, &s.subtotal, &s.returned); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// returnRow is one return_items row to insert.
type returnRow struct {
	orderItemID, productID        *int64
	variantID                     int64
	name                          string
	sku                           *string
	qty                           int64
	unit, discount, tax, subtotal decimal.Decimal
	cgst, sgst, igst              *decimal.Decimal
	reason                        string
}

// ReturnExcess is one entry of the RETURN_EXCEEDS_SOLD details.
type ReturnExcess struct {
	Index     int    `json:"index"`
	ProductID int64  `json:"productId"`
	VariantID int64  `json:"variantId"`
	SKU       string `json:"sku"`
	Sold      int64  `json:"sold"`
	Returned  int64  `json:"returned"`
	Requested int64  `json:"requested"`
}

func (s *soldLine) matches(l returnLine) bool {
	if s.variantID == nil { // cannot go back to stock
		return false
	}
	switch {
	case l.orderItemID > 0:
		return s.id == l.orderItemID
	case l.variantID > 0:
		return *s.variantID == l.variantID && (l.productID == 0 || (s.productID != nil && *s.productID == l.productID))
	default:
		return s.productID != nil && *s.productID == l.productID
	}
}

// matchSold turns linked return lines into rows. Each line takes from the
// sold lines it matches, in order, never more than is left on them. The
// refund shares are cut on the running quantity (refundShare).
func matchSold(c cleanReturn, sold []soldLine, currency string) ([]returnRow, error) {
	f := fieldErrs{}
	var excess []ReturnExcess
	var out []returnRow
	for i, l := range c.lines {
		k := fmt.Sprintf("returnItems[%d]", i)
		var cand []*soldLine
		variants := map[int64]bool{}
		for j := range sold {
			if s := &sold[j]; s.matches(l) {
				cand = append(cand, s)
				variants[*s.variantID] = true
			}
		}
		switch {
		case len(cand) == 0 && l.orderItemID > 0:
			f[k+".orderItemId"] = "Not a line of this sale."
			continue
		case len(cand) == 0:
			f[k+".productId"] = "This product is not on the sale."
			continue
		case l.orderItemID > 0 && ((l.variantID > 0 && *cand[0].variantID != l.variantID) ||
			(l.productID > 0 && (cand[0].productID == nil || *cand[0].productID != l.productID))):
			f[k+".orderItemId"] = "The sold line is a different product."
			continue
		case len(variants) > 1:
			f[k+".variantId"] = "Choose a variant: the sale has more than one variant of this product."
			continue
		}
		var soldQ, returned int64
		for _, s := range cand {
			soldQ += s.qty
			returned += s.returned
		}
		if l.qty > soldQ-returned {
			sku := ""
			if cand[0].sku != nil {
				sku = *cand[0].sku
			}
			var pid int64
			if cand[0].productID != nil {
				pid = *cand[0].productID
			}
			excess = append(excess, ReturnExcess{Index: i, ProductID: pid, VariantID: *cand[0].variantID, SKU: sku,
				Sold: soldQ, Returned: returned, Requested: l.qty})
			continue
		}
		left := l.qty
		for _, s := range cand {
			take := min(left, s.qty-s.returned)
			if take <= 0 {
				continue
			}
			out = append(out, returnRow{orderItemID: &s.id, productID: s.productID, variantID: *s.variantID, name: s.name,
				sku: s.sku, qty: take, unit: s.unit,
				discount: refundShare(s.discount, s.qty, s.returned, take, currency),
				tax:      refundShare(s.tax, s.qty, s.returned, take, currency),
				subtotal: refundShare(s.subtotal, s.qty, s.returned, take, currency),
				cgst:     refundSharePtr(s.cgst, s.qty, s.returned, take, currency),
				sgst:     refundSharePtr(s.sgst, s.qty, s.returned, take, currency),
				igst:     refundSharePtr(s.igst, s.qty, s.returned, take, currency),
				reason:   l.reason})
			s.returned += take
			if left -= take; left == 0 {
				break
			}
		}
	}
	if err := f.err(); err != nil {
		return nil, err
	}
	if len(excess) > 0 {
		e := excess[0]
		msg := fmt.Sprintf("Cannot return %d of %s: %d sold, %d already returned.", e.Requested, e.SKU, e.Sold, e.Returned)
		return nil, apperr.Conflict(apperr.CodeReturnExceedsSold, msg).WithDetails(map[string]any{"items": excess})
	}
	return out, nil
}

// ------------------------------------------------------- unlinked returns

const qReturnVariants = `SELECT v.id, v.product_id, v.sku::text, p.name FROM product_variants v JOIN products p ON p.id = v.product_id
	WHERE v.id = ANY($1) AND v.deleted_at IS NULL AND p.deleted_at IS NULL`

// unlinkedRows prices a return without a sale from the client's unit
// prices (the caller holds sales:price_override). No tax is split out.
func unlinkedRows(ctx context.Context, tx tenancy.TenantTx, c cleanReturn, currency string) ([]returnRow, error) {
	f := fieldErrs{}
	var productOnly []int64
	for i, l := range c.lines {
		if !l.priceSet {
			f[fmt.Sprintf("returnItems[%d].unitPrice", i)] = "unitPrice is required for a return without a sale."
		}
		if l.variantID <= 0 {
			productOnly = append(productOnly, l.productID)
		}
	}
	if err := f.err(); err != nil {
		return nil, err
	}
	only := map[int64]int64{}
	if len(productOnly) > 0 {
		rows, err := tx.Query(ctx, qSingleVariant, productOnly)
		if err != nil {
			return nil, err
		}
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
	}
	ids := make([]int64, len(c.lines))
	for i, l := range c.lines {
		ids[i] = l.variantID
		if l.variantID > 0 {
			continue
		}
		switch v := only[l.productID]; {
		case v == 0:
			f[fmt.Sprintf("returnItems[%d].productId", i)] = "Product not found."
		case v < 0:
			f[fmt.Sprintf("returnItems[%d].variantId", i)] = "Choose a variant for this product."
		default:
			ids[i] = v
		}
	}
	if err := f.err(); err != nil {
		return nil, err
	}
	type variant struct {
		productID int64
		sku, name string
	}
	byID := map[int64]variant{}
	rows, err := tx.Query(ctx, qReturnVariants, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var v variant
		if err := rows.Scan(&id, &v.productID, &v.sku, &v.name); err != nil {
			rows.Close()
			return nil, err
		}
		byID[id] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]returnRow, 0, len(c.lines))
	for i, l := range c.lines {
		v, ok := byID[ids[i]]
		switch {
		case !ok:
			f[fmt.Sprintf("returnItems[%d].variantId", i)] = "Product variant not found."
			continue
		case l.productID > 0 && v.productID != l.productID:
			f[fmt.Sprintf("returnItems[%d].variantId", i)] = "The variant does not belong to this product."
			continue
		}
		pid, sku := v.productID, v.sku
		out = append(out, returnRow{productID: &pid, variantID: ids[i], name: v.name, sku: &sku, qty: l.qty, unit: l.price,
			subtotal: money.Round(l.price.Mul(decimal.NewFromInt(l.qty)), currency), reason: l.reason})
	}
	return out, f.err()
}

// ----------------------------------------------------------------- create

type returnSale struct {
	userID, storeID, customerID int64
	status, currency, reference string
}

// lockReturnSale locks the sale a return refers to. Every return of a sale
// takes this lock first, so the cumulative check sees all earlier returns;
// sale edit and void take it too. A sale the principal may not see is
// reported as not found (C-10).
func lockReturnSale(ctx context.Context, tx tenancy.TenantTx, id int64, p auth.Principal) (returnSale, error) {
	var s returnSale
	err := tx.QueryRow(ctx, `SELECT user_id, store_id, customer_id, status, currency, reference FROM sales WHERE id = $1 FOR UPDATE`, id).
		Scan(&s.userID, &s.storeID, &s.customerID, &s.status, &s.currency, &s.reference)
	if isNoRows(err) {
		return s, apperr.Invalid("saleId", "Sale not found.")
	}
	if err != nil {
		return s, err
	}
	if scope := ownerScope(p); scope != 0 && s.userID != scope {
		return s, apperr.Invalid("saleId", "Sale not found.")
	}
	if s.status == "Void" {
		return s, apperr.Conflict(apperr.CodeInvalidState, "A voided sale cannot be returned.")
	}
	return s, nil
}

// nextReturnReference takes the next gapless return number (RET-000001).
// Call it after the inventory locks, as for sales.
func nextReturnReference(ctx context.Context, tx tenancy.TenantTx) (string, error) {
	var n int64
	if err := tx.QueryRow(ctx, `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'sale_return' RETURNING next_value - 1`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("RET-%06d", n), nil
}

const (
	qInsertReturn = `INSERT INTO sale_returns (reference, date, status, payment_status, total, tax, paid, due, currency, note,
			sale_id, customer_id, store_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14)
		RETURNING id`
	qInsertReturnItem = `INSERT INTO return_items (sale_return_id, order_item_id, product_id, variant_id, product_name, sku, quantity,
			unit_price, discount, tax_amount, cgst, sgst, igst, subtotal, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NULLIF($15, ''))`
)

// createReturn is POST /api/sales/returns.
func (a *API) createReturn(w http.ResponseWriter, r *http.Request) {
	key, err := idempotencyKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h := sha256.New()
	_, _ = io.WriteString(h, returnHashPrefix)
	r.Body = hashingBody{io.TeeReader(r.Body, h), r.Body}
	var in returnIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := h.Sum(nil)
	c, err := validateReturn(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	if c.saleID == 0 && !p.Has("sales:price_override") {
		httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "A return without a sale needs the price override permission."))
		return
	}
	now := a.d.Now()
	var (
		out    ReturnJSON
		stored []byte
		again  *replay
	)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" {
			var err error
			if again, err = claimKey(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		date, err := parseDate(c.date, s.loc, now)
		if err != nil {
			return err
		}
		var (
			rows   []returnRow
			saleID *int64
		)
		storeID, custID, currency := c.storeID, c.customerID, s.currency
		if c.saleID > 0 {
			sale, err := lockReturnSale(ctx, tx, c.saleID, p)
			if err != nil {
				return err
			}
			f := fieldErrs{}
			if storeID != 0 && storeID != sale.storeID {
				f["storeId"] = "The store must be the sale's store."
			}
			if custID != 0 && custID != sale.customerID {
				f["customerId"] = "The customer must be the sale's customer."
			}
			if err := f.err(); err != nil {
				return err
			}
			storeID, custID, currency, saleID = sale.storeID, sale.customerID, sale.currency, &c.saleID
			sold, err := soldLines(ctx, tx, c.saleID)
			if err != nil {
				return err
			}
			if rows, err = matchSold(c, sold, currency); err != nil {
				return err
			}
		} else {
			if custID, err = checkParties(ctx, tx, cleanSale{customerID: custID, storeID: storeID}, s); err != nil {
				return err
			}
			if rows, err = unlinkedRows(ctx, tx, c, currency); err != nil {
				return err
			}
		}
		total, tax := decimal.Zero, decimal.Zero
		deltas := map[stockKey]int{}
		for _, x := range rows {
			total, tax = total.Add(x.subtotal), tax.Add(x.tax)
			deltas[stockKey{storeID, x.variantID}] += int(x.qty)
		}
		pm := pay(total, c.paid, nil, currency)
		keys, err := lockStock(ctx, tx, deltas, nil)
		if err != nil {
			return err
		}
		ref, err := nextReturnReference(ctx, tx)
		if err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(ctx, qInsertReturn, ref, date, c.status, pm.status, total, tax, pm.paid, pm.due, currency, c.note,
			saleID, custID, storeID, p.UserID).Scan(&id); err != nil {
			return err
		}
		for _, x := range rows {
			if _, err := tx.Exec(ctx, qInsertReturnItem, id, x.orderItemID, x.productID, x.variantID, x.name, x.sku, x.qty,
				x.unit, x.discount, x.tax, x.cgst, x.sgst, x.igst, x.subtotal, x.reason); err != nil {
				return err
			}
		}
		if err := moveStock(ctx, tx, keys, deltas, inventory.ReasonReturn, inventory.Ref{Type: "sale_return", ID: id}, p.UserID); err != nil {
			return err
		}
		after := map[string]any{"reference": ref, "total": total, "lines": len(rows), "store_id": storeID}
		if saleID != nil {
			after["sale_id"] = *saleID
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "sale_return.create", Entity: "sale_return",
			EntityID: strconv.FormatInt(id, 10), After: after, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		l, err := loadReturns(ctx, tx, []int64{id})
		if err != nil || len(l) == 0 {
			return orInternal(err)
		}
		out = l[0]
		if key != "" {
			if stored, err = json.Marshal(out); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, qStoreKey, p.UserID, key, http.StatusCreated, string(stored)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if again != nil {
		w.Header().Set("Idempotent-Replayed", "true")
		httpx.WriteJSON(w, again.status, json.RawMessage(again.body))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}
