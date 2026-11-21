package sales

import (
	"errors"
	"net/http"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// quoteLine is one priced cart line, in the order sent.
type quoteLine struct {
	Index     int             `json:"index"`
	ProductID int64           `json:"productId"`
	VariantID int64           `json:"variantId"`
	SKU       string          `json:"sku"`
	Quantity  qty.Q           `json:"quantity"` // base units
	UnitPrice decimal.Decimal `json:"unitPrice"`
	Discount  decimal.Decimal `json:"discount"` // share of the cart discount
	// The unit the line is sold in (H3, the line's unitId), when not the
	// base unit: its name and size now, and the quantity in it.
	UnitID     *int64           `json:"unitId"`
	UnitName   *string          `json:"unitName"`
	UnitFactor *qty.Q           `json:"unitFactor"`
	UnitQty    *qty.Q           `json:"unitQty"`
	TaxRate    decimal.Decimal  `json:"taxRate"`
	Tax        decimal.Decimal  `json:"tax"`
	Total      decimal.Decimal  `json:"total"`
	CGST       *decimal.Decimal `json:"cgst"`
	SGST       *decimal.Decimal `json:"sgst"`
	IGST       *decimal.Decimal `json:"igst"`
	Available  qty.Q            `json:"available"` // units in stock at the store now
	// Industry P3 item 3 (A27): the batches the sale would take now, and
	// the line's warnings. Empty for untracked products.
	Batches  []ItemBatchJSON `json:"batches"`
	Warnings []string        `json:"warnings"`
	// MRP of one unit (null when unknown or the batches differ) and of the
	// line; AboveMRP: the line total is above it (a warning only).
	MRP       *decimal.Decimal `json:"mrp"`
	MRPAmount *decimal.Decimal `json:"mrpAmount"`
	AboveMRP  bool             `json:"aboveMrp"`
}

type quoteJSON struct {
	Currency string          `json:"currency"`
	Subtotal decimal.Decimal `json:"subtotal"`
	Discount decimal.Decimal `json:"discount"`
	Tax      decimal.Decimal `json:"tax"`
	Shipping decimal.Decimal `json:"shipping"`
	Total    decimal.Decimal `json:"total"`
	Lines    []quoteLine     `json:"lines"`
	// BatchProblem is the error the sale would get from its batches now
	// (BATCH_NOT_SELLABLE or INSUFFICIENT_STOCK), so the till shows it
	// before payment; null when the batches cover the cart.
	BatchProblem *batchProblem `json:"batchProblem"`
	// M3: the sale's GST: how its tax is split, the place of supply, the
	// seller's GSTIN, B2B or B2C, and warnings (a B2B line without an HSN
	// code; a sale the checkout will refuse until the buyer's address is
	// added).
	GST         GSTJSON  `json:"gst"`
	GSTWarnings []string `json:"gstWarnings"`
}

type batchProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
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
		cust, err := checkParties(ctx, tx, c, s)
		if err != nil {
			return err
		}
		tc, err := taxContext(ctx, tx, c.storeID, cust, c.place, s)
		if err != nil {
			return err
		}
		s.split = tc.split
		byID, err := resolve(ctx, tx, c.lines, c.storeID)
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
		stock := map[int64]qty.Q{}
		rows, err := tx.Query(ctx, qQuoteStock, c.storeID, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var n qty.Q
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			stock[id] = n
		}
		if err := rows.Err(); err != nil {
			return err
		}
		sb, err := allocateBatches(ctx, tx, c.storeID, pr, false)
		var ae *apperr.Error
		switch {
		case errors.As(err, &ae) && ae.Kind == apperr.KindConflict:
			out.BatchProblem = &batchProblem{Code: ae.Code, Message: ae.Message, Details: ae.Details}
		case err != nil:
			return err
		}
		q := pr.quote
		problem := out.BatchProblem
		out = quoteJSON{BatchProblem: problem, Currency: s.currency, Subtotal: q.Subtotal, Discount: q.Discount, Tax: q.Tax,
			Shipping: q.Shipping, Total: q.Total, Lines: make([]quoteLine, 0, len(pr.lines)), GSTWarnings: []string{}}
		out.GST = GSTJSON{SellerGSTIN: tc.sellerGSTIN, SellerState: tc.sellerState, BuyerGSTIN: tc.buyerGSTIN, BuyerName: tc.buyerName,
			BuyerAddress: tc.buyerAddress, BuyerState: tc.buyerState, PlaceOfSupply: tc.place, TaxSplit: tc.split, InvoiceKind: tc.kind}
		if msg := tc.interStateProblem(q.Total, s.currency); msg != "" {
			out.GSTWarnings = append(out.GSTWarnings, msg)
		}
		hsn, err := tc.hsnWarnings(ctx, tx, pr)
		if err != nil {
			return err
		}
		out.GSTWarnings = append(out.GSTWarnings, hsn...)
		for i, l := range pr.lines {
			ql := quoteLine{Index: i, ProductID: l.v.productID, VariantID: l.v.id, SKU: l.v.sku,
				Quantity: l.qty, UnitPrice: l.unit, Discount: l.q.Discount, TaxRate: l.taxRate, Tax: l.q.Tax,
				Total: l.q.Total, CGST: l.q.CGST, SGST: l.q.SGST, IGST: l.q.IGST, Available: stock[l.v.id],
				Batches: soldJSON(sb.sold[i]), Warnings: lineWarnings(sb.sold[i])}
			ql.UnitName, ql.UnitFactor, ql.UnitQty = l.line.unit.columns()
			ql.UnitID = l.line.unit.unitID()
			ql.MRP, ql.MRPAmount = lineMRP(l, sb.sold[i])
			if ql.AboveMRP = aboveMRP(l.q.Total, ql.MRPAmount); ql.AboveMRP {
				ql.Warnings = append(ql.Warnings, mrpWarning(l.q.Total, *ql.MRPAmount))
			}
			out.Lines = append(out.Lines, ql)
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
