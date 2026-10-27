package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Policy (PHASE1_DESIGN §4.1).
const (
	MinPasswordLen    = 12
	LockoutThreshold  = 5
	MaxLockout        = time.Hour
	ResetTokenTTL     = time.Hour
	InviteTokenTTL    = 7 * 24 * time.Hour
	rememberAbsoluteC = 90 * 24 * time.Hour // cap on a sliding remember-me session
)

// Config holds the service settings.
type Config struct {
	Sessions        *tenancy.Sessions
	States          *States
	Signer          *authtoken.Signer
	Hasher          *security.Hasher
	Envelope        *secrets.Envelope
	RefreshRemember time.Duration // 30 d sliding
	RefreshSession  time.Duration // 12 h absolute
	PublicScheme    string
	PublicPort      string // dev proxy port in invite links (empty in production)
	BaseDomain      string
	Log             *slog.Logger
	Now             func() time.Time
}

// Service implements tenant authentication.
type Service struct {
	cfg Config
	now func() time.Time
}

// NewService creates the service.
func NewService(c Config) *Service {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	return &Service{cfg: c, now: c.Now}
}

// Meta is request metadata.
type Meta struct {
	Host      string // exact tenant host, used as JWT iss
	IP        netip.Addr
	UserAgent string
	RequestID string
}

// UserJSON keeps the field names the existing frontend reads.
type UserJSON struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"` // legacy: "admin" or "biller"
	Status    string    `json:"status"`
	RoleID    int64     `json:"role_id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	RoleRef   *RoleRef  `json:"Role,omitempty"`
}

// RoleRef is the {id,name} the users list includes.
type RoleRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// LegacyRole maps a system key to the old "role" string.
func LegacyRole(systemKey string) string {
	if systemKey == "owner" || systemKey == "admin" {
		return "admin"
	}
	return "biller"
}

// SessionBody is the login/refresh response (C-02: same shape as before;
// token is now a 10-minute access token).
type SessionBody struct {
	User        UserJSON `json:"user"`
	Token       string   `json:"token"`
	Remember    bool     `json:"remember"`
	Permissions []string `json:"permissions"`

	refresh    string
	refreshTTL time.Duration // cookie Max-Age; 0 = browser-session cookie
}

// Refresh returns the raw refresh token for the cookie.
func (b SessionBody) Refresh() string { return b.refresh }

// CookieMaxAge is the cookie lifetime (0 = session cookie).
func (b SessionBody) CookieMaxAge() time.Duration { return b.refreshTTL }

type userRow struct {
	UserJSON
	PasswordHash string
	FailedLogins int
	LockedUntil  *time.Time
	TokenVersion int
	SystemKey    string
	Deleted      bool
}

const (
	qUserCols = `u.id, u.name, u.email::text, u.status, u.role_id, u.created_at, u.updated_at,
		coalesce(u.password_hash, ''), u.failed_logins, u.locked_until, u.token_version,
		coalesce(r.system_key, ''), r.name::text, u.deleted_at IS NOT NULL`
	qUserByEmail = `SELECT ` + qUserCols + ` FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.email = $1 AND u.deleted_at IS NULL`
	qUserByID   = `SELECT ` + qUserCols + ` FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1`
	qUserByIDFU = `SELECT ` + qUserCols + ` FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1 FOR UPDATE OF u`
)

func scanUser(row pgx.Row) (userRow, error) {
	var u userRow
	var roleName string
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.RoleID, &u.CreatedAt, &u.UpdatedAt,
		&u.PasswordHash, &u.FailedLogins, &u.LockedUntil, &u.TokenVersion, &u.SystemKey, &roleName, &u.Deleted)
	u.Role = LegacyRole(u.SystemKey)
	u.RoleRef = &RoleRef{ID: u.RoleID, Name: roleName}
	return u, err
}

func errBadLogin() error {
	return apperr.Unauthenticated(apperr.CodeInvalidCredentials, "Invalid email or password")
}

func (s *Service) tx(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return s.cfg.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (m Meta) entry(actor int64, action, entity, id string, after map[string]any) audit.Entry {
	return audit.Entry{ActorID: actor, Action: action, Entity: entity, EntityID: id, After: after,
		IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID}
}

// --------------------------------------------------------------- login

// Login verifies credentials and starts a session (C-01, C-02).
func (s *Service) Login(ctx context.Context, t tenancy.Tenant, email, password string, remember bool, m Meta) (SessionBody, error) {
	if blockedStatuses[t.Status] {
		return SessionBody{}, errSuspended()
	}
	email = strings.TrimSpace(email)
	if email == "" || password == "" || len(email) > 254 || len(password) > 256 {
		return SessionBody{}, errBadLogin()
	}
	var u userRow
	err := s.cfg.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, qUserByEmail, email))
		return err
	})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SessionBody{}, err
	}
	now := s.now()
	if found && u.LockedUntil != nil && now.Before(*u.LockedUntil) {
		_ = s.cfg.Hasher.Verify(ctx, password, "") // same work as any attempt
		_ = s.tx(context.WithoutCancel(ctx), t, func(tx tenancy.TenantTx) error {
			return audit.Write(ctx, tx, m.entry(u.ID, "auth.login_locked", "user", strconv.FormatInt(u.ID, 10), nil))
		})
		return SessionBody{}, apperr.RateLimited(apperr.CodeAccountLocked, u.LockedUntil.Sub(now))
	}
	verr := s.cfg.Hasher.Verify(ctx, password, u.PasswordHash)
	if verr != nil && ctx.Err() != nil {
		return SessionBody{}, ctx.Err()
	}
	if !found || verr != nil || u.Status != "Active" {
		_ = s.tx(context.WithoutCancel(ctx), t, func(tx tenancy.TenantTx) error {
			if found && verr != nil {
				// Progressive lockout: from the 5th failure, 2^(n-5) minutes, max 1 h.
				if _, err := tx.Exec(ctx, `UPDATE users SET failed_logins = failed_logins + 1,
						locked_until = CASE WHEN failed_logins + 1 >= $2
						  THEN $3::timestamptz + least(make_interval(mins => power(2, failed_logins + 1 - $2)::int), interval '1 hour')
						  ELSE locked_until END
					WHERE id = $1`, u.ID, LockoutThreshold, now); err != nil {
					return err
				}
			}
			e := m.entry(u.ID, "auth.login_failed", "user", "", map[string]any{"email": email})
			return audit.Write(ctx, tx, e)
		})
		s.cfg.Log.WarnContext(ctx, "tenant login failed", slog.String("event", "security.login_failed"), slog.String("tenant_id", t.ID))
		return SessionBody{}, errBadLogin()
	}

	var body SessionBody
	err = s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = $2 WHERE id = $1`, u.ID, now); err != nil {
			return err
		}
		if s.cfg.Hasher.NeedsRehash(u.PasswordHash) {
			if h, err := s.cfg.Hasher.Hash(ctx, password); err == nil {
				if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, u.ID, h); err != nil {
					return err
				}
			}
		}
		var err error
		body, err = s.startSession(ctx, tx, t, u, remember, uuid.Nil, m)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, m.entry(u.ID, "auth.login", "user", strconv.FormatInt(u.ID, 10), map[string]any{"remember": remember}))
	})
	return body, err
}

// startSession issues a refresh token (new family, or continuing family on
// rotation) and an access token.
func (s *Service) startSession(ctx context.Context, tx tenancy.TenantTx, t tenancy.Tenant, u userRow, remember bool, family uuid.UUID, m Meta) (SessionBody, error) {
	now := s.now()
	if family == uuid.Nil {
		family = uuid.New()
	}
	ttl := s.cfg.RefreshSession
	abs := now.Add(s.cfg.RefreshSession)
	cookieTTL := time.Duration(0)
	if remember {
		ttl = s.cfg.RefreshRemember
		abs = now.Add(rememberAbsoluteC)
		cookieTTL = s.cfg.RefreshRemember
	}
	raw, err := s.insertRefresh(ctx, tx, u.ID, family, remember, now, now.Add(ttl), abs, m)
	if err != nil {
		return SessionBody{}, err
	}
	return s.body(ctx, tx, t, u, remember, family, raw, cookieTTL, m)
}

func (s *Service) insertRefresh(ctx context.Context, tx tenancy.TenantTx, uid int64, family uuid.UUID, remember bool, now, exp, abs time.Time, m Meta) (string, error) {
	raw := security.NewToken(32)
	var ip *string
	if m.IP.IsValid() {
		v := m.IP.String()
		ip = &v
	}
	ua := m.UserAgent
	if len(ua) > 400 {
		ua = ua[:400]
	}
	_, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, remember, issued_at, expires_at, absolute_expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		uuid.New(), uid, family, security.HashToken(raw), remember, now, exp, abs, ip, ua)
	return raw, err
}

func (s *Service) body(ctx context.Context, tx tenancy.TenantTx, t tenancy.Tenant, u userRow, remember bool, family uuid.UUID, raw string, cookieTTL time.Duration, m Meta) (SessionBody, error) {
	tok, _, err := s.cfg.Signer.Sign(m.Host, strconv.FormatInt(u.ID, 10), authtoken.Claims{
		TenantID: t.ID, TokenVersion: u.TokenVersion, SessionID: family.String(),
	})
	if err != nil {
		return SessionBody{}, err
	}
	perms, err := rolePerms(ctx, tx, u.RoleID)
	if err != nil {
		return SessionBody{}, err
	}
	list := make([]string, 0, len(perms))
	for p := range perms {
		list = append(list, p)
	}
	uj := u.UserJSON
	uj.RoleRef = nil
	return SessionBody{User: uj, Token: tok, Remember: remember, Permissions: sortStrings(list), refresh: raw, refreshTTL: cookieTTL}, nil
}

// ------------------------------------------------------------- refresh

// Refresh rotates the refresh cookie. Reuse of a rotated token revokes the
// whole family and is audited (C-02).
func (s *Service) Refresh(ctx context.Context, t tenancy.Tenant, raw string, m Meta) (SessionBody, error) {
	if blockedStatuses[t.Status] {
		return SessionBody{}, errSuspended()
	}
	if raw == "" {
		return SessionBody{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	var (
		body   SessionBody
		reused bool
	)
	err := s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		var (
			id               uuid.UUID
			uid              int64
			family           uuid.UUID
			remember         bool
			exp, abs         time.Time
			rotated, revoked bool
		)
		err := tx.QueryRow(ctx, `SELECT id, user_id, family_id, remember, expires_at, absolute_expires_at,
				rotated_at IS NOT NULL, revoked_at IS NOT NULL
			FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, security.HashToken(raw)).
			Scan(&id, &uid, &family, &remember, &exp, &abs, &rotated, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
		}
		if err != nil {
			return err
		}
		if rotated || revoked {
			tag, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now(), revoke_reason = 'reuse'
				WHERE family_id = $1 AND revoked_at IS NULL`, family)
			if err != nil {
				return err
			}
			reused = tag.RowsAffected() > 0
			if reused {
				if err := audit.Write(ctx, tx, m.entry(uid, "auth.refresh_reuse", "user", strconv.FormatInt(uid, 10), nil)); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `SELECT pg_notify('auth_changed', $1)`, key(t.ID, uid)); err != nil {
					return err
				}
			}
			return nil // commit the revocation; the caller then fails
		}
		now := s.now()
		if !now.Before(exp) || !now.Before(abs) {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
		}
		u, err := scanUser(tx.QueryRow(ctx, qUserByID, uid))
		if err != nil {
			return err
		}
		if u.Deleted || u.Status != "Active" {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET rotated_at = $2 WHERE id = $1`, id, now); err != nil {
			return err
		}
		newExp, cookieTTL := abs, time.Duration(0)
		if remember {
			newExp = minTime(now.Add(s.cfg.RefreshRemember), abs) // sliding, capped
			cookieTTL = s.cfg.RefreshRemember
		}
		newRaw, err := s.insertRefresh(ctx, tx, uid, family, remember, now, newExp, abs, m)
		if err != nil {
			return err
		}
		body, err = s.body(ctx, tx, t, u, remember, family, newRaw, cookieTTL, m)
		return err
	})
	if err != nil {
		return SessionBody{}, err
	}
	if body.Token == "" {
		if reused {
			s.cfg.Log.WarnContext(ctx, "refresh token reuse", slog.String("event", "security.refresh_reuse"), slog.String("tenant_id", t.ID))
		}
		return SessionBody{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
	}
	return body, nil
}

// Logout revokes the refresh family behind the cookie.
func (s *Service) Logout(ctx context.Context, t tenancy.Tenant, raw string, m Meta) error {
	if raw == "" {
		return nil
	}
	return s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		var family uuid.UUID
		var uid int64
		err := tx.QueryRow(ctx, `SELECT family_id, user_id FROM refresh_tokens WHERE token_hash = $1`, security.HashToken(raw)).Scan(&family, &uid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now(), revoke_reason = 'logout'
			WHERE family_id = $1 AND revoked_at IS NULL`, family); err != nil {
			return err
		}
		return audit.Write(ctx, tx, m.entry(uid, "auth.logout", "user", strconv.FormatInt(uid, 10), nil))
	})
}

// --------------------------------------------------------- me / perms

// MeBody is GET /api/auth/me.
type MeBody struct {
	User          UserJSON       `json:"user"`
	Permissions   []string       `json:"permissions"`
	Tenant        map[string]any `json:"tenant"`
	Impersonation bool           `json:"impersonation"`
	Banners       int            `json:"banners"`
}

// Me returns the signed-in user, permissions and tenant formatting data.
func (s *Service) Me(ctx context.Context, t tenancy.Tenant, p Principal) (MeBody, error) {
	var out MeBody
	err := s.cfg.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		u, err := scanUser(tx.QueryRow(ctx, qUserByID, p.UserID))
		if err != nil {
			return err
		}
		uj := u.UserJSON
		uj.RoleRef = nil
		out.User = uj
		var name, currency, locale, tz string
		if err := tx.QueryRow(ctx, `SELECT business_name, currency, locale, timezone FROM settings WHERE id = 1`).
			Scan(&name, &currency, &locale, &tz); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out.Tenant = map[string]any{"name": name, "currency": currency, "locale": locale, "timezone": tz, "status": t.Status}
		return nil
	})
	list := make([]string, 0, len(p.Perms))
	for k := range p.Perms {
		list = append(list, k)
	}
	out.Permissions = sortStrings(list)
	return out, err
}

// ----------------------------------------------------------- passwords

// ChangePassword changes the caller's password, revokes every session and
// bumps token_version, then starts a fresh session for the caller.
func (s *Service) ChangePassword(ctx context.Context, t tenancy.Tenant, p Principal, current, next string, m Meta) (SessionBody, error) {
	var u userRow
	if err := s.cfg.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, qUserByID, p.UserID))
		return err
	}); err != nil {
		return SessionBody{}, err
	}
	if s.cfg.Hasher.Verify(ctx, current, u.PasswordHash) != nil {
		return SessionBody{}, apperr.Validation(map[string]string{"currentPassword": "is incorrect"})
	}
	hash, err := s.checkAndHash(ctx, t, u, next)
	if err != nil {
		return SessionBody{}, err
	}
	var body SessionBody
	err = s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		if err := s.setPassword(ctx, tx, t, u.ID, hash, "password_changed"); err != nil {
			return err
		}
		fresh, err := scanUser(tx.QueryRow(ctx, qUserByID, u.ID))
		if err != nil {
			return err
		}
		if body, err = s.startSession(ctx, tx, t, fresh, false, uuid.Nil, m); err != nil {
			return err
		}
		return audit.Write(ctx, tx, m.entry(u.ID, "auth.password_changed", "user", strconv.FormatInt(u.ID, 10), nil))
	})
	return body, err
}

// setPassword stores a hash, revokes every refresh family and bumps
// token_version so outstanding access tokens die too.
func (s *Service) setPassword(ctx context.Context, tx tenancy.TenantTx, t tenancy.Tenant, uid int64, hash, reason string) error {
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, password_changed_at = $3, token_version = token_version + 1,
		failed_logins = 0, locked_until = NULL, updated_at = $3 WHERE id = $1`, uid, hash, s.now()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now(), revoke_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, uid, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT pg_notify('auth_changed', $1)`, key(t.ID, uid))
	return err
}

func (s *Service) checkAndHash(ctx context.Context, t tenancy.Tenant, u userRow, pw string) (string, error) {
	if err := security.CheckPassword(pw, MinPasswordLen, u.Email, u.Name, t.Name); err != nil {
		return "", apperr.Validation(map[string]string{"password": err.Error()})
	}
	if u.PasswordHash != "" && s.cfg.Hasher.Verify(ctx, pw, u.PasswordHash) == nil {
		return "", apperr.Validation(map[string]string{"password": "must differ from the current password"})
	}
	return s.cfg.Hasher.Hash(ctx, pw)
}

// Forgot queues a reset email when the account exists. The caller always
// answers 202, so nothing reveals whether the email is registered.
func (s *Service) Forgot(ctx context.Context, t tenancy.Tenant, email string, m Meta) error {
	if blockedStatuses[t.Status] || readOnlyStatuses[t.Status] {
		return nil
	}
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 {
		return nil
	}
	return s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		u, err := scanUser(tx.QueryRow(ctx, qUserByEmail, email))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if u.Status != "Active" {
			return nil
		}
		return s.issueToken(ctx, tx, t, u, "reset", ResetTokenTTL, "password_reset", m)
	})
}

// issueToken creates a hashed auth token and queues the email. The raw
// token is envelope-encrypted inside the outbox row (bound to this tenant
// and token row), so it is never stored in clear (EMAIL_AND_RECEIPTS §4.1).
// It returns the raw token for callers that hand it over directly.
func (s *Service) issueToken(ctx context.Context, tx tenancy.TenantTx, t tenancy.Tenant, u userRow, purpose string, ttl time.Duration, template string, m Meta) error {
	_, err := s.IssueToken(ctx, tx, t, u.ID, u.Email, u.Name, purpose, ttl, template, true, m)
	return err
}

// IssueToken creates an invite or reset token. With queueEmail it also
// writes the email to the outbox. Earlier unused tokens of the same purpose
// are invalidated.
func (s *Service) IssueToken(ctx context.Context, tx tenancy.TenantTx, t tenancy.Tenant, uid int64, email, name, purpose string, ttl time.Duration, template string, queueEmail bool, m Meta) (string, error) {
	raw := security.NewToken(32)
	now := s.now()
	if _, err := tx.Exec(ctx, `UPDATE auth_tokens SET used_at = $3 WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`, uid, purpose, now); err != nil {
		return "", err
	}
	var tokID int64
	if err := tx.QueryRow(ctx, `INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at, created_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		uid, purpose, security.HashToken(raw), now.Add(ttl), now).Scan(&tokID); err != nil {
		return "", err
	}
	if queueEmail {
		sealed, err := s.cfg.Envelope.Seal([]byte(raw), fmt.Sprintf("tenant:%s|auth_token:%d", t.ID, tokID))
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(map[string]any{
			"user_id": uid, "auth_token_id": tokID, "purpose": purpose,
			"link_path": map[string]string{"reset": "/reset-password", "invite": "/accept-invite"}[purpose],
			"token_sealed": map[string]string{
				"ct":  base64.StdEncoding.EncodeToString(sealed.Ciphertext),
				"dek": base64.StdEncoding.EncodeToString(sealed.WrappedDEK),
				"kid": sealed.KeyID,
			},
		})
		if _, err := tx.Exec(ctx, `INSERT INTO email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data, sender_policy)
			VALUES ($1, 'platform_notice', $2, $3, $4, $5, 'platform') ON CONFLICT (idempotency_key) DO NOTHING`,
			fmt.Sprintf("auth_token:%d", tokID), email, name, template, string(data)); err != nil {
			return "", err
		}
	}
	return raw, audit.Write(ctx, tx, m.entry(0, "auth."+purpose+"_issued", "user", strconv.FormatInt(uid, 10), nil))
}

// UseToken completes a reset or an invite: sets the password, burns the
// token and revokes every session.
func (s *Service) UseToken(ctx context.Context, t tenancy.Tenant, purpose, raw, password string, m Meta) error {
	if blockedStatuses[t.Status] {
		return errSuspended()
	}
	invalid := apperr.Validation(map[string]string{"token": "is invalid or expired"}) //nolint:gosec // G101: error text, not a credential
	if raw == "" || len(raw) > 200 {
		return invalid
	}
	var (
		tokID int64
		u     userRow
	)
	err := s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		var uid int64
		err := tx.QueryRow(ctx, `SELECT id, user_id FROM auth_tokens
			WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3 FOR UPDATE`,
			security.HashToken(raw), purpose, s.now()).Scan(&tokID, &uid)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid
		}
		if err != nil {
			return err
		}
		u, err = scanUser(tx.QueryRow(ctx, qUserByID, uid))
		if err != nil {
			return err
		}
		if u.Deleted || u.Status != "Active" {
			return invalid
		}
		return nil
	})
	if err != nil {
		return err
	}
	hash, err := s.checkAndHash(ctx, t, u, password)
	if err != nil {
		return err
	}
	return s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		tag, err := tx.Exec(ctx, `UPDATE auth_tokens SET used_at = $2 WHERE id = $1 AND used_at IS NULL`, tokID, s.now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return invalid // used concurrently
		}
		if err := s.setPassword(ctx, tx, t, u.ID, hash, "password_"+purpose); err != nil {
			return err
		}
		return audit.Write(ctx, tx, m.entry(u.ID, "auth."+purpose+"_completed", "user", strconv.FormatInt(u.ID, 10), nil))
	})
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// OwnerInviteLink issues an invite for the tenant's owner and returns the
// link (token in the URL fragment, so it never reaches server logs). Only
// for an owner who has not set a password yet.
func (s *Service) OwnerInviteLink(ctx context.Context, t tenancy.Tenant, m Meta) (string, time.Time, error) {
	var raw string
	err := s.tx(ctx, t, func(tx tenancy.TenantTx) error {
		var (
			uid         int64
			email, name string
			hasPassword bool
		)
		err := tx.QueryRow(ctx, `SELECT u.id, u.email::text, u.name, u.password_hash IS NOT NULL FROM users u
			JOIN roles r ON r.id = u.role_id
			WHERE r.system_key = 'owner' AND u.deleted_at IS NULL AND u.status = 'Active'
			ORDER BY u.id LIMIT 1`).Scan(&uid, &email, &name, &hasPassword)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict(apperr.CodeInvalidState, "Tenant has no active owner")
		}
		if err != nil {
			return err
		}
		if hasPassword {
			return apperr.Conflict(apperr.CodeInvalidState, "The owner has already set a password; use password reset")
		}
		raw, err = s.IssueToken(ctx, tx, t, uid, email, name, "invite", InviteTokenTTL, "owner_invite", false, m)
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	url := httpx.TenantURL(s.cfg.PublicScheme, t.Slug, s.cfg.BaseDomain, s.cfg.PublicPort) + "/accept-invite#token=" + raw
	return url, s.now().Add(InviteTokenTTL), nil
}
