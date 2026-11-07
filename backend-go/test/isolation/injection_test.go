//go:build integration

// Phase 3 item 3: TENANCY §4. Injection strings in the Host, the slug and
// token claims never reach SQL: requests fail with the generic answers,
// nothing in the database changes, and no executed statement contains the
// hostile text (pg_stat_statements keeps the text of every statement; bind
// parameters show as $n, so only text built into SQL could appear).
package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

func hostileInputs(marker string) []string {
	return []string{
		`t_abc"; CREATE TABLE ` + marker + `_a (i int); --`,
		`iso-a'; CREATE TABLE ` + marker + `_b (i int); --`,
		`x" OR "1"="1`, `' OR 1=1 --`, `$1`, `*`, `%`, `_`, `\`,
		"public", "platform", "pg_catalog", "information_schema", "extensions", "pg_toast", "river",
		schema("a"), schema("a") + "_rw", "pos_app", "pos_owner",
		"іso-a",     // Cyrillic і
		"iso‐a",     // U+2010 hyphen
		"iso-a\x00", // NUL
		"iso-a%00", "%69so-a", "iso-a ", " iso-a", "../iso-a", "iso-a/..", "iso_a", "ISO-A--", "-iso-a",
		strings.Repeat("a", 300),
	}
}

func TestInjectionStringsNeverReachSQL(t *testing.T) {
	ctx := context.Background()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	marker := "pwned" + hex.EncodeToString(b)
	evil := hostileInputs(marker)

	db := admin(t)
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(ctx, q, args...).Scan(&n))
		return n
	}
	schemasBefore := count(`SELECT count(*) FROM pg_namespace`)
	tenantsBefore := count(`SELECT count(*) FROM platform.tenants`)
	rolesBefore := count(`SELECT count(*) FROM pg_roles`)

	serve := func(method, host, fwd, path, token, body string) int {
		t.Helper()
		var req = httptest.NewRequest(method, "http://placeholder"+path, strings.NewReader(body))
		req.Host = host
		if fwd != "" {
			req.RemoteAddr = "127.0.0.1:5000" // the trusted proxy
			req.Header.Set("X-Forwarded-Host", fwd)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		theApp.Handler().ServeHTTP(rec, req)
		return rec.Code
	}

	sa := superAdmin(t)
	owner, _ := ownerLogin(t, "a")
	n, accepted := 0, 0
	for _, e := range evil {
		// 1. Host and forwarded host: the generic 404 or 421, nothing else.
		for _, host := range []string{e, e + ".pos.test", "admin." + e, e + ".admin.pos.test"} {
			if strings.TrimSpace(host) == tenants["a"].Host {
				continue // surrounding spaces are trimmed: this is tenant A's real host
			}
			for _, path := range []string{"/api/public/tenant-info", "/api/auth/me", "/api/platform/me"} {
				for _, code := range []int{serve("GET", host, "", path, "", ""), serve("GET", "iso-a.pos.test", host, path, "", "")} {
					require.Contains(t, []int{404, 421}, code, "host %q %s", host, path)
					n++
				}
			}
		}
		// 2. Token claims, signed with the real key on tenant B's host.
		for name, mut := range map[string]func(jwt.MapClaims){
			"tid": func(c jwt.MapClaims) { c["tid"] = e },
			"sub": func(c jwt.MapClaims) { c["sub"] = e },
			"iss": func(c jwt.MapClaims) { c["iss"] = e },
			"sid": func(c jwt.MapClaims) { c["sid"] = e },
		} {
			tok := forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { mut(c); return nil, nil })
			r := do(t, rq{method: "GET", host: tenants["b"].Host, path: "/api/auth/me", token: tok})
			require.Contains(t, []int{200, 401}, r.Code, "%s claim %q: %s", name, e, r.Raw)
			if name != "sid" {
				require.Equal(t, 401, r.Code, "%s claim %q", name, e)
			}
			n++
		}
		kid := forge(t, func(h map[string]any, _ jwt.MapClaims) (jwt.SigningMethod, any) { h["kid"] = e; return nil, nil })
		require.Equal(t, 401, do(t, rq{method: "GET", host: tenants["b"].Host, path: "/api/auth/me", token: kid}).Code)

		// 3. Slugs and text in paths, queries and bodies.
		esc := url.PathEscape(e)
		for _, path := range []string{"/api/public/shop/" + esc, "/api/public/shop/" + esc + "/products", "/api/public/shop/" + esc + "/products/1"} {
			r := do(t, rq{method: "GET", host: tenants["a"].Host, path: path})
			require.Contains(t, []int{400, 404}, r.Code, "%s: %s", path, r.Raw)
		}
		q := url.QueryEscape(e)
		for _, path := range []string{"/api/products?q=" + q, "/api/customers?q=" + q, "/api/users?q=" + q, "/api/sales?q=" + q + "&storeId=" + q,
			"/api/products/barcode/" + esc} {
			r := do(t, rq{method: "GET", host: tenants["a"].Host, path: path, token: owner})
			require.Less(t, r.Code, 500, "%s: %s", path, r.Raw)
		}
		lr := do(t, rq{method: "POST", host: tenants["a"].Host, path: "/api/auth/login", body: map[string]any{"email": e + "@x.test", "password": e}})
		require.Contains(t, []int{400, 401}, lr.Code, lr.Raw)
		// The console refuses anything that is not a free, valid slug. A
		// valid one (e.g. "public") is just a host name: the tenant's schema
		// and role come from its random ID, never from the slug.
		cr := do(t, rq{method: "POST", host: adminHost, path: "/api/platform/tenants", token: sa, body: map[string]any{
			"name": "Injected", "slug": e, "ownerEmail": "owner@injected.test", "ownerName": "X"}})
		norm := strings.ToLower(strings.TrimSpace(e)) // the console trims and lowercases
		if !httpx.ValidSlug(norm) || tenancy.ReservedSlugs[norm] {
			require.Equal(t, 400, cr.Code, "console slug %q: %s", e, cr.Raw)
			continue
		}
		if norm == tenants["a"].Slug {
			require.Equal(t, 409, cr.Code, "console slug %q: %s", e, cr.Raw)
			continue
		}
		require.Equal(t, 202, cr.Code, "console slug %q: %s", e, cr.Raw)
		accepted++
		id := cr.Body["id"].(string)
		var schemaName, role string
		require.NoError(t, db.QueryRow(ctx, `SELECT schema_name, db_role FROM platform.tenants WHERE id = $1`, id).Scan(&schemaName, &role))
		require.Equal(t, "t_"+id, schemaName, "slug %q", e)
		require.Equal(t, "t_"+id+"_rw", role, "slug %q", e)
	}
	t.Logf("%d host requests and %d hostile inputs through claims, slugs, queries, login and the console", n, len(evil))

	t.Logf("console: %d hostile slugs that are valid, free slugs were accepted with ID-based schemas", accepted)
	require.Equal(t, schemasBefore+accepted, count(`SELECT count(*) FROM pg_namespace`), "only the accepted tenants' schemas")
	require.Equal(t, tenantsBefore+accepted, count(`SELECT count(*) FROM platform.tenants`), "only the accepted tenants")
	require.Equal(t, rolesBefore+accepted, count(`SELECT count(*) FROM pg_roles`), "only the accepted tenants' roles")
	require.Zero(t, count(`SELECT count(*) FROM pg_class WHERE relname LIKE $1`, marker+"%"))
	require.Zero(t, count(`SELECT count(*) FROM extensions.pg_stat_statements WHERE strpos(query, $1) > 0`, marker),
		"no executed statement contains the hostile text")
	// The control for the check above: statement texts are recorded.
	require.Positive(t, count(`SELECT count(*) FROM extensions.pg_stat_statements WHERE strpos(query, 'platform.tenants') > 0`))
}
