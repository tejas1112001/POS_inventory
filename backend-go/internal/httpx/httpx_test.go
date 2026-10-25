package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var testNet = Network{
	BaseDomain:     "pos.test",
	TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
}

type harness struct {
	h   http.Handler
	log *bytes.Buffer
	hc  *Health
}

func newHarness(t *testing.T, mutate func(*RouterDeps)) harness {
	t.Helper()
	var buf bytes.Buffer
	hc := NewHealth(time.Second)
	d := RouterDeps{
		Logger:         obs.NewLogger(&buf, "debug", "json"),
		Metrics:        obs.NewMetrics(),
		Network:        testNet,
		Health:         hc,
		RequestTimeout: 2 * time.Second,
		Tenant: func(r chi.Router) {
			r.Post("/echo", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Name string `json:"name"`
				}
				if err := DecodeJSON(r, &in); err != nil {
					WriteError(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]string{"name": in.Name, "slug": HostFrom(r.Context()).Slug})
			})
			r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom secret-detail") })
			r.Get("/fail", func(w http.ResponseWriter, r *http.Request) {
				WriteError(w, r, errors.New("pq: relation \"t_x.users\" does not exist"))
			})
			r.Get("/limited", func(w http.ResponseWriter, r *http.Request) {
				WriteError(w, r, apperr.RateLimited(apperr.CodeRateLimited, 1500*time.Millisecond))
			})
			r.Get("/deadline", func(w http.ResponseWriter, r *http.Request) {
				_, ok := r.Context().Deadline()
				WriteJSON(w, http.StatusOK, map[string]bool{"deadline": ok})
			})
		},
		Platform: func(r chi.Router) {
			r.Get("/platform/ping", func(w http.ResponseWriter, _ *http.Request) {
				WriteJSON(w, http.StatusOK, map[string]string{"ok": "platform"})
			})
		},
	}
	if mutate != nil {
		mutate(&d)
	}
	return harness{h: NewRouter(d), log: &buf, hc: hc}
}

func (h harness) do(t *testing.T, method, host, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, body)
	req.RemoteAddr = "203.0.113.9:4444"
	for k, v := range hdr {
		if k == "RemoteAddr" {
			req.RemoteAddr = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var b errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b), rec.Body.String())
	return b
}

func TestClassify(t *testing.T) {
	cases := map[string]struct {
		host string
		kind HostKind
		slug string
	}{
		"apex":            {"pos.test", HostApex, ""},
		"apex with port":  {"pos.test:8080", HostApex, ""},
		"apex upper":      {"POS.TEST", HostApex, ""},
		"admin":           {"admin.pos.test", HostPlatform, ""},
		"tenant":          {"acme.pos.test", HostTenant, "acme"},
		"tenant hyphen":   {"acme-shop.pos.test", HostTenant, "acme-shop"},
		"trailing dot":    {"acme.pos.test.", HostTenant, "acme"},
		"too short":       {"ab.pos.test", HostUnknown, ""},
		"double hyphen":   {"ac--me.pos.test", HostUnknown, ""},
		"leading hyphen":  {"-acme.pos.test", HostUnknown, ""},
		"two labels":      {"a.acme.pos.test", HostUnknown, ""},
		"other domain":    {"evil.com", HostUnknown, ""},
		"suffix trick":    {"acme.pos.test.evil.com", HostUnknown, ""},
		"lookalike":       {"evilpos.test", HostUnknown, ""},
		"ip":              {"127.0.0.1", HostUnknown, ""},
		"unicode":         {"аcme.pos.test", HostUnknown, ""},
		"underscore":      {"ac_me.pos.test", HostUnknown, ""},
		"injection quote": {`t_abc";drop.pos.test`, HostUnknown, ""},
		"empty":           {"", HostUnknown, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = c.host
			r.RemoteAddr = "203.0.113.9:1"
			got := testNet.Classify(r)
			require.Equal(t, c.kind, got.Kind)
			require.Equal(t, c.slug, got.Slug)
		})
	}
}

func TestForgedForwardedHeadersIgnoredFromUntrustedPeer(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "acme.pos.test"
	r.RemoteAddr = "203.0.113.9:1"
	r.Header.Set("X-Forwarded-Host", "other.pos.test")
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Forwarded-Proto", "https")
	require.Equal(t, "acme", testNet.Classify(r).Slug)
	require.Equal(t, "203.0.113.9", testNet.ClientIP(r).String())
	require.Equal(t, "http", testNet.Scheme(r))

	r.RemoteAddr = "10.1.2.3:1" // trusted proxy
	require.Equal(t, "other", testNet.Classify(r).Slug)
	require.Equal(t, "1.2.3.4", testNet.ClientIP(r).String())
	require.Equal(t, "https", testNet.Scheme(r))
}

func TestSameOrigin(t *testing.T) {
	mk := func(host, fwdHost, proto, origin string, trusted bool) *http.Request {
		r := httptest.NewRequest("POST", "/api/auth/refresh", nil)
		r.Host = host
		r.RemoteAddr = "203.0.113.9:1"
		if trusted {
			r.RemoteAddr = "10.1.2.3:1"
		}
		if fwdHost != "" {
			r.Header.Set("X-Forwarded-Host", fwdHost)
		}
		if proto != "" {
			r.Header.Set("X-Forwarded-Proto", proto)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"production 443", mk("app:8080", "acme.pos.test", "https", "https://acme.pos.test", true), true},
		{"explicit default port", mk("app:8080", "acme.pos.test:443", "https", "https://acme.pos.test", true), true},
		{"origin with default port", mk("app:8080", "acme.pos.test", "https", "https://acme.pos.test:443", true), true},
		{"dev proxy port", mk("app:8080", "acme.pos.test:8443", "https", "https://acme.pos.test:8443", true), true},
		{"case and trailing dot", mk("app:8080", "Acme.Pos.Test:8443", "https", "https://ACME.pos.test.:8443", true), true},
		{"other port", mk("app:8080", "acme.pos.test:8443", "https", "https://acme.pos.test:9999", true), false},
		{"port missing in origin", mk("app:8080", "acme.pos.test:8443", "https", "https://acme.pos.test", true), false},
		{"other tenant", mk("app:8080", "acme.pos.test", "https", "https://other.pos.test", true), false},
		{"http origin on https", mk("app:8080", "acme.pos.test", "https", "http://acme.pos.test", true), false},
		{"suffix trick", mk("app:8080", "acme.pos.test", "https", "https://acme.pos.test.evil.com", true), false},
		{"with path", mk("app:8080", "acme.pos.test", "https", "https://acme.pos.test/x", true), false},
		{"null origin", mk("app:8080", "acme.pos.test", "https", "null", true), false},
		{"no origin", mk("app:8080", "acme.pos.test", "https", "", true), false},
		{"forged forwarded host from a client", mk("acme.pos.test", "other.pos.test", "", "http://other.pos.test", false), false},
		{"direct, no proxy", mk("acme.pos.test:8080", "", "", "http://acme.pos.test:8080", false), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, testNet.SameOrigin(c.r))
		})
	}
}

func TestTenantURL(t *testing.T) {
	require.Equal(t, "https://acme.pos.test", TenantURL("https", "acme", "pos.test", ""))
	require.Equal(t, "https://acme.pos.test", TenantURL("https", "acme", "pos.test", "443"))
	require.Equal(t, "https://acme.localhost:8443", TenantURL("https", "acme", "localhost", "8443"))
	require.Equal(t, "http://acme.localhost:8080", TenantURL("http", "acme", "localhost", "8080"))
}

func TestClientIPRightmostUntrusted(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7, 10.0.0.5")
	require.Equal(t, "198.51.100.7", testNet.ClientIP(r).String(), "client-supplied left entries must not win")
	r.Header.Set("X-Forwarded-For", "10.0.0.7, 10.0.0.5")
	require.Equal(t, "10.0.0.7", testNet.ClientIP(r).String(), "all hops trusted: left-most wins")
	r.Header.Set("X-Forwarded-For", "garbage")
	require.Equal(t, "10.0.0.1", testNet.ClientIP(r).String())
}

func TestUnknownHost421ButHealthAnswers(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(t, "GET", "evil.com", "/api/echo", nil, nil)
	require.Equal(t, http.StatusMisdirectedRequest, rec.Code)
	require.Equal(t, apperr.CodeMisdirected, decodeErr(t, rec).Code)
	require.NotEmpty(t, rec.Header().Get("X-Content-Type-Options"), "security headers on 421 too")

	for _, p := range []string{"/healthz", "/health", "/readyz"} {
		rec = h.do(t, "GET", "evil.com", p, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code, p)
	}
}

func TestTenantJSONRoundTrip(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(t, "POST", "acme.pos.test", "/api/echo", strings.NewReader(`{"name":"x"}`),
		map[string]string{"Content-Type": "application/json; charset=utf-8"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"name":"x","slug":"acme"}`, rec.Body.String())
	require.Len(t, rec.Header().Get("X-Request-Id"), 32)
}

func TestStrictJSON(t *testing.T) {
	h := newHarness(t, nil)
	jsonHdr := map[string]string{"Content-Type": "application/json"}
	cases := map[string]struct {
		body, ct string
		status   int
		code     string
	}{
		"unknown field":   {`{"name":"x","role":"admin"}`, "", 400, apperr.CodeValidationFailed},
		"trailing data":   {`{"name":"x"}{"name":"y"}`, "", 400, apperr.CodeInvalidJSON},
		"trailing junk":   {`{"name":"x"} junk`, "", 400, apperr.CodeInvalidJSON},
		"malformed":       {`{"name":`, "", 400, apperr.CodeInvalidJSON},
		"empty":           {``, "", 400, apperr.CodeInvalidJSON},
		"wrong type":      {`{"name":5}`, "", 400, apperr.CodeValidationFailed},
		"array":           {`[1]`, "", 400, apperr.CodeInvalidJSON},
		"form content":    {`name=x`, "application/x-www-form-urlencoded", 415, apperr.CodeUnsupportedMedia},
		"no content type": {`{"name":"x"}`, "-", 415, apperr.CodeUnsupportedMedia},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			hdr := jsonHdr
			switch c.ct {
			case "-":
				hdr = nil
			case "":
			default:
				hdr = map[string]string{"Content-Type": c.ct}
			}
			rec := h.do(t, "POST", "acme.pos.test", "/api/echo", strings.NewReader(c.body), hdr)
			require.Equal(t, c.status, rec.Code, rec.Body.String())
			require.Equal(t, c.code, decodeErr(t, rec).Code)
		})
	}
	rec := h.do(t, "POST", "acme.pos.test", "/api/echo", strings.NewReader(`{"name":"x","role":"admin"}`), jsonHdr)
	require.Equal(t, map[string]string{"role": "unknown field"}, decodeErr(t, rec).Fields)
}

func TestBodyLimit(t *testing.T) {
	h := newHarness(t, nil)
	big := `{"name":"` + strings.Repeat("a", int(UploadBodyLimit)) + `"}`
	hdr := map[string]string{"Content-Type": "application/json"}
	rec := h.do(t, "POST", "acme.pos.test", "/api/echo", strings.NewReader(big), hdr)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, apperr.CodeBodyTooLarge, decodeErr(t, rec).Code)

	// Chunked body without Content-Length is still capped while reading.
	req := httptest.NewRequest("POST", "http://acme.pos.test/api/echo", io.MultiReader(strings.NewReader(big)))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestErrorsHideInternals(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(t, "GET", "acme.pos.test", "/api/fail", nil, nil)
	require.Equal(t, 500, rec.Code)
	require.JSONEq(t, `{"error":"Internal server error","code":"INTERNAL"}`, rec.Body.String())
	require.Contains(t, h.log.String(), "does not exist", "details go to the log")

	rec = h.do(t, "GET", "acme.pos.test", "/api/panic", nil, nil)
	require.Equal(t, 500, rec.Code)
	require.NotContains(t, rec.Body.String(), "secret-detail")
	require.Contains(t, h.log.String(), `"status":500`, "panic logged as 500 by the access log")
}

func TestRetryAfter(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(t, "GET", "acme.pos.test", "/api/limited", nil, nil)
	require.Equal(t, 429, rec.Code)
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestRequestTimeoutSetsDeadline(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(t, "GET", "acme.pos.test", "/api/deadline", nil, nil)
	require.JSONEq(t, `{"deadline":true}`, rec.Body.String())
}

func TestNotFoundIsGenericEverywhere(t *testing.T) {
	h := newHarness(t, nil)
	for _, c := range []struct{ host, path string }{
		{"acme.pos.test", "/api/nope"},
		{"acme.pos.test", "/api/platform/ping"}, // platform routes never on tenant hosts
		{"admin.pos.test", "/api/echo"},         // tenant routes never on the admin host
		{"pos.test", "/api/echo"},
		{"acme.pos.test", "/index.html"}, // no SPA configured
	} {
		rec := h.do(t, "GET", c.host, c.path, nil, nil)
		require.Equal(t, 404, rec.Code, c)
		require.JSONEq(t, `{"error":"Not found","code":"NOT_FOUND"}`, rec.Body.String())
	}
	rec := h.do(t, "GET", "admin.pos.test", "/api/platform/ping", nil, nil)
	require.Equal(t, 200, rec.Code)
}

func TestSecureHeadersPerHost(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0o600))
	spa, err := NewSPA(dir)
	require.NoError(t, err)
	h := newHarness(t, func(d *RouterDeps) {
		d.TenantSPA, d.AdminSPA, d.LandingSPA = spa, spa, spa
	})

	common := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		hd := rec.Header()
		require.Equal(t, "nosniff", hd.Get("X-Content-Type-Options"))
		require.Equal(t, "DENY", hd.Get("X-Frame-Options"))
		require.Equal(t, "strict-origin-when-cross-origin", hd.Get("Referrer-Policy"))
		require.Equal(t, "same-origin", hd.Get("Cross-Origin-Opener-Policy"))
		require.NotEmpty(t, hd.Get("Permissions-Policy"))
		require.Empty(t, hd.Get("Strict-Transport-Security"), "HSTS off until go-live")
	}

	rec := h.do(t, "GET", "admin.pos.test", "/", nil, nil)
	common(rec)
	csp := rec.Header().Get("Content-Security-Policy")
	require.Equal(t, cspStrict, csp)
	require.NotContains(t, csp, "unsafe-inline")
	require.NotContains(t, csp, "unsafe-eval")

	rec = h.do(t, "GET", "acme.pos.test", "/pos", nil, nil)
	common(rec)
	require.Equal(t, cspTenant, rec.Header().Get("Content-Security-Policy"))
	require.Contains(t, rec.Body.String(), "spa", "SPA fallback for client routes")

	rec = h.do(t, "GET", "pos.test", "/", nil, nil)
	require.Equal(t, cspStrict, rec.Header().Get("Content-Security-Policy"))

	for _, host := range []string{"acme.pos.test", "admin.pos.test"} {
		rec = h.do(t, "GET", host, "/api/x", nil, nil)
		common(rec)
		require.Equal(t, cspAPI, rec.Header().Get("Content-Security-Policy"))
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}

	h = newHarness(t, func(d *RouterDeps) { d.HSTS = true })
	rec = h.do(t, "GET", "acme.pos.test", "/api/x", nil, nil)
	require.Equal(t, "max-age=31536000; includeSubDomains", rec.Header().Get("Strict-Transport-Security"))
}

func TestSPA(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o600))
	outside := filepath.Join(filepath.Dir(dir), "outside-secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("top secret"), 0o600))
	t.Cleanup(func() { _ = os.Remove(outside) })
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link.txt")))

	spa, err := NewSPA(dir)
	require.NoError(t, err)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, httptest.NewRequest("GET", "http://acme.pos.test"+p, nil))
		return rec
	}

	rec := get("/assets/app-abc123.js")
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Header().Get("Cache-Control"), "immutable")

	rec = get("/products/12")
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

	require.Equal(t, 404, get("/missing.js").Code)
	require.NotContains(t, get("/../outside-secret.txt").Body.String(), "top secret")
	require.NotContains(t, get("/link.txt").Body.String(), "top secret", "symlink escape blocked by os.Root")

	rec = httptest.NewRecorder()
	spa.ServeHTTP(rec, httptest.NewRequest("POST", "http://acme.pos.test/", nil))
	require.Equal(t, 404, rec.Code)

	none, err := NewSPA("")
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = NewSPA(t.TempDir())
	require.Error(t, err, "index.html required")
}

func TestHealthReadiness(t *testing.T) {
	h := newHarness(t, nil)
	h.hc.Add("db", func(context.Context) error { return errors.New("connection refused to 10.0.0.5") })
	rec := h.do(t, "GET", "acme.pos.test", "/readyz", nil, nil)
	require.Equal(t, 503, rec.Code)
	require.JSONEq(t, `{"status":"not_ready","checks":{"db":"fail"}}`, rec.Body.String())

	h.hc.Add("db", func(context.Context) error { return nil })
	rec = h.do(t, "GET", "acme.pos.test", "/readyz", nil, nil)
	require.Equal(t, 200, rec.Code)

	h.hc.SetDraining()
	rec = h.do(t, "GET", "acme.pos.test", "/readyz", nil, nil)
	require.Equal(t, 503, rec.Code)
	rec = h.do(t, "GET", "acme.pos.test", "/healthz", nil, nil)
	require.Equal(t, 200, rec.Code, "liveness stays up while draining")
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestRequestIDTrust(t *testing.T) {
	h := newHarness(t, nil)
	in := map[string]string{"X-Request-Id": "proxy-id-12345"}
	rec := h.do(t, "GET", "acme.pos.test", "/healthz", nil, in)
	require.NotEqual(t, "proxy-id-12345", rec.Header().Get("X-Request-Id"), "untrusted peer cannot set the ID")

	in["RemoteAddr"] = "10.9.9.9:1"
	rec = h.do(t, "GET", "acme.pos.test", "/healthz", nil, in)
	require.Equal(t, "proxy-id-12345", rec.Header().Get("X-Request-Id"))

	in["X-Request-Id"] = "bad id\nwith newline"
	rec = h.do(t, "GET", "acme.pos.test", "/healthz", nil, in)
	require.Len(t, rec.Header().Get("X-Request-Id"), 32)
}

func TestAccessLogFields(t *testing.T) {
	h := newHarness(t, nil)
	h.do(t, "GET", "acme.pos.test", "/api/deadline", nil, map[string]string{"Authorization": "Bearer abc.def.ghi"})
	var line map[string]any
	lines := strings.Split(strings.TrimSpace(h.log.String()), "\n")
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &line))
	require.Equal(t, "http request", line["msg"])
	require.Equal(t, "/api/deadline", line["route"], "route pattern, not raw path")
	require.Equal(t, "tenant", line["host_kind"])
	require.Equal(t, "203.0.113.9", line["remote_ip"])
	require.NotEmpty(t, line["request_id"])
	require.NotContains(t, h.log.String(), "abc.def.ghi")
}

func TestCORS(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(t, "GET", "acme.pos.test", "/api/deadline", nil, map[string]string{"Origin": "http://localhost:5173"})
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"), "no CORS by default")

	h = newHarness(t, func(d *RouterDeps) { d.CORSDevOrigins = []string{"http://localhost:5173"} })
	rec = h.do(t, "OPTIONS", "acme.pos.test", "/api/echo", nil, map[string]string{
		"Origin": "http://localhost:5173", "Access-Control-Request-Method": "POST",
	})
	require.Equal(t, 204, rec.Code)
	require.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))

	rec = h.do(t, "GET", "acme.pos.test", "/api/deadline", nil, map[string]string{"Origin": "http://evil.example"})
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"), "no reflected origins")
	require.Contains(t, rec.Header().Values("Vary"), "Origin")
}

func TestLoggerFromDefault(t *testing.T) {
	require.Equal(t, slog.Default(), LoggerFrom(context.Background()))
	require.Empty(t, RequestID(context.Background()))
	require.Nil(t, Info(context.Background()))
	Info(context.Background()).Set(func(*RequestInfo) {}) // nil-safe
}
