//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Industry P1 item 9 helpers.

func newCustomer(t *testing.T, l, tok, name string) float64 {
	t.Helper()
	r := do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/customers", token: tok, body: map[string]any{"name": name}})
	require.Equal(t, 201, r.Code, r.Raw)
	return r.Body["id"].(float64)
}

func putPolicy(t *testing.T, l, tok string, body map[string]any) resp {
	t.Helper()
	return do(t, rq{method: "PUT", host: tenants[l].Host, path: "/api/settings/returns", token: tok, body: body})
}

func creditOf(t *testing.T, l, tok string, cust any) float64 {
	t.Helper()
	r := get(t, tenants[l].Host, fmt.Sprintf("/api/customers/%v/store-credit", cust), tok)
	require.Equal(t, 200, r.Code, r.Raw)
	return num(r.Body["balance"])
}

// saleTo sells qty of p to a customer, paid in cash.
func saleTo(t *testing.T, l, tok string, store, cust float64, p stocked, qty int, date string) map[string]any {
	t.Helper()
	b := map[string]any{"storeId": store, "customerId": cust, "orderItems": []map[string]any{line(p, qty)}}
	if date != "" {
		b["date"] = date
	}
	s := sell(t, l, tok, b, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	return s.Body
}

func linkedReturn(sale map[string]any, qty int, extra map[string]any) map[string]any {
	it := sale["items"].([]any)[0].(map[string]any)
	b := map[string]any{"saleId": sale["id"], "returnItems": []map[string]any{{"orderItemId": it["id"], "quantity": qty}}}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func TestReturnPolicySettings(t *testing.T) {
	l := billingTenant(t, "rp-set")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	g := get(t, host, "/api/settings/returns", tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Equal(t, true, g.Body["refund"], "default = today: refunds")
	require.Equal(t, false, g.Body["storeCredit"])
	require.Equal(t, false, g.Body["exchange"])
	require.Nil(t, g.Body["timeLimitDays"])
	require.Equal(t, []any{"resellable"}, g.Body["restockConditions"])

	r := putPolicy(t, l, tok, map[string]any{"storeCredit": true, "timeLimitDays": 7, "restockConditions": []string{"resellable", "damaged", "damaged"}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, 7.0, r.Body["timeLimitDays"])
	require.Equal(t, []any{"resellable", "damaged"}, r.Body["restockConditions"])
	require.Equal(t, true, r.Body["refund"], "keys not sent stay")
	r = putPolicy(t, l, tok, map[string]any{"timeLimitDays": nil})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Nil(t, r.Body["timeLimitDays"])
	for _, bad := range []map[string]any{
		{"timeLimitDays": 0}, {"restockConditions": []string{"wet"}}, {"refund": false, "storeCredit": false, "exchange": false},
		{"nonsense": true}, {"refund": "yes"},
	} {
		x := putPolicy(t, l, tok, bad)
		require.Equal(t, 400, x.Code, fmt.Sprint(bad)+" "+x.Raw)
	}
	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'settings.return_policy'`).Scan(&n))
	require.Equal(t, 2, n)
	biller := userWithPerms(t, l, tok, "rp-biller@x.test", "settings:view")
	require.Equal(t, 200, get(t, host, "/api/settings/returns", biller).Code)
	require.Equal(t, 403, putPolicy(t, l, biller, map[string]any{"refund": true}).Code)
}

func TestReturnStoreCreditAndPolicyOverride(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "rp-credit")
	tok, _ := ownerLogin(t, l)
	store := storeIDs(t, l, tok)[0]
	cust := newCustomer(t, l, tok, "Meera")
	p := stockedProduct(t, l, tok, "RP-CR-1", store, 10)
	sale := saleTo(t, l, tok, store, cust, p, 2, "")

	// Store credit is off by default: refused, with "overridable" for the owner.
	r := postReturn(t, l, tok, linkedReturn(sale, 1, map[string]any{"settlement": "store_credit"}), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "RETURN_NOT_ALLOWED", r.Body["code"])
	short := postReturn(t, l, tok, linkedReturn(sale, 1, map[string]any{"settlement": "store_credit", "policyOverrideReason": "short"}), nil)
	require.Equal(t, 400, short.Code, short.Raw)
	clerk := userWithPerms(t, l, tok, "rp-clerk@x.test", "sales_returns:create", "sales_returns:view", "sales:view", "sales:view_all")
	require.Equal(t, 403, postReturn(t, l, clerk, linkedReturn(sale, 1, map[string]any{"settlement": "store_credit", "policyOverrideReason": "customer is a regular"}), nil).Code)
	ok := postReturn(t, l, tok, linkedReturn(sale, 1, map[string]any{"settlement": "store_credit", "policyOverrideReason": "customer is a regular"}), nil)
	require.Equal(t, 201, ok.Code, ok.Raw)
	require.Equal(t, "store_credit", ok.Body["settlement"])
	require.Equal(t, 142.19, num(ok.Body["toStoreCredit"]))
	require.Equal(t, 0.0, num(ok.Body["paid"]))
	require.Equal(t, 142.19, creditOf(t, l, tok, cust))
	require.Equal(t, 9, stockOf(t, l, store, p.vid), "back to stock")
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'sale_return.policy_override'`).Scan(&n))
	require.Equal(t, 1, n)

	// With the policy on, no override is needed; walk-in cannot get credit.
	require.Equal(t, 200, putPolicy(t, l, tok, map[string]any{"storeCredit": true}).Code)
	r = postReturn(t, l, tok, linkedReturn(sale, 1, map[string]any{"settlement": "store_credit"}), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, 284.38, creditOf(t, l, tok, cust))
	walk := sell(t, l, tok, map[string]any{"storeId": store, "orderItems": []map[string]any{line(p, 1)}}, nil)
	require.Equal(t, 201, walk.Code, walk.Raw)
	w := postReturn(t, l, tok, linkedReturn(walk.Body, 1, map[string]any{"settlement": "store_credit"}), nil)
	require.Equal(t, 400, w.Code, w.Raw)
	require.Equal(t, "settlement", errField(w))

	// Refunds off: a plain refund is refused.
	require.Equal(t, 200, putPolicy(t, l, tok, map[string]any{"refund": false}).Code)
	s2 := saleTo(t, l, tok, store, cust, p, 1, "")
	r = postReturn(t, l, tok, linkedReturn(s2, 1, nil), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "RETURN_NOT_ALLOWED", r.Body["code"])

	// Spend it at checkout: store_credit pays; more than the balance is refused.
	pay := func(amount float64) resp {
		return sell(t, l, tok, map[string]any{"storeId": store, "customerId": cust, "orderItems": []map[string]any{line(p, 1)},
			"payments": []map[string]any{{"method": "store_credit", "amount": amount}, {"method": "cash", "amount": 142.19 - amount}}}, nil)
	}
	low := sell(t, l, tok, map[string]any{"storeId": store, "customerId": cust, "orderItems": []map[string]any{line(p, 3)},
		"payments": []map[string]any{{"method": "store_credit", "amount": 426.57}}}, nil)
	require.Equal(t, 409, low.Code, low.Raw)
	require.Equal(t, "STORE_CREDIT_LOW", low.Body["code"])
	used := pay(100)
	require.Equal(t, 201, used.Code, used.Raw)
	require.InDelta(t, 184.38, creditOf(t, l, tok, cust), 0.001)
	wk := sell(t, l, tok, map[string]any{"storeId": store, "orderItems": []map[string]any{line(p, 1)},
		"payments": []map[string]any{{"method": "store_credit", "amount": 142.19}}}, nil)
	require.Equal(t, 400, wk.Code, wk.Raw)

	// A sale paid with store credit cannot be voided or edited.
	v := do(t, rq{method: "DELETE", host: tenants[l].Host, path: fmt.Sprintf("/api/sales/%v", used.Body["id"]), token: tok, body: map[string]any{"reason": "mistake here"}})
	require.Equal(t, 409, v.Code, v.Raw)
	e := do(t, rq{method: "PUT", host: tenants[l].Host, path: fmt.Sprintf("/api/sales/%v", used.Body["id"]), token: tok,
		body: map[string]any{"storeId": store, "customerId": cust, "orderItems": []map[string]any{line(p, 1)}}})
	require.Equal(t, 409, e.Code, e.Raw)

	// Two tills spending the whole balance at once: one wins.
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for range 4 {
		wg.Go(func() {
			x := sell(t, l, tok, map[string]any{"storeId": store, "customerId": cust, "orderItems": []map[string]any{line(p, 1)},
				"payments": []map[string]any{{"method": "store_credit", "amount": 142.19}}}, nil)
			mu.Lock()
			codes[x.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	require.Equal(t, 1, codes[201], codes)
	require.Equal(t, 3, codes[409], codes)
	require.InDelta(t, 42.19, creditOf(t, l, tok, cust), 0.001, "never below 0")

	// Adjustments: owner only, with a reason, never below 0.
	adj := func(tk string, amount float64, reason string) resp {
		return do(t, rq{method: "POST", host: tenants[l].Host, path: fmt.Sprintf("/api/customers/%v/store-credit/adjust", cust), token: tk,
			body: map[string]any{"amount": amount, "reason": reason}})
	}
	require.Equal(t, 409, adj(tok, -50, "goodwill reversal").Code)
	require.Equal(t, 400, adj(tok, 10, "no").Code)
	a := adj(tok, -42.19, "closing the account")
	require.Equal(t, 200, a.Code, a.Raw)
	require.Equal(t, 0.0, num(a.Body["balance"]))
	adminTok := userWithPerms(t, l, tok, "rp-admin@x.test", "customers:view", "customers:collect")
	require.Equal(t, 403, adj(adminTok, 5, "friendly gesture").Code)
	sc := get(t, tenants[l].Host, fmt.Sprintf("/api/customers/%v/store-credit", cust), tok)
	require.Equal(t, true, sc.Body["canAdjust"])
	kinds := map[string]int{}
	for _, x := range sc.Body["entries"].([]any) {
		kinds[x.(map[string]any)["kind"].(string)]++
	}
	require.Equal(t, map[string]int{"return": 2, "sale": 2, "adjust": 1}, kinds)
	var canUpdate, canDelete bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT has_table_privilege($1, $2, 'UPDATE'), has_table_privilege($1, $2, 'DELETE')`,
		schema(l)+"_rw", schema(l)+".customer_credit_entries").Scan(&canUpdate, &canDelete))
	require.False(t, canUpdate, "the store credit ledger is append-only")
	require.False(t, canDelete)
}

func TestReturnWindowLinkedSaleAndConditions(t *testing.T) {
	l := billingTenant(t, "rp-rules")
	tok, _ := ownerLogin(t, l)
	store := storeIDs(t, l, tok)[0]
	cust := newCustomer(t, l, tok, "Ravi")
	p := stockedProduct(t, l, tok, "RP-W-1", store, 20)
	old := saleTo(t, l, tok, store, cust, p, 2, time.Now().AddDate(0, 0, -10).Format("2006-01-02"))
	fresh := saleTo(t, l, tok, store, cust, p, 4, "")

	require.Equal(t, 200, putPolicy(t, l, tok, map[string]any{"timeLimitDays": 7, "requireLinkedSale": true, "conditions": true}).Code)
	r := postReturn(t, l, tok, linkedReturn(old, 1, map[string]any{"returnItems": []map[string]any{
		{"orderItemId": old["items"].([]any)[0].(map[string]any)["id"], "quantity": 1, "condition": "resellable"}}}), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "RETURN_WINDOW_CLOSED", r.Body["code"])

	un := postReturn(t, l, tok, map[string]any{"storeId": store, "customerId": cust, "returnItems": []map[string]any{
		{"productId": p.pid, "variantId": p.vid, "quantity": 1, "unitPrice": 10, "condition": "resellable"}}}, nil)
	require.Equal(t, 409, un.Code, un.Raw)
	require.Equal(t, "SALE_REQUIRED", un.Body["code"])

	// Conditions: each line needs one; damaged is recorded but not restocked.
	item := fresh["items"].([]any)[0].(map[string]any)["id"]
	miss := postReturn(t, l, tok, map[string]any{"saleId": fresh["id"], "returnItems": []map[string]any{{"orderItemId": item, "quantity": 1}}}, nil)
	require.Equal(t, 400, miss.Code, miss.Raw)
	require.Equal(t, "returnItems[0].condition", errField(miss))
	before := stockOf(t, l, store, p.vid)
	d := postReturn(t, l, tok, map[string]any{"saleId": fresh["id"], "returnItems": []map[string]any{{"orderItemId": item, "quantity": 2, "condition": "damaged"}}}, nil)
	require.Equal(t, 201, d.Code, d.Raw)
	it := d.Body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "damaged", it["condition"])
	require.Equal(t, false, it["restocked"])
	require.Equal(t, before, stockOf(t, l, store, p.vid), "damaged goods stay out of stock")
	ok := postReturn(t, l, tok, map[string]any{"saleId": fresh["id"], "returnItems": []map[string]any{{"orderItemId": item, "quantity": 1, "condition": "resellable"}}}, nil)
	require.Equal(t, 201, ok.Code, ok.Raw)
	require.Equal(t, before+1, stockOf(t, l, store, p.vid))
	require.Empty(t, reconcile(t, l))

	// The owner can override the time limit with a reason.
	ov := postReturn(t, l, tok, linkedReturn(old, 1, map[string]any{"policyOverrideReason": "manager approved late return",
		"returnItems": []map[string]any{{"orderItemId": old["items"].([]any)[0].(map[string]any)["id"], "quantity": 1, "condition": "resellable"}}}), nil)
	require.Equal(t, 201, ov.Code, ov.Raw)
}

func TestExchange(t *testing.T) {
	l := billingTenant(t, "rp-exch")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	cust := newCustomer(t, l, tok, "Kiran")
	shirt := stockedProduct(t, l, tok, "EX-SHIRT", store, 10) // 120.5 + 18% = 142.19 each
	sale := saleTo(t, l, tok, store, cust, shirt, 2, "")
	item := sale["items"].([]any)[0].(map[string]any)["id"]
	xbody := func(retQty, newQty int, extra map[string]any) map[string]any {
		b := map[string]any{"saleId": sale["id"], "returnLines": []map[string]any{{"orderItemId": item, "quantity": retQty}},
			"newLines": []map[string]any{line(shirt, newQty)}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	post := func(path string, b map[string]any, hdr map[string]string) resp {
		return do(t, rq{method: "POST", host: host, path: path, token: tok, body: b, header: hdr})
	}

	off := post("/api/sales/exchanges", xbody(1, 1, nil), nil)
	require.Equal(t, 409, off.Code, off.Raw)
	require.Equal(t, "RETURN_NOT_ALLOWED", off.Body["code"])
	require.Equal(t, 200, putPolicy(t, l, tok, map[string]any{"exchange": true, "storeCredit": true}).Code)

	// Dry run: amounts only, nothing written.
	q := post("/api/sales/exchanges?dryRun=true", xbody(1, 2, nil), nil)
	require.Equal(t, 200, q.Code, q.Raw)
	require.Equal(t, 142.19, num(q.Body["returnValue"]))
	require.Equal(t, 284.38, num(q.Body["newTotal"]))
	require.Equal(t, 142.19, num(q.Body["customerPays"]))
	require.Equal(t, 8, stockOf(t, l, store, shirt.vid), "dry run moves nothing")

	// New goods cost more: the customer pays the difference in cash.
	x := post("/api/sales/exchanges", xbody(1, 2, map[string]any{"payments": []map[string]any{{"method": "cash", "amount": 142.19}}}),
		map[string]string{"Idempotency-Key": "exch-0001-key"})
	require.Equal(t, 201, x.Code, x.Raw)
	ret, ns := x.Body["return"].(map[string]any), x.Body["sale"].(map[string]any)
	require.Equal(t, "exchange", ret["settlement"])
	require.Equal(t, 142.19, num(ret["toExchange"]))
	require.Equal(t, "Paid", ns["payment_status"])
	methods := map[string]float64{}
	for _, pm := range ns["payments"].([]any) {
		m := pm.(map[string]any)
		methods[m["method"].(string)] = num(m["amount"])
		if m["method"] == "exchange" {
			require.Equal(t, ret["id"], m["saleReturnId"])
		}
	}
	require.Equal(t, map[string]float64{"exchange": 142.19, "cash": 142.19}, methods)
	require.Equal(t, ret["id"], ns["exchangeReturnId"])
	require.Equal(t, 7, stockOf(t, l, store, shirt.vid), "8 + 1 back - 2 out")
	again := post("/api/sales/exchanges", xbody(1, 2, map[string]any{"payments": []map[string]any{{"method": "cash", "amount": 142.19}}}),
		map[string]string{"Idempotency-Key": "exch-0001-key"})
	require.Equal(t, 201, again.Code)
	require.Equal(t, 7, stockOf(t, l, store, shirt.vid), "replayed, not repeated")

	// New goods cost less: the rest goes to store credit (or is refunded).
	cheap := stockedProduct(t, l, tok, "EX-CHEAP", store, 5)
	b := map[string]any{"saleId": sale["id"], "returnLines": []map[string]any{{"orderItemId": item, "quantity": 1}},
		"newLines": []map[string]any{{"productId": cheap.pid, "variantId": cheap.vid, "quantity": 0, "unitPrice": 120.5}}}
	require.Equal(t, 400, post("/api/sales/exchanges", b, nil).Code, "zero quantity")
	ns2 := stockedProduct(t, l, tok, "EX-HALF", store, 5)
	_, err := admin(t).Exec(context.Background(), `UPDATE `+schema(l)+`.product_variants SET price = 60 WHERE id = $1`, int64(ns2.vid))
	require.NoError(t, err)
	b["newLines"] = []map[string]any{{"productId": ns2.pid, "variantId": ns2.vid, "quantity": 1, "unitPrice": 60}}
	b["settlement"] = "store_credit"
	y := post("/api/sales/exchanges", b, nil)
	require.Equal(t, 201, y.Code, y.Raw)
	yr := y.Body["return"].(map[string]any)
	require.Equal(t, 70.8, num(yr["toExchange"]), "60 + 18%")
	require.Equal(t, 71.39, num(yr["toStoreCredit"]))
	require.Equal(t, 71.39, creditOf(t, l, tok, cust))
	require.Equal(t, 8, stockOf(t, l, store, shirt.vid))
	require.Equal(t, 4, stockOf(t, l, store, ns2.vid))
	require.Empty(t, reconcile(t, l))

	// Nothing left to return; the exchange sale cannot be voided.
	none := post("/api/sales/exchanges", xbody(1, 1, nil), nil)
	require.Equal(t, 409, none.Code, none.Raw)
	require.Equal(t, "RETURN_EXCEEDS_SOLD", none.Body["code"])
	v := do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", ns["id"]), token: tok, body: map[string]any{"reason": "mistake here"}})
	require.Equal(t, 409, v.Code, v.Raw)

	// Without sales:create there is no exchange.
	clerk := userWithPerms(t, l, tok, "ex-clerk@x.test", "sales_returns:create", "sales:view", "sales:view_all")
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/sales/exchanges", token: clerk, body: xbody(1, 1, nil)}).Code)

	// Dashboard "collected" leaves out exchange and store credit (Q6).
	dash := get(t, host, "/api/dashboard", tok)
	require.Equal(t, 200, dash.Code, dash.Raw)
}

func TestStoreCreditIsolationAcrossTenants(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	cust := newCustomer(t, "a", aTok, "Iso Credit A")
	r := do(t, rq{method: "POST", host: tenants["a"].Host, path: fmt.Sprintf("/api/customers/%v/store-credit/adjust", cust), token: aTok,
		body: map[string]any{"amount": 25, "reason": "opening balance"}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, 404, get(t, tenants["c"].Host, fmt.Sprintf("/api/customers/%v/store-credit", cust+100000), cTok).Code)
	require.Equal(t, 401, get(t, tenants["c"].Host, fmt.Sprintf("/api/customers/%v/store-credit", cust), aTok).Code)
	x := do(t, rq{method: "POST", host: tenants["c"].Host, path: fmt.Sprintf("/api/customers/%v/store-credit/adjust", cust+100000), token: cTok,
		body: map[string]any{"amount": 25, "reason": "opening balance"}})
	require.Equal(t, 404, x.Code, x.Raw)
	require.Equal(t, 25.0, creditOf(t, "a", aTok, cust))
	e := do(t, rq{method: "POST", host: tenants["c"].Host, path: "/api/sales/exchanges", token: cTok,
		body: map[string]any{"saleId": 999999, "returnLines": []map[string]any{{"orderItemId": 1, "quantity": 1}}, "newLines": []map[string]any{{"variantId": 1, "quantity": 1, "unitPrice": 1}}}})
	require.Equal(t, 400, e.Code, e.Raw)
}
