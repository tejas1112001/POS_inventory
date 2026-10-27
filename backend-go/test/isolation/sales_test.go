//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type stocked struct {
	pid, vid float64
	sku      string
}

// stockedProduct creates a single product (price 120.5, 18 % exclusive)
// with qty units in store.
func stockedProduct(t *testing.T, l, tok, sku string, store float64, qty int) stocked {
	t.Helper()
	p := createProduct(t, l, tok, addFormSingle(sku, store, qty))
	require.Equal(t, 201, p.Code, p.Raw)
	v := p.Body["ProductVariants"].([]any)[0].(map[string]any)
	return stocked{pid: p.Body["id"].(float64), vid: v["id"].(float64), sku: sku}
}

func line(p stocked, qty int) map[string]any {
	return map[string]any{"productId": p.pid, "variantId": p.vid, "quantity": qty, "unitPrice": 120.5, "total": 120.5 * float64(qty)}
}

// posSale is the body POS.jsx sends (PayForm state spread in). Client
// totals are deliberately wrong: the server must ignore them.
func posSale(store float64, lines ...map[string]any) map[string]any {
	return map[string]any{
		"receivedAmount": "1000", "payingAmount": "1", "change": "0.00", "paymentType": "cash", "paymentNote": "",
		"saleNote": "thanks", "quickCash": "10", "method": "cash", "amountTendered": 1000, "total": "1",
		"orderItems": lines, "storeId": store, "subtotal": 1, "discount": 0, "tax": 0,
	}
}

func sell(t *testing.T, l, tok string, body map[string]any, hdr map[string]string) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/sales", token: tok, body: body, header: hdr})
}

func stockOf(t *testing.T, l string, store, vid float64) int {
	t.Helper()
	var q int
	require.NoError(t, admin(t).QueryRow(context.Background(),
		`SELECT coalesce((SELECT qty FROM `+schema(l)+`.inventories WHERE store_id = $1 AND variant_id = $2), 0)`,
		int64(store), int64(vid)).Scan(&q))
	return q
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return -1
}

// userWithPerms creates a role holding exactly perms and a user in it.
func userWithPerms(t *testing.T, l, ownerTok, email string, perms ...string) string {
	t.Helper()
	rows, err := admin(t).Query(context.Background(), `SELECT id FROM `+schema(l)+`.permissions WHERE module || ':' || action = ANY($1)`, perms)
	require.NoError(t, err)
	var ids []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	rows.Close()
	require.Len(t, ids, len(perms))
	host := tenants[l].Host
	role := do(t, rq{method: "POST", host: host, path: "/api/roles", token: ownerTok, body: map[string]any{"name": "role " + email, "permissionIds": ids}})
	require.Equal(t, 201, role.Code, role.Raw)
	pw := "pw-" + strings.ReplaceAll(email, "@", "-") + "-long"
	u := do(t, rq{method: "POST", host: host, path: "/api/users", token: ownerTok, body: map[string]any{
		"name": "U " + email, "email": email, "password": pw, "role_id": role.Body["id"], "status": "Active"}})
	require.Equal(t, 201, u.Code, u.Raw)
	tok, _ := login(t, l, email, pw)
	return tok
}

func TestCheckoutPOSPayloadServerTotals(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-1", store, 10)

	r := sell(t, "a", tok, posSale(store, line(p, 2)), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	b := r.Body
	require.Equal(t, 241.0, num(b["subtotal"]), "2 x 120.5")
	require.Equal(t, 43.38, num(b["tax"]), "18 % exclusive, client tax 0 ignored")
	require.Equal(t, 284.38, num(b["total"]), "client total 1 ignored")
	require.Equal(t, 284.38, num(b["paid"]), "no paid field: paid in full")
	require.Equal(t, 0.0, num(b["due"]))
	require.Equal(t, "Paid", b["payment_status"])
	require.Equal(t, "cash", b["payment_method"], "POS alias method")
	require.Equal(t, "thanks", b["note"], "POS alias saleNote")
	require.Equal(t, 1000.0, num(b["tendered"]))
	require.Equal(t, 715.62, num(b["change"]))
	require.NotEmpty(t, b["date"], "server sets the date (P1-5)")
	require.Equal(t, "INR", b["currency"])
	require.Regexp(t, `^POS-INV-\d{6}$`, b["reference"])
	require.Equal(t, "Walk-in Customer", b["customer"].(map[string]any)["name"], "default customer")
	it := b["items"].([]any)[0].(map[string]any)
	for _, k := range []string{"qty", "quantity", "unitPrice", "unit_price", "price", "catalog_price", "product", "variant", "cgst", "sgst"} {
		require.Contains(t, it, k)
	}
	require.Equal(t, 21.69, num(it["cgst"]))
	require.Equal(t, 21.69, num(it["sgst"]))
	require.Equal(t, 284.38, num(it["subtotal"]), "line total includes exclusive tax")

	require.Equal(t, 8, stockOf(t, "a", store, p.vid))
	var reason, refType string
	var refID int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT reason, ref_type, ref_id FROM `+schema("a")+`.stock_movements
		WHERE variant_id = $1 ORDER BY id DESC LIMIT 1`, int64(p.vid)).Scan(&reason, &refType, &refID))
	require.Equal(t, "sale", reason)
	require.Equal(t, "sale", refType)
	require.EqualValues(t, b["id"], refID)
	require.Empty(t, reconcile(t, "a"))

	id := b["id"]
	one := get(t, host, fmt.Sprintf("/api/sales/%v", id), tok)
	require.Equal(t, 200, one.Code)
	require.Equal(t, b["reference"], one.Body["reference"])
	require.Contains(t, get(t, host, "/api/sales?search="+b["reference"].(string), tok).Raw, b["reference"].(string))
	pg := get(t, host, "/api/sales?page=1&limit=1", tok)
	require.Len(t, pg.Body["data"], 1)
	require.Contains(t, pg.Body, "pagination")
	inv := get(t, host, "/api/invoices", tok)
	require.Equal(t, 200, inv.Code)
	require.Contains(t, inv.Raw, `"invoiceNo":"`+b["reference"].(string)+`"`)
	for _, k := range []string{`"dueDate"`, `"amountDue"`, `"avatar"`} {
		require.Contains(t, inv.Raw, k)
	}

	// Explicit customer, paid: 0 respected, partial, back-dated, Pending.
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Asha"}})
	require.Equal(t, 201, cu.Code)
	body := posSale(store, line(p, 1))
	body["customerId"], body["paid"] = cu.Body["id"], 0
	r = sell(t, "a", tok, body, nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "Unpaid", r.Body["payment_status"], "paid: 0 is not coerced to the total")
	require.Equal(t, 142.19, num(r.Body["due"]))
	body["paid"], body["date"], body["status"] = "50", "2026-01-15", "Pending"
	r = sell(t, "a", tok, body, nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "Partial", r.Body["payment_status"])
	require.Equal(t, "Pending", r.Body["status"])
	require.Contains(t, r.Body["date"], "2026-01-1", "back-dated")
	ranged := get(t, host, "/api/sales?startDate=2026-01-15&endDate=2026-01-15", tok)
	require.Equal(t, 200, ranged.Code)
	require.Equal(t, []string{r.Body["reference"].(string)}, refs(list(t, ranged)))
}

func refs(items []map[string]any) []string {
	var out []string
	for _, i := range items {
		out = append(out, fmt.Sprint(i["reference"]))
	}
	return out
}

func TestCheckoutValidation(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-VAL", store, 3)
	vp := createProduct(t, "a", tok, map[string]any{"name": "Cap", "productType": "variable", "variants": []map[string]any{
		{"sku": "CAP-S", "price": "10", "inventories": []map[string]any{{"storeId": store, "qty": 3}}},
		{"sku": "CAP-M", "price": "10", "inventories": []map[string]any{{"storeId": store, "qty": 3}}},
	}})
	require.Equal(t, 201, vp.Code)

	cases := map[string]map[string]any{
		"no store":            {"orderItems": []any{line(p, 1)}},
		"no items":            {"storeId": store, "orderItems": []any{}},
		"unknown key":         {"storeId": store, "orderItems": []any{line(p, 1)}, "isAdmin": true},
		"unknown variant":     {"storeId": store, "orderItems": []any{map[string]any{"variantId": 999999, "quantity": 1}}},
		"variant of other":    {"storeId": store, "orderItems": []any{map[string]any{"productId": vp.Body["id"], "variantId": p.vid, "quantity": 1}}},
		"variable, no var":    {"storeId": store, "orderItems": []any{map[string]any{"productId": vp.Body["id"], "quantity": 1}}},
		"unknown store":       {"storeId": 999999, "orderItems": []any{line(p, 1)}},
		"unknown customer":    {"storeId": store, "customerId": 999999, "orderItems": []any{line(p, 1)}},
		"future date":         {"storeId": store, "date": "2099-01-01", "orderItems": []any{line(p, 1)}},
		"discount > subtotal": {"storeId": store, "discount": 1000, "orderItems": []any{line(p, 1)}},
		"line discount":       {"storeId": store, "orderItems": []any{map[string]any{"variantId": p.vid, "quantity": 1, "discount": 5}}},
	}
	for name, body := range cases {
		require.Equal(t, 400, sell(t, "a", tok, body, nil).Code, name)
	}
	require.Equal(t, 3, stockOf(t, "a", store, p.vid), "nothing sold")

	over := sell(t, "a", tok, posSale(store, line(p, 4)), nil)
	require.Equal(t, 409, over.Code)
	require.Equal(t, "INSUFFICIENT_STOCK", over.Body["code"])
	short := over.Body["details"].(map[string]any)["items"].([]any)[0].(map[string]any)
	require.EqualValues(t, 3, short["available"])
	require.EqualValues(t, 4, short["requested"])

	// Duplicate lines of one variant count together.
	require.Equal(t, 409, sell(t, "a", tok, posSale(store, line(p, 2), line(p, 2)), nil).Code)

	// AddSales.jsx form: productId only (single variant) and storeId.
	back := sell(t, "a", tok, map[string]any{"reference": "REF1", "date": "", "status": "Pending", "payment_status": "Unpaid",
		"payment_method": "", "subtotal": 1, "discount": 0, "tax": 0, "shipping": 0, "total": 1, "paid": 0, "due": 1, "note": "",
		"userId": 1, "storeId": fmt.Sprint(store), "orderItems": []any{map[string]any{"productId": p.pid, "quantity": 1, "unitPrice": 120.5, "total": 120.5}}}, nil)
	require.Equal(t, 201, back.Code, back.Raw)
	require.NotEqual(t, "REF1", back.Body["reference"], "the server generates references")

	// An inactive product cannot be sold.
	_, err := admin(t).Exec(context.Background(), `UPDATE `+schema("a")+`.products SET status = 'Inactive' WHERE id = $1`, int64(p.pid))
	require.NoError(t, err)
	require.Equal(t, 400, sell(t, "a", tok, posSale(store, line(p, 1)), nil).Code)
	require.Equal(t, 200, get(t, host, "/api/sales", tok).Code)
}

func TestPriceTamperingAndOverride(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-PRICE", store, 10)
	createUser(t, "a", tok, "biller-price@iso-a.test", "biller", "biller-price-pass-1")
	btok, _ := login(t, "a", "biller-price@iso-a.test", "biller-price-pass-1")

	cheap := line(p, 1)
	cheap["unitPrice"] = 0.01
	r := sell(t, "a", btok, posSale(store, cheap), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "PRICE_CHANGED", r.Body["code"])
	cur := r.Body["details"].(map[string]any)["items"].([]any)[0].(map[string]any)
	require.Equal(t, 120.5, num(cur["unitPrice"]), "current price returned")
	require.Equal(t, 10, stockOf(t, "a", store, p.vid))

	// No unitPrice: the catalog price is used.
	noPrice := line(p, 1)
	delete(noPrice, "unitPrice")
	r = sell(t, "a", btok, posSale(store, noPrice), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, 142.19, num(r.Body["total"]))

	// The owner holds sales:price_override: the price is used, both prices
	// are kept, and the override is audited.
	over := line(p, 1)
	over["unitPrice"] = 100
	r = sell(t, "a", tok, posSale(store, over), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	it := r.Body["items"].([]any)[0].(map[string]any)
	require.Equal(t, 100.0, num(it["unit_price"]))
	require.Equal(t, 120.5, num(it["catalog_price"]))
	require.Equal(t, 118.0, num(r.Body["total"]))
	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.audit_log
		WHERE action = 'sale.price_override' AND entity_id = $1`, fmt.Sprint(r.Body["id"])).Scan(&n))
	require.Equal(t, 1, n)
}

func TestDiscountPermissionAndCap(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-DISC", store, 20)
	createUser(t, "a", tok, "biller-disc@iso-a.test", "biller", "biller-disc-pass-1")
	btok, _ := login(t, "a", "biller-disc@iso-a.test", "biller-disc-pass-1")
	noDisc := userWithPerms(t, "a", tok, "nodisc@iso-a.test", "sales:view", "sales:create")

	body := posSale(store, line(p, 1))
	body["discount"] = 12.05 // 10 % of 120.5: at the default cap
	r := sell(t, "a", btok, body, nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, 12.05, num(r.Body["discount"]))
	require.Equal(t, 127.97, num(r.Body["total"]), "discount before tax: (120.5-12.05) x 1.18")

	body["discount"] = 12.06
	require.Equal(t, 403, sell(t, "a", btok, body, nil).Code, "over the cap without price_override")
	require.Equal(t, 201, sell(t, "a", tok, body, nil).Code, "owner may exceed the cap")
	body["discount"] = 1
	require.Equal(t, 403, sell(t, "a", noDisc, body, nil).Code, "no sales:discount")
	body["discount"] = 0
	require.Equal(t, 201, sell(t, "a", noDisc, body, nil).Code)
}

func TestOversellIsImpossibleUnderConcurrency(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-RACE", store, 5)

	var wg sync.WaitGroup
	results := make(chan resp, 20)
	for range 20 {
		wg.Go(func() { results <- sell(t, "a", tok, posSale(store, line(p, 1)), nil) })
	}
	wg.Wait()
	close(results)
	var won []int
	for r := range results {
		switch r.Code {
		case 201:
			ref := r.Body["reference"].(string)
			n, err := strconv.Atoi(ref[strings.LastIndex(ref, "-")+1:])
			require.NoError(t, err)
			won = append(won, n)
		case 409:
			require.Equal(t, "INSUFFICIENT_STOCK", r.Body["code"])
		default:
			t.Fatalf("unexpected %d: %s", r.Code, r.Raw)
		}
	}
	require.Len(t, won, 5, "exactly the stock on hand was sold")
	require.Equal(t, 0, stockOf(t, "a", store, p.vid))
	sort.Ints(won)
	for i := 1; i < len(won); i++ {
		require.Equal(t, won[i-1]+1, won[i], "references are gapless")
	}
	require.Empty(t, reconcile(t, "a"))
}

func TestOppositeLockOrderDoesNotDeadlock(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	x := stockedProduct(t, "a", tok, "SALE-X", store, 100)
	y := stockedProduct(t, "a", tok, "SALE-Y", store, 100)

	var wg sync.WaitGroup
	codes := make(chan int, 24)
	for i := range 24 {
		wg.Go(func() {
			body := posSale(store, line(x, 1), line(y, 1))
			if i%2 == 1 {
				body = posSale(store, line(y, 1), line(x, 1))
			}
			codes <- sell(t, "a", tok, body, nil).Code
		})
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		require.Equal(t, 201, c)
	}
	require.Equal(t, 76, stockOf(t, "a", store, x.vid))
	require.Equal(t, 76, stockOf(t, "a", store, y.vid))
}

func TestIdempotencyKey(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-IDEM", store, 10)
	body := posSale(store, line(p, 1))
	hdr := map[string]string{"Idempotency-Key": "pos-7f9c2b1e-0001"}

	first := sell(t, "a", tok, body, hdr)
	require.Equal(t, 201, first.Code, first.Raw)
	again := sell(t, "a", tok, body, hdr)
	require.Equal(t, 201, again.Code)
	require.Equal(t, "true", again.Header.Get("Idempotent-Replayed"))
	require.Equal(t, first.Body["id"], again.Body["id"])
	require.Equal(t, 9, stockOf(t, "a", store, p.vid), "sold once")

	other := posSale(store, line(p, 2))
	require.Equal(t, 409, sell(t, "a", tok, other, hdr).Code, "same key, different body")
	require.Equal(t, 400, sell(t, "a", tok, body, map[string]string{"Idempotency-Key": strings.Repeat("k", 101)}).Code)

	// Concurrent double-submit: one sale.
	hdr2 := map[string]string{"Idempotency-Key": "pos-7f9c2b1e-0002"}
	var wg sync.WaitGroup
	ids := make(chan any, 6)
	for range 6 {
		wg.Go(func() {
			r := sell(t, "a", tok, body, hdr2)
			if r.Code == 201 {
				ids <- r.Body["id"]
			} else {
				ids <- r.Raw
			}
		})
	}
	wg.Wait()
	close(ids)
	seen := map[any]bool{}
	for id := range ids {
		seen[id] = true
	}
	require.Len(t, seen, 1, "every retry got the same sale: %v", seen)
	require.Equal(t, 8, stockOf(t, "a", store, p.vid))

	// Keys are per user.
	createUser(t, "a", tok, "biller-idem@iso-a.test", "biller", "biller-idem-pass-1")
	btok, _ := login(t, "a", "biller-idem@iso-a.test", "biller-idem-pass-1")
	r := sell(t, "a", btok, body, hdr)
	require.Equal(t, 201, r.Code)
	require.NotEqual(t, first.Body["id"], r.Body["id"])
}

func TestEditKeepsBillerAndVoidRestocks(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	stores := storeIDs(t, "a", tok)
	store := stores[0]
	p := stockedProduct(t, "a", tok, "SALE-EDIT", store, 10)
	q := stockedProduct(t, "a", tok, "SALE-EDIT2", store, 10)
	createUser(t, "a", tok, "biller-edit@iso-a.test", "biller", "biller-edit-pass-1")
	btok, _ := login(t, "a", "biller-edit@iso-a.test", "biller-edit-pass-1")

	s := sell(t, "a", btok, posSale(store, line(p, 2)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	id := s.Body["id"]
	biller := s.Body["user_id"]
	path := fmt.Sprintf("/api/sales/%v", id)
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: path, token: btok, body: posSale(store, line(p, 1))}).Code, "billers cannot edit")
	require.Equal(t, 403, do(t, rq{method: "DELETE", host: host, path: path, token: btok}).Code, "billers cannot void")

	// The catalog price changes; keeping the sale's price is not an override.
	_, err := admin(t).Exec(context.Background(), `UPDATE `+schema("a")+`.product_variants SET price = 130 WHERE id = $1`, int64(p.vid))
	require.NoError(t, err)
	e := do(t, rq{method: "PUT", host: host, path: path, token: tok, body: posSale(store, line(p, 3), line(q, 1))})
	require.Equal(t, 200, e.Code, e.Raw)
	require.Equal(t, biller, e.Body["user_id"], "original biller kept (Q-10)")
	require.NotNil(t, e.Body["updated_by"])
	require.NotEqual(t, biller, e.Body["updated_by"])
	require.EqualValues(t, 2, e.Body["version"])
	require.Equal(t, s.Body["reference"], e.Body["reference"])
	require.Equal(t, 120.5, num(e.Body["items"].([]any)[0].(map[string]any)["unit_price"]))
	require.Equal(t, 7, stockOf(t, "a", store, p.vid))
	require.Equal(t, 9, stockOf(t, "a", store, q.vid))
	require.Empty(t, reconcile(t, "a"))

	// Void: kept, stock back, hidden from lists unless asked for.
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: path + "?reason=wrong+item", token: tok}).Code)
	require.Equal(t, 10, stockOf(t, "a", store, p.vid))
	require.Equal(t, 10, stockOf(t, "a", store, q.vid))
	v := get(t, host, path, tok)
	require.Equal(t, "Void", v.Body["status"])
	require.Equal(t, "wrong item", v.Body["void_reason"])
	ref := s.Body["reference"].(string)
	require.NotContains(t, get(t, host, "/api/sales", tok).Raw, ref)
	require.Contains(t, get(t, host, "/api/sales?includeVoided=true", tok).Raw, ref)
	require.NotContains(t, get(t, host, "/api/invoices", tok).Raw, ref)
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: path, token: tok}).Code, "void twice is a no-op")
	require.Equal(t, 10, stockOf(t, "a", store, p.vid))
	require.Equal(t, 409, do(t, rq{method: "PUT", host: host, path: path, token: tok, body: posSale(store, line(p, 1))}).Code)
	require.Empty(t, reconcile(t, "a"))
	var actions []string
	rows, err := admin(t).Query(context.Background(), `SELECT action FROM `+schema("a")+`.audit_log WHERE entity = 'sale' AND entity_id = $1 ORDER BY id`, fmt.Sprint(id))
	require.NoError(t, err)
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		actions = append(actions, a)
	}
	rows.Close()
	require.Equal(t, []string{"sale.update", "sale.void"}, actions)
}

func TestOwnSalesOnlyWithoutViewAll(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "SALE-OWN", store, 10)
	createUser(t, "a", tok, "biller-own1@iso-a.test", "biller", "biller-own1-pass-1")
	createUser(t, "a", tok, "biller-own2@iso-a.test", "biller", "biller-own2-pass-1")
	b1, _ := login(t, "a", "biller-own1@iso-a.test", "biller-own1-pass-1")
	b2, _ := login(t, "a", "biller-own2@iso-a.test", "biller-own2-pass-1")
	s1 := sell(t, "a", b1, posSale(store, line(p, 1)), nil)
	s2 := sell(t, "a", b2, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s1.Code)
	require.Equal(t, 201, s2.Code)
	r1, r2 := s1.Body["reference"].(string), s2.Body["reference"].(string)

	mine := get(t, host, "/api/sales", b1)
	require.Contains(t, mine.Raw, r1)
	require.NotContains(t, mine.Raw, r2, "C-10")
	require.Equal(t, 404, get(t, host, fmt.Sprintf("/api/sales/%v", s2.Body["id"]), b1).Code)
	inv := get(t, host, "/api/invoices", b1)
	require.Contains(t, inv.Raw, r1)
	require.NotContains(t, inv.Raw, r2)

	// A custom role with sales:edit but not view_all still edits only its own.
	ed := userWithPerms(t, "a", tok, "editor-own@iso-a.test", "sales:view", "sales:edit", "sales:delete")
	require.Equal(t, 404, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/sales/%v", s1.Body["id"]), token: ed, body: posSale(store, line(p, 1))}).Code)
	require.Equal(t, 404, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", s1.Body["id"]), token: ed}).Code)

	all := get(t, host, "/api/sales?page=1&limit=200", tok)
	require.Contains(t, all.Raw, r1)
	require.Contains(t, all.Raw, r2)
}

func TestSalesIsolationAcrossTenants(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	store := storeIDs(t, "a", aTok)[0]
	p := stockedProduct(t, "a", aTok, "SALE-ISO", store, 5)
	s := sell(t, "a", aTok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s.Code)
	ref := s.Body["reference"].(string)
	ch := tenants["c"].Host

	l := get(t, ch, "/api/sales", cTok)
	require.Equal(t, 200, l.Code)
	require.NotContains(t, l.Raw, ref)
	require.NotContains(t, get(t, ch, "/api/invoices", cTok).Raw, ref)
	var cHas bool
	for _, it := range list(t, l) {
		cHas = cHas || it["id"] == s.Body["id"]
	}
	if !cHas {
		path := fmt.Sprintf("/api/sales/%v", s.Body["id"])
		require.Equal(t, 404, get(t, ch, path, cTok).Code)
		require.Equal(t, 404, do(t, rq{method: "PUT", host: ch, path: path, token: cTok, body: posSale(store, line(p, 1))}).Code)
		require.Equal(t, 404, do(t, rq{method: "DELETE", host: ch, path: path, token: cTok}).Code)
	}
	// A's variant and store IDs mean nothing on C.
	require.Equal(t, 400, sell(t, "c", cTok, posSale(storeIDs(t, "c", cTok)[0], map[string]any{"variantId": p.vid + 100000, "quantity": 1}), nil).Code)
	require.Equal(t, 4, stockOf(t, "a", store, p.vid), "A's stock untouched")
	// A's token is refused on C's host for every sales route (the route walk
	// in TestEveryRouteRejectsAnotherTenantsToken covers these too).
	require.Equal(t, 401, sell(t, "c", aTok, posSale(store, line(p, 1)), nil).Code)
	require.Equal(t, "Void", func() any {
		require.Equal(t, 204, do(t, rq{method: "DELETE", host: tenants["a"].Host, path: fmt.Sprintf("/api/sales/%v", s.Body["id"]), token: aTok}).Code)
		return get(t, tenants["a"].Host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), aTok).Body["status"]
	}())
}
