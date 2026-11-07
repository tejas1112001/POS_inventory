//go:build integration

package isolation

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Postgres cannot store a NUL character in text, so a NUL that reaches a
// query fails there (SQLSTATE 22021) and used to surface as a 500. Every
// way in must refuse it with a 400 instead (found by the Phase 3 item 3
// injection test: GET /api/users?q=%00 answered 500).
func TestNULCharactersAreRefusedNot500(t *testing.T) {
	owner, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	const q = "?q=a%00b&search=a%00b&status=a%00b&storeId=a%00b&from=a%00b&to=a%00b&page=1&limit=10&type=a%00b&sort=a%00b"

	// Every tenant route: NUL in the query, and in the path parameters.
	// Sent without a token: the refusal comes before sign-in, and if it
	// ever went missing these requests would get 401 and change nothing.
	n := 0
	for _, rt := range sortedRoutes(t) {
		path := strings.NewReplacer("{id}", "1", "{slug}", "shop", "{product}", "1", "{barcode}", "123").Replace(rt.path)
		var body any
		if isWrite(rt.method) {
			body = map[string]any{}
		}
		r := do(t, rq{method: rt.method, host: host, path: path + q, body: body})
		require.Equal(t, 400, r.Code, "%s with NUL in the query: %s", rt.key, r.Raw)
		if strings.Contains(rt.path, "{") {
			nulPath := strings.NewReplacer("{id}", "1%00", "{slug}", "shop%00", "{product}", "1%00", "{barcode}", "12%003").Replace(rt.path)
			r = do(t, rq{method: rt.method, host: host, path: nulPath, body: body})
			require.Equal(t, 400, r.Code, "%s with NUL in the path: %s", rt.key, r.Raw)
		}
		n++
	}
	t.Logf("%d routes refuse NUL in the query and path", n)
	require.Equal(t, 400, do(t, rq{method: "GET", host: adminHost, path: "/api/platform/tenants?q=a%00b", token: superAdmin(t)}).Code)

	// JSON bodies: a NUL inside any string, at any depth.
	for _, c := range []struct {
		path string
		body map[string]any
	}{
		{"/api/customers", map[string]any{"name": "a\x00b"}},
		{"/api/roles", map[string]any{"name": "role\x00"}},
		{"/api/suppliers", map[string]any{"name": "ok", "email": "x\x00@y.test"}},
		{"/api/sales", map[string]any{"storeId": 1, "orderItems": []map[string]any{{"productId": 1, "variantId": 1, "quantity": 1, "note": "\x00"}}}},
	} {
		r := do(t, rq{method: "POST", host: host, path: c.path, token: owner, body: c.body})
		require.Equal(t, 400, r.Code, "%s: %s", c.path, r.Raw)
	}
	lr := do(t, rq{method: "POST", host: host, path: "/api/auth/login", body: map[string]any{"email": "owner\x00@iso-a.test", "password": "x"}})
	require.Equal(t, 400, lr.Code, lr.Raw)
	cr := do(t, rq{method: "POST", host: adminHost, path: "/api/platform/tenants", token: superAdmin(t), body: map[string]any{
		"name": "Nul\x00Shop", "slug": "nul-shop", "ownerEmail": "owner@nul.test", "ownerName": "X"}})
	require.Equal(t, 400, cr.Code, cr.Raw)

	// The landing contact form (it already refuses control characters per
	// field with 422 and the form shown again).
	tok := formToken(t, "/contact")
	time.Sleep(2100 * time.Millisecond)
	p := apex(t, "POST", "/contact", url.Values{"ft": {tok}, "topic": {"demo"}, "name": {"Nul\x00Name"},
		"email": {"nul-landing@iso.test"}, "message": {"hi"}}, "10.250.9.1")
	require.Equal(t, 422, p.Code, p.Body)
	var stored int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM platform.contact_requests WHERE email = 'nul-landing@iso.test'`).Scan(&stored))
	require.Zero(t, stored)

	// A CSV import with a NUL in a cell.
	var storeSlug string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT slug::text FROM `+schema("a")+`.stores WHERE deleted_at IS NULL ORDER BY id LIMIT 1`).Scan(&storeSlug))
	csv := "name,sku,price,store,opening_qty\nNul\x00Rice,NUL-1,10," + storeSlug + ",1\n"
	for _, dry := range []bool{true, false} {
		r := uploadCSV(t, "a", owner, csv, dry)
		require.Less(t, r.Code, 500, "CSV dry=%v: %s", dry, r.Raw)
	}
}
