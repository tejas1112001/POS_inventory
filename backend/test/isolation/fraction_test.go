//go:build integration

package isolation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stockQ is a variant's stock in a store, as the database keeps it
// (three decimals, ARCHITECTURE_REVIEW H3).
func stockQ(t *testing.T, l string, store, vid float64) string {
	t.Helper()
	var q string
	require.NoError(t, admin(t).QueryRow(context.Background(),
		`SELECT coalesce((SELECT qty FROM `+schema(l)+`.inventories WHERE store_id = $1 AND variant_id = $2), 0)::text`,
		int64(store), int64(vid)).Scan(&q))
	return q
}

func qline(p stocked, q any) map[string]any {
	return map[string]any{"productId": p.pid, "variantId": p.vid, "quantity": q, "unitPrice": 120.5}
}

// H3: a product sold in part units (allowFraction) takes quantities with
// up to three decimals through product stock, sales, returns, adjustments
// and purchases; a whole-unit product refuses them everywhere; fractions
// cannot be turned off while stock is in part units; the ledger agrees.
func TestFractionalQuantities(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]

	// A whole-unit product refuses part units in its stock.
	body := addFormSingle("FRAC-WHOLE", store, 0)
	body["variants"].([]map[string]any)[0]["inventories"] = []map[string]any{{"storeId": store, "qty": "2.5", "quantityAlert": 1}}
	r := createProduct(t, "a", tok, body)
	require.Equal(t, 400, r.Code, r.Raw)

	// Loose rice by the kilo: 10.5 kg in stock, low at 1.25 kg.
	body = addFormSingle("FRAC-RICE", store, 0)
	body["allowFraction"] = true
	body["variants"].([]map[string]any)[0]["inventories"] = []map[string]any{{"storeId": store, "qty": "10.5", "quantityAlert": 1.25}}
	r = createProduct(t, "a", tok, body)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, true, r.Body["allowFraction"])
	require.Equal(t, 10.5, num(r.Body["qty"]))
	v := r.Body["ProductVariants"].([]any)[0].(map[string]any)
	rice := stocked{pid: r.Body["id"].(float64), vid: v["id"].(float64), sku: "FRAC-RICE"}
	require.Equal(t, 1.25, num(v["Inventories"].([]any)[0].(map[string]any)["quantityAlert"]))
	require.Equal(t, "10.500", stockQ(t, "a", store, rice.vid))

	// More than three decimals is never a quantity.
	s := sell(t, "a", tok, posSale(store, qline(rice, 1.2345)), nil)
	require.Equal(t, 400, s.Code, s.Raw)
	require.Equal(t, "orderItems[0].quantity", errField(s))

	// Sell 1.25 kg: priced by weight, stock goes down by the weight.
	s = sell(t, "a", tok, posSale(store, qline(rice, 1.25)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	ln := lineOf(t, s.Body, rice)
	require.Equal(t, 1.25, num(ln["quantity"]))
	require.Equal(t, "177.74", dec(ln["subtotal"]).StringFixed(2), "120.50 x 1.25 = 150.63, plus 18 % tax")
	require.Equal(t, "9.250", stockQ(t, "a", store, rice.vid))

	// Return 0.25 kg of it: refunded pro rata, back in stock.
	ret := postReturn(t, "a", tok, retBody(s.Body["id"], store, map[string]any{"productId": rice.pid, "variantId": rice.vid,
		"quantity": "0.25", "reason": "spilt"}), nil)
	require.Equal(t, 201, ret.Code, ret.Raw)
	require.Equal(t, "9.500", stockQ(t, "a", store, rice.vid))
	items := ret.Body["items"].([]any)
	require.Equal(t, 0.25, num(items[0].(map[string]any)["quantity"]))
	require.Equal(t, "35.55", dec(items[0].(map[string]any)["subtotal"]).StringFixed(2), "a fifth of 177.74, rounded")

	// Adjust down by 0.5 kg.
	a := postAdjustment(t, "a", tok, adjust(rice.vid, store, "Subtraction", 0.5), nil)
	require.Equal(t, 201, a.Code, a.Raw)
	require.Equal(t, 9.0, num(a.Body["qtyAfter"]))

	// Buy 2.75 kg.
	sup := createSupplier(t, "a", tok, "Fraction Supplier")
	pl := purchaseLine(rice.pid, rice.vid, store, 0, 40)
	pl["quantity"] = 2.75
	pu := postPurchase(t, "a", tok, purchaseBody(sup, "FRAC-PO-1", "Received", pl), nil)
	require.Equal(t, 201, pu.Code, pu.Raw)
	require.Equal(t, "11.750", stockQ(t, "a", store, rice.vid))

	// The ledger sums to the stock.
	require.Empty(t, reconcile(t, "a"))

	// Fractions cannot be turned off while stock is in part units...
	edit := addFormSingle("FRAC-RICE", store, 0)
	edit["allowFraction"] = false
	edit["variants"].([]map[string]any)[0]["id"] = rice.vid
	edit["variants"].([]map[string]any)[0]["inventories"] = []map[string]any{{"storeId": store, "qty": "11.75", "quantityAlert": 1}}
	up := do(t, rq{method: "PUT", host: host, path: "/api/products/" + jsonID(rice.pid), token: tok, body: edit})
	require.Equal(t, 409, up.Code, up.Raw)
	// ...and can once it is whole again.
	edit["variants"].([]map[string]any)[0]["inventories"] = []map[string]any{{"storeId": store, "qty": "12", "quantityAlert": 1}}
	up = do(t, rq{method: "PUT", host: host, path: "/api/products/" + jsonID(rice.pid), token: tok, body: edit})
	require.Equal(t, 200, up.Code, up.Raw)
	require.Equal(t, false, up.Body["allowFraction"])
	require.Equal(t, "12.000", stockQ(t, "a", store, rice.vid))

	// Now whole units only, everywhere.
	s = sell(t, "a", tok, posSale(store, qline(rice, 0.5)), nil)
	require.Equal(t, 400, s.Code, s.Raw)
	require.Equal(t, "orderItems[0].quantity", errField(s))
	require.Equal(t, 400, postAdjustment(t, "a", tok, adjust(rice.vid, store, "Addition", 0.5), nil).Code)
	pl["quantity"] = 0.5
	require.Equal(t, 400, postPurchase(t, "a", tok, purchaseBody(sup, "FRAC-PO-2", "Received", pl), nil).Code)
	require.Equal(t, "12.000", stockQ(t, "a", store, rice.vid))
	require.Empty(t, reconcile(t, "a"))
}

// H3: a product sold by the piece and by the box of 12. Stock stays in
// pieces; a box line moves 12 pieces each and keeps the box on the line.
func TestProductUnits(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	unit := func(name, short string) float64 {
		u := do(t, rq{method: "POST", host: host, path: "/api/units", token: tok, body: map[string]any{"name": name, "short_name": short}})
		require.Equal(t, 201, u.Code, u.Raw)
		return num(u.Body["id"])
	}
	piece, box, half := unit("Piece H3", "pc"), unit("Box of 12 H3", "box"), unit("Half piece H3", "half")

	// Units need a base unit; a whole-unit product needs whole factors.
	body := addFormSingle("UNIT-PEN", store, 100)
	body["units"] = []map[string]any{{"unitId": box, "factor": 12}}
	r := createProduct(t, "a", tok, body)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "unitId", errField(r))
	body["unitId"] = piece
	body["units"] = []map[string]any{{"unitId": half, "factor": 0.5}}
	r = createProduct(t, "a", tok, body)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "units[0].factor", errField(r))
	body["units"] = []map[string]any{{"unitId": piece, "factor": 1}}
	r = createProduct(t, "a", tok, body)
	require.Equal(t, 400, r.Code, "the base unit again: %s", r.Raw)

	body["units"] = []map[string]any{{"unitId": box, "factor": 12, "forPurchase": false}}
	r = createProduct(t, "a", tok, body)
	require.Equal(t, 201, r.Code, r.Raw)
	us := r.Body["units"].([]any)
	require.Len(t, us, 1)
	u := us[0].(map[string]any)
	require.Equal(t, box, num(u["unitId"]))
	require.Equal(t, 12.0, num(u["factor"]))
	require.Equal(t, true, u["forSale"])
	require.Equal(t, false, u["forPurchase"])
	v := r.Body["ProductVariants"].([]any)[0].(map[string]any)
	pen := stocked{pid: r.Body["id"].(float64), vid: v["id"].(float64), sku: "UNIT-PEN"}

	// Two boxes: 24 pieces, priced per piece, the box kept on the line.
	l := qline(pen, 2)
	l["unitId"] = box
	s := sell(t, "a", tok, posSale(store, l), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	ln := lineOf(t, s.Body, pen)
	require.Equal(t, 24.0, num(ln["quantity"]))
	require.Equal(t, "Box of 12 H3", ln["unitName"])
	require.Equal(t, 2.0, num(ln["unitQty"]))
	require.Equal(t, 12.0, num(ln["unitFactor"]))
	require.Equal(t, "3412.56", dec(ln["subtotal"]).StringFixed(2), "24 x 120.50 = 2892.00, plus 18 % tax")
	require.Equal(t, "76.000", stockQ(t, "a", store, pen.vid))
	saleID := jsonID(num(s.Body["id"]))

	// A unit the product is not sold in.
	l["unitId"] = half
	s = sell(t, "a", tok, posSale(store, l), nil)
	require.Equal(t, 400, s.Code, s.Raw)
	require.Equal(t, "orderItems[0].unitId", errField(s))

	// Removing the units leaves old bills as they were.
	edit := addFormSingle("UNIT-PEN", store, 76)
	edit["unitId"], edit["units"] = piece, []map[string]any{}
	edit["variants"].([]map[string]any)[0]["id"] = pen.vid
	up := do(t, rq{method: "PUT", host: host, path: "/api/products/" + jsonID(pen.pid), token: tok, body: edit})
	require.Equal(t, 200, up.Code, up.Raw)
	require.Empty(t, up.Body["units"])
	sale := do(t, rq{method: "GET", host: host, path: "/api/sales/" + saleID, token: tok})
	require.Equal(t, 200, sale.Code, sale.Raw)
	ln = lineOf(t, sale.Body, pen)
	require.Equal(t, "Box of 12 H3", ln["unitName"])
	require.Equal(t, 24.0, num(ln["quantity"]))
	require.Empty(t, reconcile(t, "a"))
}

// H3: buying by the carton of 24. The quantity is in cartons, the price per
// piece; stock, received_qty and goods receipts stay in pieces, and the
// carton is kept on the line.
func TestPurchaseUnits(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	unit := func(name, short string) float64 {
		u := do(t, rq{method: "POST", host: host, path: "/api/units", token: tok, body: map[string]any{"name": name, "short_name": short}})
		require.Equal(t, 201, u.Code, u.Raw)
		return num(u.Body["id"])
	}
	piece, carton, pack, other := unit("Piece H3P", "pc"), unit("Carton of 24 H3P", "ctn"), unit("Display pack H3P", "pk"), unit("Unused H3P", "un")

	body := addFormSingle("UNIT-SOAP", store, 0)
	body["unitId"] = piece
	body["units"] = []map[string]any{{"unitId": carton, "factor": 24}, {"unitId": pack, "factor": 6, "forPurchase": false}}
	r := createProduct(t, "a", tok, body)
	require.Equal(t, 201, r.Code, r.Raw)
	v := r.Body["ProductVariants"].([]any)[0].(map[string]any)
	soap := stocked{pid: r.Body["id"].(float64), vid: v["id"].(float64), sku: "UNIT-SOAP"}
	sup := createSupplier(t, "a", tok, "Units Supplier")
	line := func(q any, u float64) map[string]any {
		l := purchaseLine(soap.pid, soap.vid, store, 0, 10)
		l["quantity"] = q
		if u > 0 {
			l["unitId"] = jsonID(u)
		}
		return l
	}

	// Two cartons, received: 48 pieces at 10 a piece.
	pu := postPurchase(t, "a", tok, purchaseBody(sup, "UNIT-PO-1", "Received", line(2, carton)), nil)
	require.Equal(t, 201, pu.Code, pu.Raw)
	ln := lineOf(t, pu.Body, soap)
	require.Equal(t, 48.0, num(ln["quantity"]))
	require.Equal(t, 48.0, num(ln["received_qty"]))
	require.Equal(t, "Carton of 24 H3P", ln["unitName"])
	require.Equal(t, 24.0, num(ln["unitFactor"]))
	require.Equal(t, 2.0, num(ln["unitQty"]))
	require.Equal(t, "10", dec(ln["unitPrice"]).String(), "the price stays per piece")
	require.Equal(t, "480.00", dec(ln["subtotal"]).StringFixed(2))
	require.Equal(t, "480.00", dec(pu.Body["total"]).StringFixed(2))
	require.Equal(t, "48.000", stockQ(t, "a", store, soap.vid))
	poID := jsonID(num(pu.Body["id"]))

	// A base-unit line has no unit copy.
	pu = postPurchase(t, "a", tok, purchaseBody(sup, "UNIT-PO-2", "Received", line(3, 0)), nil)
	require.Equal(t, 201, pu.Code, pu.Raw)
	ln = lineOf(t, pu.Body, soap)
	require.Nil(t, ln["unitName"])
	require.Nil(t, ln["unitQty"])
	require.Equal(t, "51.000", stockQ(t, "a", store, soap.vid))

	// Units the product is not bought in: sale only, or not the product's.
	for _, u := range []float64{pack, other} {
		pu = postPurchase(t, "a", tok, purchaseBody(sup, "UNIT-PO-X", "Received", line(1, u)), nil)
		require.Equal(t, 400, pu.Code, pu.Raw)
		require.Equal(t, "purchaseItems[0].unitId", errField(pu))
	}
	// The whole-unit check is on pieces: 1.5 cartons is 36 pieces, 0.01 is not whole.
	pu = postPurchase(t, "a", tok, purchaseBody(sup, "UNIT-PO-X", "Received", line(0.01, carton)), nil)
	require.Equal(t, 400, pu.Code, pu.Raw)
	require.Equal(t, "purchaseItems[0].quantity", errField(pu))
	require.Equal(t, "51.000", stockQ(t, "a", store, soap.vid))

	// Ordered in cartons, received in pieces as the goods arrive.
	pu = postPurchase(t, "a", tok, purchaseBody(sup, "UNIT-PO-3", "Ordered", line(1.5, carton)), nil)
	require.Equal(t, 201, pu.Code, pu.Raw)
	ln = lineOf(t, pu.Body, soap)
	require.Equal(t, 36.0, num(ln["quantity"]))
	require.Equal(t, 1.5, num(ln["unitQty"]))
	rc := postReceipt(t, "a", tok, pu.Body["id"], map[string]any{"lines": []any{rline(ln["id"], 30)}}, nil)
	require.Equal(t, 201, rc.Code, rc.Raw)
	require.Equal(t, "81.000", stockQ(t, "a", store, soap.vid))
	rc = postReceipt(t, "a", tok, pu.Body["id"], map[string]any{"lines": []any{rline(ln["id"], 7)}}, nil)
	require.Equal(t, 400, rc.Code, "only 6 pieces are still to come: %s", rc.Raw)

	// Removing the units leaves old purchases as they were.
	edit := addFormSingle("UNIT-SOAP", store, 81)
	edit["unitId"], edit["units"] = piece, []map[string]any{}
	edit["variants"].([]map[string]any)[0]["id"] = soap.vid
	up := do(t, rq{method: "PUT", host: host, path: "/api/products/" + jsonID(soap.pid), token: tok, body: edit})
	require.Equal(t, 200, up.Code, up.Raw)
	got := do(t, rq{method: "GET", host: host, path: "/api/purchases/" + poID, token: tok})
	require.Equal(t, 200, got.Code, got.Raw)
	ln = lineOf(t, got.Body, soap)
	require.Equal(t, "Carton of 24 H3P", ln["unitName"])
	require.Equal(t, 48.0, num(ln["quantity"]))
	require.Empty(t, reconcile(t, "a"))
}
