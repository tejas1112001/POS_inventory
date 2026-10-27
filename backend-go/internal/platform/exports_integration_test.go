//go:build integration

package platform_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

func (e *env) stepUp(t *testing.T, s session) {
	t.Helper()
	e.advance(31 * time.Second)
	r := e.do(req{method: "POST", path: "/auth/step-up", token: s.token, body: map[string]string{"code": security.TOTPCode(s.secret, e.clock())}})
	require.Equal(t, 200, r.Code, r.Raw)
}

func (e *env) newTenant(t *testing.T, s session, slug string) string {
	t.Helper()
	r := e.do(req{method: "POST", path: "/tenants", token: s.token, body: map[string]string{
		"name": "Shop " + slug, "slug": slug, "ownerEmail": "owner@" + slug + ".test", "ownerName": "Owner"}})
	require.Equal(t, 202, r.Code, r.Raw)
	return r.Body["id"].(string)
}

func ids(t *testing.T, r resp) []string {
	t.Helper()
	require.Equal(t, 200, r.Code, r.Raw)
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Raw), &body))
	out := []string{}
	for _, d := range body.Data {
		out = append(out, d.ID)
	}
	return out
}

// succeed marks an export as finished (the worker's part, tested in
// tenantio and the isolation suite).
func succeed(t *testing.T, id string) {
	t.Helper()
	_, err := tdb.Admin(t).Exec(context.Background(), `UPDATE platform.tenant_exports SET status = 'succeeded', started_at = now(),
		finished_at = now(), object_key = 'exports/x/' || id || '.tar.zst.age', sha256 = repeat('a', 64), manifest_sha256 = repeat('b', 64),
		schema_version = 1, size_bytes = 10 WHERE id = $1`, id)
	require.NoError(t, err)
}

func TestExportsAndRestoreTestsInTheConsole(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	a := e.newTenant(t, s, "xport-a")
	b := e.newTenant(t, s, "xport-b")

	// Requesting an export needs step-up.
	ex := e.do(req{method: "POST", path: "/tenants/" + a + "/exports", token: s.token, body: map[string]bool{"includeFiles": true}})
	require.Equal(t, 403, ex.Code)
	require.Equal(t, "STEP_UP_REQUIRED", ex.Body["code"])
	e.stepUp(t, s)
	ex = e.do(req{method: "POST", path: "/tenants/" + a + "/exports", token: s.token, body: map[string]bool{"includeFiles": true}})
	require.Equal(t, 202, ex.Code, ex.Raw)
	exportID := ex.Body["id"].(string)
	require.Equal(t, "queued", ex.Body["status"])
	require.Equal(t, "manual", ex.Body["kind"])
	require.Equal(t, true, ex.Body["includeFiles"])
	require.Equal(t, rootEmail, ex.Body["requestedBy"])
	e.mu.Lock()
	require.Contains(t, e.queued, uuid.MustParse(exportID), "the export job is started")
	e.mu.Unlock()
	busy := e.do(req{method: "POST", path: "/tenants/" + a + "/exports", token: s.token})
	require.Equal(t, 409, busy.Code, "one export at a time")
	require.Equal(t, 202, e.do(req{method: "POST", path: "/tenants/" + b + "/exports", token: s.token}).Code, "another tenant is not blocked")

	// Lists show only the tenant's own exports, without storage keys.
	la := e.do(req{method: "GET", path: "/tenants/" + a + "/exports", token: s.token})
	require.Equal(t, []string{exportID}, ids(t, la))
	require.NotContains(t, la.Raw, "object")
	require.NotContains(t, ids(t, e.do(req{method: "GET", path: "/tenants/" + b + "/exports", token: s.token})), exportID)

	// Unknown and malformed tenants: the generic 404.
	for _, id := range []string{"zzzzzzzzzz", "not-an-id", "abc%27--"} {
		require.Equal(t, 404, e.do(req{method: "POST", path: "/tenants/" + id + "/exports", token: s.token}).Code, id)
		require.Equal(t, 404, e.do(req{method: "GET", path: "/tenants/" + id + "/exports", token: s.token}).Code, id)
		require.Equal(t, 404, e.do(req{method: "POST", path: "/tenants/" + id + "/restore-tests", token: s.token}).Code, id)
		require.Equal(t, 404, e.do(req{method: "GET", path: "/tenants/" + id + "/restore-tests", token: s.token}).Code, id)
	}

	// Restore tests need no step-up, but a succeeded export of this tenant.
	e.advance(6 * time.Minute) // step-up expired
	require.Equal(t, 404, e.do(req{method: "POST", path: "/tenants/" + a + "/restore-tests", token: s.token}).Code, "nothing exported yet")
	succeed(t, exportID)
	rt := e.do(req{method: "POST", path: "/tenants/" + a + "/restore-tests", token: s.token})
	require.Equal(t, 202, rt.Code, rt.Raw)
	require.Equal(t, exportID, rt.Body["exportId"], "the latest succeeded export")
	require.Equal(t, "test", rt.Body["mode"])
	require.Equal(t, "queued", rt.Body["status"])
	require.Nil(t, rt.Body["verified"])
	require.Equal(t, 409, e.do(req{method: "POST", path: "/tenants/" + a + "/restore-tests", token: s.token}).Code, "one restore at a time")

	// Another tenant's export cannot be named.
	cross := e.do(req{method: "POST", path: "/tenants/" + b + "/restore-tests", token: s.token, body: map[string]string{"exportId": exportID}})
	require.Equal(t, 404, cross.Code)
	require.Equal(t, 400, e.do(req{method: "POST", path: "/tenants/" + b + "/restore-tests", token: s.token,
		body: map[string]string{"exportId": "nope"}}).Code)
	require.Equal(t, []string{rt.Body["id"].(string)}, ids(t, e.do(req{method: "GET", path: "/tenants/" + a + "/restore-tests", token: s.token})))
	require.Empty(t, ids(t, e.do(req{method: "GET", path: "/tenants/" + b + "/restore-tests", token: s.token})))

	// A finished test shows its result.
	_, err := tdb.Admin(t).Exec(context.Background(), `UPDATE platform.tenant_restores SET status = 'succeeded', finished_at = now(),
		result = '{"ok": true, "rows": 42, "tables": [{"name": "customers", "rows": 2, "ok": true}]}' WHERE id = $1`, rt.Body["id"])
	require.NoError(t, err)
	done := e.do(req{method: "GET", path: "/tenants/" + a + "/restore-tests", token: s.token})
	var lst struct {
		Data []platform.RestoreView `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(done.Raw), &lst))
	require.Len(t, lst.Data, 1)
	require.True(t, *lst.Data[0].Verified)
	require.EqualValues(t, 42, *lst.Data[0].Rows)
	require.JSONEq(t, `[{"name": "customers", "rows": 2, "ok": true}]`, string(lst.Data[0].Tables))

	// Both requests are audited against the tenant.
	audit := e.do(req{method: "GET", path: "/audit?tenant=" + a, token: s.token})
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit.Raw), &rows))
	var actions []string
	for _, r := range rows {
		actions = append(actions, r["action"].(string))
	}
	require.Contains(t, actions, "tenant.export")
	require.Contains(t, actions, "tenant.restore_test")
}

func TestExportRoutesNeedTenantsExport(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	id := e.newTenant(t, s, "xport-perm")
	for _, role := range []string{platform.RoleSupport, platform.RoleBillingAdmin} {
		email := "xport-" + role + "@platform.test"
		secret := addUser(t, e, email, role, "xport-person-pass-77")
		u := e.mfaLogin(t, email, "xport-person-pass-77", secret)
		e.stepUp(t, u)
		for _, r := range []req{
			{method: "POST", path: "/tenants/" + id + "/exports"},
			{method: "GET", path: "/tenants/" + id + "/exports"},
			{method: "POST", path: "/tenants/" + id + "/restore-tests"},
			{method: "GET", path: "/tenants/" + id + "/restore-tests"},
		} {
			r.token = u.token
			require.Equal(t, 403, e.do(r).Code, role+" "+r.method+" "+r.path)
		}
	}
	// Signed out: 401; on a tenant host: not served.
	require.Equal(t, 401, e.do(req{method: "GET", path: "/tenants/" + id + "/exports"}).Code)
	require.Equal(t, 404, e.do(req{method: "GET", path: "/tenants/" + id + "/exports", token: s.token, host: "xport-perm.pos.test"}).Code)
	// A tenant access token (even one issued for the admin host) is not a
	// platform token.
	tok, _, err := e.tenSign.Sign("admin.pos.test", "1", authtoken.Claims{SessionID: "x"})
	require.NoError(t, err)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/tenants/" + id + "/exports", token: tok}).Code)
}

func TestExportsOffRefusesRequests(t *testing.T) {
	e := newEnvWith(t, func(c *platform.Config) { c.ExportsEnabled = false })
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	id := e.newTenant(t, s, "xport-off")
	e.stepUp(t, s)
	r := e.do(req{method: "POST", path: "/tenants/" + id + "/exports", token: s.token})
	require.Equal(t, 409, r.Code)
	require.Equal(t, "INVALID_STATE", r.Body["code"])
	require.True(t, slices.Equal([]string{}, ids(t, e.do(req{method: "GET", path: "/tenants/" + id + "/exports", token: s.token}))))
}
