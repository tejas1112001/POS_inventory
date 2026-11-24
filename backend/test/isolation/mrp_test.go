//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// MRP on every variant (tenant migration 00032), strict since the pharmacy
// plan §6.2: kept when an edit leaves it out, a price above it refused at
// saving, an old price above it (saved before the rule) refused at the sale
// unless a discount brings the line down, a sale-time snapshot on sale
// lines, and the receipt's MRP and saving only when the shop turns them on.
func TestVariantMRP(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "mrp-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	variantOf := func(r resp) map[string]any { return r.Body["ProductVariants"].([]any)[0].(map[string]any) }

	// Price 100 + 18 % tax = 118: at MRP, not above it.
	body := addFormSingle("MRP-1", store, 10)
	v0 := body["variants"].([]map[string]any)[0]
	v0["price"], v0["mrp"] = 100, 118
	p := createProduct(t, l, tok, body)
	require.Equal(t, 201, p.Code, p.Raw)
	v := variantOf(p)
	require.EqualValues(t, 118, num(v["mrp"]))
	require.EqualValues(t, 1, num(v["mrpPack"]), "printed for one unit by default")
	require.Equal(t, false, v["aboveMrp"])
	pid, vid := p.Body["id"], v["id"]
	putProduct := func() resp {
		return do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/products/%v", pid), token: tok, body: body})
	}

	// An edit to 101 + 18 % = 119.18, above 118 (MRP left out: kept): refused.
	delete(v0, "mrp")
	v0["id"], v0["price"] = vid, 101
	u := putProduct()
	require.Equal(t, 409, u.Code, u.Raw)
	require.Equal(t, "ABOVE_MRP", codeOf(u))
	require.Contains(t, u.Raw, "119.18")
	// A negative MRP is refused.
	v0["mrp"] = -1
	bad := putProduct()
	require.Equal(t, 400, bad.Code, bad.Raw)
	require.Contains(t, bad.Raw, "variants[0].mrp")
	delete(v0, "mrp")
	v0["price"] = 100

	// A price saved above MRP before the rule (written directly): flagged on
	// the product, listed by the report, refused at the till.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.product_variants SET price = 101 WHERE id = $1`, int64(num(vid)))
	require.NoError(t, err)
	require.Equal(t, true, variantOf(get(t, host, fmt.Sprintf("/api/products/%v", pid), tok))["aboveMrp"])
	rep := get(t, host, "/api/products/above-mrp", tok)
	require.Equal(t, 200, rep.Code, rep.Raw)
	rows := rep.Body["rows"].([]any)
	require.Len(t, rows, 1, rep.Raw)
	require.Equal(t, "price", rows[0].(map[string]any)["kind"])
	require.EqualValues(t, 119.18, num(rows[0].(map[string]any)["priceWithTax"]))

	item := func(qty int) map[string]any {
		return map[string]any{"productId": pid, "variantId": vid, "quantity": qty, "unitPrice": 101}
	}
	quote := func(b map[string]any) map[string]any {
		r := do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: tok, body: b})
		require.Equal(t, 200, r.Code, r.Raw)
		return r.Body["lines"].([]any)[0].(map[string]any)
	}
	// 2 × 101 + 18 % = 238.36 against MRP 2 × 118 = 236: the quote says so,
	// the sale (the old POS screen's request) is refused.
	ql := quote(posSale(store, item(2)))
	require.Equal(t, true, ql["aboveMrp"])
	require.EqualValues(t, 118, num(ql["mrp"]))
	require.EqualValues(t, 236, num(ql["mrpAmount"]))
	require.Contains(t, fmt.Sprint(ql["warnings"]), "refused")
	refused := sell(t, l, tok, posSale(store, item(2)), nil)
	require.Equal(t, 409, refused.Code, refused.Raw)
	require.Equal(t, "ABOVE_MRP", codeOf(refused))
	require.Equal(t, 10, stockOf(t, l, store, num(vid)), "nothing sold")
	// A bill discount that brings the line under MRP: quoted fine, sold.
	disc := posSale(store, item(2))
	disc["discount"] = 10
	require.Equal(t, false, quote(disc)["aboveMrp"])
	s := sell(t, l, tok, disc, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	// Editing that sale to drop the discount is refused too.
	ed := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/sales/%v", s.Body["id"]), token: tok, body: posSale(store, item(2))})
	require.Equal(t, 409, ed.Code, ed.Raw)
	require.Equal(t, "ABOVE_MRP", codeOf(ed))

	// The sale keeps its MRP after the product changes.
	v0["mrp"] = 200
	require.Equal(t, 200, putProduct().Code)
	sale := get(t, host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), tok)
	it := sale.Body["items"].([]any)[0].(map[string]any)
	require.EqualValues(t, 118, num(it["mrp"]), "sale-time MRP")
	require.EqualValues(t, 1, num(it["mrpPack"]))
	require.EqualValues(t, 236, num(it["mrpAmount"]))
	require.Equal(t, false, it["aboveMrp"])
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'product.mrp_change'`).Scan(&n))
	require.Equal(t, 1, n, "118 -> 200")
	require.Empty(t, get(t, host, "/api/products/above-mrp", tok).Body["rows"], "fixed")

	// A product without MRP (not a pharmacy): no MRP on the line, sold.
	nb := addFormSingle("MRP-NONE", store, 5)
	delete(nb["variants"].([]map[string]any)[0], "mrp")
	np := createProduct(t, l, tok, nb)
	require.Equal(t, 201, np.Code, np.Raw)
	plain := stocked{pid: num(np.Body["id"]), vid: num(variantOf(np)["id"]), sku: "MRP-NONE"}
	s2 := sell(t, l, tok, posSale(store, line(plain, 1)), nil)
	require.Equal(t, 201, s2.Code, s2.Raw)
	it2 := get(t, host, fmt.Sprintf("/api/sales/%v", s2.Body["id"]), tok).Body["items"].([]any)[0].(map[string]any)
	require.Nil(t, it2["mrp"])
	require.Equal(t, false, it2["aboveMrp"])

	// Receipts: MRP and the saving only when the shop shows MRP.
	// The owner's price override 101 + 18 % = 119.18 against MRP 200: saved 80.82.
	s3 := sell(t, l, tok, posSale(store, item(1)), nil)
	require.Equal(t, 201, s3.Code, s3.Raw)
	require.Equal(t, false, get(t, host, "/api/settings", tok).Body["showMrpOnReceipt"], "off by default")
	off := receiptView(t, l, s3.Body["id"])
	require.Empty(t, off.Lines[0].MRP)
	require.False(t, off.HasSaving)

	r := do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"showMrpOnReceipt": true}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, true, r.Body["showMrpOnReceipt"])
	on := receiptView(t, l, s3.Body["id"])
	require.Contains(t, on.Lines[0].MRP, "200")
	require.True(t, on.HasSaving)
	require.True(t, strings.Contains(on.Saving, "80.82"), on.Saving)
	pdf, err := receipts.RenderPDF(on, nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
}

// receiptView loads the newest receipt of a sale as the printer sees it.
func receiptView(t *testing.T, l string, saleID any) receipts.View {
	t.Helper()
	ctx := context.Background()
	var rid int64
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT max(id) FROM `+schema(l)+`.receipts WHERE sale_id = $1`,
		int64(saleID.(float64))).Scan(&rid))
	sch, role, err := tenancy.SchemaFor(tenants[l].ID)
	require.NoError(t, err)
	var out receipts.View
	require.NoError(t, tenancy.NewSessions(tdb.AppPool(t, 1), 0, nil, nil).InTenantTx(ctx,
		tenancy.Tenant{ID: tenants[l].ID, Schema: sch, Role: role}, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
			out, err = receipts.Load(ctx, tx, rid)
			return err
		}))
	return out
}

// Strict MRP for tablets, strips and boxes (pharmacy plan §6.2): the MRP is
// printed per strip, a line's MRP is exact and rounded down to the paisa,
// tax and every discount count, store prices and price saves are checked.
func TestStrictMRPPacks(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "mrp-packs")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	call := func(method, path string, body any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}
	unit := func(name string) float64 {
		u := call("POST", "/api/units", map[string]any{"name": name, "short_name": name[:3]})
		require.Equal(t, 201, u.Code, u.Raw)
		return num(u.Body["id"])
	}
	tablet, strip, box := unit("Tablet"), unit("Strip of 10"), unit("Box of 100")
	medicine := func(sku string, price, mrp, pack float64, taxType string, tax string) map[string]any {
		b := addFormSingle(sku, store, 1000)
		b["unitId"], b["taxType"], b["tax"] = tablet, taxType, tax
		b["units"] = []map[string]any{{"unitId": strip, "factor": 10}, {"unitId": box, "factor": 100}}
		v := b["variants"].([]map[string]any)[0]
		v["price"], v["mrp"], v["mrpPack"] = price, mrp, pack
		return b
	}
	create := func(b map[string]any) (stocked, resp) {
		r := createProduct(t, l, tok, b)
		if r.Code != 201 {
			return stocked{}, r
		}
		v := r.Body["ProductVariants"].([]any)[0].(map[string]any)
		return stocked{pid: num(r.Body["id"]), vid: num(v["id"])}, r
	}
	ln := func(p stocked, qty float64, u float64) map[string]any {
		m := map[string]any{"productId": p.pid, "variantId": p.vid, "quantity": qty}
		if u > 0 {
			m["unitId"] = u
		}
		return m
	}
	saleOf := func(r resp) map[string]any {
		t.Helper()
		require.Equal(t, 201, r.Code, r.Raw)
		return r.Body["items"].([]any)[0].(map[string]any)
	}
	refusedAbove := func(r resp) {
		t.Helper()
		require.Equal(t, 409, r.Code, r.Raw)
		require.Equal(t, "ABOVE_MRP", codeOf(r))
	}
	setPrice := func(p stocked, price string) { // a price saved before the rule
		_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.product_variants SET price = $2::numeric WHERE id = $1`, int64(p.vid), price)
		require.NoError(t, err)
	}

	// ₹35.00 per strip of 10, ₹3.50 a tablet, no tax.
	para, r := create(medicine("PK-PARA", 3.5, 35, 10, "Inclusive", "0"))
	require.Equal(t, 201, r.Code, r.Raw)
	require.EqualValues(t, 10, num(r.Body["ProductVariants"].([]any)[0].(map[string]any)["mrpPack"]))
	for _, c := range []struct {
		qty, unit float64
		mrp       string
	}{{1, 0, "3.5"}, {3, 0, "10.5"}, {1, strip, "35"}, {1, box, "350"}} {
		it := saleOf(sell(t, l, tok, posSale(store, ln(para, c.qty, c.unit)), nil))
		require.Equal(t, c.mrp, dec(it["mrpAmount"]).String(), "%v × unit %v", c.qty, c.unit)
		require.EqualValues(t, 35, num(it["mrp"]))
		require.EqualValues(t, 10, num(it["mrpPack"]))
	}
	// Saving ₹3.51 a tablet (a strip at ₹35.10) is refused.
	b := medicine("PK-PARA", 3.51, 35, 10, "Inclusive", "0")
	b["variants"].([]map[string]any)[0]["id"] = para.vid
	refusedAbove(call("PUT", fmt.Sprintf("/api/products/%v", para.pid), b))
	// An old ₹3.51: a tablet and a strip are refused; a box with ₹1.00 off
	// (₹351.00 − 1.00 = ₹350.00) is at MRP and sold.
	setPrice(para, "3.51")
	refusedAbove(sell(t, l, tok, posSale(store, ln(para, 1, 0)), nil))
	refusedAbove(sell(t, l, tok, posSale(store, ln(para, 1, strip)), nil))
	boxOff := posSale(store, ln(para, 1, box))
	boxOff["discount"] = 1
	require.Equal(t, "350", dec(saleOf(sell(t, l, tok, boxOff, nil))["subtotal"]).String())
	setPrice(para, "3.5")

	// ₹25.00 per strip of 15: one tablet's share is ₹1.66 (₹1.666… rounded down).
	fifteen, r := create(medicine("PK-15", 1.66, 25, 15, "Inclusive", "0"))
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "1.66", dec(saleOf(sell(t, l, tok, posSale(store, ln(fifteen, 1, 0)), nil))["mrpAmount"]).String())
	require.Equal(t, "3.33", dec(saleOf(sell(t, l, tok, posSale(store, ln(fifteen, 2, 0)), nil))["mrpAmount"]).String())
	require.Equal(t, "25", dec(saleOf(sell(t, l, tok, posSale(store, ln(fifteen, 15, 0)), nil))["mrpAmount"]).String())
	b = medicine("PK-15", 1.6667, 25, 15, "Inclusive", "0")
	b["variants"].([]map[string]any)[0]["id"] = fifteen.vid
	refusedAbove(call("PUT", fmt.Sprintf("/api/products/%v", fifteen.pid), b)) // one tablet would be ₹1.67
	setPrice(fifteen, "1.67")
	refusedAbove(sell(t, l, tok, posSale(store, ln(fifteen, 1, 0)), nil))

	// Tax counts: ₹33.00 + 12 % = ₹36.96 above an MRP of ₹35.00.
	_, r = create(medicine("PK-TAX", 33, 35, 1, "Exclusive", "12"))
	refusedAbove(r)
	require.Contains(t, r.Raw, "36.96")
	// A discount counts: ₹40.00 a strip, ₹6.00 off the bill = ₹34.00.
	disc, r := create(medicine("PK-DISC", 3.5, 35, 10, "Inclusive", "0"))
	require.Equal(t, 201, r.Code, r.Raw)
	setPrice(disc, "4")
	refusedAbove(sell(t, l, tok, posSale(store, ln(disc, 1, strip)), nil))
	off := posSale(store, ln(disc, 1, strip))
	off["discount"] = 6
	require.Equal(t, "34", dec(saleOf(sell(t, l, tok, off, nil))["subtotal"]).String())

	// Store prices: above MRP refused; at or below saved; lowering the MRP
	// under a store price is refused, naming the store.
	sp := func(price float64) resp {
		return call("PUT", fmt.Sprintf("/api/products/%v/store-prices", para.pid),
			map[string]any{"prices": []map[string]any{{"storeId": store, "variantId": para.vid, "price": price}}})
	}
	refusedAbove(sp(3.6))
	require.Equal(t, 200, sp(3.45).Code)
	b = medicine("PK-PARA", 3.3, 34, 10, "Inclusive", "0")
	b["variants"].([]map[string]any)[0]["id"] = para.vid
	low := call("PUT", fmt.Sprintf("/api/products/%v", para.pid), b)
	refusedAbove(low)
	require.Contains(t, low.Raw, " in ", "names the store")
	require.Equal(t, 200, sp(3.4).Code)
	require.Equal(t, 200, call("PUT", fmt.Sprintf("/api/products/%v", para.pid), b).Code)

	// The receipt prints the MRP per pack.
	require.Equal(t, 200, call("PUT", "/api/settings", map[string]any{"showMrpOnReceipt": true}).Code)
	s := sell(t, l, tok, posSale(store, ln(para, 1, strip)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Contains(t, receiptView(t, l, s.Body["id"]).Lines[0].MRP, "/10")
}

// A pharmacy (pharmacy plan §6.2): every batch received or imported needs
// its MRP, a medicine with no MRP is not sold, a line drawn from batches
// with different MRPs and packs sums each batch's.
func TestStrictMRPPharmacy(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "mrp-ph")
	host := tenants[l].Host

	// Receiving a batch without its MRP is refused.
	sup := createSupplier(t, l, tok, "MRP Distributors")
	a := stockedProduct(t, l, tok, "PH-PARA", store, 0)
	c := postPurchase(t, l, tok, purchaseBody(sup, "PH-1", "Ordered", purchaseLine(a.pid, a.vid, store, 16, 1)), nil)
	require.Equal(t, 201, c.Code, c.Raw)
	ia := c.Body["items"].([]any)[0].(map[string]any)["id"]
	noMRP := bat("A1", day(200), 6)
	delete(noMRP, "mrp")
	r := postReceipt(t, l, tok, c.Body["id"], map[string]any{"lines": []any{bline(ia, 16, noMRP, bat("B1", day(400), 10))}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "lines[0].batches[0].mrp")
	// A1: ₹35.00 per strip of 10 (expires first); B1: ₹3.80 a tablet.
	a1 := bat("A1", day(200), 6)
	a1["mrp"], a1["mrpPack"] = 35, 10
	b1 := bat("B1", day(400), 10)
	b1["mrp"] = 3.8
	r = postReceipt(t, l, tok, c.Body["id"], map[string]any{"lines": []any{bline(ia, 16, a1, b1)}}, nil)
	require.Equal(t, 201, r.Code, r.Raw)

	// 10 tablets: 6 from A1 (₹21.00) + 4 from B1 (₹15.20) = ₹36.20.
	// At ₹3.62 a tablet (no tax) the line is ₹36.20: sold; ₹3.63: refused.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.products SET tax_rate = 0, tax_type = 'Inclusive' WHERE id = $1`, int64(a.pid))
	require.NoError(t, err)
	price := func(p string) {
		_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.product_variants SET price = $2::numeric WHERE id = $1`, int64(a.vid), p)
		require.NoError(t, err)
	}
	ten := map[string]any{"productId": a.pid, "variantId": a.vid, "quantity": 10}
	price("3.63")
	q := do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: tok, body: posSale(store, ten)})
	require.Equal(t, 200, q.Code, q.Raw)
	ql := q.Body["lines"].([]any)[0].(map[string]any)
	require.Equal(t, "36.2", dec(ql["mrpAmount"]).String())
	require.Equal(t, true, ql["aboveMrp"])
	require.Nil(t, ql["mrp"], "the batches differ")
	rep := get(t, host, "/api/products/above-mrp", tok)
	require.Contains(t, rep.Raw, `"kind":"batch"`)
	s := sell(t, l, tok, posSale(store, ten), nil)
	require.Equal(t, 409, s.Code, s.Raw)
	require.Equal(t, "ABOVE_MRP", codeOf(s))
	price("3.62")
	s = sell(t, l, tok, posSale(store, ten), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	it := s.Body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "36.2", dec(it["mrpAmount"]).String())
	packs := map[string]float64{}
	for _, x := range it["batches"].([]any) {
		m := x.(map[string]any)
		packs[m["batchNo"].(string)] = num(m["mrpPack"])
	}
	require.Equal(t, map[string]float64{"A1": 10, "B1": 1}, packs)

	// A medicine with no MRP anywhere (old stock) is not sold.
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET mrp = NULL WHERE variant_id = $1`, int64(a.vid))
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.product_variants SET mrp = NULL WHERE id = $1`, int64(a.vid))
	require.NoError(t, err)
	one := map[string]any{"productId": a.pid, "variantId": a.vid, "quantity": 1}
	s = sell(t, l, tok, posSale(store, one), nil)
	require.Equal(t, 409, s.Code, s.Raw)
	require.Equal(t, "MRP_REQUIRED", codeOf(s))
	q = do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: tok, body: posSale(store, one)})
	require.Equal(t, true, q.Body["lines"].([]any)[0].(map[string]any)["mrpRequired"])
	require.Contains(t, get(t, host, "/api/products/above-mrp", tok).Raw, `"kind":"no_mrp"`)

	// The pharmacy import template needs each batch's MRP.
	file := workbook(t, []string{"name", "sku", "price", "tax_rate", "batch_no", "expiry", "mrp", "opening_qty"},
		[]string{"Cough syrup", "PH-SYR", "90", "12", "S1", day(400), "", "3"})
	imp := uploadFile(t, l, tok, "ph.xlsx", file, true)
	require.Equal(t, 400, imp.Code, imp.Raw)
	require.Contains(t, imp.Raw, "mrp is required")
	// …and refuses a price above a batch's MRP: 90 + 12 % = 100.80 > 100.
	file = workbook(t, []string{"name", "sku", "price", "tax_rate", "batch_no", "expiry", "mrp", "opening_qty"},
		[]string{"Cough syrup", "PH-SYR", "90", "12", "S1", day(400), "100", "3"})
	imp = uploadFile(t, l, tok, "ph.xlsx", file, true)
	require.Equal(t, 400, imp.Code, imp.Raw)
	require.Contains(t, imp.Raw, "above this batch")
}

// The import templates take an MRP column (the variant's MRP; in the
// pharmacy template it stays the batch's); a price above it is a row error
// (pharmacy plan §6.2) and nothing is imported; labels can print it.
func TestImportAndLabelMRP(t *testing.T) {
	l := billingTenant(t, "mrp-imp")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host

	tpl := get(t, host, "/api/products/import/template", tok)
	require.Equal(t, 200, tpl.Code, tpl.Raw)
	require.Contains(t, strings.SplitN(tpl.Raw, "\n", 2)[0], ",mrp,")

	// 100 + 18 % = 118 is at its MRP; 120 + 18 % = 141.60 is above 130: refused.
	hdr := []string{"name", "sku", "price", "mrp", "tax_type", "tax_rate"}
	above := uploadFile(t, l, tok, "above.xlsx", workbook(t, hdr,
		[]string{"MRP tea", "MRPI-1", "100", "118", "Exclusive", "18"},
		[]string{"MRP coffee", "MRPI-2", "120", "130", "Exclusive", "18"}), false)
	require.Equal(t, 400, above.Code, above.Raw)
	errs := above.Body["errors"].([]any)
	require.Len(t, errs, 1, above.Raw)
	require.EqualValues(t, 3, num(errs[0].(map[string]any)["row"]), "the coffee row")
	require.Equal(t, "price", errs[0].(map[string]any)["field"])
	require.Empty(t, list(t, get(t, host, "/api/products", tok)), "nothing imported")
	file := workbook(t, hdr,
		[]string{"MRP tea", "MRPI-1", "100", "118", "Exclusive", "18"},
		[]string{"MRP coffee", "MRPI-2", "110", "130", "Exclusive", "18"},
		[]string{"No MRP", "MRPI-3", "50", "", "", ""},
	)
	bad := uploadFile(t, l, tok, "bad.xlsx", workbook(t, []string{"name", "sku", "price", "mrp"}, []string{"X", "MRPI-X", "1", "-5"}), true)
	require.Equal(t, 400, bad.Code, bad.Raw)
	require.Contains(t, bad.Raw, `"mrp"`)

	dry := uploadFile(t, l, tok, "mrp.xlsx", file, true)
	require.Equal(t, 200, dry.Code, dry.Raw)
	pv := dry.Body["preview"].([]any)
	require.EqualValues(t, 118, num(pv[0].(map[string]any)["variants"].([]any)[0].(map[string]any)["mrp"]))
	require.Empty(t, dry.Body["warnings"], dry.Raw)

	r := uploadFile(t, l, tok, "mrp.xlsx", file, false)
	require.Equal(t, 200, r.Code, r.Raw)
	require.EqualValues(t, 3, r.Body["created"])
	byName := map[string]map[string]any{}
	for _, p := range list(t, get(t, host, "/api/products", tok)) {
		byName[p["name"].(string)] = p["ProductVariants"].([]any)[0].(map[string]any)
	}
	require.EqualValues(t, 118, num(byName["MRP tea"]["mrp"]))
	require.Equal(t, false, byName["MRP tea"]["aboveMrp"])
	require.Equal(t, false, byName["MRP coffee"]["aboveMrp"])
	require.Nil(t, byName["No MRP"]["mrp"])

	// Labels: the MRP field is offered and a sheet with it prints.
	setModules(t, l, "clothing")
	lay := get(t, host, "/api/settings/labels", tok)
	require.Equal(t, 200, lay.Code, lay.Raw)
	require.Contains(t, fmt.Sprint(lay.Body["presets"].([]any)[0].(map[string]any)["fields"]), "mrp")
	pdf := do(t, rq{method: "POST", host: host, path: "/api/labels/pdf", token: tok, body: map[string]any{
		"layoutId": "a4-3x8", "items": []map[string]any{{"variantId": byName["MRP tea"]["id"], "copies": 2},
			{"variantId": byName["No MRP"]["id"], "copies": 1}}}})
	isPDF(t, pdf)
}
