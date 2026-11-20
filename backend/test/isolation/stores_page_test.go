//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStoresPageRules (UI S5): the Stores page lists inactive stores with
// the default-store marker and units in stock; pickers still get active
// stores only; the last active store cannot be turned off or deleted;
// a phone can be set; reactivating stays inside the plan's store count.
func TestStoresPageRules(t *testing.T) {
	l := billingTenant(t, "stores-page")
	onPlan(t, l, limitPlan(t, "limits_stores_page", `{"max_stores":3,"max_users":5,"max_products":50,"max_emails_per_day":300,"max_storage_mb":1000}`))
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	first := storeIDs(t, l, tok)[0]
	put := func(id any, body map[string]any) resp {
		return do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%v", id), token: tok, body: body})
	}

	// Only one active store: it cannot be turned off.
	off := put(first, map[string]any{"status": "Inactive"})
	require.Equal(t, 409, off.Code, off.Raw)
	require.Contains(t, off.Raw, "At least one store must stay active")

	// A second store with a phone; then the first can be turned off.
	c := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Annex Store", "phone": "+91 98765 43210", "address": "2 Side Road"}})
	require.Equal(t, 201, c.Code, c.Raw)
	require.Equal(t, "+91 98765 43210", c.Body["phone"])
	second := c.Body["id"]
	stockedProduct(t, l, tok, "STORES-PAGE-1", first, 7)

	r := put(first, map[string]any{"status": "Inactive"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "Inactive", r.Body["status"])
	require.Equal(t, "2 Side Road", put(second, map[string]any{"phone": ""}).Body["address"], "missing fields keep their value")

	// Pickers: active only, without the page extras.
	picker := list(t, get(t, host, "/api/stores", tok))
	require.Len(t, picker, 1)
	require.Equal(t, second, picker[0]["id"])
	require.NotContains(t, picker[0], "isDefault")

	// The page: all stores, marker and stock, filters.
	all := list(t, get(t, host, "/api/stores?includeInactive=true", tok))
	require.Len(t, all, 2)
	byID := map[any]map[string]any{}
	for _, s := range all {
		byID[s["id"]] = s
	}
	require.EqualValues(t, 7, byID[first]["stockUnits"])
	require.EqualValues(t, 0, byID[second]["stockUnits"])
	require.Equal(t, false, byID[first]["isDefault"], "an inactive store is never the default")
	inactive := list(t, get(t, host, "/api/stores?includeInactive=true&status=Inactive", tok))
	require.Len(t, inactive, 1)
	require.Equal(t, first, inactive[0]["id"])
	require.Len(t, list(t, get(t, host, "/api/stores?includeInactive=true&search=annex", tok)), 1)

	// The only active store now is the second: it cannot be turned off or deleted.
	require.Equal(t, 409, put(second, map[string]any{"status": "Inactive"}).Code)
	del := do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/stores/%v", second), token: tok})
	require.Equal(t, 409, del.Code, del.Raw)

	// Reactivating is fine; an inactive store still counts towards the plan
	// (3 stores here), so turning stores off never frees a slot.
	require.Equal(t, 200, put(first, map[string]any{"status": "Active"}).Code)
	require.Equal(t, 201, do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Third", "status": "Inactive"}}).Code)
	full := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Fourth"}})
	require.Equal(t, 403, full.Code, full.Raw)

	// Cashiers (no stores:edit) cannot change stores.
	createUser(t, l, tok, "cashier@stores-page.test", "biller", "stores-page-cashier-1")
	cashier, _ := login(t, l, "cashier@stores-page.test", "stores-page-cashier-1")
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%v", first), token: cashier, body: map[string]any{"status": "Inactive"}}).Code)
}
