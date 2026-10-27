package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

// RequestInfo collects per-request facts that later middleware fills in
// (tenant, user) so the access log can include them.
type RequestInfo struct {
	mu              sync.Mutex
	TenantID        string
	UserID          string
	PlatformUserID  string
	ImpersonationID string
}

type infoKey struct{}

// Info returns the mutable request info, or nil outside a request.
func Info(ctx context.Context) *RequestInfo {
	ri, _ := ctx.Value(infoKey{}).(*RequestInfo)
	return ri
}

// Set updates fields under the lock.
func (ri *RequestInfo) Set(f func(*RequestInfo)) {
	if ri == nil {
		return
	}
	ri.mu.Lock()
	defer ri.mu.Unlock()
	f(ri)
}

func (ri *RequestInfo) attrs() []slog.Attr {
	ri.mu.Lock()
	defer ri.mu.Unlock()
	var out []slog.Attr
	for k, v := range map[string]string{
		"tenant_id": ri.TenantID, "user_id": ri.UserID,
		"platform_user_id": ri.PlatformUserID, "impersonation_id": ri.ImpersonationID,
	} {
		if v != "" {
			out = append(out, slog.String(k, v))
		}
	}
	return out
}

// AssignRequestID assigns a request ID. An inbound X-Request-Id is accepted only
// from a trusted proxy and only if it looks like an ID.
func AssignRequestID(n Network) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if n.trusted(r) {
				if in := r.Header.Get("X-Request-Id"); requestIDPattern.MatchString(in) {
					id = in
				}
			}
			if id == "" {
				id = newRequestID()
			}
			w.Header().Set("X-Request-Id", id)
			ctx := context.WithValue(r.Context(), keyRequestID, id)
			ctx = context.WithValue(ctx, infoKey{}, &RequestInfo{})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b[:])
}

// AccessLog logs one line per request and records HTTP metrics. It also puts
// a request-scoped logger (with request_id) into the context.
func AccessLog(base *slog.Logger, m *obs.Metrics, n Network) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqLog := base.With(slog.String("request_id", RequestID(r.Context())))
			ctx := WithLogger(r.Context(), reqLog)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				status := ww.Status()
				if status == 0 {
					status = http.StatusOK
				}
				route := "unmatched"
				if rc := chi.RouteContext(ctx); rc != nil {
					if p := rc.RoutePattern(); p != "" {
						route = p
					}
				}
				hostKind := HostFrom(ctx).Kind.String()
				if hk, ok := ctx.Value(hostKindOverride{}).(*string); ok && *hk != "" {
					hostKind = *hk
				}
				d := time.Since(start)
				if m != nil {
					m.HTTPRequests.WithLabelValues(route, r.Method, strconv.Itoa(status/100)+"xx", hostKind).Inc()
					m.HTTPDuration.WithLabelValues(route, r.Method).Observe(d.Seconds())
				}
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("route", route),
					slog.Int("status", status),
					slog.Float64("duration_ms", float64(d.Microseconds())/1000),
					slog.Int("bytes", ww.BytesWritten()),
					slog.String("remote_ip", n.ClientIP(r).String()),
					slog.String("host_kind", hostKind),
				}
				if ri := Info(ctx); ri != nil {
					attrs = append(attrs, ri.attrs()...)
				}
				level := slog.LevelInfo
				if status >= 500 {
					level = slog.LevelError
				}
				reqLog.LogAttrs(ctx, level, "http request", attrs...)
			}()

			hk := new(string)
			ctx = context.WithValue(ctx, hostKindOverride{}, hk)
			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}

// hostKindOverride lets the host classifier, which runs after AccessLog,
// report the host kind back to the access log.
type hostKindOverride struct{}

// Recover turns a panic into a logged 500 with the standard error body.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
				panic(rec)
			}
			LoggerFrom(r.Context()).ErrorContext(r.Context(), "panic",
				slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
			WriteError(w, r, apperr.Internal(errors.New("panic")))
		}()
		next.ServeHTTP(w, r)
	})
}

// CSPs (OPERATIONS §7).
const (
	cspAPI      = "default-src 'none'; frame-ancestors 'none'"
	cspStrict   = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'"
	cspTenant   = "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
	permsPolicy = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), interest-cohort=()"
)

// CSPStrict is the policy of the admin console and the main address.
const CSPStrict = cspStrict

// IsAPIPath reports whether a path is an API or health endpoint (JSON).
func IsAPIPath(p string) bool {
	return strings.HasPrefix(p, "/api/") || p == "/api" ||
		p == "/healthz" || p == "/health" || p == "/readyz"
}

// SecureHeaders sets the security headers for every response. The CSP
// depends on the host kind: the admin and apex hosts get the strict policy,
// tenant pages the tenant policy, and every JSON response the API policy.
func SecureHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Permissions-Policy", permsPolicy)
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			switch {
			case IsAPIPath(r.URL.Path):
				h.Set("Content-Security-Policy", cspAPI)
				h.Set("Cache-Control", "no-store")
			case HostFrom(r.Context()).Kind == HostTenant:
				h.Set("Content-Security-Policy", cspTenant)
			default:
				h.Set("Content-Security-Policy", cspStrict)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit caps the request body. A declared Content-Length over the cap is
// rejected before reading.
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				WriteError(w, r, apperr.TooLarge())
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout puts a deadline on the request context. Handlers and DB calls must
// honour the context; the server WriteTimeout is the hard stop.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClassifyHost stores the host kind in the context. It does not reject
// anything, so health checks still answer on any host and SecureHeaders can
// pick the CSP by host kind.
func ClassifyHost(n Network) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := n.Classify(r)
			if hk, ok := r.Context().Value(hostKindOverride{}).(*string); ok {
				*hk = h.Kind.String()
			}
			next.ServeHTTP(w, r.WithContext(withHost(r.Context(), h)))
		})
	}
}

// RejectUnknownHost answers 421 for hosts this server does not serve.
func RejectUnknownHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if HostFrom(r.Context()).Kind == HostUnknown {
			WriteError(w, r, apperr.Misdirected())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CORS implements TENANCY §7.5: no CORS headers by default (same origin).
// In development only, exact origins from CORS_DEV_ORIGINS are allowed.
func CORS(devOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(devOrigins) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")
			if origin == "" || !slices.Contains(devOrigins, origin) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Expose-Headers", "X-Request-Id, X-Total-Count, X-Truncated, Retry-After")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, Idempotency-Key")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
