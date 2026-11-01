//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPurchaseListFilters (UI S8): ?search= (number or supplier name) and
// ?paymentStatus= on the purchases list; payment recorded through PUT.
func TestPurchaseListFilters(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "PUR-F", store, 1)
	sup := createSupplier(t, "a", tok, "Quillfeather Wholesale")
	a := postPurchase(t, "a", tok, purchaseBody(sup, "QF-INV-1", "Ordered", purchaseLine(p.pid, p.vid, store, 2, 50)), nil)
	require.Equal(t, 201, a.Code, a.Raw)
	b := postPurchase(t, "a", tok, purchaseBody(sup, "QF-INV-2", "Ordered", purchaseLine(p.pid, p.vid, store, 1, 50)), nil)
	require.Equal(t, 201, b.Code, b.Raw)

	// Record a full payment on the second one.
	pay := putPurchase(t, "a", tok, b.Body["id"], map[string]any{"paid": b.Body["total"]})
	require.Equal(t, 200, pay.Code, pay.Raw)
	require.Equal(t, "Paid", pay.Body["payment_status"])

	ids := func(q string) []any {
		var out []any
		for _, x := range list(t, get(t, host, "/api/purchases?"+q, tok)) {
			out = append(out, x["id"])
		}
		return out
	}
	require.ElementsMatch(t, []any{a.Body["id"], b.Body["id"]}, ids("search=quillfeather"), "supplier name")
	require.Equal(t, []any{a.Body["id"]}, ids("search=QF-INV-1"), "purchase number")
	require.Equal(t, []any{b.Body["id"]}, ids("search=quillfeather&paymentStatus=Paid"))
	require.Equal(t, []any{a.Body["id"]}, ids("search=quillfeather&paymentStatus=Unpaid"))
	require.Len(t, ids("search=no%25such_"), 0, "LIKE characters are literal")
}
