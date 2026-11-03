//go:build integration

package isolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCustomerDuesCollectReverseStatement (S10 item 5, D6, D7): the
// credit limit is set only with customers:credit_limit; a collection pays
// the oldest dues first and is refused above what is owed; a reversal is
// a new negative entry with a reason (the tables are append-only); the
// statement's running balance ends at what the customer owes; another
// shop sees nothing.
func TestCustomerDuesCollectReverseStatement(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "DUES-P", store, 100)
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Dues Meera"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	cid := cu.Body["id"]
	cpath := fmt.Sprintf("/api/customers/%v", cid)

	// Credit limit: owner yes; admin and biller no; walk-in never.
	createUser(t, "a", tok, "dues-admin@iso-a.test", "admin", "dues-admin-pass-1")
	atok, _ := login(t, "a", "dues-admin@iso-a.test", "dues-admin-pass-1")
	createUser(t, "a", tok, "dues-biller@iso-a.test", "biller", "dues-biller-pass-1")
	btok, _ := login(t, "a", "dues-biller@iso-a.test", "dues-biller-pass-1")
	limit := func(tk string, v any) resp {
		return do(t, rq{method: "PUT", host: host, path: cpath + "/credit-limit", token: tk, body: map[string]any{"creditLimit": v}})
	}
	require.Equal(t, 403, limit(atok, 1000).Code)
	require.Equal(t, 403, limit(btok, 1000).Code)
	require.Equal(t, 400, limit(tok, -1).Code)
	require.Equal(t, 200, limit(tok, 1000).Code)
	var walkIn int64
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT default_customer_id FROM `+schema("a")+`.settings`).Scan(&walkIn))
	require.Equal(t, 400, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/customers/%d/credit-limit", walkIn), token: tok,
		body: map[string]any{"creditLimit": 50}}).Code)
	cl := get(t, host, "/api/customers?search=Dues%20Meera", tok)
	require.Contains(t, cl.Raw, `"creditLimit":1000`)

	// Two credit sales (142.19 each, nothing paid), the older first.
	mk := func(day string) map[string]any {
		r := sell(t, "a", tok, map[string]any{"orderItems": []map[string]any{line(p, 1)}, "storeId": store, "customerId": cid,
			"payments": []map[string]any{}, "date": day}, nil)
		require.Equal(t, 201, r.Code, r.Raw)
		return r.Body
	}
	older, newer := mk("2026-01-05"), mk("2026-02-05")
	d := get(t, host, cpath+"/dues", tok)
	require.Equal(t, 200, d.Code, d.Raw)
	require.Equal(t, 284.38, num(d.Body["totalDue"]))
	require.Equal(t, 715.62, num(d.Body["available"]))
	require.Len(t, d.Body["sales"], 2)
	require.Equal(t, true, d.Body["canCollect"])

	// Collect: biller refused (no customers:collect), over-collect refused,
	// 200 pays the older sale in full and 57.81 of the newer one.
	collect := func(tk string, amount any) resp {
		return do(t, rq{method: "POST", host: host, path: cpath + "/payments", token: tk,
			body: map[string]any{"amount": amount, "method": "upi", "reference": "upi-77"}})
	}
	require.Equal(t, 403, collect(btok, 10).Code)
	over := collect(tok, 300)
	require.Equal(t, 400, over.Code)
	require.Contains(t, over.Raw, "owes ₹284.38")
	c := collect(atok, 200)
	require.Equal(t, 201, c.Code, c.Raw)
	al := c.Body["allocations"].([]any)
	require.Len(t, al, 2)
	require.Equal(t, older["reference"], al[0].(map[string]any)["reference"])
	require.Equal(t, 142.19, num(al[0].(map[string]any)["amount"]))
	require.Equal(t, 57.81, num(al[1].(map[string]any)["amount"]))
	o := get(t, host, fmt.Sprintf("/api/sales/%v", older["id"]), tok)
	require.Equal(t, "Paid", o.Body["payment_status"])
	n := get(t, host, fmt.Sprintf("/api/sales/%v", newer["id"]), tok)
	require.Equal(t, 84.38, num(n.Body["due"]))
	require.Equal(t, 57.81, num(n.Body["paid"]))
	require.Equal(t, 409, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", newer["id"]), token: tok}).Code,
		"a collected sale cannot be voided")

	// Reverse: a reason is required; then the dues are back; only once.
	rpath := fmt.Sprintf("/api/customer-payments/%v/reverse", c.Body["id"])
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: rpath, token: tok, body: map[string]any{"reason": "no"}}).Code)
	rv := do(t, rq{method: "POST", host: host, path: rpath, token: tok, body: map[string]any{"reason": "Bounced UPI payment"}})
	require.Equal(t, 201, rv.Code, rv.Raw)
	require.Equal(t, -200.0, num(rv.Body["amount"]))
	require.Equal(t, 409, do(t, rq{method: "POST", host: host, path: rpath, token: tok, body: map[string]any{"reason": "Bounced UPI payment"}}).Code)
	require.Equal(t, 409, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/customer-payments/%v/reverse", rv.Body["id"]), token: tok,
		body: map[string]any{"reason": "Undo the undo"}}).Code)
	d = get(t, host, cpath+"/dues", tok)
	require.Equal(t, 284.38, num(d.Body["totalDue"]))
	hist := get(t, host, cpath+"/payments", tok)
	require.Contains(t, hist.Raw, `"reversedBy":`+fmt.Sprint(rv.Body["id"]))

	// Collect again, then the statement: opening 0, closing = owed now.
	require.Equal(t, 201, collect(tok, 100).Code)
	st := get(t, host, cpath+"/statement?startDate=2026-01-01&endDate=2030-12-31", tok)
	require.Equal(t, 200, st.Code, st.Raw)
	require.Equal(t, 0.0, num(st.Body["opening"]))
	require.Equal(t, 184.38, num(st.Body["closing"]))
	d = get(t, host, cpath+"/dues", tok)
	require.Equal(t, num(st.Body["closing"]), num(d.Body["totalDue"]), "the statement ends at what is owed")
	kinds := map[string]int{}
	for _, l := range st.Body["lines"].([]any) {
		kinds[l.(map[string]any)["kind"].(string)]++
	}
	require.Equal(t, map[string]int{"sale": 2, "collection": 2, "reversal": 1}, kinds)
	part := get(t, host, cpath+"/statement?startDate=2026-02-01&endDate=2030-12-31", tok)
	require.Equal(t, 142.19, num(part.Body["opening"]), "the January sale is in the opening balance")
	require.Equal(t, 184.38, num(part.Body["closing"]))

	// The audit trail and the append-only tables.
	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.audit_log
		WHERE action IN ('customer_payment.collect','customer_payment.reverse','customer.credit_limit')`).Scan(&audits))
	require.GreaterOrEqual(t, audits, 4)
	for _, tbl := range []string{"customer_payments", "customer_payment_allocations"} {
		var canUpdate, canDelete bool
		require.NoError(t, admin(t).QueryRow(ctx, `SELECT has_table_privilege($1, $2, 'UPDATE'), has_table_privilege($1, $2, 'DELETE')`,
			schema("a")+"_rw", schema("a")+"."+tbl).Scan(&canUpdate, &canDelete))
		require.False(t, canUpdate, tbl)
		require.False(t, canDelete, tbl)
	}

	// Another shop: no such customer there, and A's token is refused.
	btk, _ := ownerLogin(t, "b")
	bh := tenants["b"].Host
	bd := get(t, bh, cpath+"/dues", btk)
	require.NotContains(t, bd.Raw, "Dues Meera")
	require.NotContains(t, get(t, bh, cpath+"/statement", btk).Raw, "Dues Meera")
	require.Equal(t, 401, get(t, bh, cpath+"/dues", tok).Code)
	require.Equal(t, 401, do(t, rq{method: "POST", host: bh, path: cpath + "/payments", token: tok, body: map[string]any{"amount": 1, "method": "cash"}}).Code)
}
