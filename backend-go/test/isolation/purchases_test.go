//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// id formats a JSON number ID the way FormData sends it ("12").
func jsonID(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func createSupplier(t *testing.T, l, tok, name string) float64 {
	t.Helper()
	r := do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/suppliers", token: tok, body: map[string]any{"name": name}})
	require.Equal(t, 201, r.Code, r.Raw)
	return r.Body["id"].(float64)
}

// purchaseBody is what AddPurchase.jsx posts: IDs as strings (FormData)
// and client totals, which the server ignores.
func purchaseBody(supplier float64, ref, status string, items ...map[string]any) map[string]any {
	return map[string]any{"reference": ref, "date": time.Now().Format("2006-01-02"), "status": status,
		"payment_status": "Unpaid", "total": 1, "paid": 0, "due": 0, "supplierId": jsonID(supplier), "purchaseItems": items}
}

func purchaseLine(product, variant, store float64, qty int, price float64) map[string]any {
	return map[string]any{"productId": jsonID(product), "variantId": jsonID(variant), "storeId": jsonID(store),
		"quantity": qty, "unitPrice": price, "subtotal": 999999}
}

func postPurchase(t *testing.T, l, tok string, body map[string]any, hdr map[string]string) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/purchases", token: tok, body: body, header: hdr})
}

func putPurchase(t *testing.T, l, tok string, id any, body map[string]any) resp {
	t.Helper()
	return do(t, rq{method: "PUT", host: tenants[l].Host, path: fmt.Sprintf("/api/purchases/%v", id), token: tok, body: body})
}

func TestPurchaseCreateReceiveOnceAndShape(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	main := storeIDs(t, "a", tok)[0]
	annex := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Purchase Annex", "status": "Active"}})
	require.Equal(t, 201, annex.Code, annex.Raw)
	annexID := annex.Body["id"].(float64)
	sup := createSupplier(t, "a", tok, "Bulk Traders A")
	p := stockedProduct(t, "a", tok, "PUR-SINGLE", main, 2)
	vp := createProduct(t, "a", tok, map[string]any{"name": "Kurta", "productType": "variable", "variants": []map[string]any{
		{"sku": "PUR-KURTA-S", "price": "500"}, {"sku": "PUR-KURTA-M", "price": "500"}}})
	require.Equal(t, 201, vp.Code, vp.Raw)
	vs := vp.Body["ProductVariants"].([]any)
	second := vs[1].(map[string]any)["id"].(float64)

	// Pending: lines are stored with their variant and store; no stock moves.
	disc := purchaseLine(p.pid, p.vid, main, 3, 80)
	disc["discount"] = 10
	b := purchaseBody(sup, "INV-7781", "Pending", purchaseLine(vp.Body["id"].(float64), second, annexID, 4, 25.5), disc)
	c := postPurchase(t, "a", tok, b, nil)
	require.Equal(t, 201, c.Code, c.Raw)
	id := c.Body["id"]
	require.Equal(t, "INV-7781", c.Body["reference"])
	require.Equal(t, "Pending", c.Body["status"])
	require.Equal(t, 342.0, num(c.Body["subtotal"]))
	require.Equal(t, 10.0, num(c.Body["discount"]))
	require.Equal(t, 332.0, num(c.Body["total"]), "4 x 25.5 + 3 x 80 - 10; client total and subtotals ignored")
	require.Equal(t, 332.0, num(c.Body["due"]))
	require.Equal(t, "Unpaid", c.Body["paymentStatus"])
	require.Equal(t, "Bulk Traders A", c.Body["Supplier"].(map[string]any)["name"], "Purchases.jsx reads Supplier")
	require.Equal(t, "Bulk Traders A", c.Body["supplier"].(map[string]any)["name"])
	require.Nil(t, c.Body["received_at"])
	it := c.Body["items"].([]any)[0].(map[string]any)
	require.Equal(t, second, it["variant_id"])
	require.Equal(t, annexID, it["store_id"])
	require.Equal(t, 25.5, num(it["unitPrice"]))
	require.Equal(t, 102.0, num(it["subtotal"]))
	require.Equal(t, 0, stockOf(t, "a", annexID, second))
	require.Equal(t, 2, stockOf(t, "a", main, p.vid))

	// Received: each line goes to its own variant and store; storeId in
	// the body is ignored (C-09), and paid makes it Partial.
	r := putPurchase(t, "a", tok, id, map[string]any{"status": "Received", "storeId": 1, "paid": 100, "due": 0})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "Received", r.Body["status"])
	require.NotNil(t, r.Body["received_at"])
	require.Equal(t, "Partial", r.Body["payment_status"])
	require.Equal(t, 232.0, num(r.Body["due"]))
	require.Equal(t, 4, stockOf(t, "a", annexID, second), "the line's variant and store, not the first variant in store 1")
	require.Equal(t, 0, stockOf(t, "a", main, num(vs[0].(map[string]any)["id"])))
	require.Equal(t, 5, stockOf(t, "a", main, p.vid))
	var moves int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.stock_movements
		WHERE reason = 'purchase_receive' AND ref_type = 'purchase' AND ref_id = $1`, int64(num(id))).Scan(&moves))
	require.Equal(t, 2, moves)

	// Received is final: received again changes nothing, back to Pending is
	// refused; paying the rest is still allowed.
	require.Equal(t, 200, putPurchase(t, "a", tok, id, map[string]any{"status": "Received"}).Code)
	back := putPurchase(t, "a", tok, id, map[string]any{"status": "Pending"})
	require.Equal(t, 409, back.Code, back.Raw)
	require.Equal(t, "INVALID_STATE", back.Body["code"])
	require.Equal(t, 4, stockOf(t, "a", annexID, second))
	require.Equal(t, 5, stockOf(t, "a", main, p.vid))
	paid := putPurchase(t, "a", tok, id, map[string]any{"paid": 332})
	require.Equal(t, 200, paid.Code, paid.Raw)
	require.Equal(t, "Paid", paid.Body["payment_status"])
	require.Equal(t, 0.0, num(paid.Body["due"]))
	over := putPurchase(t, "a", tok, id, map[string]any{"paid": 400})
	require.Equal(t, 400, over.Code)
	require.Equal(t, "paid", errField(over))

	// A supplier's invoice number is used once; another supplier may reuse it.
	dup := postPurchase(t, "a", tok, purchaseBody(sup, "INV-7781", "Pending", purchaseLine(p.pid, p.vid, main, 1, 80)), nil)
	require.Equal(t, 409, dup.Code, dup.Raw)
	require.Equal(t, "CONFLICT", dup.Body["code"])
	other := createSupplier(t, "a", tok, "Other Traders A")
	require.Equal(t, 201, postPurchase(t, "a", tok, purchaseBody(other, "INV-7781", "Pending", purchaseLine(p.pid, p.vid, main, 1, 80)), nil).Code)
	gen := postPurchase(t, "a", tok, purchaseBody(sup, "  ", "Ordered", purchaseLine(p.pid, p.vid, main, 1, 80)), nil)
	require.Equal(t, 201, gen.Code, gen.Raw)
	require.Regexp(t, `^PUR-\d{6}$`, gen.Body["reference"])

	// Reads.
	one := get(t, host, fmt.Sprintf("/api/purchases/%v", id), tok)
	require.Equal(t, 200, one.Code)
	require.Equal(t, "INV-7781", one.Body["reference"])
	bySupplier := list(t, get(t, host, fmt.Sprintf("/api/purchases?supplierId=%v", sup), tok))
	require.Len(t, bySupplier, 2)
	require.Len(t, list(t, get(t, host, fmt.Sprintf("/api/purchases?supplierId=%v&status=Received", sup), tok)), 1)
	require.Empty(t, reconcile(t, "a"))
}

func TestPurchaseApproveValidationAndStates(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	sup := createSupplier(t, "a", tok, "Approve Traders A")
	p := stockedProduct(t, "a", tok, "PUR-APPR", store, 0)
	vp := createProduct(t, "a", tok, map[string]any{"name": "Shirt", "productType": "variable", "variants": []map[string]any{
		{"sku": "PUR-SHIRT-S", "price": "300"}, {"sku": "PUR-SHIRT-M", "price": "300"}}})
	require.Equal(t, 201, vp.Code, vp.Raw)

	// Receiving needs purchases:approve, at creation and later.
	clerk := userWithPerms(t, "a", tok, "purchase-clerk@iso-a.test", "purchases:view", "purchases:create", "purchases:edit")
	require.Equal(t, 403, postPurchase(t, "a", clerk, purchaseBody(sup, "APPR-1", "Received", purchaseLine(p.pid, p.vid, store, 2, 10)), nil).Code)
	c := postPurchase(t, "a", clerk, purchaseBody(sup, "APPR-1", "Pending", purchaseLine(p.pid, p.vid, store, 2, 10)), nil)
	require.Equal(t, 201, c.Code, c.Raw)
	require.Equal(t, 403, putPurchase(t, "a", clerk, c.Body["id"], map[string]any{"status": "Received"}).Code)
	require.Equal(t, 0, stockOf(t, "a", store, p.vid))
	require.Equal(t, 200, putPurchase(t, "a", tok, c.Body["id"], map[string]any{"status": "Received"}).Code)
	require.Equal(t, 2, stockOf(t, "a", store, p.vid))

	line := func(extra map[string]any) map[string]any {
		l := purchaseLine(p.pid, p.vid, store, 1, 10)
		for k, v := range extra {
			l[k] = v
		}
		return l
	}
	bad := map[string]struct {
		body  map[string]any
		field string
	}{
		"no supplier":        {purchaseBody(0, "V-1", "Pending", line(nil)), "supplierId"},
		"unknown supplier":   {purchaseBody(99999999, "V-1", "Pending", line(nil)), "supplierId"},
		"no lines":           {purchaseBody(sup, "V-1", "Pending"), "purchaseItems"},
		"zero quantity":      {purchaseBody(sup, "V-1", "Pending", line(map[string]any{"quantity": 0})), "purchaseItems[0].quantity"},
		"negative price":     {purchaseBody(sup, "V-1", "Pending", line(map[string]any{"unitPrice": -1})), "purchaseItems[0].unitPrice"},
		"no store":           {purchaseBody(sup, "V-1", "Pending", line(map[string]any{"storeId": ""})), "purchaseItems[0].storeId"},
		"unknown store":      {purchaseBody(sup, "V-1", "Pending", line(map[string]any{"storeId": "99999999"})), "purchaseItems[0].storeId"},
		"discount too large": {purchaseBody(sup, "V-1", "Pending", line(map[string]any{"discount": 11})), "purchaseItems[0].discount"},
		"variant of another product": {purchaseBody(sup, "V-1", "Pending",
			line(map[string]any{"variantId": fmt.Sprint(vp.Body["ProductVariants"].([]any)[0].(map[string]any)["id"])})), "purchaseItems[0].variantId"},
		"variable without variant": {purchaseBody(sup, "V-1", "Pending",
			map[string]any{"productId": vp.Body["id"], "storeId": store, "quantity": 1, "unitPrice": 5}), "purchaseItems[0].variantId"},
		"paid above total": {func() map[string]any {
			b := purchaseBody(sup, "V-1", "Pending", line(nil))
			b["paid"] = 11
			return b
		}(), "paid"},
		"unknown status": {purchaseBody(sup, "V-1", "Shipped", line(nil)), "status"},
		"unknown payment status": {func() map[string]any {
			b := purchaseBody(sup, "V-1", "Pending", line(nil))
			b["payment_status"] = "Maybe"
			return b
		}(), "payment_status"},
		"control characters": {purchaseBody(sup, "V-1\r\nX", "Pending", line(nil)), "reference"},
	}
	for name, tc := range bad {
		r := postPurchase(t, "a", tok, tc.body, nil)
		require.Equal(t, 400, r.Code, "%s: %s", name, r.Raw)
		require.Equal(t, tc.field, errField(r), "%s: %s", name, r.Raw)
	}

	// Cancelled is final; Overdue is kept while something is due.
	o := postPurchase(t, "a", tok, purchaseBody(sup, "STATE-1", "Ordered", purchaseLine(p.pid, p.vid, store, 1, 50)), nil)
	require.Equal(t, 201, o.Code)
	od := putPurchase(t, "a", tok, o.Body["id"], map[string]any{"payment_status": "Overdue"})
	require.Equal(t, 200, od.Code, od.Raw)
	require.Equal(t, "Overdue", od.Body["payment_status"])
	require.Equal(t, "Overdue", putPurchase(t, "a", tok, o.Body["id"], map[string]any{"paid": 20}).Body["payment_status"])
	require.Equal(t, "Paid", putPurchase(t, "a", tok, o.Body["id"], map[string]any{"paid": 50}).Body["payment_status"])
	require.Equal(t, 200, putPurchase(t, "a", tok, o.Body["id"], map[string]any{"status": "Cancelled"}).Code)
	reopen := putPurchase(t, "a", tok, o.Body["id"], map[string]any{"status": "Pending"})
	require.Equal(t, 409, reopen.Code)
	require.Equal(t, "INVALID_STATE", reopen.Body["code"])
	require.Equal(t, 409, putPurchase(t, "a", tok, o.Body["id"], map[string]any{"status": "Received"}).Code)
	require.Equal(t, 2, stockOf(t, "a", store, p.vid), "a cancelled purchase never stocks")
	require.Equal(t, 404, putPurchase(t, "a", tok, 99999999, map[string]any{"status": "Received"}).Code)

	biller := createUser(t, "a", tok, "purchase-biller@iso-a.test", "biller", "purchase-biller-pass-1")
	_ = biller
	btok, _ := login(t, "a", "purchase-biller@iso-a.test", "purchase-biller-pass-1")
	require.Equal(t, 403, get(t, host, "/api/purchases", btok).Code)
	require.Equal(t, 403, postPurchase(t, "a", btok, purchaseBody(sup, "B-1", "Pending", line(nil)), nil).Code)
	require.Empty(t, reconcile(t, "a"))
}

func TestConcurrentReceiveStocksOnce(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	sup := createSupplier(t, "a", tok, "Race Traders A")
	p := stockedProduct(t, "a", tok, "PUR-RACE", store, 0)
	c := postPurchase(t, "a", tok, purchaseBody(sup, "RACE-1", "Pending", purchaseLine(p.pid, p.vid, store, 5, 10)), nil)
	require.Equal(t, 201, c.Code, c.Raw)

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for range 8 {
		wg.Go(func() {
			r := putPurchase(t, "a", tok, c.Body["id"], map[string]any{"status": "Received"})
			mu.Lock()
			codes[r.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	require.Equal(t, map[int]int{200: 8}, codes)
	require.Equal(t, 5, stockOf(t, "a", store, p.vid), "stocked exactly once")
	var receives int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.audit_log
		WHERE action = 'purchase.receive' AND entity_id = $1`, fmt.Sprint(c.Body["id"])).Scan(&receives))
	require.Equal(t, 1, receives)
	require.Empty(t, reconcile(t, "a"))
}

func TestPurchaseIdempotencyKey(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	sup := createSupplier(t, "a", tok, "Idem Traders A")
	p := stockedProduct(t, "a", tok, "PUR-IDEM", store, 0)
	body := purchaseBody(sup, "IDEM-1", "Received", purchaseLine(p.pid, p.vid, store, 3, 10))
	hdr := map[string]string{"Idempotency-Key": "pur-idem-0001"}
	first := postPurchase(t, "a", tok, body, hdr)
	require.Equal(t, 201, first.Code, first.Raw)
	again := postPurchase(t, "a", tok, body, hdr)
	require.Equal(t, 201, again.Code)
	require.Equal(t, "true", again.Header.Get("Idempotent-Replayed"))
	require.Equal(t, first.Body["id"], again.Body["id"])
	require.Equal(t, 3, stockOf(t, "a", store, p.vid), "received once")
	require.Equal(t, 409, postPurchase(t, "a", tok, purchaseBody(sup, "IDEM-2", "Received", purchaseLine(p.pid, p.vid, store, 3, 10)), hdr).Code)
}

func TestPurchasesIsolationAcrossTenants(t *testing.T) {
	ctx := context.Background()
	adm := admin(t)
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	ch := tenants["c"].Host
	// A's next purchase and supplier IDs do not exist in C.
	for _, tbl := range []string{"purchases", "suppliers"} {
		_, err := adm.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1, 'id'), GREATEST((SELECT coalesce(max(id), 0) FROM `+
			schema("a")+`.`+tbl+`), 800000))`, schema("a")+"."+tbl)
		require.NoError(t, err)
	}
	aStore := storeIDs(t, "a", aTok)[0]
	aSup := createSupplier(t, "a", aTok, "Secret Supplier ISO-A")
	p := stockedProduct(t, "a", aTok, "PUR-ISO-A", aStore, 0)
	ap := postPurchase(t, "a", aTok, purchaseBody(aSup, "ISO-A-SECRET-REF", "Pending", purchaseLine(p.pid, p.vid, aStore, 4, 10)), nil)
	require.Equal(t, 201, ap.Code, ap.Raw)
	var inC int
	require.NoError(t, adm.QueryRow(ctx, `SELECT (SELECT count(*) FROM `+schema("c")+`.purchases WHERE id = $1) +
		(SELECT count(*) FROM `+schema("c")+`.suppliers WHERE id = $2)`, int64(num(ap.Body["id"])), int64(aSup)).Scan(&inC))
	require.Zero(t, inC)

	// A's purchase cannot be read or received on C.
	path := fmt.Sprintf("/api/purchases/%v", ap.Body["id"])
	require.Equal(t, 404, get(t, ch, path, cTok).Code)
	require.Equal(t, 404, putPurchase(t, "c", cTok, ap.Body["id"], map[string]any{"status": "Received"}).Code)
	require.Equal(t, 0, stockOf(t, "a", aStore, p.vid), "A's stock untouched")
	require.Equal(t, "Pending", get(t, tenants["a"].Host, path, aTok).Body["status"])

	// A's supplier and variant IDs mean nothing on C.
	cStore := storeIDs(t, "c", cTok)[0]
	cSup := createSupplier(t, "c", cTok, "Supplier C")
	cp := stockedProduct(t, "c", cTok, "PUR-ISO-C", cStore, 0)
	r := postPurchase(t, "c", cTok, purchaseBody(aSup, "C-1", "Pending", purchaseLine(cp.pid, cp.vid, cStore, 1, 1)), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "supplierId", errField(r))
	r = postPurchase(t, "c", cTok, purchaseBody(cSup, "C-1", "Pending",
		map[string]any{"variantId": p.vid + 100000, "storeId": cStore, "quantity": 1, "unitPrice": 1}), nil)
	require.Equal(t, 400, r.Code, r.Raw)

	l := get(t, ch, "/api/purchases", cTok)
	require.Equal(t, 200, l.Code)
	require.NotContains(t, l.Raw, "ISO-A-SECRET-REF")
	require.False(t, strings.Contains(l.Raw, "Secret Supplier ISO-A"))
	require.Empty(t, list(t, get(t, ch, fmt.Sprintf("/api/purchases?supplierId=%v", aSup), cTok)))
	require.Equal(t, 401, postPurchase(t, "c", aTok, purchaseBody(cSup, "C-2", "Pending", purchaseLine(cp.pid, cp.vid, cStore, 1, 1)), nil).Code)
	require.Equal(t, 401, get(t, ch, "/api/purchases", aTok).Code)
}
