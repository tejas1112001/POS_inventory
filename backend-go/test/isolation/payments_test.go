//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSplitPartPaymentAndCredit (S10 item 5, B6, refund rule A): a sale
// paid with several methods and the rest on account; credit needs
// sales:credit, a named customer and room under the credit limit (with
// the "Credit limit reached" message); pending orders are not credit; a
// return first reduces the sale's due; overdue sales; the payment-method
// report; nothing leaks to another shop.
func TestSplitPartPaymentAndCredit(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "PAY-SPLIT", store, 100)
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Credit Kiran"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	cid := cu.Body["id"]
	setCreditLimit(t, "a", cid, "300")
	sale := func(tk string, qty int, customer any, payments []map[string]any, extra map[string]any) resp {
		b := map[string]any{"orderItems": []map[string]any{line(p, qty)}, "storeId": store, "payments": payments}
		if customer != nil {
			b["customerId"] = customer
		}
		for k, v := range extra {
			b[k] = v
		}
		return sell(t, "a", tk, b, nil)
	}
	credit := map[string]any{"method": "credit"}

	// 2 units = 284.38: cash 100 (tendered 200), UPI 50, the rest on account.
	s1 := sale(tok, 2, cid, []map[string]any{{"method": "cash", "amount": 100, "tendered": 200}, {"method": "upi", "amount": 50, "reference": "txn-1"}, credit}, nil)
	require.Equal(t, 201, s1.Code, s1.Raw)
	require.Equal(t, 284.38, num(s1.Body["total"]))
	require.Equal(t, 150.0, num(s1.Body["paid"]))
	require.Equal(t, 134.38, num(s1.Body["due"]))
	require.Equal(t, "Partial", s1.Body["payment_status"])
	require.Equal(t, 100.0, num(s1.Body["change"]))
	pays := s1.Body["payments"].([]any)
	require.Len(t, pays, 3)
	require.Equal(t, "credit", pays[2].(map[string]any)["method"])
	require.Equal(t, 134.38, num(pays[2].(map[string]any)["amount"]))
	var want string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT to_char((now() AT TIME ZONE timezone)::date + 30, 'YYYY-MM-DD') FROM `+schema("a")+`.settings`).Scan(&want))
	require.Equal(t, want, s1.Body["dueDate"], "30 days after the sale")

	// Over the limit: owes 134.38, a fully-credit sale of 284.38 would pass 300.
	over := sale(tok, 2, cid, []map[string]any{credit}, nil)
	require.Equal(t, 409, over.Code)
	require.Equal(t, "CREDIT_LIMIT", over.Body["code"])
	require.Equal(t, "Credit limit reached: Credit Kiran owes ₹134.38 of ₹300.00. Take ₹118.76 now or ask the owner to raise the limit.", over.Body["error"])
	ok := sale(tok, 2, cid, []map[string]any{{"method": "card", "amount": 118.76, "reference": "4242"}, credit}, nil)
	require.Equal(t, 201, ok.Code, ok.Raw)
	require.Equal(t, 165.62, num(ok.Body["due"]))

	// Walk-in, no permission, payments above the total.
	require.Equal(t, 400, sale(tok, 1, nil, []map[string]any{{"method": "cash", "amount": 10}}, nil).Code, "never Walk-in")
	createUser(t, "a", tok, "credit-admin@iso-a.test", "admin", "credit-admin-pass-1")
	atok, _ := login(t, "a", "credit-admin@iso-a.test", "credit-admin-pass-1")
	require.Equal(t, 403, sale(atok, 1, cid, []map[string]any{{"method": "cash", "amount": 10}}, nil).Code, "admins do not sell on credit by default")
	require.Equal(t, 201, sale(atok, 1, cid, []map[string]any{{"method": "cash", "amount": 142.19}}, nil).Code, "paid in full is fine")
	createUser(t, "a", tok, "credit-biller@iso-a.test", "biller", "credit-biller-pass-1")
	btok, _ := login(t, "a", "credit-biller@iso-a.test", "credit-biller-pass-1")
	require.Equal(t, 403, sale(btok, 1, cid, []map[string]any{credit}, nil).Code)
	require.Equal(t, 400, sale(tok, 1, cid, []map[string]any{{"method": "card", "amount": 200}}, nil).Code, "more than the total")
	require.Equal(t, 400, sale(tok, 1, cid, []map[string]any{{"method": "bitcoin", "amount": 1}}, nil).Code)

	// A pending order is not credit: no check, no due date.
	pend := sale(btok, 1, nil, []map[string]any{}, map[string]any{"status": "Pending"})
	require.Equal(t, 201, pend.Code, pend.Raw)
	require.Nil(t, pend.Body["dueDate"])

	// An edit without payment fields keeps the split.
	e := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/sales/%v", ok.Body["id"]), token: tok,
		body: map[string]any{"orderItems": []map[string]any{line(p, 2)}, "storeId": store, "customerId": cid, "note": "edited"}})
	require.Equal(t, 200, e.Code, e.Raw)
	ep := e.Body["payments"].([]any)
	require.Equal(t, "card", ep[0].(map[string]any)["method"])
	require.Equal(t, "4242", ep[0].(map[string]any)["reference"])

	// Return 1 unit (142.19) of sale 1, which still owes 134.38: the due
	// drops to 0 and only 7.81 is refunded (option A).
	rb := retBody(s1.Body["id"], store, map[string]any{"productId": p.pid, "quantity": 1, "reason": "too big"})
	delete(rb, "paid")
	ret := postReturn(t, "a", tok, rb, nil)
	require.Equal(t, 201, ret.Code, ret.Raw)
	require.Equal(t, 134.38, num(ret.Body["appliedToDue"]))
	require.Equal(t, 7.81, num(ret.Body["paid"]), "refunded")
	require.Equal(t, 0.0, num(ret.Body["due"]))
	after := get(t, host, fmt.Sprintf("/api/sales/%v", s1.Body["id"]), tok)
	require.Equal(t, 0.0, num(after.Body["due"]))
	require.Equal(t, "Paid", after.Body["payment_status"])
	require.Nil(t, after.Body["dueDate"])
	require.Equal(t, 409, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", s1.Body["id"]), token: tok}).Code)

	// Overdue: a credit sale from 2003 is long past its due date.
	old := sale(tok, 1, cid, []map[string]any{{"method": "cash", "amount": 42.19}, credit}, map[string]any{"date": "2003-05-01"})
	require.Equal(t, 201, old.Code, old.Raw)
	require.Equal(t, "Overdue", old.Body["payment_status"])
	require.Equal(t, "2003-05-31", old.Body["dueDate"])
	ov := list(t, get(t, host, "/api/sales?startDate=2003-01-01&endDate=2003-12-31&paymentStatus=Overdue", tok))
	require.Len(t, ov, 1)
	inv := get(t, host, "/api/invoices?limit=200", tok)
	require.Contains(t, inv.Raw, `"dueDate":"31 May 2003"`)

	// Sales by payment method.
	rep := get(t, host, "/api/sales/report/payment-methods?startDate=2003-01-01&endDate=2003-12-31", tok)
	require.Equal(t, 200, rep.Code, rep.Raw)
	require.Contains(t, rep.Raw, `"method":"cash"`)
	require.Contains(t, rep.Raw, `"method":"credit"`)
	own := get(t, host, "/api/sales/report/payment-methods?startDate=2003-01-01&endDate=2003-12-31", btok)
	require.Equal(t, 200, own.Code)
	require.Equal(t, "[]", strings.TrimSpace(own.Raw), "a biller sees only their own sales' payments")

	// The payments sum to paid on every sale of this test.
	var bad int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.sales s WHERE s.customer_id = $1
		AND s.paid <> coalesce((SELECT sum(amount) FROM `+schema("a")+`.sale_payments p WHERE p.sale_id = s.id AND p.method <> 'credit'), 0)`,
		int64(num(cid))).Scan(&bad))
	require.Zero(t, bad)

	// Another shop sees none of it, and A's token is refused there.
	btk, _ := ownerLogin(t, "b")
	// IDs are per shop, so B may have a sale with the same ID: it must not be A's.
	bsale := get(t, tenants["b"].Host, fmt.Sprintf("/api/sales/%v", s1.Body["id"]), btk)
	require.NotContains(t, bsale.Raw, s1.Body["reference"].(string))
	require.NotContains(t, bsale.Raw, "txn-1")
	bs := get(t, tenants["b"].Host, "/api/sales/report/payment-methods?startDate=2003-01-01&endDate=2003-12-31", btk)
	require.Equal(t, 200, bs.Code)
	require.False(t, strings.Contains(bs.Raw, "credit"), "B has no 2003 sales on account")
	require.Equal(t, 401, get(t, tenants["b"].Host, "/api/sales/report/payment-methods", tok).Code)
}
