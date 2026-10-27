// Package auth is tenant authentication: login, refresh rotation, logout,
// password and invite flows, the per-request auth state, the tenant status
// gate and the RBAC guard (PHASE1_DESIGN §4.1, TENANCY §5).
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Cache TTLs (TENANCY §5 steps 4 and 6, §5.5).
const (
	AuthStateTTL   = 30 * time.Second
	PermissionsTTL = 60 * time.Second
	DegradedTTL    = 5 * time.Second
)

// Principal is the signed-in tenant user of a request. Permissions come
// from the user's CURRENT role, never from a token claim.
type Principal struct {
	TenantID     string
	UserID       int64
	RoleID       int64
	TokenVersion int
	FamilyID     string
	Perms        map[string]bool
}

// Has reports whether the principal holds module:action.
func (p Principal) Has(perm string) bool { return p.Perms[perm] }

type principalKey struct{}

// PrincipalFrom returns the request principal.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

type authState struct {
	status       string
	roleID       int64
	tokenVersion int
	deleted      bool
}

// States caches per-user auth state and per-role permissions.
type States struct {
	sessions  *tenancy.Sessions
	authCache *tenancy.TTLCache[string, authState]
	permCache *tenancy.TTLCache[string, map[string]bool]
	healthy   func() bool
	log       *slog.Logger
}

// NewStates creates the caches. healthy reports whether the LISTEN
// connection is up (TTLs drop to 5 s while it is down).
func NewStates(s *tenancy.Sessions, healthy func() bool, log *slog.Logger) *States {
	if healthy == nil {
		healthy = func() bool { return true }
	}
	if log == nil {
		log = slog.Default()
	}
	return &States{sessions: s, authCache: tenancy.NewTTLCache[string, authState](),
		permCache: tenancy.NewTTLCache[string, map[string]bool](), healthy: healthy, log: log}
}

func (st *States) ttl(normal time.Duration) time.Duration {
	if st.healthy() {
		return normal
	}
	return DegradedTTL
}

// InvalidateUser handles auth_changed "<tenant>:<user>" payloads.
func (st *States) InvalidateUser(payload string) { st.authCache.Delete(payload) }

// InvalidateRole handles role_changed "<tenant>:<role>" payloads.
func (st *States) InvalidateRole(payload string) { st.permCache.Delete(payload) }

// Flush drops both caches (listener reconnect).
func (st *States) Flush() {
	st.authCache.Flush()
	st.permCache.Flush()
}

func key(tid string, id int64) string { return tid + ":" + strconv.FormatInt(id, 10) }

const (
	qAuthState = `SELECT status, role_id, token_version, deleted_at IS NOT NULL FROM users WHERE id = $1`
	qRolePerms = `SELECT p.module || ':' || p.action FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		JOIN roles r ON r.id = rp.role_id
		WHERE rp.role_id = $1 AND r.deleted_at IS NULL`
)

func (st *States) load(ctx context.Context, t tenancy.Tenant, uid int64) (authState, map[string]bool, error) {
	as, haveState := st.authCache.Get(key(t.ID, uid))
	var perms map[string]bool
	havePerms := false
	if haveState {
		perms, havePerms = st.permCache.Get(key(t.ID, as.roleID))
	}
	if haveState && havePerms {
		return as, perms, nil
	}
	err := st.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		if !haveState {
			err := tx.QueryRow(ctx, qAuthState, uid).Scan(&as.status, &as.roleID, &as.tokenVersion, &as.deleted)
			if errors.Is(err, pgx.ErrNoRows) {
				as = authState{deleted: true}
				return nil
			}
			if err != nil {
				return err
			}
		}
		var err error
		perms, err = rolePerms(ctx, tx, as.roleID)
		return err
	})
	if err != nil {
		return authState{}, nil, err
	}
	st.authCache.Set(key(t.ID, uid), as, st.ttl(AuthStateTTL))
	if as.roleID != 0 {
		st.permCache.Set(key(t.ID, as.roleID), perms, st.ttl(PermissionsTTL))
	}
	return as, perms, nil
}

func rolePerms(ctx context.Context, tx tenancy.TenantTx, roleID int64) (map[string]bool, error) {
	perms := map[string]bool{}
	if roleID == 0 {
		return perms, nil
	}
	rows, err := tx.Query(ctx, qRolePerms, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms[p] = true
	}
	return perms, rows.Err()
}

// Gate rules (TENANCY §5 step 5, BILLING_DESIGN §3).
var (
	blockedStatuses  = map[string]bool{"suspended": true, "pending_deletion": true}
	readOnlyStatuses = map[string]bool{"read_only": true}
)

func errSuspended() error {
	return apperr.Forbidden(apperr.CodeTenantSuspended, "This account is suspended. Contact the account owner.")
}

// CheckGate applies the tenant status rule to a request.
func CheckGate(t tenancy.Tenant, method string, req rbac.Requirement) error {
	if blockedStatuses[t.Status] {
		return errSuspended()
	}
	if readOnlyStatuses[t.Status] && !req.AllowReadOnly && method != http.MethodGet && method != http.MethodHead {
		return apperr.Forbidden(apperr.CodeTenantReadOnly, "This account is read-only until the subscription is renewed.")
	}
	return nil
}

// Guard returns the rbac.Guard for tenant routes: authentication,
// auth state, tenant gate and permission check.
func (st *States) Guard(signer *authtoken.Signer) rbac.Guard {
	return func(req rbac.Requirement) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t, ok := tenancy.TenantFrom(r.Context())
				if !ok {
					httpx.WriteError(w, r, apperr.NotFound())
					return
				}
				if req.Kind == rbac.KindPublic {
					// Public routes still respect read_only for writes. Suspended
					// tenants are handled by each endpoint (login refuses,
					// tenant-info still answers for the sign-in page).
					if readOnlyStatuses[t.Status] && !req.AllowReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
						httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeTenantReadOnly, "This account is read-only until the subscription is renewed."))
						return
					}
					next.ServeHTTP(w, r)
					return
				}
				p, err := st.authenticate(r, t, signer)
				if err != nil {
					httpx.WriteError(w, r, err)
					return
				}
				if err := CheckGate(t, r.Method, req); err != nil {
					httpx.WriteError(w, r, err)
					return
				}
				if req.Kind == rbac.KindPermission && !p.Has(req.Perm) {
					httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "You do not have permission for this action"))
					return
				}
				httpx.Info(r.Context()).Set(func(ri *httpx.RequestInfo) { ri.UserID = strconv.FormatInt(p.UserID, 10) })
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
			})
		}
	}
}

func (st *States) authenticate(r *http.Request, t tenancy.Tenant, signer *authtoken.Signer) (Principal, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return Principal{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	host := httpx.HostFrom(r.Context()).Name
	c, err := signer.Verify(tok, host)
	if errors.Is(err, authtoken.ErrExpired) {
		return Principal{}, apperr.Unauthenticated(apperr.CodeTokenExpired, "Access token expired")
	}
	if err != nil {
		return Principal{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	if c.TenantID != t.ID {
		// A token for another tenant. Never a valid situation.
		st.log.WarnContext(r.Context(), "tenant mismatch", slog.String("event", "security.tenant_mismatch"),
			slog.String("tenant_id", t.ID))
		return Principal{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	uid, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || uid <= 0 {
		return Principal{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	as, perms, err := st.load(r.Context(), t, uid)
	if err != nil {
		return Principal{}, err
	}
	if as.deleted || as.status != "Active" || c.TokenVersion < as.tokenVersion {
		return Principal{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
	}
	return Principal{TenantID: t.ID, UserID: uid, RoleID: as.roleID, TokenVersion: as.tokenVersion,
		FamilyID: c.SessionID, Perms: perms}, nil
}
