package sales

import (
	"net/http"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// quoteLine is one priced cart line, in the order sent.
type quoteLine struct {
	Index     int              `json:"index"`
	ProductID int64            `json:"productId"`
	VariantID int64            `json:"variantId"`
	SKU       string           `json:"sku"`
	Quantity  int64            `json:"quantity"`
	UnitPrice decimal.Decimal  `json:"unitPrice"`
	Discount  decimal.Decimal  `json:"discount"` // share of the cart discount
	TaxRate   decimal.Decimal  `json:"taxRate"`
	Tax       decimal.Decimal  `json:"tax"`
	Total     decimal.Decimal  `json:"total"`
	CGST      *decimal.Decimal `json:"cgst"`
	SGST      *decimal.Decimal `json:"sgst"`
	IGST      *decimal.Decimal `json:"igst"`
	Available int64            `json:"available"` // units in stock at the store now
}

type quoteJSON struct {
	Currency string          `json:"currency"`
	Subtotal decimal.Decimal `json:"subtotal"`
	Discount decimal.Decimal `json:"discount"`
	Tax      decimal.Decimal `json:"tax"`
	Shipping decimal.Decimal `json:"shipping"`
	Total    decimal.Decimal `json:"total"`
	Lines    []quoteLine     `json:"lines"`
}

const qQuoteStock = `SELECT variant_id, qty FROM inventories WHERE store_id = $1 AND variant_id = ANY($2)`

// quote is POST /api/sales/quote (UI S6): the checkout's own pricing for a
// cart, without saving, locking or reserving anything. The body is the
// POST /api/sales body; the same checks apply (store, customer, inactive
// products, changed prices, discount permission and cap), so the total
// shown before PAY is the total the sale will have. Stock is reported, not
// checked: the sale itself still refuses a line without stock.
func (a *API) quote(w http.ResponseWriter, r *http.Request) {
	var in saleIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var out quoteJSON
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := checkParties(ctx, tx, c, s); err != nil {
			return err
		}
		byID, err := resolve(ctx, tx, c.lines)
		if err != nil {
			return err
		}
		pr, err := price(c, byID, s, p, nil)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(pr.lines))
		for _, l := range pr.lines {
			ids = append(ids, l.v.id)
		}
		stock := map[int64]int64{}
		rows, err := tx.Query(ctx, qQuoteStock, c.storeID, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, n int64
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			stock[id] = n
		}
		if err := rows.Err(); err != nil {
			return err
		}
		q := pr.quote
		out = quoteJSON{Currency: s.currency, Subtotal: q.Subtotal, Discount: q.Discount, Tax: q.Tax,
			Shipping: q.Shipping, Total: q.Total, Lines: make([]quoteLine, 0, len(pr.lines))}
		for i, l := range pr.lines {
			out.Lines = append(out.Lines, quoteLine{Index: i, ProductID: l.v.productID, VariantID: l.v.id, SKU: l.v.sku,
				Quantity: l.qty, UnitPrice: l.unit, Discount: l.q.Discount, TaxRate: l.taxRate, Tax: l.q.Tax,
				Total: l.q.Total, CGST: l.q.CGST, SGST: l.q.SGST, IGST: l.q.IGST, Available: stock[l.v.id]})
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
