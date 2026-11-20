//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOrderListFilters (UI S8): ?paymentStatus=, ?status= (Void implies
// voided orders) and the search on customer names.
func TestOrderListFilters(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "ORD-F", store, 20)
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Zebulon Orderfilter"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	// 2004: a year no other test uses.
	setCreditLimit(t, "a", cu.Body["id"], "10000")
	paid := dated(store, "2004-02-01", nil, line(p, 1))
	paid["customerId"] = cu.Body["id"]
	unpaid := dated(store, "2004-02-02", 0, line(p, 1))
	unpaid["customerId"] = cu.Body["id"]
	void := dated(store, "2004-02-03", nil, line(p, 1))
	void["customerId"] = cu.Body["id"]
	var ids []any
	for _, b := range []map[string]any{paid, unpaid, void} {
		r := sell(t, "a", tok, b, nil)
		require.Equal(t, 201, r.Code, r.Raw)
		ids = append(ids, r.Body["id"])
	}
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v?reason=test", ids[2]), token: tok}).Code)

	refs := func(q string) []any {
		var out []any
		for _, s := range list(t, get(t, host, "/api/sales?startDate=2004-01-01&endDate=2004-12-31"+q, tok)) {
			out = append(out, s["id"])
		}
		return out
	}
	require.ElementsMatch(t, ids[:2], refs(""), "voided orders hidden by default")
	require.ElementsMatch(t, ids[:2], refs("&search=orderfilter"), "search by customer name")
	// Sold in 2004 with nothing paid: due 30 days later, so now Overdue
	// (S10 item 5), and no longer listed as Unpaid.
	require.Equal(t, []any{ids[1]}, refs("&paymentStatus=Overdue"))
	require.Empty(t, refs("&paymentStatus=Unpaid"))
	require.Equal(t, []any{ids[0]}, refs("&paymentStatus=Paid"))
	require.Equal(t, []any{ids[2]}, refs("&status=Void"))
	require.Len(t, refs("&paymentStatus=Bogus"), 2, "unknown values are ignored")
}
