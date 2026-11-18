package rbac

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
)

// Kind is the access level a route requires.
type Kind int

// Access levels.
const (
	KindPublic        Kind = iota // no token
	KindAuthenticated             // any signed-in tenant user
	KindPermission                // a module:action permission
)

// Requirement is declared for every tenant route. Routes without one fail
// the startup self-check (deny by default, TENANCY §5 step 6).
type Requirement struct {
	Kind          Kind
	Perm          string
	AllowReadOnly bool   // passes the read_only tenant gate (billing exemption)
	BodyLimit     int64  // 0 = httpx.DefaultBodyLimit (1 MiB)
	Module        string // industry module that must be entitled and on (403 MODULE_OFF)
}

// Public marks an unauthenticated route.
func Public() Requirement { return Requirement{Kind: KindPublic} }

// Authenticated marks a route for any signed-in user.
func Authenticated() Requirement { return Requirement{Kind: KindAuthenticated} }

// Require marks a route that needs module:action.
func Require(module, action string) Requirement {
	return Requirement{Kind: KindPermission, Perm: module + ":" + action}
}

// Uploads raises the body limit to httpx.UploadBodyLimit (8 MiB) for routes
// that accept images.
func (r Requirement) Uploads() Requirement { r.BodyLimit = httpx.UploadBodyLimit; return r }

// InModule returns r refused with 403 MODULE_OFF unless the industry
// module is entitled and switched on (docs/INDUSTRY_PHASE1_DESIGN.md item 1).
func (r Requirement) InModule(key string) Requirement { r.Module = key; return r }

// ReadOnlyOK returns r with the read_only gate exemption. Only the routes on
// AllowReadOnlyList may use it (checked by a router-walk test).
func (r Requirement) ReadOnlyOK() Requirement { r.AllowReadOnly = true; return r }

// AllowReadOnlyList is the code allow-list of write routes that a read_only
// tenant may still call (TENANCY §5 step 5).
var AllowReadOnlyList = map[string]bool{
	"POST /api/auth/login":   true,
	"POST /api/auth/refresh": true,
	"POST /api/auth/logout":  true,
	"GET /api/auth/me":       true,
	// Credentials, so a read-only tenant's owner can sign in to pay.
	"POST /api/auth/password/change": true,
	"POST /api/auth/password/forgot": true,
	"POST /api/auth/password/reset":  true,
	"POST /api/auth/invite/accept":   true,
	// Billing (C-21): a read-only tenant can still pay to come back.
	"POST /api/billing/checkout": true,
}

// Guard builds the middleware for a requirement.
type Guard func(Requirement) func(http.Handler) http.Handler

// ModuleGuard builds the middleware that refuses a route when its module
// is off. It runs after Guard (signed in, permitted).
type ModuleGuard func(module string) func(http.Handler) http.Handler

// Router registers tenant routes together with their requirement.
type Router struct {
	r      chi.Router
	prefix string
	guard  Guard
	module ModuleGuard
	mu     *sync.Mutex
	decl   map[string]Requirement
}

// WithModuleGuard sets the module middleware. Routes registered with
// InModule before it is set, or without it, fail the self-check.
func (rt *Router) WithModuleGuard(g ModuleGuard) *Router { rt.module = g; return rt }

// NewRouter wraps r (mounted at prefix, e.g. "/api") with guard.
func NewRouter(r chi.Router, prefix string, guard Guard) *Router {
	return &Router{r: r, prefix: prefix, guard: guard, mu: &sync.Mutex{}, decl: map[string]Requirement{}}
}

// Handle registers method pattern with its requirement.
func (rt *Router) Handle(method, pattern string, req Requirement, h http.HandlerFunc) {
	rt.mu.Lock()
	rt.decl[method+" "+rt.prefix+pattern] = req
	rt.mu.Unlock()
	limit := req.BodyLimit
	if limit <= 0 {
		limit = httpx.DefaultBodyLimit
	}
	mws := []func(http.Handler) http.Handler{httpx.BodyLimit(limit), rt.guard(req)}
	if req.Module != "" && rt.module != nil {
		mws = append(mws, rt.module(req.Module))
	}
	rt.r.With(mws...).Method(method, pattern, h)
}

// Declarations returns every registered route and requirement.
func (rt *Router) Declarations() map[string]Requirement {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make(map[string]Requirement, len(rt.decl))
	for k, v := range rt.decl {
		out[k] = v
	}
	return out
}

// SelfCheck walks the chi router and fails if any route was registered
// without a requirement, or if a route outside the allow-list carries the
// read_only exemption.
func (rt *Router) SelfCheck() error {
	decl := rt.Declarations()
	var problems []string
	err := chi.Walk(rt.r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + rt.prefix + strings.TrimSuffix(route, "/")
		if strings.HasSuffix(route, "/*") {
			return nil // sub-router mount points
		}
		if _, ok := decl[key]; !ok {
			problems = append(problems, "no requirement: "+key)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for k, v := range decl {
		if v.AllowReadOnly && !AllowReadOnlyList[k] {
			problems = append(problems, "read_only exemption not allow-listed: "+k)
		}
		if v.Module != "" && rt.module == nil {
			problems = append(problems, "module route without a module guard: "+k)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("rbac: route self-check failed:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
