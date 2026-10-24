//go:build integration

// Package isolation is the step-5 gate suite (PHASE1_DESIGN §8 exit
// criterion 3): tenant auth, RBAC and the tenant-isolation checks, run
// against the real app wiring on PostgreSQL 18.
package isolation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/app"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer/smtptest"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

var (
	tdb     *testdb.DB
	theApp  *app.App
	baseURL string
	tenants = map[string]tenantInfo{} // by letter
	smtpSrv *smtptest.Server          // tenant SMTP target (127.0.0.2, STARTTLS)
	platSrv *smtptest.Server          // platform relay (plain, like Mailpit)
)

// SMTP test identity and the names the test resolver knows. Every other
// name fails to resolve; the hostile ones point at blocked addresses.
const (
	smtpHost = "smtp.example.test"
	smtpUser = "shop@iso-b.test"
	smtpPass = "smtp-app-password-123"
)

func testResolver() smtptest.StaticResolver {
	a := netip.MustParseAddr
	return smtptest.StaticResolver{
		smtpHost:             {a("127.0.0.2")},
		"metadata.evil.test": {a("169.254.169.254")},
		"private.evil.test":  {a("10.0.0.5")},
		"mapped.evil.test":   {a("::ffff:127.0.0.1")},
		"mixed.evil.test":    {a("127.0.0.2"), a("192.168.1.10")},
	}
}

type tenantInfo struct {
	ID, Slug, Host, OwnerEmail string
}

const ownerPW = "orchid-lantern-copper-77"

func rk() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

var storageDir string

func run(m *testing.M) (int, error) {
	ctx := context.Background()
	var err0 error
	if storageDir, err0 = os.MkdirTemp("", "posd-files-"); err0 != nil {
		return 0, err0
	}
	defer func() { _ = os.RemoveAll(storageDir) }()
	var err error
	if tdb, err = testdb.Start(ctx); err != nil {
		return 0, err
	}
	defer func() { _ = tdb.Stop(ctx) }()

	log := slog.New(slog.DiscardHandler)
	if os.Getenv("ISO_LOG") != "" { // debug aid: ISO_LOG=1 go test -tags integration ./test/isolation
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if smtpSrv, err = smtptest.New(smtptest.Options{Mode: smtptest.StartTLS, Host: "127.0.0.2", User: smtpUser, Pass: smtpPass}); err != nil {
		return 0, err
	}
	defer smtpSrv.Close()
	if platSrv, err = smtptest.New(smtptest.Options{Mode: smtptest.Plain, Host: "127.0.0.2"}); err != nil {
		return 0, err
	}
	defer platSrv.Close()
	cfg, err := config.LoadFrom(map[string]string{
		"BASE_DOMAIN": "pos.test", "PLATFORM_NAME": "Test", "APP_ENV": "test", "PUBLIC_SCHEME": "http",
		"DATABASE_URL": tdb.AppDSN.Reveal(), "DATABASE_JOBS_URL": tdb.JobsDSN.Reveal(),
		"DATABASE_MIGRATOR_URL": tdb.MigratorDSN.Reveal(),
		"JWT_KEYS":              "k1:" + rk(), "PLATFORM_JWT_KEYS": "p1:" + rk(), "MASTER_KEYS": "m1:" + rk(),
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32", "SHUTDOWN_DRAIN_DELAY": "0s",
		"STORAGE_FS_ROOT": storageDir, "RAZORPAY_WEBHOOK_SECRET": webhookSecret,
		"SMTP_HOST": "127.0.0.2", "SMTP_PORT": strconv.Itoa(platSrv.Port), "SMTP_TLS": "none",
	})
	if err != nil {
		return 0, err
	}

	guard := mailer.Guard{Resolver: testResolver(), Allow: []netip.Addr{netip.MustParseAddr("127.0.0.2")}, DialFunc: smtptest.PortDialer(smtpSrv.Port)}
	// The suite signs in far more often than a shop does; the per-tenant
	// login limit (100 a minute) would refuse later tests.
	theApp, err = app.New(ctx, cfg, log, app.WithMailTesting(guard, smtpSrv.RootCAs), app.WithSweepInterval(time.Second),
		app.WithTenantLoginLimit(100_000))
	if err != nil {
		return 0, err
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	iln, _ := net.Listen("tcp", "127.0.0.1:0")
	baseURL = "http://" + ln.Addr().String()
	sctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- theApp.Serve(sctx, ln, iln) }()
	defer func() { stop(); <-done }()

	deadline := time.Now().Add(10 * time.Second)
	for !theApp.ListenerHealthy() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	// The suite creates many users per tenant; the trial plan's 10-user cap
	// (tested by the plan-limit code) would get in the way. Stores keep 3.
	adm, err := pgx.Connect(ctx, tdb.AdminDSN.Reveal())
	if err != nil {
		return 0, err
	}
	if _, err := adm.Exec(ctx, `UPDATE platform.plans SET limits = jsonb_set(limits, '{max_users}', '1000') WHERE code = 'trial'`); err != nil {
		return 0, err
	}
	_ = adm.Close(ctx)

	o := provisioning.New(tdb.MigratorDSN, "http", "pos.test", slog.New(slog.DiscardHandler))
	for _, l := range []string{"a", "b", "c"} {
		slug := "iso-" + l
		res, err := o.Provision(ctx, provisioning.Request{Slug: slug, Name: "Shop " + strings.ToUpper(l), OwnerEmail: "owner@" + slug + ".test", OwnerName: "Owner " + l})
		if err != nil {
			return 0, err
		}
		tenants[l] = tenantInfo{ID: res.TenantID, Slug: slug, Host: slug + ".pos.test", OwnerEmail: "owner@" + slug + ".test"}
	}
	// The welcome emails issue owner invite tokens (which replace earlier
	// ones); let them go out before tests create their own tokens.
	if err := theApp.Worker().EnqueueMail(ctx, nil, true); err != nil {
		return 0, err
	}
	deadline = time.Now().Add(45 * time.Second)
	for len(platSrv.Messages()) < 3 {
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("welcome emails not delivered: %d of 3; %s", len(platSrv.Messages()), jobDiagnostics(ctx))
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m.Run(), nil
}

// jobDiagnostics describes the outbox and River state when setup stalls.
func jobDiagnostics(ctx context.Context) string {
	adm, err := pgx.Connect(ctx, tdb.AdminDSN.Reveal())
	if err != nil {
		return err.Error()
	}
	defer func() { _ = adm.Close(ctx) }()
	var outbox, jobs, leader string
	_ = adm.QueryRow(ctx, `SELECT coalesce(string_agg(status || '/' || attempts || '/' || coalesce(last_error, ''), '; '), 'none') FROM platform.email_outbox`).Scan(&outbox)
	_ = adm.QueryRow(ctx, `SELECT coalesce(string_agg(kind || ':' || state || ':' || coalesce(errors::text, ''), '; '), 'none') FROM river.river_job`).Scan(&jobs)
	_ = adm.QueryRow(ctx, `SELECT coalesce(string_agg(leader_id, ','), 'none') FROM river.river_leader`).Scan(&leader)
	return fmt.Sprintf("outbox=[%s] jobs=[%s] leader=%s started=%v", outbox, jobs, leader, theApp.Worker().Started())
}

// ------------------------------------------------------------- HTTP helper

var ipSeq atomic.Int64

type resp struct {
	Code   int
	Body   map[string]any
	Raw    string
	Header http.Header
}

type rq struct {
	method, host, path string
	body               any
	token, cookie      string
	ip                 string
	csrf               bool
	header             map[string]string
}

func do(t *testing.T, r rq) resp {
	t.Helper()
	var b io.Reader
	if r.body != nil {
		j, _ := json.Marshal(r.body)
		b = bytes.NewReader(j)
	}
	req, err := http.NewRequest(r.method, baseURL+r.path, b)
	require.NoError(t, err)
	req.Host = r.host
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.ip == "" {
		r.ip = fmt.Sprintf("10.%d.%d.%d", ipSeq.Add(1)%200, ipSeq.Load()%250, 1+ipSeq.Load()%250)
	}
	req.Header.Set("X-Forwarded-For", r.ip) // trusted proxy = 127.0.0.1
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.cookie != "" {
		req.Header.Set("Cookie", "__Host-rt="+r.cookie)
	}
	if r.csrf {
		req.Header.Set("Origin", "http://"+r.host)
		req.Header.Set("X-Requested-With", "fetch")
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Code: res.StatusCode, Raw: string(raw), Header: res.Header}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func refreshCookie(t *testing.T, r resp) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == "__Host-rt" {
			return c
		}
	}
	t.Fatalf("no refresh cookie: %v", r.Header)
	return nil
}

// ----------------------------------------------------------- DB helpers

func admin(t *testing.T) *pgx.Conn { return tdb.Admin(t) }

func schema(l string) string { return "t_" + tenants[l].ID }

// setPasswordViaInvite inserts an invite token for a user and accepts it
// through the API (exercises POST /api/auth/invite/accept).
func setPasswordViaInvite(t *testing.T, l string, email, pw string) {
	t.Helper()
	ctx := context.Background()
	raw := security.NewToken(32)
	_, err := admin(t).Exec(ctx, `INSERT INTO `+schema(l)+`.auth_tokens (user_id, purpose, token_hash, expires_at)
		SELECT id, 'invite', $2, now() + interval '1 hour' FROM `+schema(l)+`.users WHERE email = $1`, email, security.HashToken(raw))
	require.NoError(t, err)
	r := do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/auth/invite/accept", body: map[string]string{"token": raw, "password": pw}})
	require.Equal(t, 204, r.Code, r.Raw)
}

var ownerReady = map[string]bool{}

func ownerLogin(t *testing.T, l string) (token, cookie string) {
	t.Helper()
	if !ownerReady[l] {
		setPasswordViaInvite(t, l, tenants[l].OwnerEmail, ownerPW)
		ownerReady[l] = true
	}
	return login(t, l, tenants[l].OwnerEmail, ownerPW)
}

func login(t *testing.T, l, email, pw string) (string, string) {
	t.Helper()
	r := do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/auth/login", body: map[string]any{"email": email, "password": pw, "remember": true}})
	require.Equal(t, 200, r.Code, r.Raw)
	return r.Body["token"].(string), refreshCookie(t, r).Value
}

func roleID(t *testing.T, l, key string) int64 {
	var id int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id FROM `+schema(l)+`.roles WHERE system_key = $1`, key).Scan(&id))
	return id
}

func createUser(t *testing.T, l, token, email, roleKey, pw string) int64 {
	t.Helper()
	r := do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/users", token: token, body: map[string]any{
		"name": "User " + email, "email": email, "password": pw, "role_id": roleID(t, l, roleKey), "status": "Active"}})
	require.Equal(t, 201, r.Code, r.Raw)
	return int64(r.Body["id"].(float64))
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("timed out: " + msg)
}

// ----------------------------------------------------------------- tests

func TestLoginShapeRefreshAndLogout(t *testing.T) {
	a := tenants["a"]
	tok, cookie := ownerLogin(t, "a")

	r := do(t, rq{method: "POST", host: a.Host, path: "/api/auth/login", body: map[string]any{"email": a.OwnerEmail, "password": ownerPW}})
	require.Equal(t, 200, r.Code)
	user := r.Body["user"].(map[string]any)
	for _, k := range []string{"id", "name", "email", "role", "status", "role_id", "createdAt", "updatedAt"} {
		require.Contains(t, user, k)
	}
	require.Equal(t, "admin", user["role"], "legacy role string for owner")
	require.NotContains(t, r.Raw, "password")
	require.Equal(t, true, r.Body["remember"])
	require.Contains(t, r.Body["permissions"], "billing:manage")
	c := refreshCookie(t, r)
	require.True(t, c.HttpOnly && c.Secure)
	require.Equal(t, http.SameSiteStrictMode, c.SameSite)
	require.Empty(t, c.Domain)
	require.Equal(t, int((720 * time.Hour).Seconds()), c.MaxAge, "remember-me: 30 days")

	bad := do(t, rq{method: "POST", host: a.Host, path: "/api/auth/login", body: map[string]any{"email": a.OwnerEmail, "password": "wrong-password-xx"}})
	unknown := do(t, rq{method: "POST", host: a.Host, path: "/api/auth/login", body: map[string]any{"email": "ghost@iso-a.test", "password": "wrong-password-xx"}})
	require.Equal(t, 401, bad.Code)
	require.Equal(t, bad.Raw, unknown.Raw, "C-01: no user enumeration")

	me := do(t, rq{method: "GET", host: a.Host, path: "/api/auth/me", token: tok})
	require.Equal(t, 200, me.Code, me.Raw)
	require.Equal(t, "INR", me.Body["tenant"].(map[string]any)["currency"])

	require.Equal(t, 403, do(t, rq{method: "POST", host: a.Host, path: "/api/auth/refresh", cookie: cookie}).Code, "refresh needs the CSRF header")
	rf := do(t, rq{method: "POST", host: a.Host, path: "/api/auth/refresh", cookie: cookie, csrf: true})
	require.Equal(t, 200, rf.Code, rf.Raw)
	next := refreshCookie(t, rf).Value
	reuse := do(t, rq{method: "POST", host: a.Host, path: "/api/auth/refresh", cookie: cookie, csrf: true})
	require.Equal(t, 401, reuse.Code, "rotated token reused")
	require.Equal(t, 401, do(t, rq{method: "POST", host: a.Host, path: "/api/auth/refresh", cookie: next, csrf: true}).Code, "whole family revoked")

	_, c2 := login(t, "a", a.OwnerEmail, ownerPW)
	require.Equal(t, 204, do(t, rq{method: "POST", host: a.Host, path: "/api/auth/logout", cookie: c2, csrf: true}).Code)
	require.Equal(t, 401, do(t, rq{method: "POST", host: a.Host, path: "/api/auth/refresh", cookie: c2, csrf: true}).Code)

	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.audit_log WHERE action IN ('auth.login','auth.login_failed','auth.refresh_reuse','auth.logout','auth.invite_completed')`).Scan(&n))
	require.GreaterOrEqual(t, n, 5)
}

func TestSignupRemoved(t *testing.T) {
	r := do(t, rq{method: "POST", host: tenants["a"].Host, path: "/api/auth/signup", body: map[string]string{"email": "x@y.test", "password": "whatever-pw-1234", "role": "admin"}})
	require.Equal(t, 404, r.Code, "C-05")
}

func TestLockout(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	createUser(t, "a", tok, "lock@iso-a.test", "biller", "biller-pass-word-99")
	for i := range 5 {
		r := do(t, rq{method: "POST", host: tenants["a"].Host, path: "/api/auth/login", body: map[string]any{"email": "lock@iso-a.test", "password": fmt.Sprintf("bad-guess-%d-xx", i)}})
		require.Equal(t, 401, r.Code)
	}
	r := do(t, rq{method: "POST", host: tenants["a"].Host, path: "/api/auth/login", body: map[string]any{"email": "lock@iso-a.test", "password": "biller-pass-word-99"}})
	require.Equal(t, 429, r.Code)
	require.Equal(t, "ACCOUNT_LOCKED", r.Body["code"])
	require.NotEmpty(t, r.Header.Get("Retry-After"))
}

func TestDeactivationAndRoleChangeTakeEffectImmediately(t *testing.T) {
	host := tenants["a"].Host
	owner, _ := ownerLogin(t, "a")
	uid := createUser(t, "a", owner, "cashier@iso-a.test", "biller", "cashier-pass-word-1")
	btok, _ := login(t, "a", "cashier@iso-a.test", "cashier-pass-word-1")

	require.Equal(t, 403, do(t, rq{method: "GET", host: host, path: "/api/users", token: btok}).Code, "biller lacks users:view")

	// Grant users:view to the biller role; effective within seconds via NOTIFY.
	roles := do(t, rq{method: "GET", host: host, path: "/api/roles", token: owner})
	require.Equal(t, 200, roles.Code)
	var billerIDs []int64
	var billerID int64
	var raw []map[string]any
	require.NoError(t, json.Unmarshal([]byte(roles.Raw), &raw))
	for _, r := range raw {
		if r["name"] == "Biller" {
			billerID = int64(r["id"].(float64))
			for _, p := range r["permissionIds"].([]any) {
				billerIDs = append(billerIDs, int64(p.(float64)))
			}
		}
	}
	var usersView int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id FROM `+schema("a")+`.permissions WHERE module = 'users' AND action = 'view'`).Scan(&usersView))
	up := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/roles/%d", billerID), token: owner, body: map[string]any{"permissionIds": append(billerIDs, usersView)}})
	require.Equal(t, 200, up.Code, up.Raw)
	eventually(t, func() bool { return do(t, rq{method: "GET", host: host, path: "/api/users", token: btok}).Code == 200 }, "role_changed applied")

	// Deactivate: the existing access token stops working at once.
	r := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/users/%d", uid), token: owner, body: map[string]any{"name": "Cashier", "status": "Inactive"}})
	require.Equal(t, 200, r.Code, r.Raw)
	eventually(t, func() bool {
		return do(t, rq{method: "GET", host: host, path: "/api/auth/me", token: btok}).Code == 401
	}, "auth_changed applied")
	inactive := do(t, rq{method: "POST", host: host, path: "/api/auth/login", body: map[string]any{"email": "cashier@iso-a.test", "password": "cashier-pass-word-1"}})
	require.Equal(t, 401, inactive.Code, "inactive users cannot log in (C-01)")
}

func TestAntiEscalation(t *testing.T) {
	host := tenants["b"].Host
	owner, _ := ownerLogin(t, "b")
	adminID := createUser(t, "b", owner, "manager@iso-b.test", "admin", "manager-pass-word-1")
	atok, _ := login(t, "b", "manager@iso-b.test", "manager-pass-word-1")
	var ownerID int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id FROM `+schema("b")+`.users WHERE email = $1`, tenants["b"].OwnerEmail).Scan(&ownerID))

	check := func(name string, r resp, code int) {
		t.Helper()
		require.Equal(t, code, r.Code, "%s: %s", name, r.Raw)
	}
	// Admin cannot hand out owner or anything with billing.
	check("assign owner", do(t, rq{method: "POST", host: host, path: "/api/users", token: atok, body: map[string]any{
		"name": "X", "email": "x1@iso-b.test", "password": "some-good-pass-11", "role_id": roleID(t, "b", "owner")}}), 403)
	var billingManage int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id FROM `+schema("b")+`.permissions WHERE module = 'billing' AND action = 'manage'`).Scan(&billingManage))
	check("role with billing", do(t, rq{method: "POST", host: host, path: "/api/roles", token: atok, body: map[string]any{"name": "Sneaky", "permissionIds": []int64{billingManage}}}), 403)
	// Nobody changes their own role or status, or deletes themselves.
	check("own status", do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/users/%d", adminID), token: atok, body: map[string]any{"name": "M", "status": "Inactive"}}), 403)
	check("own role", do(t, rq{method: "POST", host: host, path: "/api/roles/assign-user", token: atok, body: map[string]any{"userId": adminID, "roleId": roleID(t, "b", "biller")}}), 403)
	check("delete self", do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/users/%d", adminID), token: atok}), 403)
	// Admin cannot touch the more powerful owner.
	check("edit owner", do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/users/%d", ownerID), token: atok, body: map[string]any{"name": "Pwned"}}), 403)
	check("delete owner", do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/users/%d", ownerID), token: atok}), 403)
	// System role definitions are immutable.
	check("edit admin role", do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/roles/%d", roleID(t, "b", "admin")), token: owner, body: map[string]any{"permissionIds": []int64{}}}), 403)
	check("edit owner role", do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/roles/%d", roleID(t, "b", "owner")), token: owner, body: map[string]any{"name": "Boss"}}), 403)
	check("delete system role", do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/roles/%d", roleID(t, "b", "biller")), token: owner}), 400)
	// The last owner stays (owner cannot even change own status).
	check("last owner", do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/users/%d", ownerID), token: owner, body: map[string]any{"name": "Owner", "status": "Inactive"}}), 403)

	// Custom roles within the actor's permissions work, and can be removed.
	var dashView int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id FROM `+schema("b")+`.permissions WHERE module = 'dashboard' AND action = 'view'`).Scan(&dashView))
	cr := do(t, rq{method: "POST", host: host, path: "/api/roles", token: atok, body: map[string]any{"name": "Viewer", "description": "read only", "permissionIds": []int64{dashView}}})
	require.Equal(t, 201, cr.Code, cr.Raw)
	require.Equal(t, []any{"dashboard:view"}, cr.Body["permissions"])
	vid := int64(cr.Body["id"].(float64))
	check("duplicate role", do(t, rq{method: "POST", host: host, path: "/api/roles", token: atok, body: map[string]any{"name": "viewer"}}), 409)
	uid := createUser(t, "b", owner, "viewer@iso-b.test", "biller", "viewer-pass-word-12")
	check("assign custom", do(t, rq{method: "POST", host: host, path: "/api/roles/assign-user", token: atok, body: map[string]any{"userId": uid, "roleId": vid}}), 200)
	check("delete role in use", do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/roles/%d", vid), token: atok}), 400)
	check("delete user", do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/users/%d", uid), token: atok}), 204)
	check("delete role", do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/roles/%d", vid), token: atok}), 204)

	// Missing email on PUT keeps the email (C-07).
	r := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/billers/%d", adminID), token: owner, body: map[string]any{"name": "Manager Two"}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "manager@iso-b.test", r.Body["email"])

	list := do(t, rq{method: "GET", host: host, path: "/api/users?q=manager", token: owner})
	require.Equal(t, 200, list.Code)
	var users []map[string]any
	require.NoError(t, json.Unmarshal([]byte(list.Raw), &users))
	require.Len(t, users, 1)
	require.Equal(t, "Admin", users[0]["Role"].(map[string]any)["name"])
	require.Equal(t, "1", list.Header.Get("X-Total-Count"))
}

func setStatus(t *testing.T, l, status string) {
	t.Helper()
	_, err := admin(t).Exec(context.Background(), `UPDATE platform.tenants SET status = $2 WHERE id = $1`, tenants[l].ID, status)
	require.NoError(t, err)
}

func TestTenantGate(t *testing.T) {
	c := tenants["c"]
	owner, cookie := ownerLogin(t, "c")
	defer setStatus(t, "c", "trialing")

	setStatus(t, "c", "read_only")
	eventually(t, func() bool {
		return do(t, rq{method: "POST", host: c.Host, path: "/api/roles", token: owner, body: map[string]any{"name": "Nope"}}).Code == 403
	}, "read_only applied")
	ro := do(t, rq{method: "POST", host: c.Host, path: "/api/roles", token: owner, body: map[string]any{"name": "Nope"}})
	require.Equal(t, "TENANT_READ_ONLY", ro.Body["code"])
	require.Equal(t, 200, do(t, rq{method: "GET", host: c.Host, path: "/api/users", token: owner}).Code, "reads still work")
	require.Equal(t, 200, do(t, rq{method: "GET", host: c.Host, path: "/api/auth/me", token: owner}).Code)
	rf := do(t, rq{method: "POST", host: c.Host, path: "/api/auth/refresh", cookie: cookie, csrf: true})
	require.Equal(t, 200, rf.Code, "refresh allowed on read_only")
	cookie = refreshCookie(t, rf).Value
	_, _ = login(t, "c", c.OwnerEmail, ownerPW) // login allowed too
	// Credential flows stay open on read_only (step 16): the owner must be
	// able to sign in to pay.
	require.Equal(t, 202, do(t, rq{method: "POST", host: c.Host, path: "/api/auth/password/forgot", body: map[string]string{"email": c.OwnerEmail}}).Code)

	setStatus(t, "c", "suspended")
	eventually(t, func() bool {
		return do(t, rq{method: "GET", host: c.Host, path: "/api/auth/me", token: owner}).Code == 403
	}, "suspension applied")
	sus := do(t, rq{method: "POST", host: c.Host, path: "/api/auth/login", body: map[string]any{"email": c.OwnerEmail, "password": ownerPW}})
	require.Equal(t, 403, sus.Code)
	require.Equal(t, "TENANT_SUSPENDED", sus.Body["code"])
	require.Equal(t, 403, do(t, rq{method: "POST", host: c.Host, path: "/api/auth/refresh", cookie: cookie, csrf: true}).Code, "refresh rejected too")
	info := do(t, rq{method: "GET", host: c.Host, path: "/api/public/tenant-info"})
	require.Equal(t, 200, info.Code, "the sign-in page can still show the name")
}

func TestTenantInfo(t *testing.T) {
	r := do(t, rq{method: "GET", host: tenants["a"].Host, path: "/api/public/tenant-info"})
	require.Equal(t, 200, r.Code)
	require.JSONEq(t, `{"name":"Shop A","logoUrl":null}`, r.Raw)
	require.Equal(t, "public, max-age=300", r.Header.Get("Cache-Control"))
	etag := r.Header.Get("ETag")
	require.NotEmpty(t, etag)
	r2 := do(t, rq{method: "GET", host: tenants["a"].Host, path: "/api/public/tenant-info", header: map[string]string{"If-None-Match": etag}})
	require.Equal(t, 304, r2.Code)
}

func TestForgotAndReset(t *testing.T) {
	host := tenants["a"].Host
	owner, _ := ownerLogin(t, "a")
	createUser(t, "a", owner, "forgetful@iso-a.test", "biller", "forgetful-pass-11")
	_, oldCookie := login(t, "a", "forgetful@iso-a.test", "forgetful-pass-11")

	known := do(t, rq{method: "POST", host: host, path: "/api/auth/password/forgot", body: map[string]string{"email": "forgetful@iso-a.test"}})
	unknown := do(t, rq{method: "POST", host: host, path: "/api/auth/password/forgot", body: map[string]string{"email": "nobody@iso-a.test"}})
	require.Equal(t, 202, known.Code)
	require.Equal(t, known.Raw, unknown.Raw, "same answer either way")

	var data string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT template_data::text FROM `+schema("a")+`.email_outbox
		WHERE to_addr = 'forgetful@iso-a.test' AND template = 'password_reset'`).Scan(&data))
	require.Contains(t, data, "token_sealed")

	// Reset with a token we know (the real one is only in the sealed outbox data).
	raw := security.NewToken(32)
	_, err := admin(t).Exec(context.Background(), `INSERT INTO `+schema("a")+`.auth_tokens (user_id, purpose, token_hash, expires_at)
		SELECT id, 'reset', $1, now() + interval '1 hour' FROM `+schema("a")+`.users WHERE email = 'forgetful@iso-a.test'`, security.HashToken(raw))
	require.NoError(t, err)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/auth/password/reset", body: map[string]string{"token": raw, "password": "short"}}).Code)
	require.Equal(t, 204, do(t, rq{method: "POST", host: host, path: "/api/auth/password/reset", body: map[string]string{"token": raw, "password": "brand-new-pass-word-5"}}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/auth/password/reset", body: map[string]string{"token": raw, "password": "another-new-pass-66"}}).Code, "single use")
	require.Equal(t, 401, do(t, rq{method: "POST", host: host, path: "/api/auth/refresh", cookie: oldCookie, csrf: true}).Code, "reset revokes sessions")
	login(t, "a", "forgetful@iso-a.test", "brand-new-pass-word-5")
}

// ---------------------------------------------------- isolation (criterion 3)

func fill(path string) string {
	path = strings.TrimPrefix(path, "/api")
	return "/api" + strings.ReplaceAll(path, "{id}", "1")
}

func TestEveryRouteRejectsAnotherTenantsToken(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	routes := theApp.TenantRoutes()
	require.NotEmpty(t, routes)
	checked := 0
	for key, req := range routes {
		if req.Kind == rbac.KindPublic {
			continue
		}
		method, path, _ := strings.Cut(key, " ")
		for _, l := range []string{"b", "c"} {
			r := do(t, rq{method: method, host: tenants[l].Host, path: fill(path), token: aTok, body: map[string]any{}})
			require.Equal(t, 401, r.Code, "%s on tenant %s with tenant A's token: %s", key, l, r.Raw)
		}
		checked++
	}
	require.Greater(t, checked, 10)
	t.Logf("checked %d authenticated routes x 2 foreign tenants", checked)
}

func TestTokenIssuedForOneHostRejectedOnAnother(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	// Same tenant, different host spelling (e.g. an old slug) is not this issuer.
	_, err := admin(t).Exec(context.Background(), `SELECT 1`)
	require.NoError(t, err)
	r := do(t, rq{method: "GET", host: "admin.pos.test", path: "/api/auth/me", token: aTok})
	require.Equal(t, 404, r.Code, "tenant routes do not exist on the admin host")
}

func TestForgedForwardedHostFromUntrustedPeer(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	h := theApp.Handler()
	req := httptest.NewRequest("GET", "http://"+tenants["a"].Host+"/api/auth/me", nil)
	req.RemoteAddr = "203.0.113.50:1234" // not a trusted proxy
	req.Header.Set("X-Forwarded-Host", tenants["b"].Host)
	req.Header.Set("Authorization", "Bearer "+aTok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code, "forged header ignored: still tenant A")

	req = httptest.NewRequest("GET", "http://"+tenants["b"].Host+"/api/auth/me", nil)
	req.RemoteAddr = "203.0.113.50:1234"
	req.Header.Set("X-Forwarded-Host", tenants["a"].Host)
	req.Header.Set("Authorization", "Bearer "+aTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, 401, rec.Code, "cannot borrow tenant A by forging the header on B's host")
}

func TestGenericNotFoundForUnknownDeletedProvisioningHosts(t *testing.T) {
	ctx := context.Background()
	deleted, err := tdb.CreateTenant(ctx, testdb.TenantSpec{Slug: "iso-deleted", Status: "deleted"})
	require.NoError(t, err)
	_ = deleted
	_, err = tdb.CreateTenant(ctx, testdb.TenantSpec{Slug: "iso-prov", Status: "provisioning"})
	require.NoError(t, err)

	for _, path := range []string{"/api/public/tenant-info", "/api/auth/login", "/api/users"} {
		method := "GET"
		var body any
		if path == "/api/auth/login" {
			method, body = "POST", map[string]string{"email": "a@b.test", "password": "x"}
		}
		ref := do(t, rq{method: method, host: "nosuchtenant.pos.test", path: path, body: body})
		require.Equal(t, 404, ref.Code)
		for _, host := range []string{"iso-deleted.pos.test", "iso-prov.pos.test", "admin2x.pos.test"} {
			r := do(t, rq{method: method, host: host, path: path, body: body})
			require.Equal(t, 404, r.Code, host+path)
			require.Equal(t, ref.Raw, r.Raw, "identical body for %s%s", host, path)
		}
	}
	require.Equal(t, 421, do(t, rq{method: "GET", host: "evil.example", path: "/api/public/tenant-info"}).Code)
}

func TestPrivilegeMatrixAcrossTenants(t *testing.T) {
	ctx := context.Background()
	c := admin(t)
	for _, owner := range []string{"a", "b", "c"} {
		role := schema(owner) + "_rw"
		for _, other := range []string{"a", "b", "c"} {
			rows, err := c.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = $1 AND tablename <> 'goose_db_version'`, schema(other))
			require.NoError(t, err)
			tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
			require.NoError(t, err)
			for _, tbl := range tables {
				var sel bool
				require.NoError(t, c.QueryRow(ctx, `SELECT has_table_privilege($1, $2, 'SELECT')`, role, schema(other)+"."+tbl).Scan(&sel))
				require.Equal(t, owner == other, sel, "%s SELECT on %s.%s", role, schema(other), tbl)
			}
			var usage bool
			require.NoError(t, c.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'USAGE')`, role, schema(other)).Scan(&usage))
			require.Equal(t, owner == other, usage)
		}
		var platformUsage bool
		require.NoError(t, c.QueryRow(ctx, `SELECT has_schema_privilege($1, 'platform', 'USAGE')`, role).Scan(&platformUsage))
		require.False(t, platformUsage, "tenant role cannot see platform")
	}
	var appUsage bool
	require.NoError(t, c.QueryRow(ctx, `SELECT has_schema_privilege('pos_app', $1, 'USAGE')`, schema("a")).Scan(&appUsage))
	require.False(t, appUsage, "pos_app has no privileges of its own")
}
