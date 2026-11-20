//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Industry P1 item 2 (DB4, D3, A4, Q4): every payment is "recorded";
// non-cash methods carry "(recorded)" on the statement; reports say so.
func TestPaymentVerificationRecorded(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "PAY-VERIFY", store, 50)
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Verify Vani"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	cid := cu.Body["id"]
	setCreditLimit(t, "a", cid, "1000")

	// Cash, UPI and the rest on account (1 unit = 142.19).
	body := posSale(store, line(p, 1))
	body["customerId"] = cid
	body["date"] = "2004-03-01"
	body["payments"] = []map[string]any{{"method": "cash", "amount": 40}, {"method": "upi", "amount": 30, "reference": "txn-v1"}, {"method": "credit"}}
	s := sell(t, "a", tok, body, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	for _, x := range s.Body["payments"].([]any) {
		require.Equal(t, "recorded", x.(map[string]any)["verification"])
	}
	got := get(t, host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), tok)
	require.Contains(t, got.Raw, `"verification":"recorded"`)

	// A collection is recorded too.
	cpath := fmt.Sprintf("/api/customers/%v", cid)
	c := do(t, rq{method: "POST", host: host, path: cpath + "/payments", token: tok, body: map[string]any{"amount": 20, "method": "card", "reference": "card-9"}})
	require.Equal(t, 201, c.Code, c.Raw)
	require.Equal(t, "recorded", c.Body["verification"])
	hist := get(t, host, cpath+"/payments", tok)
	require.Contains(t, hist.Raw, `"verification":"recorded"`)

	// Statement text: UPI and card labelled recorded, cash not.
	st := get(t, host, cpath+"/statement?startDate=2004-01-01&endDate=2030-12-31", tok)
	require.Equal(t, 200, st.Code, st.Raw)
	var descs []string
	for _, l := range st.Body["lines"].([]any) {
		descs = append(descs, l.(map[string]any)["description"].(string))
	}
	require.Contains(t, descs, "Upi (recorded) txn-v1")
	require.Contains(t, descs, "Cash")
	require.Contains(t, descs, "Payment received (card, recorded)")

	// Report rows say recorded; only non-cash methods are labelled.
	rep := get(t, host, "/api/sales/report/payment-methods?startDate=2004-01-01&endDate=2004-12-31", tok)
	require.Equal(t, 200, rep.Code, rep.Raw)
	seen := map[string]bool{}
	for _, m := range list(t, rep) {
		require.Equal(t, "recorded", m["verification"])
		seen[m["method"].(string)] = m["recorded"].(bool)
	}
	require.Equal(t, false, seen["cash"])
	require.Equal(t, true, seen["upi"])

	// The column allows only "recorded" in this release.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema("a")+`.sale_payments SET verification = 'verified' WHERE sale_id = $1`, int64(num(s.Body["id"])))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sale_payments_verification_check")

	// Another shop sees none of it; A's token is refused there.
	btk, _ := ownerLogin(t, "b")
	bh := tenants["b"].Host
	bs := get(t, bh, "/api/sales/report/payment-methods?startDate=2004-01-01&endDate=2004-12-31", btk)
	require.Equal(t, 200, bs.Code)
	require.False(t, strings.Contains(bs.Raw, `"upi"`), "B has no 2004 UPI payments")
	require.NotContains(t, get(t, bh, cpath+"/payments", btk).Raw, "card-9")
	require.Equal(t, 401, get(t, bh, cpath+"/payments", tok).Code)
}
