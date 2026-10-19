package platform

import (
	"context"
	"net/http"
	"net/mail"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

// CookieName is the platform refresh cookie (host-only, TENANCY §7.4).
const CookieName = "__Host-prt"

// HTTP wires the service to routes.
type HTTP struct {
	svc       *Service
	net       httpx.Network
	allowlist []netip.Prefix
	metrics   *obs.Metrics
	scheme    string
}

// NewHTTP creates the platform HTTP handlers.
func NewHTTP(svc *Service, n httpx.Network, allowlist []netip.Prefix, m *obs.Metrics, publicScheme string) *HTTP {
	return &HTTP{svc: svc, net: n, allowlist: allowlist, metrics: m, scheme: publicScheme}
}

type actorKey struct{}

func actorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

func (h *HTTP) meta(r *http.Request) Meta {
	return Meta{IP: h.net.ClientIP(r), UserAgent: r.UserAgent(), RequestID: httpx.RequestID(r.Context())}
}

func (h *HTTP) limiter(name string, n int, per time.Duration, key func(*http.Request) string) func(http.Handler) http.Handler {
	return httprate.NewRateLimiter(n, per,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) { return name + ":" + key(r), nil }),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			if h.metrics != nil {
				h.metrics.RateLimited.WithLabelValues(name).Inc()
			}
			httpx.WriteError(w, r, apperr.RateLimited(apperr.CodeRateLimited, per))
		})).Handler
}

func (h *HTTP) byIP(r *http.Request) string { return h.net.ClientIP(r).String() }

// Mount registers /api/platform/* on the admin-host router.
func (h *HTTP) Mount(api chi.Router) {
	api.Route("/platform", func(r chi.Router) {
		r.Use(h.ipAllowlist)
		r.Group(func(pub chi.Router) {
			pub.With(h.limiter("platform_login", 5, time.Minute, h.byIP)).Post("/auth/login", h.login)
			mfa := h.limiter("platform_mfa", 10, time.Minute, h.byIP)
			pub.With(mfa).Post("/auth/mfa/verify", h.mfaVerify)
			pub.With(mfa).Post("/auth/mfa/enroll", h.enrollStart)
			pub.With(mfa).Post("/auth/mfa/enroll/confirm", h.enrollConfirm)
			pub.With(mfa).Post("/auth/password/change", h.passwordChange)
			pub.With(h.limiter("platform_refresh", 30, time.Minute, h.byIP), h.csrf).Post("/auth/refresh", h.refresh)
			pub.With(h.csrf).Post("/auth/logout", h.logout)
		})
		r.Group(func(p chi.Router) {
			p.Use(h.authenticate)
			p.Get("/me", h.me)
			p.With(h.limiter("platform_stepup", 5, time.Minute, h.byIP)).Post("/auth/step-up", h.stepUp)

			p.With(h.need(PermTenantsView)).Get("/tenants", h.listTenants)
			p.With(h.need(PermTenantsCreate)).Post("/tenants", h.createTenant)
			p.With(h.need(PermTenantsView)).Get("/tenants/{id}", h.getTenant)
			p.With(h.need(PermTenantsEdit)).Patch("/tenants/{id}", h.patchTenant)
			p.With(h.need(PermTenantsEdit), h.stepUpRequired).Post("/tenants/{id}/slug", h.renameSlug)
			p.With(h.need(PermTenantsSuspend), h.stepUpRequired).Post("/tenants/{id}/suspend", h.suspend)
			p.With(h.need(PermTenantsSuspend), h.stepUpRequired).Post("/tenants/{id}/reactivate", h.reactivate)
			p.With(h.need(PermTenantsDelete), h.stepUpRequired).Delete("/tenants/{id}", h.deleteTenant)
			p.With(h.need(PermTenantsDelete), h.stepUpRequired).Post("/tenants/{id}/restore", h.restore)
			p.With(h.need(PermTenantsCreate)).Post("/tenants/{id}/provisioning/retry", h.retry)
			p.With(h.needAny(PermAuditViewAll, PermAuditViewOwn)).Get("/audit", h.audit)
		})
	})
}

// ipAllowlist enforces PLATFORM_IP_ALLOWLIST when set.
func (h *HTTP) ipAllowlist(next http.Handler) http.Handler {
	if len(h.allowlist) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := h.net.ClientIP(r)
		for _, p := range h.allowlist {
			if p.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		httpx.WriteError(w, r, apperr.NotFound())
	})
}

// csrf protects the cookie-authenticated endpoints: the Origin must be this
// exact host and the custom header must be present (TENANCY §7.4).
func (h *HTTP) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := h.net.Scheme(r) + "://" + httpx.HostFrom(r.Context()).Name
		if r.Header.Get("X-Requested-With") != "fetch" || stripDefaultPort(r.Header.Get("Origin")) != want {
			httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "Cross-site request refused"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func stripDefaultPort(origin string) string {
	origin = strings.TrimSuffix(origin, ":443")
	return strings.TrimSuffix(origin, ":80")
}

func (h *HTTP) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		tok, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || tok == "" {
			httpx.WriteError(w, r, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in"))
			return
		}
		a, err := h.svc.Authenticate(r.Context(), tok)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.Info(r.Context()).Set(func(ri *httpx.RequestInfo) { ri.PlatformUserID = strconv.FormatInt(a.UserID, 10) })
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}

func (h *HTTP) need(perm string) func(http.Handler) http.Handler { return h.needAny(perm) }

func (h *HTTP) needAny(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := actorFrom(r.Context())
			for _, p := range perms {
				if Can(a.Role, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "You do not have permission for this action"))
		})
	}
}

func (h *HTTP) stepUpRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.svc.now().Before(actorFrom(r.Context()).StepUpUntil) {
			httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeStepUpRequired, "Confirm with your authenticator code first"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *HTTP) setRefreshCookie(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: raw, Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int(h.svc.cfg.SessionMax.Seconds()),
	})
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func (h *HTTP) writeSession(w http.ResponseWriter, res SessionResult) {
	h.setRefreshCookie(w, res.Refresh())
	httpx.WriteJSON(w, http.StatusOK, res)
}

// --------------------------------------------------------------- handlers

func (h *HTTP) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.Login(r.Context(), in.Email, in.Password, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (h *HTTP) passwordChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID     string `json:"challengeId"`
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if in.ChallengeID != "" {
		res, err := h.svc.ChangePasswordForced(r.Context(), in.ChallengeID, in.NewPassword, h.meta(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, res)
		return
	}
	// Signed-in change: needs a valid access token.
	h.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.svc.ChangePassword(r.Context(), actorFrom(r.Context()), in.CurrentPassword, in.NewPassword, h.meta(r)); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)
}

func (h *HTTP) enrollStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID string `json:"challengeId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.StartEnroll(r.Context(), in.ChallengeID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (h *HTTP) enrollConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID string `json:"challengeId"`
		Code        string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.ConfirmEnroll(r.Context(), in.ChallengeID, in.Code, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.writeSession(w, res)
}

func (h *HTTP) mfaVerify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChallengeID  string `json:"challengeId"`
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.VerifyMFA(r.Context(), in.ChallengeID, in.Code, in.RecoveryCode, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.writeSession(w, res)
}

func (h *HTTP) refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	raw := ""
	if err == nil {
		raw = c.Value
	}
	res, err := h.svc.Refresh(r.Context(), raw, h.meta(r))
	if err != nil {
		clearRefreshCookie(w)
		httpx.WriteError(w, r, err)
		return
	}
	h.writeSession(w, res)
}

func (h *HTTP) logout(w http.ResponseWriter, r *http.Request) {
	raw := ""
	if c, err := r.Cookie(CookieName); err == nil {
		raw = c.Value
	}
	if err := h.svc.Logout(r.Context(), raw, nil, h.meta(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTP) me(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.svc.Me(actorFrom(r.Context())))
}

func (h *HTTP) stepUp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	until, err := h.svc.StepUp(r.Context(), actorFrom(r.Context()), in.Code, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]time.Time{"stepUpUntil": until})
}

func (h *HTTP) listTenants(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	list, total, err := h.svc.ListTenants(r.Context(), ListFilter{Status: q.Get("status"), Q: q.Get("q"), Limit: limit, Offset: (page - 1) * limit})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if list == nil {
		list = []TenantView{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"data":       list,
		"pagination": map[string]int{"total": total, "page": page, "limit": limit, "totalPages": (total + limit - 1) / limit},
	})
}

func (h *HTTP) createTenant(w http.ResponseWriter, r *http.Request) {
	var in CreateTenantInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.CreateTenant(r.Context(), actorFrom(r.Context()), in, h.meta(r))
	if err != nil && res.TenantID == "" {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"id": res.TenantID, "status": res.Status, "url": res.TenantURL, "ownerUserId": res.OwnerUserID,
	})
}

func (h *HTTP) getTenant(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.GetTenant(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *HTTP) patchTenant(w http.ResponseWriter, r *http.Request) {
	var in PatchTenantInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	v, err := h.svc.PatchTenant(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *HTTP) renameSlug(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug string `json:"slug"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	v, err := h.svc.RenameSlug(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.Slug, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

type reasonBody struct {
	Reason string `json:"reason"`
}

// optionalReason accepts an empty body or {"reason": "..."}.
func optionalReason(r *http.Request) (string, error) {
	if r.ContentLength == 0 {
		return "", nil
	}
	var in reasonBody
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return "", err
	}
	return in.Reason, nil
}

func (h *HTTP) suspend(w http.ResponseWriter, r *http.Request) {
	var in reasonBody
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.respondTenant(w, r)(h.svc.Suspend(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.Reason, h.meta(r)))
}

func (h *HTTP) reactivate(w http.ResponseWriter, r *http.Request) {
	h.respondTenant(w, r)(h.svc.Reactivate(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), h.meta(r)))
}

func (h *HTTP) deleteTenant(w http.ResponseWriter, r *http.Request) {
	reason, err := optionalReason(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.respondTenant(w, r)(h.svc.Delete(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), reason, h.meta(r)))
}

func (h *HTTP) restore(w http.ResponseWriter, r *http.Request) {
	h.respondTenant(w, r)(h.svc.Restore(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), h.meta(r)))
}

func (h *HTTP) retry(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.RetryProvisioning(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"id": res.TenantID, "status": res.Status, "url": res.TenantURL})
}

func (h *HTTP) respondTenant(w http.ResponseWriter, r *http.Request) func(TenantView, error) {
	return func(v TenantView, err error) {
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, v)
	}
}

func (h *HTTP) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	rows, err := h.svc.AuditLog(r.Context(), actorFrom(r.Context()), q.Get("tenant"), before, limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if rows == nil {
		rows = []AuditRow{}
	}
	httpx.WriteJSON(w, http.StatusOK, rows)
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && len(s) <= 254
}
