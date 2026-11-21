//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Published GSTINs with a correct check character: Maharashtra (27) and
// Karnataka (29).
const (
	gstinMH = "27AAPFU0939F1ZV"
	gstinKA = "29AAGCB7383J1Z4"
)

// M3 step 1 (docs/M3_GST_DESIGN.md DB1-DB4, A1-A3, A6): the state list,
// the shop's state, and the GSTIN and state of stores, customers and
// suppliers. A GSTIN sets its own state, a GSTIN of another state is
// refused (Q6), and fields a request leaves out are kept.
func TestGSTParties(t *testing.T) {
	l := billingTenant(t, "gst-parties")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}

	// The state list.
	st := call("GET", "/api/gst/states", nil)
	require.Equal(t, 200, st.Code, st.Raw)
	require.Contains(t, st.Raw, `{"code":"27","name":"Maharashtra","ut":false}`)
	require.Contains(t, st.Raw, `{"code":"04","name":"Chandigarh","ut":true}`)

	// The shop's state; its GSTIN must be of that state.
	r := call("PUT", "/api/settings", map[string]any{"stateCode": "7"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "stateCode", errField(r))
	r = call("PUT", "/api/settings", map[string]any{"taxLabel": "GST", "taxRegistrationNo": gstinKA, "stateCode": "27"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "taxRegistrationNo", errField(r))
	r = call("PUT", "/api/settings", map[string]any{"taxLabel": "GST", "taxRegistrationNo": gstinMH, "stateCode": "27"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "27", r.Body["stateCode"])
	r = call("PUT", "/api/settings", map[string]any{"stateCode": ""})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Nil(t, r.Body["stateCode"], `"" clears it`)

	// A store in Karnataka: the state comes from its GSTIN.
	s := call("POST", "/api/stores", map[string]any{"name": "Bengaluru branch", "gstin": " 29aagcb7383j1z4 ", "invoiceCode": "blr"})
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, gstinKA, s.Body["gstin"])
	require.Equal(t, "29", s.Body["stateCode"])
	require.Equal(t, "BLR", s.Body["invoiceCode"])
	blr := jsonID(num(s.Body["id"]))

	// Refused: another store's invoice code, a GSTIN of another state, a
	// bad check character, a bad code.
	main := jsonID(storeIDs(t, l, tok)[0])
	for _, c := range []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"invoiceCode": "BLR"}, "invoiceCode"},
		{map[string]any{"gstin": gstinKA, "stateCode": "27"}, "gstin"},
		{map[string]any{"gstin": "27AAPFU0939F1ZW"}, "gstin"},
		{map[string]any{"invoiceCode": "TOO-LONG"}, "invoiceCode"},
		{map[string]any{"stateCode": "00"}, "stateCode"},
	} {
		r = call("PUT", "/api/stores/"+main, c.body)
		require.Equal(t, 400, r.Code, "%v: %s", c.body, r.Raw)
		require.Equal(t, c.field, errField(r), "%v", c.body)
	}

	// Fields left out are kept; "" clears the GSTIN and keeps the state.
	r = call("PUT", "/api/stores/"+blr, map[string]any{"name": "Bengaluru MG Road"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, gstinKA, r.Body["gstin"])
	require.Equal(t, "BLR", r.Body["invoiceCode"])
	r = call("PUT", "/api/stores/"+blr, map[string]any{"gstin": ""})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Nil(t, r.Body["gstin"])
	require.Equal(t, "29", r.Body["stateCode"])

	// A registered buyer from Karnataka, then without the GSTIN; an
	// unregistered buyer with only a state.
	cu := call("POST", "/api/customers", map[string]any{"name": "Kaveri Traders", "gstin": gstinKA,
		"legalName": "Kaveri Traders Pvt Ltd", "billingAddress": "4 Residency Road, Bengaluru"})
	require.Equal(t, 201, cu.Code, cu.Raw)
	require.Equal(t, gstinKA, cu.Body["gstin"])
	require.Equal(t, "29", cu.Body["stateCode"])
	require.Equal(t, "Kaveri Traders Pvt Ltd", cu.Body["legalName"])
	cid := jsonID(num(cu.Body["id"]))
	r = call("PUT", "/api/customers/"+cid, map[string]any{"name": "Kaveri Traders", "phone": "080 1234"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, gstinKA, r.Body["gstin"], "an old form without the fields keeps them")
	require.Equal(t, "4 Residency Road, Bengaluru", r.Body["billingAddress"])
	r = call("PUT", "/api/customers/"+cid, map[string]any{"name": "Kaveri Traders", "gstin": gstinMH, "stateCode": "29"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "gstin", errField(r))
	walk := call("POST", "/api/customers", map[string]any{"name": "Asha from Pune", "stateCode": "27"})
	require.Equal(t, 201, walk.Code, walk.Raw)
	require.Nil(t, walk.Body["gstin"])
	require.Equal(t, "27", walk.Body["stateCode"])

	// A supplier's GSTIN (purchase tax is a later module).
	su := call("POST", "/api/suppliers", map[string]any{"name": "Deccan Wholesale", "gstin": gstinMH})
	require.Equal(t, 201, su.Code, su.Raw)
	require.Equal(t, "27", su.Body["stateCode"])
	r = call("POST", "/api/suppliers", map[string]any{"name": "Typo Supplies", "gstin": "27AAPFU0939F1Z"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "gstin", errField(r))
}

// M3 step 2 (§4): the split per sale. A shop in Maharashtra with a store
// in Karnataka and one in Chandigarh: CGST+SGST within a state, IGST to
// another, CGST+UTGST in Chandigarh; B2B buyers copied onto the sale; a
// B2C sale above ₹50,000 to another state needs the buyer's address (Q7);
// a return copies the sale's identity. A shop with no state is unchanged.
func TestGSTSales(t *testing.T) {
	l := billingTenant(t, "gst-sales")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}
	gstOf := func(r resp) map[string]any {
		t.Helper()
		g, ok := r.Body["gst"].(map[string]any)
		require.True(t, ok, "gst copy in %s", r.Raw)
		return g
	}
	require.Equal(t, 200, call("PUT", "/api/settings", map[string]any{"taxLabel": "GST", "taxRegistrationNo": gstinMH, "stateCode": "27", "currency": "INR"}).Code)
	mum := storeIDs(t, l, tok)[0]
	blr := call("POST", "/api/stores", map[string]any{"name": "Bengaluru", "gstin": gstinKA})
	require.Equal(t, 201, blr.Code, blr.Raw)
	chd := call("POST", "/api/stores", map[string]any{"name": "Chandigarh", "stateCode": "04"})
	require.Equal(t, 201, chd.Code, chd.Raw)
	pm := stockedProduct(t, l, tok, "GST-MUM", mum, 500)
	pb := stockedProduct(t, l, tok, "GST-BLR", num(blr.Body["id"]), 10)
	pc := stockedProduct(t, l, tok, "GST-CHD", num(chd.Body["id"]), 10)

	// Mumbai, walk-in: CGST + SGST, place of supply Maharashtra.
	s := sell(t, l, tok, posSale(mum, line(pm, 1)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	g := gstOf(s)
	require.Equal(t, "cgst_sgst", g["taxSplit"])
	require.Equal(t, "27", g["placeOfSupply"])
	require.Equal(t, gstinMH, g["sellerGstin"])
	require.Equal(t, "b2c", g["invoiceKind"])
	ln := lineOf(t, s.Body, pm)
	require.Equal(t, "10.85", dec(ln["cgst"]).StringFixed(2), "18 % of 120.50 = 21.69: 10.85 + 10.84")
	require.Equal(t, "10.84", dec(ln["sgst"]).StringFixed(2))
	require.Nil(t, ln["igst"])

	// Delivered to Karnataka: IGST. The quote says so first.
	body := posSale(mum, line(pm, 1))
	body["placeOfSupply"] = "29"
	q := call("POST", "/api/sales/quote", body)
	require.Equal(t, 200, q.Code, q.Raw)
	require.Equal(t, "igst", gstOf(q)["taxSplit"])
	s = sell(t, l, tok, body, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, "igst", gstOf(s)["taxSplit"])
	require.Equal(t, "29", gstOf(s)["placeOfSupply"])
	ln = lineOf(t, s.Body, pm)
	require.Equal(t, "21.69", dec(ln["igst"]).StringFixed(2))
	require.Nil(t, ln["cgst"])
	igstSale := s.Body["id"]

	// A credit note copies the sale's identity.
	ret := postReturn(t, l, tok, retBody(igstSale, mum, map[string]any{"productId": pm.pid, "variantId": pm.vid, "quantity": 1, "reason": "wrong"}), nil)
	require.Equal(t, 201, ret.Code, ret.Raw)
	require.Equal(t, "igst", gstOf(ret)["taxSplit"])
	require.Equal(t, "29", gstOf(ret)["placeOfSupply"])

	// A registered buyer in Karnataka at the Bengaluru store: B2B, within the state.
	cu := call("POST", "/api/customers", map[string]any{"name": "Kaveri", "gstin": gstinKA, "legalName": "Kaveri Traders Pvt Ltd",
		"billingAddress": "4 Residency Road, Bengaluru"})
	require.Equal(t, 201, cu.Code, cu.Raw)
	body = posSale(num(blr.Body["id"]), line(pb, 1))
	body["customerId"] = cu.Body["id"]
	q = call("POST", "/api/sales/quote", body)
	require.Equal(t, 200, q.Code, q.Raw)
	require.Contains(t, q.Raw, "has no HSN code", "a B2B line without HSN is warned about")
	s = sell(t, l, tok, body, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	g = gstOf(s)
	require.Equal(t, "b2b", g["invoiceKind"])
	require.Equal(t, "cgst_sgst", g["taxSplit"])
	require.Equal(t, gstinKA, g["sellerGstin"], "the store's own GSTIN")
	require.Equal(t, gstinKA, g["buyerGstin"])
	require.Equal(t, "Kaveri Traders Pvt Ltd", g["buyerName"])
	require.Equal(t, "4 Residency Road, Bengaluru", g["buyerAddress"])

	// Chandigarh: CGST + UTGST (carried in sgst), under the shop's GSTIN.
	s = sell(t, l, tok, posSale(num(chd.Body["id"]), line(pc, 1)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, "cgst_utgst", gstOf(s)["taxSplit"])
	require.NotNil(t, lineOf(t, s.Body, pc)["sgst"])

	// Q7: more than ₹50,000 to another state without the buyer's address.
	body = posSale(mum, line(pm, 400))
	body["placeOfSupply"] = "29"
	body["receivedAmount"], body["amountTendered"] = "100000", 100000
	q = call("POST", "/api/sales/quote", body)
	require.Equal(t, 200, q.Code, q.Raw)
	require.Contains(t, q.Raw, "needs the buyer's name and address")
	s = sell(t, l, tok, body, nil)
	require.Equal(t, 400, s.Code, s.Raw)
	require.Equal(t, "customerId", errField(s))

	// An unknown place of supply.
	body = posSale(mum, line(pm, 1))
	body["placeOfSupply"] = "99"
	s = sell(t, l, tok, body, nil)
	require.Equal(t, 400, s.Code, s.Raw)
	require.Equal(t, "placeOfSupply", errField(s))
	require.Empty(t, reconcile(t, l))
}

// M3 §4 rules 1 and 2: a shop with no state keeps its tax_split setting
// (no place of supply); with a state but no GSTIN, tax is not split.
func TestGSTSalesNoState(t *testing.T) {
	l := billingTenant(t, "gst-nostate")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	set := func(body map[string]any) {
		r := do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: body})
		require.Equal(t, 200, r.Code, r.Raw)
	}
	set(map[string]any{"taxSplit": "igst", "taxRegistrationNo": ""})
	p := stockedProduct(t, l, tok, "GST-LEGACY", store, 10)
	s := sell(t, l, tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	g := s.Body["gst"].(map[string]any)
	require.Equal(t, "igst", g["taxSplit"], "the shop's setting, as before M3")
	require.Nil(t, g["placeOfSupply"])
	require.NotNil(t, lineOf(t, s.Body, p)["igst"])

	body := posSale(store, line(p, 1))
	body["placeOfSupply"] = "29"
	r := sell(t, l, tok, body, nil)
	require.Equal(t, 400, r.Code, "a place of supply needs a state first: %s", r.Raw)

	set(map[string]any{"stateCode": "27"})
	s = sell(t, l, tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	g = s.Body["gst"].(map[string]any)
	require.Equal(t, "none", g["taxSplit"], "not registered: no CGST/SGST/IGST lines")
	require.Equal(t, "27", g["placeOfSupply"])
	require.Nil(t, lineOf(t, s.Body, p)["igst"])
}
