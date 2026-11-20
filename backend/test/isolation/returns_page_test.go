//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReturnListAndPage (UI S8): ?search= (return number, order number,
// customer), dates and store on the returns list; GET /api/sales/returns/{id}.
func TestReturnListAndPage(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RET-PAGE", store, 10)
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Wilhelmina Returnpage"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	// 2005: a year no other test uses.
	sb := dated(store, "2005-03-01", nil, line(p, 3))
	sb["customerId"] = cu.Body["id"]
	sale := sell(t, "a", tok, sb, nil)
	require.Equal(t, 201, sale.Code, sale.Raw)
	rb := retBody(sale.Body["id"], store, retItem(p, 1))
	rb["date"] = "2005-03-02"
	ret := postReturn(t, "a", tok, rb, nil)
	require.Equal(t, 201, ret.Code, ret.Raw)

	ids := func(q string) []any {
		var out []any
		for _, x := range list(t, get(t, host, "/api/sales/returns?"+q, tok)) {
			out = append(out, x["id"])
		}
		return out
	}
	want := []any{ret.Body["id"]}
	require.Equal(t, want, ids("search=returnpage"), "customer name")
	require.Equal(t, want, ids("search="+fmt.Sprint(sale.Body["reference"])), "order number")
	require.Equal(t, want, ids("search="+fmt.Sprint(ret.Body["reference"])), "return number")
	require.Equal(t, want, ids("startDate=2005-03-02&endDate=2005-03-02"))
	require.Empty(t, ids("startDate=2005-03-03&endDate=2005-03-31&search=returnpage"))
	require.Equal(t, want, ids(fmt.Sprintf("search=returnpage&storeId=%v", store)))

	one := get(t, host, fmt.Sprintf("/api/sales/returns/%v", ret.Body["id"]), tok)
	require.Equal(t, 200, one.Code, one.Raw)
	require.Equal(t, ret.Body["reference"], one.Body["reference"])
	require.Len(t, one.Body["items"], 1)
	require.Equal(t, 404, get(t, host, "/api/sales/returns/999999999", tok).Code)
	other, _ := ownerLogin(t, "b")
	require.Contains(t, []int{401, 404}, get(t, tenants["b"].Host, fmt.Sprintf("/api/sales/returns/%v", ret.Body["id"]), other).Code)
}
