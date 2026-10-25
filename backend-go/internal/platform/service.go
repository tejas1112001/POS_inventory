package platform

import (
	"context"
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
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Policy constants (PHASE1_DESIGN §7.2).
const (
	MinPasswordLen     = 14
	ChallengeTTL       = 5 * time.Minute
	MaxChallengeTries  = 5
	LockoutAfter       = 10
	StepUpTTL          = 5 * time.Minute
	RecoveryCodeCount  = 10
	lastSeenWriteEvery = time.Minute
)

// Config holds the service settings.
type Config struct {
	BaseDomain    string
	PlatformName  string
	SessionMax    time.Duration // absolute session lifetime (8h)
	IdleTimeout   time.Duration // idle timeout (30m)
	SecureCookies bool
	Provisioner   *provisioning.Orchestrator // nil when DATABASE_MIGRATOR_URL is not set
	Registry      *tenancy.Registry          // flushed after tenant changes
	Log           *slog.Logger
	Hasher        *security.Hasher
	Envelope      *secrets.Envelope
	Signer        *authtoken.Signer
	Sessions      *tenancy.Sessions
	Now           func() time.Time
	// IssueOwnerInvite creates a one-time invite link for a tenant's owner
	// (until the email worker of step 9 sends it). Nil disables the action.
	IssueOwnerInvite func(ctx context.Context, tenantID string, m Meta) (url string, expires time.Time, err error)
	// Billing is the SaaS billing service (step 16); its Provider is nil
	// when billing is off.
	Billing *billing.Service
	// ExportsEnabled is set when EXPORT_AGE_RECIPIENT and
	// EXPORT_MANIFEST_KEY are configured (step 18).
	ExportsEnabled bool
	// ExportQueued starts the job of a newly queued export (optional: the
	// worker's export sweep finds queued exports too).
	ExportQueued func(ctx context.Context, exportID uuid.UUID) error
}

// Service implements the platform API.
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

// Issuer is the exact admin host used as JWT iss.
func (s *Service) Issuer() string { return "admin." + s.cfg.BaseDomain }

// Meta is request metadata for audit and sessions.
type Meta struct {
	IP        netip.Addr
	UserAgent string
	RequestID string
}

// Actor is the authenticated platform user of a request.
type Actor struct {
	UserID      int64
	Email       string
	Name        string
	Role        string
	SessionID   uuid.UUID
	StepUpUntil time.Time
}

// user is a platform_users row.
type user struct {
	ID                int64
	Email, Name, Role string
	Status            string
	PasswordHash      string
	MustChange        bool
	MFAEnrolled       bool
	TOTP              secrets.Sealed
	TOTPLastStep      int64
	PendingTOTP       secrets.Sealed
	FailedLogins      int
	Locked            bool
}

const qUserCols = `id, email::text, name, role, status, password_hash, must_change_password, mfa_enrolled,
	coalesce(totp_ciphertext, ''), coalesce(totp_dek, ''), coalesce(totp_key_id, ''), totp_last_step,
	coalesce(pending_totp_ciphertext, ''), coalesce(pending_totp_dek, ''), coalesce(pending_totp_key_id, ''),
	failed_logins, locked_at IS NOT NULL`

const (
	qUserByEmail = `SELECT ` + qUserCols + ` FROM platform.platform_users WHERE email = $1`
	qUserByID    = `SELECT ` + qUserCols + ` FROM platform.platform_users WHERE id = $1`
	qUserByIDFU  = `SELECT ` + qUserCols + ` FROM platform.platform_users WHERE id = $1 FOR UPDATE`
)

func scanUser(row pgx.Row) (user, error) {
	var u user
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.PasswordHash, &u.MustChange, &u.MFAEnrolled,
		&u.TOTP.Ciphertext, &u.TOTP.WrappedDEK, &u.TOTP.KeyID, &u.TOTPLastStep,
		&u.PendingTOTP.Ciphertext, &u.PendingTOTP.WrappedDEK, &u.PendingTOTP.KeyID,
		&u.FailedLogins, &u.Locked)
	return u, err
}

func (s *Service) rw(ctx context.Context, fn func(tenancy.PlatformTx) error) error {
	return s.cfg.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (s *Service) ro(ctx context.Context, fn func(tenancy.PlatformTx) error) error {
	return s.cfg.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

func errInvalidCredentials() error {
	return apperr.Unauthenticated(apperr.CodeInvalidCredentials, "Invalid email or password")
}

func errChallenge() error {
	return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Sign-in step expired or invalid; start again")
}

func totpBinding(userID int64, pending bool) string {
	b := "platform_user:" + strconv.FormatInt(userID, 10) + "|totp"
	if pending {
		b += "_pending"
	}
	return b
}

// ---------------------------------------------------------------- login

// LoginResult tells the client which step comes next.
type LoginResult struct {
	ChallengeID            string `json:"challengeId"`
	MFARequired            bool   `json:"mfaRequired,omitempty"`
	MFAEnrollRequired      bool   `json:"mfaEnrollRequired,omitempty"`
	PasswordChangeRequired bool   `json:"passwordChangeRequired,omitempty"`
}

// Login checks email and password and returns the next step. It never
// issues a session directly: MFA is mandatory.
func (s *Service) Login(ctx context.Context, email, password string, m Meta) (LoginResult, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" || len(email) > 254 || len(password) > 256 {
		return LoginResult{}, errInvalidCredentials()
	}
	var u user
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, qUserByEmail, email))
		return err
	})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LoginResult{}, err
	}
	// Always run argon2 so timing does not reveal whether the user exists.
	verr := s.cfg.Hasher.Verify(ctx, password, u.PasswordHash)
	if verr != nil && !errors.Is(verr, security.ErrMismatch) {
		if ctx.Err() != nil {
			return LoginResult{}, ctx.Err()
		}
	}

	if !found || verr != nil || u.Status != "Active" || u.Locked {
		action := "login.failure"
		if found && u.Locked {
			action = "login.locked"
		}
		_ = s.rw(ctx, func(tx tenancy.PlatformTx) error {
			if found && verr != nil && !u.Locked {
				if _, err := tx.Exec(ctx, `UPDATE platform.platform_users
					SET failed_logins = failed_logins + 1,
					    locked_at = CASE WHEN failed_logins + 1 >= $2 THEN now() ELSE locked_at END
					WHERE id = $1`, u.ID, LockoutAfter); err != nil {
					return err
				}
			}
			return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: email, Action: action,
				IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
		})
		s.cfg.Log.WarnContext(ctx, "platform login failed", slog.String("event", "security."+strings.ReplaceAll(action, ".", "_")))
		return LoginResult{}, errInvalidCredentials()
	}

	var res LoginResult
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET failed_logins = 0 WHERE id = $1`, u.ID); err != nil {
			return err
		}
		purpose := "mfa"
		switch {
		case u.MustChange:
			purpose, res.PasswordChangeRequired = "password_change", true
		case !u.MFAEnrolled:
			purpose, res.MFAEnrollRequired = "enroll", true
		default:
			res.MFARequired = true
		}
		id, err := s.newChallenge(ctx, tx, u.ID, purpose, m)
		res.ChallengeID = id
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: u.Email, Action: "login.password_ok",
			After: map[string]any{"next": purpose}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return res, err
}

func (s *Service) newChallenge(ctx context.Context, tx tenancy.PlatformTx, userID int64, purpose string, m Meta) (string, error) {
	id := uuid.New()
	var ip *string
	if m.IP.IsValid() {
		v := m.IP.String()
		ip = &v
	}
	_, err := tx.Exec(ctx, `INSERT INTO platform.platform_login_challenges (id, user_id, purpose, expires_at, ip)
		VALUES ($1, $2, $3, $4, $5)`, id, userID, purpose, s.now().Add(ChallengeTTL), ip)
	return id.String(), err
}

// useChallenge loads a live challenge for purpose and locks the user row.
func (s *Service) useChallenge(ctx context.Context, tx tenancy.PlatformTx, id, purpose string) (user, uuid.UUID, error) {
	cid, err := uuid.Parse(id)
	if err != nil {
		return user{}, uuid.Nil, errChallenge()
	}
	var (
		userID   int64
		attempts int
		expires  time.Time
		used     bool
	)
	err = tx.QueryRow(ctx, `SELECT user_id, attempts, expires_at, used_at IS NOT NULL
		FROM platform.platform_login_challenges WHERE id = $1 AND purpose = $2 FOR UPDATE`, cid, purpose).
		Scan(&userID, &attempts, &expires, &used)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (used || attempts >= MaxChallengeTries || !s.now().Before(expires))) {
		return user{}, uuid.Nil, errChallenge()
	}
	if err != nil {
		return user{}, uuid.Nil, err
	}
	u, err := scanUser(tx.QueryRow(ctx, qUserByIDFU, userID))
	if err != nil {
		return user{}, uuid.Nil, err
	}
	if u.Status != "Active" || u.Locked {
		return user{}, uuid.Nil, errChallenge()
	}
	return u, cid, nil
}

// failChallenge counts a wrong code in its own transaction, so the count
// survives the rollback of the request transaction.
func (s *Service) failChallenge(ctx context.Context, cid uuid.UUID, userID int64, m Meta, action string) {
	_ = s.rw(context.WithoutCancel(ctx), func(tx tenancy.PlatformTx) error {
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_login_challenges SET attempts = attempts + 1 WHERE id = $1`, cid); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: userID, Action: action, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
}

func (s *Service) burnChallenge(ctx context.Context, tx tenancy.PlatformTx, cid uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE platform.platform_login_challenges SET used_at = now() WHERE id = $1`, cid)
	return err
}

// ------------------------------------------------------ password change

// ChangePasswordForced completes the first-login password change and
// returns the next challenge (enrolment or MFA).
func (s *Service) ChangePasswordForced(ctx context.Context, challengeID, newPassword string, m Meta) (LoginResult, error) {
	var (
		u   user
		cid uuid.UUID
	)
	// useChallenge locks rows (FOR UPDATE), so this needs a read-write tx.
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var err error
		u, cid, err = s.useChallenge(ctx, tx, challengeID, "password_change")
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	hash, err := s.checkAndHash(ctx, u, newPassword)
	if err != nil {
		return LoginResult{}, err
	}
	var res LoginResult
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u2, cid2, err := s.useChallenge(ctx, tx, challengeID, "password_change")
		if err != nil {
			return err
		}
		if cid2 != cid || u2.ID != u.ID {
			return errChallenge()
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET password_hash = $2, must_change_password = false, updated_at = now()
			WHERE id = $1`, u.ID, hash); err != nil {
			return err
		}
		if err := s.burnChallenge(ctx, tx, cid); err != nil {
			return err
		}
		purpose := "mfa"
		if !u.MFAEnrolled {
			purpose, res.MFAEnrollRequired = "enroll", true
		} else {
			res.MFARequired = true
		}
		if res.ChallengeID, err = s.newChallenge(ctx, tx, u.ID, purpose, m); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: u.Email, Action: "password.changed",
			After: map[string]any{"forced": true}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return res, err
}

// ChangePassword changes the password of a signed-in user and signs out
// every other session.
func (s *Service) ChangePassword(ctx context.Context, a Actor, current, newPassword string, m Meta) error {
	var u user
	if err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, qUserByID, a.UserID))
		return err
	}); err != nil {
		return err
	}
	if err := s.cfg.Hasher.Verify(ctx, current, u.PasswordHash); err != nil {
		return apperr.Validation(map[string]string{"currentPassword": "is incorrect"})
	}
	hash, err := s.checkAndHash(ctx, u, newPassword)
	if err != nil {
		return err
	}
	return s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET password_hash = $2, updated_at = now() WHERE id = $1`, u.ID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET revoked_at = now(), revoke_reason = 'password_changed'
			WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`, u.ID, a.SessionID); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: u.Email, Action: "password.changed",
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
}

func (s *Service) checkAndHash(ctx context.Context, u user, pw string) (string, error) {
	if err := security.CheckPassword(pw, MinPasswordLen, u.Email, u.Name); err != nil {
		return "", apperr.Validation(map[string]string{"newPassword": err.Error()})
	}
	if s.cfg.Hasher.Verify(ctx, pw, u.PasswordHash) == nil {
		return "", apperr.Validation(map[string]string{"newPassword": "must differ from the current password"})
	}
	return s.cfg.Hasher.Hash(ctx, pw)
}

// ------------------------------------------------------------ enrolment

// EnrollStart creates a pending TOTP secret and returns it for the QR code.
type EnrollStart struct {
	Secret     string `json:"secret"`
	OtpauthURI string `json:"otpauthUri"`
}

// StartEnroll begins mandatory MFA enrolment.
func (s *Service) StartEnroll(ctx context.Context, challengeID string) (EnrollStart, error) {
	var out EnrollStart
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u, _, err := s.useChallenge(ctx, tx, challengeID, "enroll")
		if err != nil {
			return err
		}
		secret := security.NewTOTPSecret()
		sealed, err := s.cfg.Envelope.Seal(secret, totpBinding(u.ID, true))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users
			SET pending_totp_ciphertext = $2, pending_totp_dek = $3, pending_totp_key_id = $4 WHERE id = $1`,
			u.ID, sealed.Ciphertext, sealed.WrappedDEK, sealed.KeyID); err != nil {
			return err
		}
		out = EnrollStart{Secret: security.TOTPSecretBase32(secret), OtpauthURI: security.TOTPURI(s.cfg.PlatformName, u.Email, secret)}
		return nil
	})
	return out, err
}

// SessionResult is returned when a session starts.
type SessionResult struct {
	AccessToken   string    `json:"accessToken"`
	ExpiresAt     time.Time `json:"expiresAt"`
	User          UserView  `json:"user"`
	RecoveryCodes []string  `json:"recoveryCodes,omitempty"`
	refresh       string    // set as the __Host-prt cookie, never in JSON
	session       uuid.UUID
}

// Refresh returns the raw refresh token (for the cookie).
func (r SessionResult) Refresh() string { return r.refresh }

// UserView is the safe public shape of a platform user.
type UserView struct {
	ID          int64    `json:"id"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

func view(u user) UserView {
	return UserView{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, Permissions: Permissions(u.Role)}
}

// ConfirmEnroll verifies the first code, activates MFA, issues recovery
// codes (shown once) and starts a session.
func (s *Service) ConfirmEnroll(ctx context.Context, challengeID, code string, m Meta) (SessionResult, error) {
	var res SessionResult
	var failed struct {
		cid uuid.UUID
		uid int64
	}
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u, cid, err := s.useChallenge(ctx, tx, challengeID, "enroll")
		if err != nil {
			return err
		}
		if len(u.PendingTOTP.Ciphertext) == 0 {
			return apperr.Conflict(apperr.CodeInvalidState, "Start enrolment first")
		}
		secret, err := s.cfg.Envelope.Open(u.PendingTOTP, totpBinding(u.ID, true))
		if err != nil {
			return err
		}
		step, ok := security.VerifyTOTP(secret, code, s.now(), 0)
		if !ok {
			failed.cid, failed.uid = cid, u.ID
			return apperr.Unauthenticated(apperr.CodeInvalidCredentials, "Invalid code")
		}
		sealed, err := s.cfg.Envelope.Seal(secret, totpBinding(u.ID, false))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users
			SET totp_ciphertext = $2, totp_dek = $3, totp_key_id = $4, totp_last_step = $5, mfa_enrolled = true,
			    pending_totp_ciphertext = NULL, pending_totp_dek = NULL, pending_totp_key_id = NULL, updated_at = now()
			WHERE id = $1`, u.ID, sealed.Ciphertext, sealed.WrappedDEK, sealed.KeyID, step); err != nil {
			return err
		}
		codes, err := s.replaceRecoveryCodes(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if err := s.burnChallenge(ctx, tx, cid); err != nil {
			return err
		}
		if res, err = s.startSession(ctx, tx, u, m); err != nil {
			return err
		}
		res.RecoveryCodes = codes
		return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: u.Email, Action: "mfa.enrolled",
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if failed.uid != 0 {
		s.failChallenge(ctx, failed.cid, failed.uid, m, "mfa.enroll_failure")
	}
	return res, err
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, tx tenancy.PlatformTx, userID int64) ([]string, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM platform.platform_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, RecoveryCodeCount)
	for i := range codes {
		codes[i] = security.NewRecoveryCode()
		if _, err := tx.Exec(ctx, `INSERT INTO platform.platform_recovery_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, security.HashToken(codes[i])); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// ------------------------------------------------------------------ MFA

// VerifyMFA completes sign-in with a TOTP code or a recovery code.
func (s *Service) VerifyMFA(ctx context.Context, challengeID, code, recoveryCode string, m Meta) (SessionResult, error) {
	var res SessionResult
	var failed struct {
		cid uuid.UUID
		uid int64
	}
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u, cid, err := s.useChallenge(ctx, tx, challengeID, "mfa")
		if err != nil {
			return err
		}
		method := "totp"
		switch {
		case recoveryCode != "":
			method = "recovery_code"
			tag, err := tx.Exec(ctx, `UPDATE platform.platform_recovery_codes SET used_at = now()
				WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, u.ID, security.HashToken(security.NormalizeRecoveryCode(recoveryCode)))
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				failed.cid, failed.uid = cid, u.ID
				return apperr.Unauthenticated(apperr.CodeInvalidCredentials, "Invalid code")
			}
		default:
			ok, err := s.checkTOTP(ctx, tx, u, code)
			if err != nil {
				return err
			}
			if !ok {
				failed.cid, failed.uid = cid, u.ID
				return apperr.Unauthenticated(apperr.CodeInvalidCredentials, "Invalid code")
			}
		}
		if err := s.burnChallenge(ctx, tx, cid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET last_login_at = now() WHERE id = $1`, u.ID); err != nil {
			return err
		}
		if res, err = s.startSession(ctx, tx, u, m); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: u.Email, Action: "login.success",
			After: map[string]any{"method": method}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if failed.uid != 0 {
		s.failChallenge(ctx, failed.cid, failed.uid, m, "mfa.failure")
	}
	return res, err
}

// checkTOTP verifies a code with the replay guard and stores the step.
func (s *Service) checkTOTP(ctx context.Context, tx tenancy.PlatformTx, u user, code string) (bool, error) {
	secret, err := s.cfg.Envelope.Open(u.TOTP, totpBinding(u.ID, false))
	if err != nil {
		return false, fmt.Errorf("platform: open totp secret: %w", err)
	}
	step, ok := security.VerifyTOTP(secret, code, s.now(), u.TOTPLastStep)
	if !ok {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE platform.platform_users SET totp_last_step = $2 WHERE id = $1 AND totp_last_step < $2`, u.ID, step)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ------------------------------------------------------------- sessions

func (s *Service) startSession(ctx context.Context, tx tenancy.PlatformTx, u user, m Meta) (SessionResult, error) {
	sid := uuid.New()
	var ip *string
	if m.IP.IsValid() {
		v := m.IP.String()
		ip = &v
	}
	ua := m.UserAgent
	if len(ua) > 400 {
		ua = ua[:400]
	}
	// Session times come from the service clock, the same clock they are
	// compared with (Authenticate, Refresh).
	now := s.now()
	if _, err := tx.Exec(ctx, `INSERT INTO platform.platform_sessions (id, user_id, created_at, last_seen_at, absolute_expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $3, $4, $5, $6)`, sid, u.ID, now, now.Add(s.cfg.SessionMax), ip, ua); err != nil {
		return SessionResult{}, err
	}
	refresh, err := s.issueRefresh(ctx, tx, sid)
	if err != nil {
		return SessionResult{}, err
	}
	tok, exp, err := s.cfg.Signer.Sign(s.Issuer(), strconv.FormatInt(u.ID, 10), authtoken.Claims{SessionID: sid.String()})
	if err != nil {
		return SessionResult{}, err
	}
	return SessionResult{AccessToken: tok, ExpiresAt: exp, User: view(u), refresh: refresh, session: sid}, nil
}

func (s *Service) issueRefresh(ctx context.Context, tx tenancy.PlatformTx, sid uuid.UUID) (string, error) {
	raw := security.NewToken(32)
	_, err := tx.Exec(ctx, `INSERT INTO platform.platform_refresh_tokens (id, session_id, token_hash) VALUES ($1, $2, $3)`,
		uuid.New(), sid, security.HashToken(raw))
	return raw, err
}

// Refresh rotates the refresh token. Reusing an already rotated token
// revokes the whole session (theft detection).
func (s *Service) Refresh(ctx context.Context, raw string, m Meta) (SessionResult, error) {
	if raw == "" {
		return SessionResult{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	var (
		res    SessionResult
		reused bool
	)
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var (
			tokID           uuid.UUID
			sid             uuid.UUID
			rotated, revoke bool
			userID          int64
			lastSeen, abs   time.Time
			sessRevoked     bool
		)
		err := tx.QueryRow(ctx, `SELECT t.id, t.session_id, t.rotated_at IS NOT NULL, t.revoked_at IS NOT NULL,
				s.user_id, s.last_seen_at, s.absolute_expires_at, s.revoked_at IS NOT NULL
			FROM platform.platform_refresh_tokens t JOIN platform.platform_sessions s ON s.id = t.session_id
			WHERE t.token_hash = $1 FOR UPDATE OF t, s`, security.HashToken(raw)).
			Scan(&tokID, &sid, &rotated, &revoke, &userID, &lastSeen, &abs, &sessRevoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
		}
		if err != nil {
			return err
		}
		if rotated || revoke {
			reused = !sessRevoked
			if _, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET revoked_at = coalesce(revoked_at, now()),
				revoke_reason = coalesce(revoke_reason, 'refresh_reuse') WHERE id = $1`, sid); err != nil {
				return err
			}
			if reused {
				if err := writeAudit(ctx, tx, AuditEntry{ActorID: userID, Action: "session.refresh_reuse",
					Target: sid.String(), IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID}); err != nil {
					return err
				}
			}
			return nil // commit the revocation, then fail
		}
		now := s.now()
		if sessRevoked || !now.Before(abs) || now.Sub(lastSeen) > s.cfg.IdleTimeout {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
		}
		u, err := scanUser(tx.QueryRow(ctx, qUserByID, userID))
		if err != nil {
			return err
		}
		if u.Status != "Active" || u.Locked {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_refresh_tokens SET rotated_at = now() WHERE id = $1`, tokID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET last_seen_at = $2 WHERE id = $1`, sid, now); err != nil {
			return err
		}
		newRaw, err := s.issueRefresh(ctx, tx, sid)
		if err != nil {
			return err
		}
		tok, exp, err := s.cfg.Signer.Sign(s.Issuer(), strconv.FormatInt(u.ID, 10), authtoken.Claims{SessionID: sid.String()})
		if err != nil {
			return err
		}
		res = SessionResult{AccessToken: tok, ExpiresAt: exp, User: view(u), refresh: newRaw, session: sid}
		return nil
	})
	if err != nil {
		return SessionResult{}, err
	}
	if res.AccessToken == "" {
		if reused {
			s.cfg.Log.WarnContext(ctx, "platform refresh token reuse", slog.String("event", "security.refresh_reuse"))
		}
		return SessionResult{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
	}
	return res, nil
}

// Logout revokes the session behind a refresh token (or an actor's session).
func (s *Service) Logout(ctx context.Context, raw string, a *Actor, m Meta) error {
	return s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var sid uuid.UUID
		var uid int64
		switch {
		case a != nil:
			sid, uid = a.SessionID, a.UserID
		case raw != "":
			err := tx.QueryRow(ctx, `SELECT s.id, s.user_id FROM platform.platform_refresh_tokens t
				JOIN platform.platform_sessions s ON s.id = t.session_id WHERE t.token_hash = $1`, security.HashToken(raw)).Scan(&sid, &uid)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
		default:
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET revoked_at = now(), revoke_reason = 'logout'
			WHERE id = $1 AND revoked_at IS NULL`, sid); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: uid, Action: "logout", IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
}

// Authenticate turns an access token into an Actor, checking the session
// and the user on every request so revocation is immediate.
func (s *Service) Authenticate(ctx context.Context, token string) (Actor, error) {
	c, err := s.cfg.Signer.Verify(token, s.Issuer())
	if errors.Is(err, authtoken.ErrExpired) {
		return Actor{}, apperr.Unauthenticated(apperr.CodeTokenExpired, "Access token expired")
	}
	if err != nil {
		return Actor{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	uid, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return Actor{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	sid, err := uuid.Parse(c.SessionID)
	if err != nil {
		return Actor{}, apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
	}
	var a Actor
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var (
			revoked       bool
			lastSeen, abs time.Time
			stepup        *time.Time
			status        string
			locked, mfaOK bool
		)
		err := tx.QueryRow(ctx, `SELECT s.revoked_at IS NOT NULL, s.last_seen_at, s.absolute_expires_at, s.stepup_until,
				u.email::text, u.name, u.role, u.status, u.locked_at IS NOT NULL, u.mfa_enrolled
			FROM platform.platform_sessions s JOIN platform.platform_users u ON u.id = s.user_id
			WHERE s.id = $1 AND s.user_id = $2`, sid, uid).
			Scan(&revoked, &lastSeen, &abs, &stepup, &a.Email, &a.Name, &a.Role, &status, &locked, &mfaOK)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Not signed in")
		}
		if err != nil {
			return err
		}
		now := s.now()
		if revoked || !now.Before(abs) || now.Sub(lastSeen) > s.cfg.IdleTimeout || status != "Active" || locked || !mfaOK {
			return apperr.Unauthenticated(apperr.CodeUnauthenticated, "Session expired")
		}
		if now.Sub(lastSeen) > lastSeenWriteEvery {
			if _, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET last_seen_at = $2 WHERE id = $1`, sid, now); err != nil {
				return err
			}
		}
		a.UserID, a.SessionID = uid, sid
		if stepup != nil {
			a.StepUpUntil = *stepup
		}
		return nil
	})
	return a, err
}

// StepUp re-verifies TOTP and opens a 5-minute window for destructive
// actions.
func (s *Service) StepUp(ctx context.Context, a Actor, code string, m Meta) (time.Time, error) {
	until := s.now().Add(StepUpTTL)
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u, err := scanUser(tx.QueryRow(ctx, qUserByIDFU, a.UserID))
		if err != nil {
			return err
		}
		ok, err := s.checkTOTP(ctx, tx, u, code)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Unauthenticated(apperr.CodeInvalidCredentials, "Invalid code")
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET stepup_until = $2 WHERE id = $1`, a.SessionID, until); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "stepup",
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Code == apperr.CodeInvalidCredentials {
			_ = s.rw(context.WithoutCancel(ctx), func(tx tenancy.PlatformTx) error {
				return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "stepup.failure",
					IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
			})
		}
		return time.Time{}, err
	}
	return until, nil
}

// Me returns the actor's profile.
func (s *Service) Me(a Actor) UserView {
	return UserView{ID: a.UserID, Email: a.Email, Name: a.Name, Role: a.Role, Permissions: Permissions(a.Role)}
}
