//go:build integration

package isolation

import (
	"context"
	"encoding/base32"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

const adminHost = "admin.pos.test"

var (
	saOnce   sync.Once
	saToken  string
	saSecret []byte
	saStepAt time.Time
)

// superAdmin signs in the suite's Super Admin (bootstrap, password change,
// TOTP enrolment) once and returns a session that has passed step-up.
// TOTP codes of the previous and current 30-second step are both
// accepted, so enrolment and step-up need no wait.
func superAdmin(t *testing.T) string {
	t.Helper()
	saOnce.Do(func() {
		const email, pw, newPW = "root@platform.test", "bootstrap-iso-pass-x9", "iso-console-glacier-4242"
		_, err := platform.BootstrapSuperAdmin(context.Background(), tdb.MigratorDSN, email, "Root", pw,
			security.NewHasher(security.Argon2Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}, 1))
		require.NoError(t, err)
		p := func(path string, body any, tok string) resp {
			return do(t, rq{method: "POST", host: adminHost, path: "/api/platform" + path, body: body, token: tok})
		}
		r := p("/auth/login", map[string]string{"email": email, "password": pw}, "")
		require.Equal(t, 200, r.Code, r.Raw)
		r = p("/auth/password/change", map[string]string{"challengeId": r.Body["challengeId"].(string), "newPassword": newPW}, "")
		require.Equal(t, 200, r.Code, r.Raw)
		cid := r.Body["challengeId"].(string)
		r = p("/auth/mfa/enroll", map[string]string{"challengeId": cid}, "")
		require.Equal(t, 200, r.Code, r.Raw)
		u, err := url.Parse(r.Body["otpauthUri"].(string))
		require.NoError(t, err)
		saSecret, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(u.Query().Get("secret"))
		require.NoError(t, err)
		now := time.Now()
		r = p("/auth/mfa/enroll/confirm", map[string]string{"challengeId": cid, "code": security.TOTPCode(saSecret, now.Add(-30*time.Second))}, "")
		require.Equal(t, 200, r.Code, r.Raw)
		saToken = r.Body["accessToken"].(string)
		r = p("/auth/step-up", map[string]string{"code": security.TOTPCode(saSecret, now)}, saToken)
		require.Equal(t, 200, r.Code, r.Raw)
		saStepAt = now
	})
	require.NotEmpty(t, saToken, "super admin sign-in failed earlier")
	if time.Since(saStepAt) > 4*time.Minute { // step-up lasts 5 minutes; a later step's code is new
		now := time.Now()
		r := do(t, rq{method: "POST", host: adminHost, path: "/api/platform/auth/step-up", token: saToken,
			body: map[string]string{"code": security.TOTPCode(saSecret, now)}})
		require.Equal(t, 200, r.Code, r.Raw)
		saStepAt = now
	}
	return saToken
}

func console(t *testing.T, method, path string, body any) resp {
	t.Helper()
	return do(t, rq{method: method, host: adminHost, path: "/api/platform" + path, body: body, token: superAdmin(t)})
}

// The console flow end to end on the real app: request an export, the
// worker runs it, request a restore test, posctl's restorer runs it into a
// scratch schema, the console shows it verified.
func TestConsoleExportAndRestoreTest(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "exp-console")
	id := tenants[l].ID
	markCustomer(t, l, "marker-exp-console-mine")
	markCustomer(t, "b", "marker-exp-console-other")

	r := console(t, "POST", "/tenants/"+id+"/exports", map[string]bool{"includeFiles": true})
	require.Equal(t, 202, r.Code, r.Raw)
	exportID := r.Body["id"].(string)
	s := waitExport(t, l, tenantio.KindManual)
	require.Equal(t, exportID, s.ID)
	require.Equal(t, "succeeded", s.Status, s.Err)
	list := console(t, "GET", "/tenants/"+id+"/exports", nil)
	require.Equal(t, 200, list.Code)
	first := list.Body["data"].([]any)[0].(map[string]any)
	require.Equal(t, "succeeded", first["status"])
	require.NotNil(t, first["sha256"])
	_, tables := archiveTables(t, s.Key)
	require.Contains(t, tables["customers"], "marker-exp-console-mine")
	require.NotContains(t, tables["customers"], "marker-exp-console-other")

	rt := console(t, "POST", "/tenants/"+id+"/restore-tests", nil)
	require.Equal(t, 202, rt.Code, rt.Raw)
	require.Equal(t, exportID, rt.Body["exportId"])
	store, err := files.NewFSStorage(storageDir)
	require.NoError(t, err)
	restorer := &tenantio.Restorer{DSN: tdb.MigratorDSN, Storage: store, Identity: exportIdent, ManifestKey: []byte(exportKey)}
	rep, err := restorer.Run(ctx, uuid.MustParse(rt.Body["id"].(string)))
	require.NoError(t, err)
	require.True(t, rep.OK)
	var scratchLeft bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, rep.Scratch).Scan(&scratchLeft))
	require.False(t, scratchLeft, "a restore test drops its scratch schema")

	done := console(t, "GET", "/tenants/"+id+"/restore-tests", nil)
	require.Equal(t, 200, done.Code)
	v := done.Body["data"].([]any)[0].(map[string]any)
	require.Equal(t, "succeeded", v["status"])
	require.Equal(t, true, v["verified"])
	require.NotEmpty(t, v["tables"])
}

// Every new console route: tenant credentials never work on the admin
// host, and the routes do not exist on tenant hosts.
func TestConsoleExportRoutesAcrossHostsAndTokens(t *testing.T) {
	l := billingTenant(t, "exp-cross")
	id := tenants[l].ID
	// A shared tenant's owner (a new tenant's welcome email would replace
	// the invite token the sign-in helper uses).
	ownerTok, _ := ownerLogin(t, "c")
	sa := superAdmin(t)
	for _, r := range []struct{ method, path string }{
		{"POST", "/api/platform/tenants/" + id + "/exports"},
		{"GET", "/api/platform/tenants/" + id + "/exports"},
		{"POST", "/api/platform/tenants/" + id + "/restore-tests"},
		{"GET", "/api/platform/tenants/" + id + "/restore-tests"},
	} {
		require.Equal(t, 401, do(t, rq{method: r.method, host: adminHost, path: r.path, token: ownerTok}).Code,
			"tenant token on the admin host: %s %s", r.method, r.path)
		require.Equal(t, 401, do(t, rq{method: r.method, host: adminHost, path: r.path}).Code, "signed out")
		for _, host := range []string{tenants[l].Host, tenants["a"].Host} {
			got := do(t, rq{method: r.method, host: host, path: r.path, token: sa})
			require.Equal(t, 404, got.Code, "platform route on tenant host %s: %s %s", host, r.method, r.path)
			require.NotContains(t, got.Raw, id)
		}
		// A forged X-Forwarded-Host from a client that is not the proxy.
		req := httptest.NewRequest(r.method, "http://"+tenants[l].Host+r.path, nil)
		req.RemoteAddr = "203.0.113.50:1234"
		req.Header.Set("X-Forwarded-Host", adminHost)
		req.Header.Set("Authorization", "Bearer "+sa)
		rec := httptest.NewRecorder()
		theApp.Handler().ServeHTTP(rec, req)
		require.Equal(t, 404, rec.Code, "X-Forwarded-Host does not reach the console: %s %s", r.method, r.path)
	}
	// Nothing was queued by any of those.
	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM platform.tenant_exports WHERE tenant_id = $1`, id).Scan(&n))
	require.Zero(t, n)
}
