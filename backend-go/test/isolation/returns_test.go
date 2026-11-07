//go:build integration

package isolation

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// retBody is what AddSalesReturn.jsx posts: IDs as strings (FormData), a
// client reference and client totals, which the server ignores.
func retBody(sale any, store float64, items ...map[string]any) map[string]any {
	b := map[string]any{"reference": "RET123456", "date": time.Now().Format("2006-01-02"), "status": "Completed",
		"payment_status": "Paid", "total": 1, "paid": 0, "due": 0, "storeId": fmt.Sprint(store), "saleId": nil,
		"returnItems": items}
	if sale != nil {
		b["saleId"] = fmt.Sprint(sale)
	}
	return b
}

// retItem returns qty of p with a tampered unit price and total.
func retItem(p stocked, qty any) map[string]any {
	return map[string]any{"productId": fmt.Sprint(p.pid), "variantId": nil, "quantity": qty, "unitPrice": 9999,
		"reason": "damaged", "total": 9999}
}

func postReturn(t *testing.T, l, tok string, body map[string]any, hdr map[string]string) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/sales/returns", token: tok, body: body, header: hdr})
}

func dec(v any) decimal.Decimal { return decimal.NewFromFloat(num(v)) }

func lineOf(t *testing.T, sale map[string]any, p stocked) map[string]any {
	t.Helper()
	for _, it := range sale["items"].([]any) {
		if m := it.(map[string]any); m["variant_id"] == p.vid {
			return m
		}
	}
	t.Fatalf("no line for %s", p.sku)
	return nil
}

func errField(r resp) string {
	if f, ok := r.Body["fields"].(map[string]any); ok {
		for k := range f {
			return k
		}
	}
	return ""
}

func TestReturnsAreCumulativeAndRefundTheSoldAmount(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RET-CUM-P", store, 10)
	q := stockedProduct(t, "a", tok, "RET-CUM-Q", store, 10)
	sb := posSale(store, line(p, 3), line(q, 1))
	sb["discount"] = 10
	s := sell(t, "a", tok, sb, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	saleID := s.Body["id"]
	pl := lineOf(t, s.Body, p)
	require.Equal(t, 7, stockOf(t, "a", store, p.vid))

	// One unit, paid nothing yet: the refund is a third of what the line
	// cost (after its share of the discount, with tax). 9999 is ignored.
	r1 := postReturn(t, "a", tok, retBody(saleID, store, retItem(p, "1")), nil)
	require.Equal(t, 201, r1.Code, r1.Raw)
	b1 := r1.Body
	require.Regexp(t, `^RET-\d{6}$`, b1["reference"], "server reference, client RET123456 ignored")
	third := dec(pl["subtotal"]).Div(decimal.NewFromInt(3)).Round(2)
	require.True(t, third.Equal(dec(b1["total"])), "refund %v, want %v", b1["total"], third)
	require.True(t, dec(pl["tax_amount"]).Div(decimal.NewFromInt(3)).Round(2).Equal(dec(b1["tax"])))
	require.Equal(t, "Unpaid", b1["payment_status"], "paid 0 is kept")
	require.Equal(t, "Unpaid", b1["paymentStatus"])
	require.Equal(t, num(b1["total"]), num(b1["due"]))
	require.Equal(t, s.Body["customer_id"], b1["customer_id"], "customer from the sale")
	require.Equal(t, saleID, b1["sale_id"])
	require.Equal(t, s.Body["reference"], b1["saleReference"])
	it := b1["items"].([]any)[0].(map[string]any)
	require.Equal(t, 120.5, num(it["unit_price"]), "the sold price")
	require.Equal(t, 120.5, num(it["unitPrice"]))
	require.Equal(t, pl["id"], it["order_item_id"])
	require.Equal(t, "damaged", it["reason"])
	require.Equal(t, 8, stockOf(t, "a", store, p.vid))
	var reason, refType string
	var delta int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT reason, ref_type, delta FROM `+schema("a")+`.stock_movements
		WHERE ref_type = 'sale_return' AND ref_id = $1`, int64(num(b1["id"]))).Scan(&reason, &refType, &delta))
	require.Equal(t, "return", reason)
	require.Equal(t, 1, delta)

	// The other two, paid in full (no paid field): both parts add up to
	// exactly what the line cost, tax and CGST/SGST included.
	body := retBody(saleID, store, retItem(p, 2))
	delete(body, "paid")
	r2 := postReturn(t, "a", tok, body, nil)
	require.Equal(t, 201, r2.Code, r2.Raw)
	require.Equal(t, "Paid", r2.Body["payment_status"])
	require.True(t, dec(b1["total"]).Add(dec(r2.Body["total"])).Equal(dec(pl["subtotal"])), "parts add up to the line")
	require.True(t, dec(b1["tax"]).Add(dec(r2.Body["tax"])).Equal(dec(pl["tax_amount"])))
	cgst := func(r resp) decimal.Decimal { return dec(r.Body["items"].([]any)[0].(map[string]any)["cgst"]) }
	require.True(t, cgst(r1).Add(cgst(r2)).Equal(dec(pl["cgst"])))
	require.Equal(t, 10, stockOf(t, "a", store, p.vid))

	// A fourth unit was never sold.
	r3 := postReturn(t, "a", tok, retBody(saleID, store, retItem(p, 1)), nil)
	require.Equal(t, 409, r3.Code, r3.Raw)
	require.Equal(t, "RETURN_EXCEEDS_SOLD", r3.Body["code"])
	d := r3.Body["details"].(map[string]any)["items"].([]any)[0].(map[string]any)
	require.Equal(t, []float64{3, 3, 1}, []float64{num(d["sold"]), num(d["returned"]), num(d["requested"])})
	require.Equal(t, 10, stockOf(t, "a", store, p.vid))

	// The sale shows what came back, and can no longer be edited or voided.
	sale := get(t, host, fmt.Sprintf("/api/sales/%v", saleID), tok)
	require.Equal(t, 3.0, num(lineOf(t, sale.Body, p)["returnedQty"]))
	require.Equal(t, 0.0, num(lineOf(t, sale.Body, q)["returnedQty"]))
	path := fmt.Sprintf("/api/sales/%v", saleID)
	ed := do(t, rq{method: "PUT", host: host, path: path, token: tok, body: posSale(store, line(q, 1))})
	require.Equal(t, 409, ed.Code, ed.Raw)
	require.Equal(t, "INVALID_STATE", ed.Body["code"])
	require.Equal(t, 409, do(t, rq{method: "DELETE", host: host, path: path, token: tok}).Code)
	require.Equal(t, 9, stockOf(t, "a", store, q.vid), "nothing restocked twice")

	// The list keeps the old fields and has what SalesReturnList.jsx reads.
	l := get(t, host, fmt.Sprintf("/api/sales/returns?saleId=%v", saleID), tok)
	require.Equal(t, 200, l.Code, l.Raw)
	rows := list(t, l)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "Walk-in Customer", row["customer"].(map[string]any)["name"])
		require.Equal(t, "Walk-in Customer", row["customerName"])
		require.Contains(t, row["product"].(map[string]any)["name"], "RET-CUM-P")
		require.NotEmpty(t, row["productName"])
		require.Contains(t, row, "paymentStatus")
		require.EqualValues(t, 1, row["itemsCount"])
	}
	require.Empty(t, reconcile(t, "a"))
}

func TestReturnValidationOwnOnlyAndUnlinked(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RET-VAL-P", store, 10)
	q := stockedProduct(t, "a", tok, "RET-VAL-Q", store, 10)
	s := sell(t, "a", tok, posSale(store, line(p, 2)), nil)
	require.Equal(t, 201, s.Code)
	saleID := s.Body["id"]
	other := sell(t, "a", tok, posSale(store, line(q, 1)), nil)
	require.Equal(t, 201, other.Code)
	otherLine := lineOf(t, other.Body, q)["id"]

	bad := map[string]struct {
		body  map[string]any
		field string
	}{
		"negative quantity": {retBody(saleID, store, retItem(p, -1)), "returnItems[0].quantity"},
		"zero quantity":     {retBody(saleID, store, retItem(p, 0)), "returnItems[0].quantity"},
		"not on the sale":   {retBody(saleID, store, retItem(q, 1)), "returnItems[0].productId"},
		"another store":     {retBody(saleID, store+1000, retItem(p, 1)), "storeId"},
		"another customer": {func() map[string]any {
			b := retBody(saleID, store, retItem(p, 1))
			b["customerId"] = "999999"
			return b
		}(), "customerId"},
		"unknown sale": {retBody(99999999, store, retItem(p, 1)), "saleId"},
		"no items":     {retBody(saleID, store), "returnItems"},
		"another sale's line": {retBody(saleID, store, map[string]any{"orderItemId": otherLine, "quantity": 1}),
			"returnItems[0].orderItemId"},
	}
	for name, c := range bad {
		r := postReturn(t, "a", tok, c.body, nil)
		require.Equal(t, 400, r.Code, "%s: %s", name, r.Raw)
		require.Equal(t, c.field, errField(r), "%s: %s", name, r.Raw)
	}
	require.Equal(t, 400, postReturn(t, "a", tok, retBody(saleID, store, retItem(p, 1.5)), nil).Code, "fractional quantity")
	require.Equal(t, 8, stockOf(t, "a", store, p.vid), "nothing returned")

	// A voided sale cannot be returned.
	v := sell(t, "a", tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", v.Body["id"]), token: tok}).Code)
	vr := postReturn(t, "a", tok, retBody(v.Body["id"], store, retItem(p, 1)), nil)
	require.Equal(t, 409, vr.Code)
	require.Equal(t, "INVALID_STATE", vr.Body["code"])

	// Without a sale: the owner (price override) sets the price; no tax.
	ub := retBody(nil, store, map[string]any{"productId": fmt.Sprint(q.pid), "variantId": fmt.Sprint(q.vid), "quantity": 2, "unitPrice": 50})
	u := postReturn(t, "a", tok, ub, nil)
	require.Equal(t, 201, u.Code, u.Raw)
	require.Equal(t, 100.0, num(u.Body["total"]))
	require.Equal(t, 0.0, num(u.Body["tax"]))
	require.Nil(t, u.Body["sale_id"])
	require.Nil(t, u.Body["items"].([]any)[0].(map[string]any)["order_item_id"])
	require.Equal(t, 11, stockOf(t, "a", store, q.vid))
	np := postReturn(t, "a", tok, retBody(nil, store, map[string]any{"variantId": q.vid, "quantity": 1}), nil)
	require.Equal(t, 400, np.Code)
	require.Equal(t, "returnItems[0].unitPrice", errField(np))

	// A clerk without sales:view_all or price override returns only own sales.
	clerk := userWithPerms(t, "a", tok, "returns-clerk@iso-a.test", "sales:view", "sales:create", "sales_returns:view", "sales_returns:create")
	own := sell(t, "a", clerk, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, own.Code, own.Raw)
	require.Equal(t, 201, postReturn(t, "a", clerk, retBody(own.Body["id"], store, retItem(p, 1)), nil).Code)
	notMine := postReturn(t, "a", clerk, retBody(saleID, store, retItem(p, 1)), nil)
	require.Equal(t, 400, notMine.Code, "another biller's sale is not found (C-10)")
	require.Equal(t, "saleId", errField(notMine))
	require.Equal(t, 403, postReturn(t, "a", clerk, ub, nil).Code, "no sale needs the price override")
	biller := createUser(t, "a", tok, "returns-biller@iso-a.test", "biller", "returns-biller-pass-1")
	_ = biller
	btok, _ := login(t, "a", "returns-biller@iso-a.test", "returns-biller-pass-1")
	require.Equal(t, 403, postReturn(t, "a", btok, retBody(saleID, store, retItem(p, 1)), nil).Code, "billers have no sales_returns:create")
	require.Equal(t, 403, get(t, host, "/api/sales/returns", btok).Code)
	require.Empty(t, reconcile(t, "a"))
}

func TestReturnIdempotencyKey(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RET-IDEM", store, 10)
	s := sell(t, "a", tok, posSale(store, line(p, 3)), map[string]string{"Idempotency-Key": "ret-idem-sale-0001"})
	require.Equal(t, 201, s.Code)
	body := retBody(s.Body["id"], store, retItem(p, 1))
	hdr := map[string]string{"Idempotency-Key": "ret-idem-0001"}

	first := postReturn(t, "a", tok, body, hdr)
	require.Equal(t, 201, first.Code, first.Raw)
	again := postReturn(t, "a", tok, body, hdr)
	require.Equal(t, 201, again.Code)
	require.Equal(t, "true", again.Header.Get("Idempotent-Replayed"))
	require.Equal(t, first.Body["id"], again.Body["id"])
	require.Equal(t, 8, stockOf(t, "a", store, p.vid), "returned once")
	require.Equal(t, 409, postReturn(t, "a", tok, retBody(s.Body["id"], store, retItem(p, 2)), hdr).Code, "same key, different body")
	require.Equal(t, 409, postReturn(t, "a", tok, body, map[string]string{"Idempotency-Key": "ret-idem-sale-0001"}).Code,
		"a sale's key never replays as a return")
	require.Equal(t, 8, stockOf(t, "a", store, p.vid))
}

func TestConcurrentReturnsNeverExceedWhatWasSold(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RET-RACE", store, 5)
	s := sell(t, "a", tok, posSale(store, line(p, 5)), nil)
	require.Equal(t, 201, s.Code)
	require.Equal(t, 0, stockOf(t, "a", store, p.vid))

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	var refs []string
	for range 12 {
		wg.Go(func() {
			r := postReturn(t, "a", tok, retBody(s.Body["id"], store, retItem(p, 1)), nil)
			mu.Lock()
			defer mu.Unlock()
			codes[r.Code]++
			if r.Code == 201 {
				refs = append(refs, r.Body["reference"].(string))
			}
		})
	}
	wg.Wait()
	require.Equal(t, map[int]int{201: 5, 409: 7}, codes, "exactly the 5 sold units come back")
	require.Equal(t, 5, stockOf(t, "a", store, p.vid))
	nums := make([]int, len(refs))
	for i, r := range refs {
		nums[i], _ = strconv.Atoi(regexp.MustCompile(`\d+$`).FindString(r))
	}
	sort.Ints(nums)
	for i := 1; i < len(nums); i++ {
		require.Equal(t, nums[i-1]+1, nums[i], "return numbers are consecutive: %v", refs)
	}
	require.Empty(t, reconcile(t, "a"))
}

func TestReturnsIsolationAcrossTenants(t *testing.T) {
	ctx := context.Background()
	adm := admin(t)
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	ch := tenants["c"].Host
	// A's next sale and order line IDs do not exist in C.
	for _, tbl := range []string{"sales", "order_items"} {
		_, err := adm.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1, 'id'), GREATEST((SELECT coalesce(max(id), 0) FROM `+
			schema("a")+`.`+tbl+`), 700000))`, schema("a")+"."+tbl)
		require.NoError(t, err)
	}
	aStore := storeIDs(t, "a", aTok)[0]
	p := stockedProduct(t, "a", aTok, "RET-ISO-A", aStore, 5)
	s := sell(t, "a", aTok, posSale(aStore, line(p, 2)), nil)
	require.Equal(t, 201, s.Code)
	aLine := lineOf(t, s.Body, p)["id"]
	var inC int
	require.NoError(t, adm.QueryRow(ctx, `SELECT (SELECT count(*) FROM `+schema("c")+`.sales WHERE id = $1) +
		(SELECT count(*) FROM `+schema("c")+`.order_items WHERE id = $2)`, int64(num(s.Body["id"])), int64(num(aLine))).Scan(&inC))
	require.Zero(t, inC)

	cStore := storeIDs(t, "c", cTok)[0]
	cp := stockedProduct(t, "c", cTok, "RET-ISO-C", cStore, 5)
	cs := sell(t, "c", cTok, posSale(cStore, line(cp, 1)), nil)
	require.Equal(t, 201, cs.Code)

	// A's sale, A's sold line and A's variant mean nothing on C.
	r := postReturn(t, "c", cTok, retBody(s.Body["id"], cStore, retItem(p, 1)), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "saleId", errField(r))
	r = postReturn(t, "c", cTok, retBody(cs.Body["id"], cStore, map[string]any{"orderItemId": aLine, "quantity": 1}), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "returnItems[0].orderItemId", errField(r))
	r = postReturn(t, "c", cTok, retBody(nil, cStore, map[string]any{"variantId": p.vid + 100000, "quantity": 1, "unitPrice": 1}), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, 3, stockOf(t, "a", aStore, p.vid), "A's stock untouched")

	// A's returns never show on C, and A's token is refused on C's host.
	ar := postReturn(t, "a", aTok, retBody(s.Body["id"], aStore, map[string]any{"productId": p.pid, "quantity": 1,
		"reason": "iso-a-private-reason"}), nil)
	require.Equal(t, 201, ar.Code, ar.Raw)
	cl := get(t, ch, "/api/sales/returns", cTok)
	require.Equal(t, 200, cl.Code)
	require.NotContains(t, cl.Raw, "iso-a-private-reason")
	require.NotContains(t, cl.Raw, "RET-ISO-A")
	require.Empty(t, list(t, get(t, ch, fmt.Sprintf("/api/sales/returns?saleId=%v", s.Body["id"]), cTok)))
	require.Equal(t, 401, postReturn(t, "c", aTok, retBody(s.Body["id"], aStore, retItem(p, 1)), nil).Code)
	require.Equal(t, 401, get(t, ch, "/api/sales/returns", aTok).Code)
	var aReturns int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.sale_returns WHERE sale_id = $1`, int64(num(s.Body["id"]))).Scan(&aReturns))
	require.Equal(t, 1, aReturns)
}
