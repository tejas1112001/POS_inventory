//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProductPage (UI S8): GET /api/products/{id} returns one product with
// its variants and stock in every store; other routes under /products
// still win; another tenant cannot read it.
func TestProductPage(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "PAGE-1", store, 7)

	r := get(t, host, fmt.Sprintf("/api/products/%v", p.pid), tok)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "Product PAGE-1", r.Body["name"])
	v := r.Body["ProductVariants"].([]any)[0].(map[string]any)
	require.Equal(t, "PAGE-1", v["sku"])
	inv := v["Inventories"].([]any)[0].(map[string]any)
	require.EqualValues(t, 7, num(inv["qty"]))
	require.Equal(t, store, inv["store_id"])

	require.Equal(t, 404, get(t, host, "/api/products/999999999", tok).Code)
	require.Equal(t, 200, get(t, host, "/api/products/pos?storeId="+fmt.Sprint(store), tok).Code, "the POS list still answers")

	createUser(t, "a", tok, "page-cashier@iso-a.test", "biller", "page-cashier-pass-1")
	ctok, _ := login(t, "a", "page-cashier@iso-a.test", "page-cashier-pass-1")
	require.Equal(t, 200, get(t, host, fmt.Sprintf("/api/products/%v", p.pid), ctok).Code)

	other, _ := ownerLogin(t, "b")
	require.Contains(t, []int{401, 404}, get(t, tenants["b"].Host, fmt.Sprintf("/api/products/%v", p.pid), other).Code, "tenant b never sees tenant a's product")
}
