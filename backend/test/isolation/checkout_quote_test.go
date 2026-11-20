//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSalesQuoteMatchesTheSale (UI S6): POST /api/sales/quote prices a cart
// exactly like the sale, changes nothing, reports stock, and refuses the
// same things the sale refuses.
func TestSalesQuoteMatchesTheSale(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "QUOTE-1", store, 5)
	quote := func(body map[string]any, token string) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: token, body: body})
	}

	body := posSale(store, line(p, 3))
	body["discount"] = 10
	q := quote(body, tok)
	require.Equal(t, 200, q.Code, q.Raw)
	require.Equal(t, 5, stockOf(t, "a", store, p.vid), "a quote never touches stock")
	lines := q.Body["lines"].([]any)
	require.Len(t, lines, 1)
	l0 := lines[0].(map[string]any)
	require.EqualValues(t, 5, num(l0["available"]))
	require.Equal(t, "QUOTE-1", l0["sku"])

	// More than in stock: priced and reported, not refused (the sale refuses).
	big := posSale(store, line(p, 9))
	r := quote(big, tok)
	require.Equal(t, 200, r.Code, r.Raw)
	require.EqualValues(t, 5, num(r.Body["lines"].([]any)[0].(map[string]any)["available"]))

	// The sale's totals equal the quote's.
	s := sell(t, "a", tok, body, map[string]string{"Idempotency-Key": "quote-match-0001"})
	require.Equal(t, 201, s.Code, s.Raw)
	for _, k := range []string{"subtotal", "discount", "tax", "total"} {
		require.Equal(t, num(q.Body[k]), num(s.Body[k]), k)
	}

	// Missing store, unknown variant: field errors.
	require.Equal(t, 400, quote(map[string]any{"orderItems": []any{map[string]any{"variantId": 999999, "quantity": 1}}}, tok).Code)
	require.Equal(t, 400, quote(posSale(store, map[string]any{"variantId": 999999, "quantity": 1}), tok).Code)

	// Cashiers may quote (sales:create); another tenant's token may not.
	createUser(t, "a", tok, "quote-cashier@iso-a.test", "biller", "quote-cashier-pass-1")
	cashier, _ := login(t, "a", "quote-cashier@iso-a.test", "quote-cashier-pass-1")
	require.Equal(t, 200, quote(posSale(store, line(p, 1)), cashier).Code)

	// A changed price is reported before PAY, like at PAY (the owner may
	// override prices, a cashier may not).
	stale := posSale(store, line(p, 1))
	stale["orderItems"].([]map[string]any)[0]["unitPrice"] = 99
	sq := quote(stale, cashier)
	require.Equal(t, 409, sq.Code, sq.Raw)
	require.Contains(t, sq.Raw, "PRICE_CHANGED")
	other, _ := ownerLogin(t, "b")
	require.Equal(t, 401, quote(posSale(store, line(p, 1)), other).Code)
}

// TestNewCheckoutSetting (UI S6): off by default, owners switch it, cashiers
// read it but cannot change it.
func TestNewCheckoutSetting(t *testing.T) {
	l := billingTenant(t, "new-checkout")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	require.Equal(t, false, get(t, host, "/api/settings", tok).Body["newCheckout"])
	on := do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"newCheckout": true}})
	require.Equal(t, 200, on.Code, on.Raw)
	require.Equal(t, true, get(t, host, "/api/settings", tok).Body["newCheckout"])

	createUser(t, l, tok, "cashier@new-checkout.test", "biller", "new-checkout-cash-1")
	cashier, _ := login(t, l, "cashier@new-checkout.test", "new-checkout-cash-1")
	require.Equal(t, true, get(t, host, "/api/settings", cashier).Body["newCheckout"])
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: "/api/settings", token: cashier, body: map[string]any{"newCheckout": false}}).Code)
}
