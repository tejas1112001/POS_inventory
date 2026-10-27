package tenancy

import (
	"context"
	"net/http"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
)

type tenantKey struct{}

// TenantFrom returns the tenant resolved for this request.
func TenantFrom(ctx context.Context) (Tenant, bool) {
	t, ok := ctx.Value(tenantKey{}).(Tenant)
	return t, ok
}

// WithTenant stores a tenant in the context (tests, jobs).
func WithTenant(ctx context.Context, t Tenant) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}

// Resolve looks up the tenant for a tenant host. Unknown, deleted, failed and
// provisioning tenants all get the same 404 body through the same code path.
// The tenant comes only from the stored registry row, never from the request.
// An old slug (renamed tenant) redirects page loads with 308; API calls get
// the generic 404 (TENANCY §5 step 2).
func Resolve(reg *Registry, n httpx.Network, baseDomain string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := httpx.HostFrom(r.Context())
			if h.Kind != httpx.HostTenant {
				httpx.WriteError(w, r, apperr.NotFound())
				return
			}
			t, err := reg.BySlug(r.Context(), h.Slug)
			if err != nil {
				if e, ok := apperr.As(err); ok && e.Kind == apperr.KindNotFound && r.Method == http.MethodGet && !httpx.IsAPIPath(r.URL.Path) {
					if newSlug, ok := reg.RedirectSlug(r.Context(), h.Slug); ok {
						target := n.Scheme(r) + "://" + newSlug + "." + baseDomain + r.URL.RequestURI()
						// The host is always <stored slug>.<BASE_DOMAIN>; only path and
						// query come from the request, so this cannot leave our domain.
						http.Redirect(w, r, target, http.StatusPermanentRedirect) //nolint:gosec // G710: host fixed, see above
						return
					}
				}
				httpx.WriteError(w, r, err)
				return
			}
			httpx.Info(r.Context()).Set(func(ri *httpx.RequestInfo) { ri.TenantID = t.ID })
			next.ServeHTTP(w, r.WithContext(WithTenant(r.Context(), t)))
		})
	}
}
