package tenancy

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/httprate"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
)

// UncachedLookupsPerMinute is how many host lookups that miss the registry
// cache one client (its IP, or its IPv6 /64) may cause per minute. Beyond
// it the request gets 429 without a database query (ARCHITECTURE_REVIEW
// H4). A shop's own staff hit the cache after their first request, so only
// floods of new subdomains reach the limit.
const UncachedLookupsPerMinute = 60

// LookupLimiter limits uncached host lookups per client. A nil limiter
// allows everything.
type LookupLimiter struct {
	l         *httprate.RateLimiter
	network   httpx.Network
	onLimited func()
}

// NewLookupLimiter allows perMinute uncached lookups per client; onLimited
// (optional) runs on each refused request.
func NewLookupLimiter(perMinute int, n httpx.Network, onLimited func()) *LookupLimiter {
	quiet := httprate.WithResponseHeaders(httprate.ResponseHeaders{})
	return &LookupLimiter{l: httprate.NewRateLimiter(perMinute, time.Minute, quiet), network: n, onLimited: onLimited}
}

func (ll *LookupLimiter) over(r *http.Request) bool {
	if ll == nil {
		return false
	}
	ip := ll.network.ClientIP(r)
	if !ip.IsValid() {
		return false
	}
	if !ll.l.OnLimit(discard{h: http.Header{}}, r, httprate.CanonicalizeIP(ip.String())) {
		return false
	}
	if ll.onLimited != nil {
		ll.onLimited()
	}
	return true
}

// discard swallows what the limiter writes; Resolve sends its own error.
type discard struct{ h http.Header }

func (d discard) Header() http.Header         { return d.h }
func (d discard) Write(b []byte) (int, error) { return len(b), nil }
func (d discard) WriteHeader(int)             {}

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
// the generic 404 (TENANCY §5 step 2). lim (optional) caps the lookups one
// client can send past the cache.
func Resolve(reg *Registry, n httpx.Network, baseDomain string, lim *LookupLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := httpx.HostFrom(r.Context())
			if h.Kind != httpx.HostTenant {
				httpx.WriteError(w, r, apperr.NotFound())
				return
			}
			if !reg.SlugCached(h.Slug) && lim.over(r) {
				httpx.WriteError(w, r, apperr.RateLimited(apperr.CodeRateLimited, time.Minute))
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
