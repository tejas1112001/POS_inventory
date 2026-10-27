//go:build integration

// Phase 3 item 1: the API security matrix for tenant routes. Each test
// walks every declared route (theApp.TenantRoutes), so a route added later
// is covered without editing the test.
package isolation

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
)

type route struct {
	key, method, path string
	req               rbac.Requirement
}

// sortedRoutes returns every declared tenant route in a stable order.
func sortedRoutes(t *testing.T) []route {
	t.Helper()
	decl := theApp.TenantRoutes()
	require.NotEmpty(t, decl)
	out := make([]route, 0, len(decl))
	for key, req := range decl {
		method, path, _ := strings.Cut(key, " ")
		out = append(out, route{key: key, method: method, path: path, req: req})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func isWrite(method string) bool { return method != http.MethodGet && method != http.MethodHead }

// call sends one request with an empty JSON body on writes.
func call(t *testing.T, rt route, host, token string, hdr map[string]string) resp {
	t.Helper()
	var body any
	if isWrite(rt.method) {
		body = map[string]any{}
	}
	return do(t, rq{method: rt.method, host: host, path: fill(rt.path), token: token, body: body, header: hdr})
}

func codeOf(r resp) string { s, _ := r.Body["code"].(string); return s }

func TestEveryPermissionRouteNamesACatalogPermission(t *testing.T) {
	known := map[string]bool{}
	for _, p := range rbac.Catalog {
		known[p.Key()] = true
	}
	n := 0
	for _, rt := range sortedRoutes(t) {
		if rt.req.Kind != rbac.KindPermission {
			continue
		}
		n++
		require.True(t, known[rt.req.Perm], "%s requires %q, which is not in the permission catalog: no role could ever call it", rt.key, rt.req.Perm)
	}
	require.Greater(t, n, 50)
}

// ---------------------------------------------------------- forged tokens

// forgeFor caches tenant B's owner (ID and token version) for one test.
var forgeFor struct {
	t   *testing.T
	uid int64
	tv  int
}

// forge signs claims as the app would, then lets mutate break one thing.
func forge(t *testing.T, mutate func(h map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any)) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(jwtKey)
	require.NoError(t, err)
	if forgeFor.t != t { // one lookup per test: each admin(t) is a new connection
		forgeFor.t = t
		require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id, token_version FROM `+schema("b")+`.users WHERE email = $1`,
			tenants["b"].OwnerEmail).Scan(&forgeFor.uid, &forgeFor.tv))
	}
	uid, tv := forgeFor.uid, forgeFor.tv
	now := time.Now()
	c := jwt.MapClaims{
		"iss": tenants["b"].Host, "sub": strconv.FormatInt(uid, 10), "aud": []string{authtoken.AudTenant},
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"tid": tenants["b"].ID, "tv": tv,
	}
	h := map[string]any{"kid": "k1"}
	method, signKey := jwt.SigningMethod(jwt.SigningMethodHS256), any(key)
	if mutate != nil {
		method, signKey = mutate(h, c)
		if method == nil {
			method, signKey = jwt.SigningMethodHS256, key
		}
	}
	tok := jwt.NewWithClaims(method, c)
	for k, v := range h {
		tok.Header[k] = v
	}
	s, err := tok.SignedString(signKey)
	require.NoError(t, err)
	return s
}

func TestEveryProtectedRouteRejectsMissingAndForgedTokens(t *testing.T) {
	ownerLogin(t, "b") // the owner has a password and a session row
	key, _ := base64.StdEncoding.DecodeString(jwtKey)
	other := []byte("a-completely-different-signing-key-0123456789")
	keep := func(map[string]any, jwt.MapClaims) (jwt.SigningMethod, any) { return nil, nil }

	// Positive control: the forging code makes tokens the app accepts.
	good := forge(t, keep)
	require.Equal(t, 200, do(t, rq{method: "GET", host: tenants["b"].Host, path: "/api/auth/me", token: good}).Code, "control token must work")

	bad := map[string]string{
		"not a JWT": "not-a-token",
		"alg none": forge(t, func(map[string]any, jwt.MapClaims) (jwt.SigningMethod, any) {
			return jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType
		}),
		"HS512, right key": forge(t, func(map[string]any, jwt.MapClaims) (jwt.SigningMethod, any) { return jwt.SigningMethodHS512, key }),
		"wrong key":        forge(t, func(map[string]any, jwt.MapClaims) (jwt.SigningMethod, any) { return jwt.SigningMethodHS256, other }),
		"unknown kid":      forge(t, func(h map[string]any, _ jwt.MapClaims) (jwt.SigningMethod, any) { h["kid"] = "k9"; return nil, nil }),
		"no kid":           forge(t, func(h map[string]any, _ jwt.MapClaims) (jwt.SigningMethod, any) { delete(h, "kid"); return nil, nil }),
		"expired": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["iat"], c["nbf"], c["exp"] = time.Now().Add(-20*time.Minute).Unix(), time.Now().Add(-20*time.Minute).Unix(), time.Now().Add(-10*time.Minute).Unix()
			return nil, nil
		}),
		"no exp": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { delete(c, "exp"); return nil, nil }),
		"no iat": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { delete(c, "iat"); return nil, nil }),
		"lifetime over 15 min": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["exp"] = time.Now().Add(time.Hour).Unix()
			return nil, nil
		}),
		"not yet valid": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["nbf"] = time.Now().Add(10 * time.Minute).Unix()
			return nil, nil
		}),
		"platform audience": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["aud"] = []string{authtoken.AudPlatform}
			return nil, nil
		}),
		"no audience": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { delete(c, "aud"); return nil, nil }),
		"issuer is tenant A": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["iss"] = tenants["a"].Host
			return nil, nil
		}),
		"tid is tenant A": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["tid"] = tenants["a"].ID
			return nil, nil
		}),
		"no tid":       forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { delete(c, "tid"); return nil, nil }),
		"unknown user": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { c["sub"] = "999999"; return nil, nil }),
		"non-numeric sub": forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) {
			c["sub"] = "1 OR 1=1"
			return nil, nil
		}),
		"empty sub":         forge(t, func(_ map[string]any, c jwt.MapClaims) (jwt.SigningMethod, any) { c["sub"] = ""; return nil, nil }),
		"super admin token": superAdmin(t),
	}
	headers := map[string]map[string]string{
		"no Authorization":    nil,
		"empty Bearer":        {"Authorization": "Bearer "},
		"Basic scheme":        {"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("owner:"+ownerPW))},
		"token in cookie":     {"Cookie": "token=" + good},
		"lowercase bearer":    {"Authorization": "bearer " + good},
		"token in query":      nil, // the path below appends ?token=
		"refresh cookie only": {"Cookie": "__Host-rt=whatever"},
	}

	host := tenants["b"].Host
	checked := 0
	for _, rt := range sortedRoutes(t) {
		if rt.req.Kind == rbac.KindPublic {
			continue
		}
		for name, tok := range bad {
			r := call(t, rt, host, tok, nil)
			require.Equal(t, 401, r.Code, "%s with %s: %s", rt.key, name, r.Raw)
		}
		for name, h := range headers {
			path := fill(rt.path)
			if name == "token in query" {
				path += "?token=" + good + "&access_token=" + good
			}
			var body any
			if isWrite(rt.method) {
				body = map[string]any{}
			}
			r := do(t, rq{method: rt.method, host: host, path: path, body: body, header: h})
			require.Equal(t, 401, r.Code, "%s with %s: %s", rt.key, name, r.Raw)
		}
		checked++
	}
	require.Greater(t, checked, 50)
	t.Logf("checked %d protected routes x %d bad tokens and %d bad headers", checked, len(bad), len(headers))
}

// ------------------------------------------------------------ permissions

func TestEveryPermissionRouteRefusesAUserWithoutIt(t *testing.T) {
	owner, _ := ownerLogin(t, "b")
	none := userWithPerms(t, "b", owner, "matrix-no-perms@iso-b.test")
	host := tenants["b"].Host
	perm, authed := 0, 0
	for _, rt := range sortedRoutes(t) {
		switch rt.req.Kind {
		case rbac.KindPermission:
			r := call(t, rt, host, none, nil)
			require.Equal(t, 403, r.Code, "%s needs %s: %s", rt.key, rt.req.Perm, r.Raw)
			require.Equal(t, apperr.CodeForbidden, codeOf(r), rt.key)
			perm++
		case rbac.KindAuthenticated:
			// Any signed-in user passes the guard; the handler may still say
			// no for its own reasons, but never "not signed in".
			r := call(t, rt, host, none, nil)
			require.NotEqual(t, 401, r.Code, "%s: %s", rt.key, r.Raw)
			require.Less(t, r.Code, 500, "%s: %s", rt.key, r.Raw)
			authed++
		}
	}
	t.Logf("%d permission routes refused, %d signed-in routes reachable, for a user with no permissions", perm, authed)
}

// ------------------------------------------------------------ tenant gate

func waitGate(t *testing.T, l, status string, probe func() bool) {
	t.Helper()
	setStatus(t, l, status)
	eventually(t, probe, l+" -> "+status)
}

func TestTenantGateCoversEveryRoute(t *testing.T) {
	c := tenants["c"]
	owner, _ := ownerLogin(t, "c")
	me := func() int { return do(t, rq{method: "GET", host: c.Host, path: "/api/auth/me", token: owner}).Code }
	defer waitGate(t, "c", "trialing", func() bool { return me() == 200 })

	routes := sortedRoutes(t)
	waitGate(t, "c", "read_only", func() bool {
		return codeOf(do(t, rq{method: "POST", host: c.Host, path: "/api/roles", token: owner, body: map[string]any{"name": "gate"}})) == apperr.CodeTenantReadOnly
	})
	blocked, exempt := 0, 0
	for _, rt := range routes {
		if !isWrite(rt.method) {
			continue
		}
		r := call(t, rt, c.Host, owner, nil)
		if rt.req.AllowReadOnly {
			require.True(t, rbac.AllowReadOnlyList[rt.key], rt.key)
			require.NotEqual(t, apperr.CodeTenantReadOnly, codeOf(r), "%s is exempt: %s", rt.key, r.Raw)
			exempt++
			continue
		}
		require.Equal(t, 403, r.Code, "%s on a read_only tenant: %s", rt.key, r.Raw)
		require.Equal(t, apperr.CodeTenantReadOnly, codeOf(r), rt.key)
		blocked++
	}
	declared := theApp.TenantRoutes()
	for k := range rbac.AllowReadOnlyList {
		_, ok := declared[k]
		require.True(t, ok, "allow-listed route %s is not declared", k)
	}
	t.Logf("read_only: %d write routes blocked, %d exempt", blocked, exempt)

	for _, status := range []string{"suspended", "pending_deletion"} {
		waitGate(t, "c", status, func() bool { return me() == 403 })
		n := 0
		for _, rt := range routes {
			if rt.req.Kind == rbac.KindPublic {
				continue
			}
			r := call(t, rt, c.Host, owner, nil)
			require.Equal(t, 403, r.Code, "%s on a %s tenant: %s", rt.key, status, r.Raw)
			require.Equal(t, apperr.CodeTenantSuspended, codeOf(r), rt.key)
			n++
		}
		t.Logf("%s: %d protected routes refused", status, n)
	}
}

// ------------------------------------------------------------ body limits

func TestEveryWriteRouteEnforcesItsBodyLimit(t *testing.T) {
	buf := make([]byte, httpx.UploadBodyLimit+1)
	host := tenants["b"].Host
	n := 0
	for _, rt := range sortedRoutes(t) {
		if !isWrite(rt.method) {
			continue
		}
		limit := rt.req.BodyLimit
		if limit <= 0 {
			limit = httpx.DefaultBodyLimit
		}
		req := httptest.NewRequest(rt.method, "http://"+host+fill(rt.path), bytes.NewReader(buf[:limit+1]))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		theApp.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "%s with %d bytes: %s", rt.key, limit+1, rec.Body.String())
		n++
	}
	t.Logf("%d write routes refuse a body over their limit before anything else runs", n)

	// Without a Content-Length the limit applies while reading
	// (POST /api/roles has the default 1 MiB limit).
	require.Zero(t, theApp.TenantRoutes()["POST /api/roles"].BodyLimit)
	owner, _ := ownerLogin(t, "b")
	big := strings.Repeat("x", int(httpx.DefaultBodyLimit))
	body := io.MultiReader(strings.NewReader(`{"name":"`), strings.NewReader(big), strings.NewReader(`"}`))
	req := httptest.NewRequest("POST", "http://"+host+"/api/roles", io.NopCloser(body))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+owner)
	rec := httptest.NewRecorder()
	theApp.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
}

// ------------------------------------------------------------------ CSRF

func TestCookieRoutesRefuseCrossSiteRequests(t *testing.T) {
	a := tenants["a"]
	for _, path := range []string{"/api/auth/refresh", "/api/auth/logout"} {
		for name, h := range map[string]map[string]string{
			"no headers":           {},
			"header, no Origin":    {"X-Requested-With": "fetch"},
			"Origin, no header":    {"Origin": "http://" + a.Host},
			"foreign Origin":       {"Origin": "http://evil.example", "X-Requested-With": "fetch"},
			"other tenant Origin":  {"Origin": "http://" + tenants["b"].Host, "X-Requested-With": "fetch"},
			"apex Origin":          {"Origin": "http://pos.test", "X-Requested-With": "fetch"},
			"wrong scheme":         {"Origin": "https://" + a.Host, "X-Requested-With": "fetch"},
			"null Origin":          {"Origin": "null", "X-Requested-With": "fetch"},
			"Origin with path":     {"Origin": "http://" + a.Host + "/x", "X-Requested-With": "fetch"},
			"suffix-match Origin":  {"Origin": "http://" + a.Host + ".evil.example", "X-Requested-With": "fetch"},
			"wrong header value":   {"Origin": "http://" + a.Host, "X-Requested-With": "XMLHttpRequest"},
			"Referer only":         {"Referer": "http://" + a.Host + "/", "X-Requested-With": "fetch"},
			"userinfo in Origin":   {"Origin": "http://user@" + a.Host, "X-Requested-With": "fetch"},
			"port-mismatch Origin": {"Origin": "http://" + a.Host + ":8080", "X-Requested-With": "fetch"},
		} {
			_, cookie := ownerLogin(t, "a")
			r := do(t, rq{method: "POST", host: a.Host, path: path, cookie: cookie, header: h})
			require.Equal(t, 403, r.Code, "%s %s: %s", path, name, r.Raw)
			// The session is untouched: the same cookie still refreshes.
			ok := do(t, rq{method: "POST", host: a.Host, path: "/api/auth/refresh", cookie: cookie, csrf: true})
			require.Equal(t, 200, ok.Code, "%s %s must not have used the cookie: %s", path, name, ok.Raw)
		}
	}
}

// ------------------------------------------------------- unknown methods

func TestUnknownMethodsAndPathsAreGeneric404(t *testing.T) {
	owner, _ := ownerLogin(t, "b")
	host := tenants["b"].Host
	ref := do(t, rq{method: "GET", host: host, path: "/api/no-such-route", token: owner})
	require.Equal(t, 404, ref.Code)
	declared := theApp.TenantRoutes()
	n := 0
	for _, rt := range sortedRoutes(t) {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if _, ok := declared[m+" "+rt.path]; ok {
				continue
			}
			r := do(t, rq{method: m, host: host, path: fill(rt.path), token: owner, body: map[string]any{}})
			require.Equal(t, 404, r.Code, "%s %s: %s", m, rt.path, r.Raw)
			require.Equal(t, ref.Raw, r.Raw, "%s %s: same body as any unknown path", m, rt.path)
			n++
		}
	}
	t.Logf("%d undeclared method/path pairs answer the generic 404", n)
	require.Equal(t, 404, do(t, rq{method: "TRACE", host: host, path: "/api/auth/me", token: owner}).Code)
}
