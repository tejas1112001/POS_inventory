// Package tenantapi mounts the tenant-host API. Every route is registered
// through rbac.Router with an explicit requirement; the startup self-check
// refuses any route without one.
package tenantapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/users"
)

// CookieName is the tenant refresh cookie (host-only, TENANCY §7.4).
const CookieName = "__Host-rt"

// Deps are the tenant API dependencies.
type Deps struct {
	Auth     *auth.Service
	Users    *users.Service
	States   *auth.States
	Signer   *authtoken.Signer
	Sessions *tenancy.Sessions
	Network  httpx.Network
	Metrics  *obs.Metrics
	MaxRows  int
	// LoginPerTenant is the login rate limit per tenant per minute
	// (0: 100). Only tests raise it.
	LoginPerTenant int
	// Modules register their routes on the same rbac router (steps 6+).
	Modules []interface{ Mount(*rbac.Router) }
}

// API holds the handlers.
type API struct {
	d             Deps
	loginByEmail  *httprate.RateLimiter
	forgotByEmail *httprate.RateLimiter
	router        *rbac.Router
}

// New creates the API.
func New(d Deps) *API {
	a := &API{d: d}
	a.loginByEmail = httprate.NewRateLimiter(5, time.Minute, httprate.WithLimitHandler(a.limited("login_ip_email", time.Minute)))
	a.forgotByEmail = httprate.NewRateLimiter(3, time.Minute, httprate.WithLimitHandler(a.limited("forgot_email", time.Minute)))
	return a
}

func (a *API) limited(name string, per time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.d.Metrics != nil {
			a.d.Metrics.RateLimited.WithLabelValues(name).Inc()
		}
		httpx.WriteError(w, r, apperr.RateLimited(apperr.CodeRateLimited, per))
	}
}

func (a *API) limit(name string, n int, per time.Duration, key func(*http.Request) string) func(http.Handler) http.Handler {
	return httprate.NewRateLimiter(n, per,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) { return name + ":" + key(r), nil }),
		httprate.WithLimitHandler(a.limited(name, per))).Handler
}

func (a *API) byIP(r *http.Request) string { return a.d.Network.ClientIP(r).String() }

func byTenant(r *http.Request) string {
	t, _ := tenancy.TenantFrom(r.Context())
	return t.ID
}

// Router returns the registered routes (tests, self-check).
func (a *API) Router() *rbac.Router { return a.router }

// Mount registers every tenant route under /api and runs the self-check.
func (a *API) Mount(api chi.Router) error {
	rt := rbac.NewRouter(api, "/api", a.d.States.Guard(a.d.Signer))
	a.router = rt
	ipLogin := a.limit("login_ip", 20, time.Minute, a.byIP)
	perTenant := a.d.LoginPerTenant
	if perTenant <= 0 {
		perTenant = 100
	}
	tenantLogin := a.limit("login_tenant", perTenant, time.Minute, byTenant)
	pubIP := a.limit("public_ip", 30, time.Minute, a.byIP)
	pubTenant := a.limit("public_tenant", 300, time.Minute, byTenant)
	tokenIP := a.limit("token_ip", 10, time.Minute, a.byIP)

	with := func(h http.HandlerFunc, mws ...func(http.Handler) http.Handler) http.HandlerFunc {
		var out http.Handler = h
		for i := len(mws) - 1; i >= 0; i-- {
			out = mws[i](out)
		}
		return out.ServeHTTP
	}

	// Auth (C-01, C-02, C-05, §5.1)
	rt.Handle(http.MethodPost, "/auth/login", rbac.Public().ReadOnlyOK(), with(a.login, ipLogin, tenantLogin))
	rt.Handle(http.MethodPost, "/auth/refresh", rbac.Public().ReadOnlyOK(), with(a.refresh, a.limit("refresh_ip", 60, time.Minute, a.byIP), a.csrf))
	rt.Handle(http.MethodPost, "/auth/logout", rbac.Public().ReadOnlyOK(), with(a.logout, a.csrf))
	rt.Handle(http.MethodGet, "/auth/me", rbac.Authenticated().ReadOnlyOK(), a.me)
	rt.Handle(http.MethodGet, "/auth/permissions", rbac.Authenticated(), a.permissions)
	// Credential flows pass the read-only gate: a read-only tenant's owner
	// must be able to sign in to pay (C-21).
	rt.Handle(http.MethodPost, "/auth/password/change", rbac.Authenticated().ReadOnlyOK(), a.passwordChange)
	rt.Handle(http.MethodPost, "/auth/password/forgot", rbac.Public().ReadOnlyOK(), with(a.forgot, tokenIP))
	rt.Handle(http.MethodPost, "/auth/password/reset", rbac.Public().ReadOnlyOK(), with(a.useToken("reset"), tokenIP))
	rt.Handle(http.MethodPost, "/auth/invite/accept", rbac.Public().ReadOnlyOK(), with(a.useToken("invite"), tokenIP))

	// Sign-in page branding (§5.1)
	rt.Handle(http.MethodGet, "/public/tenant-info", rbac.Public(), with(a.tenantInfo, pubIP, pubTenant))

	// Users (+ legacy /api/billers alias) and roles (C-07)
	for _, base := range []string{"/users", "/billers"} {
		rt.Handle(http.MethodGet, base, rbac.Require("users", "view"), a.listUsers)
		rt.Handle(http.MethodPut, base+"/{id}", rbac.Require("users", "edit"), a.updateUser)
		rt.Handle(http.MethodDelete, base+"/{id}", rbac.Require("users", "delete"), a.deleteUser)
	}
	rt.Handle(http.MethodPost, "/users", rbac.Require("users", "create"), a.createUser)
	rt.Handle(http.MethodGet, "/roles", rbac.Require("roles", "view"), a.listRoles)
	rt.Handle(http.MethodGet, "/roles/permissions", rbac.Require("roles", "view"), a.listPermissions)
	rt.Handle(http.MethodPost, "/roles", rbac.Require("roles", "create"), a.createRole)
	rt.Handle(http.MethodPost, "/roles/assign-user", rbac.Require("users", "edit"), a.assignRole)
	rt.Handle(http.MethodPut, "/roles/{id}", rbac.Require("roles", "edit"), a.updateRole)
	rt.Handle(http.MethodDelete, "/roles/{id}", rbac.Require("roles", "delete"), a.deleteRole)

	// Tenant audit log (§5.1)
	rt.Handle(http.MethodGet, "/audit-log", rbac.Require("audit", "view"), a.auditLog)

	for _, m := range a.d.Modules {
		m.Mount(rt)
	}
	return rt.SelfCheck()
}

// ------------------------------------------------------------ helpers

func (a *API) meta(r *http.Request) auth.Meta {
	return auth.Meta{Host: httpx.HostFrom(r.Context()).Name, IP: a.d.Network.ClientIP(r), UserAgent: r.UserAgent(), RequestID: httpx.RequestID(r.Context())}
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

// csrf: the refresh/logout cookie endpoints need Origin == this host and
// the custom header (TENANCY §7.4).
func (a *API) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := a.d.Network.Scheme(r) + "://" + httpx.HostFrom(r.Context()).Name
		origin := strings.TrimSuffix(strings.TrimSuffix(r.Header.Get("Origin"), ":443"), ":80")
		if r.Header.Get("X-Requested-With") != "fetch" || origin != want {
			httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "Cross-site request refused"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setCookie(w http.ResponseWriter, b auth.SessionBody) {
	c := &http.Cookie{Name: CookieName, Value: b.Refresh(), Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}
	if ttl := b.CookieMaxAge(); ttl > 0 {
		c.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, c)
}

func clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func cookieValue(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// ------------------------------------------------------------ auth

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Remember *bool  `json:"remember"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key := byTenant(r) + ":" + a.byIP(r) + ":" + strings.ToLower(strings.TrimSpace(in.Email))
	if a.loginByEmail.RespondOnLimit(w, r, key) {
		return
	}
	remember := in.Remember == nil || *in.Remember
	body, err := a.d.Auth.Login(r.Context(), tenant(r), in.Email, in.Password, remember, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	setCookie(w, body)
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	body, err := a.d.Auth.Refresh(r.Context(), tenant(r), cookieValue(r), a.meta(r))
	if err != nil {
		clearCookie(w)
		httpx.WriteError(w, r, err)
		return
	}
	setCookie(w, body)
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.d.Auth.Logout(r.Context(), tenant(r), cookieValue(r), a.meta(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	body, err := a.d.Auth.Me(r.Context(), tenant(r), principal(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (a *API) permissions(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	list := make([]string, 0, len(p.Perms))
	for k := range p.Perms {
		list = append(list, k)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]string{"permissions": sorted(list)})
}

func (a *API) passwordChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	body, err := a.d.Auth.ChangePassword(r.Context(), tenant(r), principal(r), in.CurrentPassword, in.NewPassword, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	setCookie(w, body)
	httpx.WriteJSON(w, http.StatusOK, body)
}

func (a *API) forgot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key := byTenant(r) + ":" + strings.ToLower(strings.TrimSpace(in.Email))
	if !a.forgotByEmail.OnLimit(w, r, key) {
		if err := a.d.Auth.Forgot(r.Context(), tenant(r), in.Email, a.meta(r)); err != nil {
			httpx.LoggerFrom(r.Context()).ErrorContext(r.Context(), "password forgot failed", "err", err)
		}
	}
	// Always the same answer: nothing reveals whether the email exists.
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"message": "If the account exists, a reset link has been sent."})
}

func (a *API) useToken(purpose string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := httpx.DecodeJSON(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if err := a.d.Auth.UseToken(r.Context(), tenant(r), purpose, in.Token, in.Password, a.meta(r)); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// tenantInfo returns only the business name and logo (§5.1). Unknown hosts
// never reach here: tenant resolution already answered with the generic 404.
func (a *API) tenantInfo(w http.ResponseWriter, r *http.Request) {
	t := tenant(r)
	var name string
	err := a.d.Sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), `SELECT business_name FROM settings WHERE id = 1`).Scan(&name)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		name, err = t.Name, nil
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	body := map[string]any{"name": name, "logoUrl": nil} // logos arrive with files (step 6)
	sum := sha256.Sum256([]byte(name))
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

// ------------------------------------------------------------ users / roles

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, total, truncated, err := a.d.Users.List(r.Context(), tenant(r), q.Get("q"), q.Get("status"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	if truncated {
		w.Header().Set("X-Truncated", "true")
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var in users.CreateInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, err := a.d.Users.Create(r.Context(), tenant(r), principal(r), in, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in users.UpdateInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, err := a.d.Users.Update(r.Context(), tenant(r), principal(r), id, in, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u.RoleRef = nil
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := a.d.Users.Delete(r.Context(), tenant(r), principal(r), id, a.meta(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listRoles(w http.ResponseWriter, r *http.Request) {
	list, err := a.d.Users.Roles(r.Context(), tenant(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

func (a *API) listPermissions(w http.ResponseWriter, r *http.Request) {
	list, err := a.d.Users.Permissions(r.Context(), tenant(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

func (a *API) createRole(w http.ResponseWriter, r *http.Request) {
	var in users.RoleInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	role, err := a.d.Users.CreateRole(r.Context(), tenant(r), principal(r), in, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, role)
}

func (a *API) updateRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in users.RoleInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	role, err := a.d.Users.UpdateRole(r.Context(), tenant(r), principal(r), id, in, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, role)
}

func (a *API) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := a.d.Users.DeleteRole(r.Context(), tenant(r), principal(r), id, a.meta(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) assignRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64 `json:"userId"`
		RoleID int64 `json:"roleId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, err := a.d.Users.AssignRole(r.Context(), tenant(r), principal(r), in.UserID, in.RoleID, a.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Role assigned successfully",
		"user":    map[string]any{"id": u.ID, "name": u.Name, "role_id": u.RoleID, "role": u.Role},
	})
}

// ------------------------------------------------------------ audit

type auditRow struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	ActorType string         `json:"actorType"`
	ActorID   *int64         `json:"actorId,omitempty"`
	Action    string         `json:"action"`
	Entity    string         `json:"entity"`
	EntityID  string         `json:"entityId,omitempty"`
	Before    map[string]any `json:"before,omitempty"`
	After     map[string]any `json:"after,omitempty"`
	IP        string         `json:"ip,omitempty"`
}

func (a *API) auditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	entity := q.Get("entity")
	var (
		rows  []auditRow
		total int
	)
	err := a.d.Sessions.InTenantTx(r.Context(), tenant(r), tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE ($1 = '' OR entity = $1)`, entity).Scan(&total); err != nil {
			return err
		}
		res, err := tx.Query(ctx, `SELECT id, at, actor_type, actor_id, action, entity, coalesce(entity_id, ''), before, after, coalesce(host(ip), '')
			FROM audit_log WHERE ($1 = '' OR entity = $1) ORDER BY id DESC LIMIT $2 OFFSET $3`, entity, limit, (page-1)*limit)
		if err != nil {
			return err
		}
		defer res.Close()
		for res.Next() {
			var x auditRow
			if err := res.Scan(&x.ID, &x.At, &x.ActorType, &x.ActorID, &x.Action, &x.Entity, &x.EntityID, &x.Before, &x.After, &x.IP); err != nil {
				return err
			}
			rows = append(rows, x)
		}
		return res.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if rows == nil {
		rows = []auditRow{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"data":       rows,
		"pagination": map[string]int{"total": total, "page": page, "limit": limit, "totalPages": (total + limit - 1) / limit},
	})
}
