//go:build integration

package platform_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

var tdb *testdb.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if tdb, err = testdb.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = tdb.Stop(ctx)
	os.Exit(code)
}

var fast = security.Argon2Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}

const (
	rootEmail = "root@platform.test"
	rootPW    = "initial-bootstrap-pass-x9"
	newPW     = "glacier-violet-tram-4242"
)

type env struct {
	t        *testing.T
	h        http.Handler
	svcEnv   *secrets.Envelope
	tenSign  *authtoken.Signer
	ipSeq    atomic.Int64
	sessions *tenancy.Sessions
	registry *tenancy.Registry
}

// One clock for every test: TOTP replay state lives in the database, so
// time must only move forward across tests.
var testNow atomic.Int64

func init() { testNow.Store(time.Now().UnixNano()) }

func (e *env) clock() time.Time        { return time.Unix(0, testNow.Load()) }
func (e *env) advance(d time.Duration) { testNow.Add(int64(d)) }

func keyRing(t *testing.T, name string) config.KeyRing {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	k := base64.StdEncoding.EncodeToString(b)
	vars := map[string]string{
		"BASE_DOMAIN": "pos.test", "PLATFORM_NAME": "T", "APP_ENV": "test", "PUBLIC_SCHEME": "http",
		"DATABASE_URL": "postgres://a:b@h/d", "DATABASE_JOBS_URL": "postgres://a:b@h/d",
		"JWT_KEYS": "j:" + rk(), "PLATFORM_JWT_KEYS": "p:" + rk(), "MASTER_KEYS": "m:" + rk(),
	}
	vars[name] = "k1:" + k
	c, err := config.LoadFrom(vars)
	require.NoError(t, err)
	switch name {
	case "JWT_KEYS":
		return c.JWTKeys
	case "PLATFORM_JWT_KEYS":
		return c.PlatformJWTKeys
	}
	return c.MasterKeys
}

func rk() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

var (
	setupOnce sync.Once
	shared    *env
)

func newEnv(t *testing.T) *env {
	setupOnce.Do(func() {
		_, err := platform.BootstrapSuperAdmin(context.Background(), tdb.MigratorDSN, rootEmail, "Root", rootPW, security.NewHasher(fast, 2))
		if err != nil {
			panic(err)
		}
	})
	e := &env{t: t}
	mk := keyRing(t, "MASTER_KEYS")[0]
	var err error
	e.svcEnv, err = secrets.NewEnvelope([]secrets.KEK{{ID: "m1", Key: mk.Bytes()}})
	require.NoError(t, err)
	// The platform's envelope must be shared across envs (TOTP secrets persist).
	if shared != nil {
		e.svcEnv = shared.svcEnv
	}
	signer, err := authtoken.NewSigner(keyRing(t, "PLATFORM_JWT_KEYS"), authtoken.AudPlatform, 5*time.Minute)
	require.NoError(t, err)
	if shared != nil {
		signer = sharedSigner
	} else {
		sharedSigner = signer
	}
	e.tenSign, err = authtoken.NewSigner(keyRing(t, "JWT_KEYS"), authtoken.AudTenant, 10*time.Minute)
	require.NoError(t, err)
	e.sessions = tenancy.NewSessions(tdb.AppPool(t, 4), 5*time.Second, nil, nil)
	e.registry = tenancy.NewRegistry(e.sessions, nil)
	svc := platform.NewService(platform.Config{
		BaseDomain: "pos.test", PlatformName: "POS Test", SessionMax: 8 * time.Hour, IdleTimeout: 30 * time.Minute,
		Provisioner: provisioning.New(tdb.MigratorDSN, "https", "pos.test", slog.New(slog.DiscardHandler)),
		Registry:    e.registry, Log: slog.New(slog.DiscardHandler),
		Hasher: security.NewHasher(fast, 4), Envelope: e.svcEnv, Signer: signer, Sessions: e.sessions,
		Now: e.clock,
	})
	n := httpx.Network{BaseDomain: "pos.test"}
	ph := platform.NewHTTP(svc, n, nil, nil, "http")
	e.h = httpx.NewRouter(httpx.RouterDeps{
		Logger: slog.New(slog.DiscardHandler), Network: n, Health: httpx.NewHealth(time.Second), RequestTimeout: 10 * time.Second,
		Platform:       func(r chi.Router) { ph.Mount(r) },
		Tenant:         func(r chi.Router) {},
		TenantResolver: tenancy.Resolve(e.registry, n, "pos.test"),
	})
	if shared == nil {
		shared = e
	}
	return e
}

var sharedSigner *authtoken.Signer

type resp struct {
	Code   int
	Body   map[string]any
	Raw    string
	Header http.Header
}

type req struct {
	method, path string
	body         any
	token        string
	cookie       string
	ip           string
	host         string
	csrf         bool
}

func (e *env) do(r req) resp {
	e.t.Helper()
	var b io.Reader
	if r.body != nil {
		j, _ := json.Marshal(r.body)
		b = bytes.NewReader(j)
	}
	host := r.host
	if host == "" {
		host = "admin.pos.test"
	}
	hr := httptest.NewRequest(r.method, "http://"+host+"/api/platform"+r.path, b)
	if r.body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	if r.ip == "" {
		r.ip = fmt.Sprintf("198.51.100.%d", e.ipSeq.Add(1)%250+1)
	}
	hr.RemoteAddr = r.ip + ":5555"
	if r.token != "" {
		hr.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.cookie != "" {
		hr.AddCookie(&http.Cookie{Name: platform.CookieName, Value: r.cookie})
	}
	if r.csrf {
		hr.Header.Set("Origin", "http://"+host)
		hr.Header.Set("X-Requested-With", "fetch")
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, hr)
	out := resp{Code: rec.Code, Raw: rec.Body.String(), Header: rec.Header()}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.Body)
	return out
}

func cookieOf(t *testing.T, r resp) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == platform.CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", platform.CookieName, r.Header)
	return nil
}

func secretFromURI(t *testing.T, uri string) []byte {
	u, err := url.Parse(uri)
	require.NoError(t, err)
	s, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(u.Query().Get("secret"))
	require.NoError(t, err)
	return s
}

// session holds a signed-in platform user.
type session struct {
	token, cookie string
	secret        []byte
	recovery      []string
}

// firstLogin runs bootstrap login → password change → enrol (once per run).
var (
	rootOnce sync.Once
	rootSess session
)

func rootSession(t *testing.T, e *env) session {
	rootOnce.Do(func() {
		r := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": rootEmail, "password": rootPW}})
		require.Equal(t, 200, r.Code, r.Raw)
		require.Equal(t, true, r.Body["passwordChangeRequired"])
		cid := r.Body["challengeId"].(string)

		// The enroll endpoint refuses a password-change challenge.
		bad := e.do(req{method: "POST", path: "/auth/mfa/enroll", body: map[string]string{"challengeId": cid}})
		require.Equal(t, 401, bad.Code)

		weak := e.do(req{method: "POST", path: "/auth/password/change", body: map[string]string{"challengeId": cid, "newPassword": "short"}})
		require.Equal(t, 400, weak.Code)
		same := e.do(req{method: "POST", path: "/auth/password/change", body: map[string]string{"challengeId": cid, "newPassword": rootPW}})
		require.Equal(t, 400, same.Code, "must differ from current")

		r = e.do(req{method: "POST", path: "/auth/password/change", body: map[string]string{"challengeId": cid, "newPassword": newPW}})
		require.Equal(t, 200, r.Code, r.Raw)
		require.Equal(t, true, r.Body["mfaEnrollRequired"])
		cid = r.Body["challengeId"].(string)

		r = e.do(req{method: "POST", path: "/auth/mfa/enroll", body: map[string]string{"challengeId": cid}})
		require.Equal(t, 200, r.Code, r.Raw)
		secret := secretFromURI(t, r.Body["otpauthUri"].(string))

		wrong := e.do(req{method: "POST", path: "/auth/mfa/enroll/confirm", body: map[string]string{"challengeId": cid, "code": "000000"}})
		require.Equal(t, 401, wrong.Code)

		r = e.do(req{method: "POST", path: "/auth/mfa/enroll/confirm", body: map[string]string{"challengeId": cid, "code": security.TOTPCode(secret, e.clock())}})
		require.Equal(t, 200, r.Code, r.Raw)
		codes := r.Body["recoveryCodes"].([]any)
		require.Len(t, codes, platform.RecoveryCodeCount)
		c := cookieOf(t, r)
		require.True(t, c.HttpOnly)
		require.True(t, c.Secure)
		require.Equal(t, http.SameSiteStrictMode, c.SameSite)
		require.Equal(t, "/", c.Path)
		require.Empty(t, c.Domain, "__Host- cookies have no Domain")
		require.NotContains(t, r.Raw, c.Value, "refresh token never in the JSON body")

		rootSess = session{token: r.Body["accessToken"].(string), cookie: c.Value, secret: secret}
		for _, x := range codes {
			rootSess.recovery = append(rootSess.recovery, x.(string))
		}
		e.advance(31 * time.Second) // next TOTP step for later codes
	})
	return rootSess
}

func (e *env) mfaLogin(t *testing.T, email, pw string, secret []byte) session {
	t.Helper()
	r := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": email, "password": pw}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, true, r.Body["mfaRequired"])
	e.advance(31 * time.Second)
	v := e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": r.Body["challengeId"].(string), "code": security.TOTPCode(secret, e.clock())}})
	require.Equal(t, 200, v.Code, v.Raw)
	return session{token: v.Body["accessToken"].(string), cookie: cookieOf(t, v).Value, secret: secret}
}

func TestLoginEnrolAndMe(t *testing.T) {
	e := newEnv(t)
	s := rootSession(t, e)
	me := e.do(req{method: "GET", path: "/me", token: s.token})
	require.Equal(t, 200, me.Code, me.Raw)
	require.Equal(t, rootEmail, me.Body["email"])
	require.Equal(t, "super_admin", me.Body["role"])

	require.Equal(t, 401, e.do(req{method: "GET", path: "/me"}).Code)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: s.token + "x"}).Code)

	// A tenant-audience token signed with tenant keys is refused.
	tt, _, err := e.tenSign.Sign("admin.pos.test", "1", authtoken.Claims{SessionID: "x"})
	require.NoError(t, err)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: tt}).Code)

	// Platform routes do not exist on tenant hosts.
	require.Equal(t, 404, e.do(req{method: "GET", path: "/me", token: s.token, host: "acme.pos.test"}).Code)
}

func TestMFAReplayAndRecoveryCodes(t *testing.T) {
	e := newEnv(t)
	s := rootSession(t, e)

	r := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": rootEmail, "password": newPW}})
	require.Equal(t, 200, r.Code, r.Raw)
	cid := r.Body["challengeId"].(string)
	e.advance(31 * time.Second)
	code := security.TOTPCode(s.secret, e.clock())
	ok := e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": cid, "code": code}})
	require.Equal(t, 200, ok.Code, ok.Raw)

	// Same code again (same step) on a fresh challenge: replay refused.
	r = e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": rootEmail, "password": newPW}})
	cid = r.Body["challengeId"].(string)
	replay := e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": cid, "code": code}})
	require.Equal(t, 401, replay.Code)

	// Recovery code works once.
	rc := strings.ToUpper(strings.ReplaceAll(s.recovery[0], "-", ""))
	good := e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": cid, "recoveryCode": rc}})
	require.Equal(t, 200, good.Code, good.Raw)
	r = e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": rootEmail, "password": newPW}})
	again := e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": r.Body["challengeId"].(string), "recoveryCode": s.recovery[0]}})
	require.Equal(t, 401, again.Code)
}

func TestChallengeBurnsAfterFiveTries(t *testing.T) {
	e := newEnv(t)
	s := rootSession(t, e)
	r := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": rootEmail, "password": newPW}})
	cid := r.Body["challengeId"].(string)
	for range platform.MaxChallengeTries {
		require.Equal(t, 401, e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": cid, "code": "000000"}}).Code)
	}
	e.advance(31 * time.Second)
	late := e.do(req{method: "POST", path: "/auth/mfa/verify", body: map[string]string{"challengeId": cid, "code": security.TOTPCode(s.secret, e.clock())}})
	require.Equal(t, 401, late.Code, "challenge is burned after 5 wrong codes")
}

func TestRefreshRotationReuseAndCSRF(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)

	require.Equal(t, 403, e.do(req{method: "POST", path: "/auth/refresh", cookie: s.cookie}).Code, "no CSRF header")
	r := e.do(req{method: "POST", path: "/auth/refresh", cookie: s.cookie, csrf: true})
	require.Equal(t, 200, r.Code, r.Raw)
	next := cookieOf(t, r).Value
	require.NotEqual(t, s.cookie, next)

	reuse := e.do(req{method: "POST", path: "/auth/refresh", cookie: s.cookie, csrf: true})
	require.Equal(t, 401, reuse.Code, "rotated token reused")
	stolen := e.do(req{method: "POST", path: "/auth/refresh", cookie: next, csrf: true})
	require.Equal(t, 401, stolen.Code, "reuse revoked the whole session")
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: r.Body["accessToken"].(string)}).Code)
}

func TestLogoutAndIdleTimeout(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	require.Equal(t, 204, e.do(req{method: "POST", path: "/auth/logout", cookie: s.cookie, csrf: true}).Code)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: s.token}).Code, "revoked immediately")

	s = e.mfaLogin(t, rootEmail, newPW, root.secret)
	e.advance(31 * time.Minute)
	require.Equal(t, 401, e.do(req{method: "POST", path: "/auth/refresh", cookie: s.cookie, csrf: true}).Code, "idle timeout")
}

func TestStepUpAndTenantAdmin(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)

	created := e.do(req{method: "POST", path: "/tenants", token: s.token, body: map[string]string{
		"name": "Console Shop", "slug": "console-shop", "ownerEmail": "owner@console.test", "ownerName": "Ravi",
	}})
	require.Equal(t, 202, created.Code, created.Raw)
	id := created.Body["id"].(string)
	require.Equal(t, "trialing", created.Body["status"])

	list := e.do(req{method: "GET", path: "/tenants?q=console", token: s.token})
	require.Equal(t, 200, list.Code)
	require.EqualValues(t, 1, list.Body["pagination"].(map[string]any)["total"])

	got := e.do(req{method: "GET", path: "/tenants/" + id, token: s.token})
	require.Equal(t, 200, got.Code)
	require.Equal(t, "trialing", got.Body["subscription"].(map[string]any)["status"])
	require.NotContains(t, got.Raw, "password")

	patched := e.do(req{method: "PATCH", path: "/tenants/" + id, token: s.token, body: map[string]string{"name": "Console Shop Ltd", "billingEmail": "bills@console.test"}})
	require.Equal(t, 200, patched.Code, patched.Raw)
	require.Equal(t, "Console Shop Ltd", patched.Body["name"])

	// Destructive actions need step-up.
	sus := e.do(req{method: "POST", path: "/tenants/" + id + "/suspend", token: s.token, body: map[string]string{"reason": "chargeback dispute"}})
	require.Equal(t, 403, sus.Code)
	require.Equal(t, "STEP_UP_REQUIRED", sus.Body["code"])
	require.Equal(t, 401, e.do(req{method: "POST", path: "/auth/step-up", token: s.token, body: map[string]string{"code": "000000"}}).Code)
	e.advance(31 * time.Second)
	su := e.do(req{method: "POST", path: "/auth/step-up", token: s.token, body: map[string]string{"code": security.TOTPCode(root.secret, e.clock())}})
	require.Equal(t, 200, su.Code, su.Raw)

	sus = e.do(req{method: "POST", path: "/tenants/" + id + "/suspend", token: s.token, body: map[string]string{"reason": "chargeback dispute"}})
	require.Equal(t, 200, sus.Code, sus.Raw)
	require.Equal(t, "suspended", sus.Body["status"])
	tn, err := e.registry.BySlug(context.Background(), "console-shop")
	require.NoError(t, err)
	require.Equal(t, "suspended", tn.Status, "registry cache flushed")

	re := e.do(req{method: "POST", path: "/tenants/" + id + "/reactivate", token: s.token})
	require.Equal(t, 200, re.Code)
	require.Equal(t, "trialing", re.Body["status"], "back to trialing while the trial runs")

	slug := e.do(req{method: "POST", path: "/tenants/" + id + "/slug", token: s.token, body: map[string]string{"slug": "renamed-shop"}})
	require.Equal(t, 200, slug.Code, slug.Raw)
	require.Equal(t, "renamed-shop", slug.Body["slug"])
	// Page loads on the old host redirect; API calls do not.
	old := httptest.NewRequest("GET", "http://console-shop.pos.test/pos?x=1", nil)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, old)
	require.Equal(t, http.StatusPermanentRedirect, rec.Code)
	require.Equal(t, "http://renamed-shop.pos.test/pos?x=1", rec.Header().Get("Location"))
	oldAPI := httptest.NewRequest("GET", "http://console-shop.pos.test/api/products", nil)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, oldAPI)
	require.Equal(t, 404, rec.Code)

	taken := e.do(req{method: "POST", path: "/tenants", token: s.token, body: map[string]string{
		"name": "Squatter", "slug": "console-shop", "ownerEmail": "x@y.test"}})
	require.Equal(t, 409, taken.Code, "old slug stays reserved")

	del := e.do(req{method: "DELETE", path: "/tenants/" + id, token: s.token})
	require.Equal(t, 200, del.Code, del.Raw)
	require.Equal(t, "pending_deletion", del.Body["status"])
	res := e.do(req{method: "POST", path: "/tenants/" + id + "/restore", token: s.token})
	require.Equal(t, 200, res.Code)
	require.Equal(t, "suspended", res.Body["status"])
	require.Equal(t, 409, e.do(req{method: "POST", path: "/tenants/" + id + "/restore", token: s.token}).Code)

	// Step-up expires after 5 minutes.
	e.advance(6 * time.Minute)
	require.Equal(t, 403, e.do(req{method: "POST", path: "/tenants/" + id + "/reactivate", token: s.token}).Code)

	audit := e.do(req{method: "GET", path: "/audit?tenant=" + id, token: s.token})
	require.Equal(t, 200, audit.Code)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit.Raw), &rows))
	actions := map[string]bool{}
	for _, r := range rows {
		actions[r["action"].(string)] = true
	}
	for _, a := range []string{"tenant.create", "tenant.edit", "tenant.suspend", "tenant.reactivate", "tenant.slug_change", "tenant.delete", "tenant.restore"} {
		require.True(t, actions[a], a)
	}

	admin := tdb.Admin(t)
	_, err = admin.Exec(context.Background(), `SELECT 1`)
	require.NoError(t, err)
	// The audit log is append-only for the application role.
	var canUpdate, canDelete bool
	require.NoError(t, admin.QueryRow(context.Background(), `SELECT has_table_privilege('pos_platform', 'platform.platform_audit_log', 'UPDATE'),
		has_table_privilege('pos_platform', 'platform.platform_audit_log', 'DELETE')`).Scan(&canUpdate, &canDelete))
	require.False(t, canUpdate)
	require.False(t, canDelete)
}

// addUser inserts an enrolled platform user directly (test fixture).
func addUser(t *testing.T, e *env, email, role, pw string) []byte {
	t.Helper()
	ctx := context.Background()
	hash, err := security.NewHasher(fast, 1).Hash(ctx, pw)
	require.NoError(t, err)
	admin := tdb.Admin(t)
	var id int64
	require.NoError(t, admin.QueryRow(ctx, `INSERT INTO platform.platform_users (email, name, role, password_hash, must_change_password, mfa_enrolled, totp_ciphertext, totp_dek, totp_key_id)
		VALUES ($1, 'Fixture', $2, $3, false, false, NULL, NULL, NULL) RETURNING id`, email, role, hash).Scan(&id))
	secret := security.NewTOTPSecret()
	sealed, err := e.svcEnv.Seal(secret, fmt.Sprintf("platform_user:%d|totp", id))
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `UPDATE platform.platform_users SET totp_ciphertext = $2, totp_dek = $3, totp_key_id = $4, mfa_enrolled = true WHERE id = $1`,
		id, sealed.Ciphertext, sealed.WrappedDEK, sealed.KeyID)
	require.NoError(t, err)
	return secret
}

func TestRoleMatrixAndOwnAudit(t *testing.T) {
	e := newEnv(t)
	rootSession(t, e)
	secret := addUser(t, e, "support@platform.test", platform.RoleSupport, "support-person-pass-77")
	s := e.mfaLogin(t, "support@platform.test", "support-person-pass-77", secret)

	require.Equal(t, 200, e.do(req{method: "GET", path: "/tenants", token: s.token}).Code)
	require.Equal(t, 403, e.do(req{method: "POST", path: "/tenants", token: s.token, body: map[string]string{"name": "x", "slug": "nope-shop", "ownerEmail": "a@b.test"}}).Code)
	require.Equal(t, 403, e.do(req{method: "PATCH", path: "/tenants/abcdefghij", token: s.token, body: map[string]string{"name": "x"}}).Code)
	require.Equal(t, 403, e.do(req{method: "DELETE", path: "/tenants/abcdefghij", token: s.token}).Code)

	audit := e.do(req{method: "GET", path: "/audit", token: s.token})
	require.Equal(t, 200, audit.Code)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit.Raw), &rows))
	require.NotEmpty(t, rows)
	for _, r := range rows {
		require.Equal(t, "support@platform.test", r["actorEmail"], "support sees only own actions")
	}
}

func TestLockoutAndRateLimit(t *testing.T) {
	e := newEnv(t)
	rootSession(t, e)
	secret := addUser(t, e, "victim@platform.test", platform.RoleSupport, "victim-real-pass-1234")
	_ = secret
	for i := range platform.LockoutAfter {
		r := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": "victim@platform.test", "password": fmt.Sprintf("wrong-%d-guess-xx", i)}})
		require.Equal(t, 401, r.Code)
	}
	r := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": "victim@platform.test", "password": "victim-real-pass-1234"}})
	require.Equal(t, 401, r.Code, "locked after 10 failures; manual unlock only")
	unknown := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": "nobody@platform.test", "password": "whatever-pass-1234"}})
	require.Equal(t, r.Raw, unknown.Raw, "locked and unknown look identical")

	codes := []int{}
	for range 7 {
		codes = append(codes, e.do(req{method: "POST", path: "/auth/login", ip: "203.0.113.77", body: map[string]string{"email": "nobody@platform.test", "password": "x"}}).Code)
	}
	require.Contains(t, codes, 429, "5/min per IP on login")
}

func TestSignedInPasswordChangeRevokesOtherSessions(t *testing.T) {
	e := newEnv(t)
	rootSession(t, e)
	secret := addUser(t, e, "changer@platform.test", platform.RoleSupport, "changer-old-pass-5678")
	a := e.mfaLogin(t, "changer@platform.test", "changer-old-pass-5678", secret)
	b := e.mfaLogin(t, "changer@platform.test", "changer-old-pass-5678", secret)
	r := e.do(req{method: "POST", path: "/auth/password/change", token: a.token, body: map[string]string{
		"currentPassword": "changer-old-pass-5678", "newPassword": "changer-new-pass-9012"}})
	require.Equal(t, 204, r.Code, r.Raw)
	require.Equal(t, 200, e.do(req{method: "GET", path: "/me", token: a.token}).Code, "current session kept")
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: b.token}).Code, "other sessions revoked")
}

func TestBootstrapRefusesSecondRun(t *testing.T) {
	newEnv(t)
	_, err := platform.BootstrapSuperAdmin(context.Background(), tdb.MigratorDSN, "second@platform.test", "", "another-good-pass-123", security.NewHasher(fast, 1))
	require.ErrorIs(t, err, platform.ErrAlreadyBootstrapped)
}
