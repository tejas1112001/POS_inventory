//go:build integration

package isolation

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListSearchAndStatusFilters: ?search= and ?status= on the catalogue
// lists, and ?search= / ?stockStatus= on stock, narrow the results on the
// server (used by the paged tables in the shop app).
func TestListSearchAndStatusFilters(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	q := func(path string, params url.Values) []string {
		r := get(t, host, path+"?"+params.Encode(), tok)
		require.Equal(t, 200, r.Code, r.Raw)
		return names(list(t, r))
	}

	for _, kind := range []string{"suppliers", "categories", "brands", "units", "customers"} {
		active := fmt.Sprintf("Filter %s Alpha", kind)
		inactive := fmt.Sprintf("Filter %s Beta", kind)
		for _, n := range []struct{ name, status string }{{active, "Active"}, {inactive, "Inactive"}} {
			body := map[string]any{"name": n.name, "status": n.status}
			if kind == "units" {
				body["short_name"] = "f"
			}
			c := do(t, rq{method: "POST", host: host, path: "/api/" + kind, token: tok, body: body})
			require.Equal(t, 201, c.Code, "%s: %s", kind, c.Raw)
		}
		require.ElementsMatch(t, []string{active, inactive}, q("/api/"+kind, url.Values{"search": {"filter " + kind}}), kind)
		require.Equal(t, []string{active}, q("/api/"+kind, url.Values{"search": {"filter " + kind}, "status": {"Active"}}), kind)
		require.Equal(t, []string{inactive}, q("/api/"+kind, url.Values{"search": {"filter " + kind}, "status": {"Inactive"}}), kind)
		require.Empty(t, q("/api/"+kind, url.Values{"search": {"no such %_ name"}}), kind)
		// An unknown status is ignored, not an error.
		require.Len(t, q("/api/"+kind, url.Values{"search": {"filter " + kind}, "status": {"Gone"}}), 2, kind)
	}

	store := storeIDs(t, "a", tok)[0]
	stockedProduct(t, "a", tok, "FILTER-OUT-1", store, 0)
	stockedProduct(t, "a", tok, "FILTER-IN-1", store, 50)
	skus := func(params url.Values) []string {
		r := get(t, host, "/api/inventory?"+params.Encode(), tok)
		require.Equal(t, 200, r.Code, r.Raw)
		var out []string
		for _, x := range list(t, r) {
			out = append(out, fmt.Sprint(x["sku"]))
		}
		return out
	}
	require.ElementsMatch(t, []string{"FILTER-OUT-1", "FILTER-IN-1"}, skus(url.Values{"search": {"filter-"}}))
	require.Equal(t, []string{"FILTER-OUT-1"}, skus(url.Values{"search": {"filter-"}, "stockStatus": {"Out of Stock"}}))
	require.Equal(t, []string{"FILTER-IN-1"}, skus(url.Values{"search": {"filter-"}, "stockStatus": {"In Stock"}}))
	paged := get(t, host, "/api/inventory?page=1&limit=1&search=filter-", tok)
	require.Contains(t, paged.Raw, `"total":2`)
}
