//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Price per store (tenant 00042): a variant at 120.50 sells at 99 in the
// second store. The POS list, the lock, the quote and the sale of that
// store use 99; the first store keeps 120.50; removing the store price
// brings back the variant price.
func TestStorePrices(t *testing.T) {
	l := billingTenant(t, "store-prices")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}
	s1 := storeIDs(t, l, tok)[0]
	st := call("POST", "/api/stores", map[string]any{"name": "Airport kiosk"})
	require.Equal(t, 201, st.Code, st.Raw)
	s2 := num(st.Body["id"])
	p := twoStoreProduct(t, l, tok, "SP-TEA", s1, s2, 10, 10)
	other := stockedProduct(t, l, tok, "SP-OTHER", s1, 1)
	path := "/api/products/" + jsonID(p.pid) + "/store-prices"

	// Refused: another product's variant, a negative price, an unknown store.
	for _, c := range []struct {
		line  map[string]any
		field string
	}{
		{map[string]any{"variantId": other.vid, "storeId": s2, "price": 99}, "prices[0].variantId"},
		{map[string]any{"variantId": p.vid, "storeId": s2, "price": -1}, "prices[0].price"},
		{map[string]any{"variantId": p.vid, "storeId": 99999999, "price": 99}, "prices[0].storeId"},
	} {
		r := call("PUT", path, map[string]any{"prices": []map[string]any{c.line}})
		require.Equal(t, 400, r.Code, r.Raw)
		require.Equal(t, c.field, errField(r))
	}

	r := call("PUT", path, map[string]any{"prices": []map[string]any{{"variantId": p.vid, "storeId": s2, "price": 99}}})
	require.Equal(t, 200, r.Code, r.Raw)
	v := r.Body["ProductVariants"].([]any)[0].(map[string]any)
	require.Equal(t, 120.5, num(v["price"]), "the variant price is unchanged")
	sps := v["storePrices"].([]any)
	require.Len(t, sps, 1)
	require.Equal(t, s2, num(sps[0].(map[string]any)["storeId"]))
	require.Equal(t, 99.0, num(sps[0].(map[string]any)["price"]))

	posPrice := func(store float64) float64 {
		t.Helper()
		res := call("GET", fmt.Sprintf("/api/products/pos?storeId=%v", store), nil)
		require.Equal(t, 200, res.Code, res.Raw)
		for _, m := range list(t, res) {
			if num(m["id"]) == p.pid {
				return num(m["ProductVariants"].([]any)[0].(map[string]any)["price"])
			}
		}
		t.Fatalf("product not in the POS list of %v", store)
		return 0
	}
	require.Equal(t, 99.0, posPrice(s2))
	require.Equal(t, 120.5, posPrice(s1))
	lk := call("PUT", "/api/products/"+jsonID(p.pid)+"/lock", map[string]any{"lock": true, "storeId": s2})
	require.Equal(t, 200, lk.Code, lk.Raw)
	lv := lk.Body["ProductVariants"].([]any)[0].(map[string]any)
	require.Equal(t, 99.0, num(lv["price"]))
	require.Equal(t, 120.5, num(lv["basePrice"]))
	require.Equal(t, 200, call("PUT", "/api/products/"+jsonID(p.pid)+"/lock", map[string]any{"lock": false, "storeId": s2}).Code)

	// The quote and the sale at the kiosk: 99 a unit (18 % on top).
	at := func(store float64, price any) map[string]any {
		ln := map[string]any{"productId": p.pid, "variantId": p.vid, "quantity": 1}
		if price != nil {
			ln["unitPrice"] = price
		}
		return posSale(store, ln)
	}
	q := call("POST", "/api/sales/quote", at(s2, nil))
	require.Equal(t, 200, q.Code, q.Raw)
	require.Equal(t, 99.0, num(q.Body["lines"].([]any)[0].(map[string]any)["unitPrice"]))
	s := sell(t, l, tok, at(s2, 99), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, "116.82", dec(lineOf(t, s.Body, p)["subtotal"]).StringFixed(2))
	s = sell(t, l, tok, at(s1, 120.5), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, "142.19", dec(lineOf(t, s.Body, p)["subtotal"]).StringFixed(2), "the main store keeps 120.50")

	// Removed: the variant price again.
	r = call("PUT", path, map[string]any{"prices": []map[string]any{{"variantId": p.vid, "storeId": s2, "price": nil}}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Empty(t, r.Body["ProductVariants"].([]any)[0].(map[string]any)["storePrices"])
	require.Equal(t, 120.5, posPrice(s2))
	require.Empty(t, reconcile(t, l))
}
