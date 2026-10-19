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
func Resolve(reg *Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := httpx.HostFrom(r.Context())
			if h.Kind != httpx.HostTenant {
				httpx.WriteError(w, r, apperr.NotFound())
				return
			}
			t, err := reg.BySlug(r.Context(), h.Slug)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			httpx.Info(r.Context()).Set(func(ri *httpx.RequestInfo) { ri.TenantID = t.ID })
			next.ServeHTTP(w, r.WithContext(WithTenant(r.Context(), t)))
		})
	}
}
