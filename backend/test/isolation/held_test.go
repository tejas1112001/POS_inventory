//go:build integration

package isolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHeldBills (S10 item 7, D9): a cashier holds a cart (no stock moves);
// every cashier of that store sees it with who held it and can recall it
// once; carts of another store are not listed; at most 20 per user;
// expired carts disappear; staff without pos:create and other shops get
// nothing.
func TestHeldBills(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	stores := storeIDs(t, "a", tok)
	store := stores[0]
	p := stockedProduct(t, "a", tok, "HELD-1", store, 10)
	before := stockOf(t, "a", store, p.vid)
	createUser(t, "a", tok, "held-one@iso-a.test", "biller", "held-one-pass-12")
	createUser(t, "a", tok, "held-two@iso-a.test", "biller", "held-two-pass-12")
	one, _ := login(t, "a", "held-one@iso-a.test", "held-one-pass-12")
	two, _ := login(t, "a", "held-two@iso-a.test", "held-two-pass-12")
	body := func(label string) map[string]any {
		return map[string]any{"storeId": store, "label": label, "discount": 5,
			"lines": []map[string]any{{"productId": p.pid, "variantId": p.vid, "quantity": 2, "unitPrice": 120.5, "name": "Held product", "label": ""}}}
	}
	hold := func(tk string, b map[string]any) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/held-carts", token: tk, body: b})
	}
	held := func(tk string, s float64) []map[string]any {
		r := get(t, host, fmt.Sprintf("/api/held-carts?storeId=%v", s), tk)
		require.Equal(t, 200, r.Code, r.Raw)
		return list(t, r)
	}

	h := hold(one, body("Table 4"))
	require.Equal(t, 201, h.Code, h.Raw)
	require.Equal(t, "Table 4", h.Body["label"])
	require.Equal(t, 236.0, num(h.Body["estimate"]), "2 × 120.50 − 5")
	require.Equal(t, before, stockOf(t, "a", store, p.vid), "nothing is reserved")
	require.Equal(t, 400, hold(one, map[string]any{"storeId": store, "lines": []any{}}).Code)

	// The other cashier of the store sees it, with who held it.
	l := held(two, store)
	require.NotEmpty(t, l)
	var found map[string]any
	for _, m := range l {
		if m["id"] == h.Body["id"] {
			found = m
		}
	}
	require.NotNil(t, found)
	require.Equal(t, "User held-one@iso-a.test", found["userName"])
	if len(stores) > 1 {
		for _, x := range held(two, stores[1]) {
			require.NotEqual(t, h.Body["id"], x["id"], "another store's list")
		}
	}

	// Recall once (by the other cashier); a second recall is 404.
	rp := fmt.Sprintf("/api/held-carts/%v/recall", h.Body["id"])
	rc := do(t, rq{method: "POST", host: host, path: rp, token: two})
	require.Equal(t, 200, rc.Code, rc.Raw)
	require.Len(t, rc.Body["lines"], 1)
	require.Equal(t, 404, do(t, rq{method: "POST", host: host, path: rp, token: one}).Code)

	// Discard; expiry; the 20 cap.
	d := hold(one, body("To discard"))
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/held-carts/%v", d.Body["id"]), token: two}).Code)
	old := hold(one, body("Old"))
	_, err := admin(t).Exec(ctx, `UPDATE `+schema("a")+`.held_carts SET created_at = now() - interval '8 days', expires_at = now() - interval '1 day' WHERE id = $1`,
		int64(num(old.Body["id"])))
	require.NoError(t, err)
	for _, x := range held(one, store) {
		require.NotEqual(t, old.Body["id"], x["id"], "expired carts are not listed")
	}
	require.Equal(t, 404, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/held-carts/%v/recall", old.Body["id"]), token: one}).Code)
	n, err := theApp.Worker().Maintenance().SweepPOSLocks(ctx)
	require.NoError(t, err)
	require.Positive(t, n, "the sweep removes expired carts")
	for i := range 20 {
		require.Equal(t, 201, hold(two, body(fmt.Sprintf("Cap %d", i))).Code)
	}
	capped := hold(two, body("One too many"))
	require.Equal(t, 409, capped.Code)
	require.Contains(t, capped.Raw, "already hold 20 bills")

	// No pos:create (a custom role with only stock view): refused.
	role := do(t, rq{method: "POST", host: host, path: "/api/roles", token: tok, body: map[string]any{"name": "held-viewer-role"}})
	require.Equal(t, 201, role.Code, role.Raw)
	u := do(t, rq{method: "POST", host: host, path: "/api/users", token: tok, body: map[string]any{"name": "Held Viewer",
		"email": "held-viewer@iso-a.test", "password": "held-viewer-pass-1", "role_id": role.Body["id"], "status": "Active"}})
	require.Equal(t, 201, u.Code, u.Raw)
	viewer, _ := login(t, "a", "held-viewer@iso-a.test", "held-viewer-pass-1")
	require.Equal(t, 403, get(t, host, fmt.Sprintf("/api/held-carts?storeId=%v", store), viewer).Code)
	require.Equal(t, 403, hold(viewer, body("Not allowed")).Code)
	btk, _ := ownerLogin(t, "b")
	bStore := storeIDs(t, "b", btk)[0]
	br := get(t, tenants["b"].Host, fmt.Sprintf("/api/held-carts?storeId=%v", bStore), btk)
	require.Equal(t, 200, br.Code)
	require.NotContains(t, br.Raw, "Cap 1")
	require.Equal(t, 401, get(t, tenants["b"].Host, fmt.Sprintf("/api/held-carts?storeId=%v", bStore), tok).Code)
}
