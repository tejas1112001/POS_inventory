package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

// RouterDeps are the inputs of the HTTP router.
type RouterDeps struct {
	Logger         *slog.Logger
	Metrics        *obs.Metrics
	Network        Network
	Health         *Health
	HSTS           bool
	RequestTimeout time.Duration
	CORSDevOrigins []string

	// Per-host route registration. Later steps mount their modules here.
	Tenant   func(r chi.Router)
	Platform func(r chi.Router)

	// TenantRoot registers non-API routes on tenant hosts (/files/t/...).
	TenantRoot func(r chi.Router)

	// TenantResolver runs first on every tenant-host request (step 2).
	TenantResolver func(http.Handler) http.Handler

	// Optional static builds (STATIC_DIR/tenant, /admin).
	TenantSPA, AdminSPA http.Handler

	// Apex registers the landing site on the main address; ApexNotFound
	// answers every other path there.
	Apex         func(r chi.Router)
	ApexNotFound http.Handler
}

// NewRouter builds the root handler:
//
//	request-id → access log → recover → host classify → secure headers
//	→ reject NUL → (health, any host) → reject unknown host → per-host router
//	  (body limit → timeout → CORS → routes)
func NewRouter(d RouterDeps) http.Handler {
	root := chi.NewRouter()
	// Recover sits inside the access log so a panic is logged as a 500.
	root.Use(AssignRequestID(d.Network), AccessLog(d.Logger, d.Metrics, d.Network), Recover,
		ClassifyHost(d.Network), SecureHeaders(d.HSTS), RejectNUL)

	root.Get("/healthz", d.Health.Live)
	root.Get("/health", d.Health.Live)
	root.Get("/readyz", d.Health.Ready)

	tenant := hostRouter(d, d.Tenant, d.TenantSPA, d.TenantRoot)
	if d.TenantResolver != nil {
		tenant = d.TenantResolver(tenant)
	}
	// The Super Admin console is never listed by search engines.
	platform := Noindex(hostRouter(d, d.Platform, d.AdminSPA, nil))
	apex := hostRouter(d, nil, d.ApexNotFound, d.Apex)

	root.Group(func(r chi.Router) {
		r.Use(RejectUnknownHost)
		r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch HostFrom(req.Context()).Kind {
			case HostTenant:
				tenant.ServeHTTP(w, req)
			case HostPlatform:
				platform.ServeHTTP(w, req)
			default:
				apex.ServeHTTP(w, req)
			}
		}))
	})
	root.NotFound(func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, apperr.NotFound()) })
	root.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, apperr.NotFound()) })
	return root
}

// Noindex adds "X-Robots-Tag: noindex, nofollow" to every response.
func Noindex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

func hostRouter(d RouterDeps, mount func(chi.Router), spa http.Handler, root func(chi.Router)) http.Handler {
	r := chi.NewRouter()
	// Outer cap; each API route applies its own (1 MiB default, 8 MiB uploads).
	r.Use(BodyLimit(UploadBodyLimit), Timeout(d.RequestTimeout), CORS(d.CORSDevOrigins))
	notFound := func(w http.ResponseWriter, req *http.Request) { WriteError(w, req, apperr.NotFound()) }
	r.Route("/api", func(api chi.Router) {
		if mount != nil {
			mount(api)
		}
		api.NotFound(notFound)
		api.MethodNotAllowed(notFound)
	})
	if root != nil {
		root(r)
	}
	if spa != nil {
		r.NotFound(spa.ServeHTTP)
	} else {
		r.NotFound(notFound)
	}
	r.MethodNotAllowed(notFound)
	return r
}
