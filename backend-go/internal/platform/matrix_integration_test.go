//go:build integration

package platform_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
)

// consoleSpec is the expected access rule for one console route, taken
// from PHASE1_DESIGN §7.1 (roles) and §7.2 (step-up), not from the code.
type consoleSpec struct {
	public bool            // no token (sign-in flow)
	roles  map[string]bool // roles that may call it; nil = any signed-in user
	stepUp bool
}

const (
	sa  = platform.RoleSuperAdmin
	ba  = platform.RoleBillingAdmin
	sup = platform.RoleSupport
)

func roles(rs ...string) map[string]bool {
	m := map[string]bool{}
	for _, r := range rs {
		m[r] = true
	}
	return m
}

var consoleMatrix = map[string]consoleSpec{
	"POST /auth/login":              {public: true},
	"POST /auth/mfa/verify":         {public: true},
	"POST /auth/mfa/enroll":         {public: true},
	"POST /auth/mfa/enroll/confirm": {public: true},
	"POST /auth/password/change":    {public: true}, // challenge or signed-in session, checked inside
	"POST /auth/refresh":            {public: true}, // cookie + CSRF
	"POST /auth/logout":             {public: true}, // cookie + CSRF

	"GET /me":            {},
	"POST /auth/step-up": {},

	// tenants:view = all three roles.
	"GET /tenants":      {roles: roles(sa, ba, sup)},
	"GET /tenants/{id}": {roles: roles(sa, ba, sup)},
	// tenants:create / edit / slug = super_admin; slug needs step-up.
	"POST /tenants":                         {roles: roles(sa)},
	"PATCH /tenants/{id}":                   {roles: roles(sa)},
	"POST /tenants/{id}/slug":               {roles: roles(sa), stepUp: true},
	"POST /tenants/{id}/provisioning/retry": {roles: roles(sa)},
	"POST /tenants/{id}/owner-invite":       {roles: roles(sa), stepUp: true},
	// suspend / reactivate: super_admin and billing_admin, step-up.
	"POST /tenants/{id}/suspend":    {roles: roles(sa, ba), stepUp: true},
	"POST /tenants/{id}/reactivate": {roles: roles(sa, ba), stepUp: true},
	// delete / restore: super_admin, step-up.
	"DELETE /tenants/{id}":       {roles: roles(sa), stepUp: true},
	"POST /tenants/{id}/restore": {roles: roles(sa), stepUp: true},
	// export / restore-test: super_admin. §7.1 lists step-up for both; the
	// build asks for it on export only (PHASE2_GATE_REPORT, step 18). A
	// restore test only writes a scratch schema. Raised in Phase 3.
	"POST /tenants/{id}/exports":       {roles: roles(sa), stepUp: true},
	"GET /tenants/{id}/exports":        {roles: roles(sa)},
	"POST /tenants/{id}/restore-tests": {roles: roles(sa)},
	"GET /tenants/{id}/restore-tests":  {roles: roles(sa)},
	// plans:manage = super_admin, billing_admin; the plan list is also
	// readable with billing:view (support).
	"GET /plans":        {roles: roles(sa, ba, sup)},
	"POST /plans":       {roles: roles(sa, ba)},
	"PATCH /plans/{id}": {roles: roles(sa, ba)},
	// billing:view = all three; manage = super_admin, billing_admin.
	"GET /tenants/{id}/billing":                    {roles: roles(sa, ba, sup)},
	"GET /billing/expiring":                        {roles: roles(sa, ba, sup)},
	"GET /billing/past-due":                        {roles: roles(sa, ba, sup)},
	"GET /billing/invoices":                        {roles: roles(sa, ba, sup)},
	"POST /tenants/{id}/subscription":              {roles: roles(sa, ba)},
	"POST /tenants/{id}/subscription/extend-trial": {roles: roles(sa, ba)},
	"POST /tenants/{id}/subscription/grace":        {roles: roles(sa, ba)},
	"POST /tenants/{id}/subscription/cancel":       {roles: roles(sa, ba), stepUp: true},
	// audit: everyone (own actions or all).
	"GET /audit": {roles: roles(sa, ba, sup)},
}

// consoleRoutes walks the console router as mounted in posd.
func consoleRoutes(t *testing.T, e *env) []string {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) { e.ph.Mount(api) })
	var out []string
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.TrimPrefix(route, "/api/platform"), "/")
		out = append(out, method+" "+route)
		return nil
	}))
	sort.Strings(out)
	return out
}

func codeOf(r resp) string { s, _ := r.Body["code"].(string); return s }

func TestConsoleRouteMatrix(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	sessions := map[string]session{
		sa:  e.mfaLogin(t, rootEmail, newPW, root.secret), // a fresh session: no step-up yet
		ba:  e.mfaLogin(t, "matrix-billing@platform.test", "matrix-billing-pass-321", addUser(t, e, "matrix-billing@platform.test", ba, "matrix-billing-pass-321")),
		sup: e.mfaLogin(t, "matrix-support@platform.test", "matrix-support-pass-321", addUser(t, e, "matrix-support@platform.test", sup, "matrix-support-pass-321")),
	}

	// The table and the router list the same routes.
	routes := consoleRoutes(t, e)
	want := make([]string, 0, len(consoleMatrix))
	for k := range consoleMatrix {
		want = append(want, k)
	}
	sort.Strings(want)
	require.Equal(t, want, routes, "every console route needs a row in consoleMatrix (and every row a route)")

	// A tenant token is signed with a different key and audience.
	tenantTok, _, err := e.tenSign.Sign("admin.pos.test", "1", authtoken.Claims{TenantID: "abcdefghij"})
	require.NoError(t, err)

	const ghost = "zzzzzzzzzz" // a well-formed tenant ID that does not exist
	checked := 0
	for _, key := range routes {
		spec := consoleMatrix[key]
		method, pattern, _ := strings.Cut(key, " ")
		id := ghost
		if strings.HasPrefix(pattern, "/plans/") {
			id = "999999"
		}
		path := strings.ReplaceAll(pattern, "{id}", id)
		var body any
		if method != http.MethodGet {
			body = map[string]any{}
		}
		if spec.public {
			r := e.do(req{method: method, path: path, body: body})
			require.NotEqual(t, 404, r.Code, "%s exists", key)
			require.Less(t, r.Code, 500, "%s: %s", key, r.Raw)
			continue
		}
		for name, tok := range map[string]string{"no token": "", "tenant token": tenantTok, "garbage": "x.y.z"} {
			r := e.do(req{method: method, path: path, body: body, token: tok})
			require.Equal(t, 401, r.Code, "%s with %s: %s", key, name, r.Raw)
		}
		for role, s := range sessions {
			r := e.do(req{method: method, path: path, body: body, token: s.token})
			switch {
			case spec.roles != nil && !spec.roles[role]:
				require.Equal(t, 403, r.Code, "%s as %s: %s", key, role, r.Raw)
				require.Equal(t, apperr.CodeForbidden, codeOf(r), "%s as %s", key, role)
			case spec.stepUp:
				require.Equal(t, 403, r.Code, "%s as %s without step-up: %s", key, role, r.Raw)
				require.Equal(t, apperr.CodeStepUpRequired, codeOf(r), "%s as %s", key, role)
			default:
				// Allowed: the request reaches the handler (a missing tenant,
				// plan or field is the handler's answer).
				require.NotContains(t, []string{apperr.CodeUnauthenticated, apperr.CodeForbidden, apperr.CodeStepUpRequired}, codeOf(r),
					"%s as %s: %s", key, role, r.Raw)
				require.Less(t, r.Code, 500, "%s as %s: %s", key, role, r.Raw)
			}
		}
		checked++
	}
	t.Logf("console matrix: %d routes, %d protected x 3 roles", len(routes), checked)
}
