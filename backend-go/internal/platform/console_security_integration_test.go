//go:build integration

package platform_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
)

// TestIPAllowlistHidesTheConsole (PHASE1_DESIGN §7.2, PLATFORM_IP_ALLOWLIST):
// outside the allowlist every console route, public or not, answers 404,
// so the console is not even revealed; inside it works as usual.
func TestIPAllowlistHidesTheConsole(t *testing.T) {
	e := newEnv(t)
	n := httpx.Network{BaseDomain: "pos.test"}
	ph := platform.NewHTTP(e.svc, n, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, nil, "http")
	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) { ph.Mount(api) })
	call := func(ip, method, path, body string) int {
		req := httptest.NewRequest(method, "http://admin.pos.test/api/platform"+path, strings.NewReader(body))
		req.RemoteAddr = ip + ":4444"
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/auth/login", `{"email":"root@platform.test","password":"x"}`},
		{"GET", "/me", ""},
		{"GET", "/tenants", ""},
	} {
		require.Equal(t, http.StatusNotFound, call("198.51.100.9", c.method, c.path, c.body), "outside: %s %s", c.method, c.path)
	}
	require.NotEqual(t, http.StatusNotFound, call("203.0.113.7", "POST", "/auth/login", `{"email":"root@platform.test","password":"wrong-password-123"}`), "inside the allowlist")
}
