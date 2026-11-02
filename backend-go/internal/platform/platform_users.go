package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Platform users page (S10 item 1, docs/S10_DESIGN.md §1): add by invite
// link, change role, deactivate / reactivate, reset another admin's MFA.
// Every change needs platform_users:manage, step-up and a reason; it is
// audited and the affected user (and the platform inbox) get an email.

// InviteTTL is how long an invite link works.
const InviteTTL = 7 * 24 * time.Hour

var platformRoles = map[string]bool{RoleSuperAdmin: true, RoleBillingAdmin: true, RoleSupport: true}

// AdminUserView is one row of the platform users page.
type AdminUserView struct {
	ID              int64      `json:"id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	MFAEnrolled     bool       `json:"mfaEnrolled"`
	Locked          bool       `json:"locked"`
	LastLoginAt     *time.Time `json:"lastLoginAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	InvitePending   bool       `json:"invitePending"`
	InviteExpiresAt *time.Time `json:"inviteExpiresAt"`
}

// InviteResult is returned once to the admin who invites: the link is
// never shown again (only a hash is stored).
type InviteResult struct {
	User      AdminUserView `json:"user"`
	InviteURL string        `json:"inviteUrl"`
	ExpiresAt time.Time     `json:"expiresAt"`
}

// NewPlatformUser is the POST body.
type NewPlatformUser struct {
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

// InviteURL is the console page that accepts an invite. The token is in
// the fragment, so it never reaches a server log or a Referer header.
func InviteURL(scheme, baseDomain, port, token string) string {
	return httpx.OriginURL(scheme, "admin."+baseDomain, port) + "/invite#token=" + token
}

// IssueInvite stores a new invite token for a user and returns it. The
// worker calls it when it sends the invite email, so the emailed token is
// never stored in clear (not even in the outbox).
func IssueInvite(ctx context.Context, tx tenancy.PlatformTx, userID int64, via string, createdBy int64, expires time.Time) (string, error) {
	token := security.NewToken(32)
	_, err := tx.Exec(ctx, `INSERT INTO platform.platform_invites (user_id, token_hash, via, expires_at, created_by)
		VALUES ($1, $2, $3, $4, NULLIF($5, 0))`, userID, security.HashToken(token), via, expires, createdBy)
	return token, err
}

const qPlatformUsers = `SELECT u.id, u.email::text, u.name, u.role, u.status, u.mfa_enrolled, u.locked_at IS NOT NULL,
		u.last_login_at, u.created_at, i.expires_at
	FROM platform.platform_users u
	LEFT JOIN LATERAL (SELECT max(expires_at) AS expires_at FROM platform.platform_invites
		WHERE user_id = u.id AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()) i ON true`

func scanPlatformUser(row pgx.Row) (AdminUserView, error) {
	var v AdminUserView
	err := row.Scan(&v.ID, &v.Email, &v.Name, &v.Role, &v.Status, &v.MFAEnrolled, &v.Locked, &v.LastLoginAt, &v.CreatedAt, &v.InviteExpiresAt)
	v.InvitePending = v.InviteExpiresAt != nil
	return v, err
}

// ListPlatformUsers returns every platform user, by email.
func (s *Service) ListPlatformUsers(ctx context.Context) ([]AdminUserView, error) {
	out := []AdminUserView{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qPlatformUsers+` ORDER BY u.email`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanPlatformUser(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func reasonText(r string) (string, error) {
	r = strings.TrimSpace(strings.ReplaceAll(r, "\x00", ""))
	if l := len([]rune(r)); l < 10 || l > 500 {
		return "", apperr.Validation(map[string]string{"reason": "10-500 characters"})
	}
	return r, nil
}

// AddPlatformUser creates a user without a usable password and issues the
// invite: one link returned here (to copy once), one emailed by the worker.
func (s *Service) AddPlatformUser(ctx context.Context, a Actor, in NewPlatformUser, m Meta) (InviteResult, error) {
	f := map[string]string{}
	in.Email, in.Name, in.Role = strings.TrimSpace(in.Email), strings.TrimSpace(in.Name), strings.TrimSpace(in.Role)
	if !validEmail(in.Email) {
		f["email"] = "must be a plain email address"
	}
	if l := len([]rune(in.Name)); l < 1 || l > 120 || strings.ContainsAny(in.Name, "\r\n\x00") {
		f["name"] = "1-120 characters"
	}
	if !platformRoles[in.Role] {
		f["role"] = "super_admin, billing_admin or support"
	}
	reason, rerr := reasonText(in.Reason)
	if rerr != nil {
		f["reason"] = "10-500 characters"
	}
	if len(f) > 0 {
		return InviteResult{}, apperr.Validation(f)
	}
	// A password nobody knows: the account works only after the invite.
	unusable, err := s.cfg.Hasher.Hash(ctx, security.NewToken(32))
	if err != nil {
		return InviteResult{}, err
	}
	var out InviteResult
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO platform.platform_users (email, name, role, password_hash, must_change_password, mfa_enrolled)
			VALUES ($1, $2, $3, $4, false, false) ON CONFLICT (email) DO NOTHING RETURNING id`, in.Email, in.Name, in.Role, unusable).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict(apperr.CodeConflict, "A platform user with this email already exists.")
		}
		if err != nil {
			return err
		}
		if out.InviteURL, out.ExpiresAt, err = s.issueInvites(ctx, tx, a, id, in.Name, in.Email); err != nil {
			return err
		}
		if out.User, err = scanPlatformUser(tx.QueryRow(ctx, qPlatformUsers+` WHERE u.id = $1`, id)); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "platform_user.invite",
			Target: "platform_user:" + strconv.FormatInt(id, 10), After: map[string]any{"email": in.Email, "role": in.Role, "reason": reason},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// issueInvites revokes open invites, issues the copy link and queues the
// email (whose token the worker issues when it sends).
func (s *Service) issueInvites(ctx context.Context, tx tenancy.PlatformTx, a Actor, userID int64, name, email string) (string, time.Time, error) {
	if _, err := tx.Exec(ctx, `UPDATE platform.platform_invites SET revoked_at = now() WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL`, userID); err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().Add(InviteTTL)
	token, err := IssueInvite(ctx, tx, userID, "copy", a.UserID, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	data, _ := json.Marshal(map[string]any{"userId": userID, "expiresAt": expires, "createdBy": a.UserID, "invitedBy": a.Name})
	if _, err := tx.Exec(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data)
		VALUES ($1, 'platform_notice', $2, $3, '`+mailtmpl.PlatformInviteTemplate+`', $4::text::jsonb) ON CONFLICT (idempotency_key) DO NOTHING`,
		fmt.Sprintf("platform-invite:%d:%s", userID, security.NewToken(9)), email, name, string(data)); err != nil {
		return "", time.Time{}, err
	}
	return InviteURL(s.cfg.PublicScheme, s.cfg.BaseDomain, s.cfg.PublicPort, token), expires, nil
}

// Reinvite replaces a pending invite (new links, old ones revoked).
func (s *Service) Reinvite(ctx context.Context, a Actor, id int64, reason string, m Meta) (InviteResult, error) {
	reason, err := reasonText(reason)
	if err != nil {
		return InviteResult{}, err
	}
	var out InviteResult
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u, err := scanUser(tx.QueryRow(ctx, qUserByIDFU, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if u.Status != "Active" {
			return apperr.Conflict(apperr.CodeInvalidState, "Reactivate the user first.")
		}
		if u.MFAEnrolled {
			return apperr.Conflict(apperr.CodeInvalidState, "This user has already set up their account; reset their MFA instead.")
		}
		if out.InviteURL, out.ExpiresAt, err = s.issueInvites(ctx, tx, a, id, u.Name, u.Email); err != nil {
			return err
		}
		if out.User, err = scanPlatformUser(tx.QueryRow(ctx, qPlatformUsers+` WHERE u.id = $1`, id)); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "platform_user.reinvite",
			Target: "platform_user:" + strconv.FormatInt(id, 10), After: map[string]any{"reason": reason},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// changeTarget loads and locks the target of a change and applies the
// guards: never yourself, and one active super_admin with MFA remains.
func (s *Service) changeTarget(ctx context.Context, tx tenancy.PlatformTx, a Actor, id int64, removesSuperAdmin func(user) bool) (user, error) {
	if id == a.UserID {
		return user{}, apperr.Conflict(apperr.CodeInvalidState, "You cannot change your own account here.")
	}
	// Lock every super_admin row: two admins cannot remove the last two at once.
	if _, err := tx.Exec(ctx, `SELECT id FROM platform.platform_users WHERE role = 'super_admin' ORDER BY id FOR UPDATE`); err != nil {
		return user{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, qUserByIDFU, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return user{}, apperr.NotFound()
	}
	if err != nil {
		return user{}, err
	}
	if removesSuperAdmin(u) {
		var others int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.platform_users
			WHERE role = 'super_admin' AND status = 'Active' AND mfa_enrolled AND id <> $1`, id).Scan(&others); err != nil {
			return user{}, err
		}
		if others == 0 {
			return user{}, apperr.Conflict(apperr.CodeInvalidState, "At least one active super admin with MFA must remain.")
		}
	}
	return u, nil
}

func isActiveSuperAdmin(u user) bool {
	return u.Role == RoleSuperAdmin && u.Status == "Active" && u.MFAEnrolled
}

// endSessions signs a user out everywhere.
func endSessions(ctx context.Context, tx tenancy.PlatformTx, userID int64, why string) error {
	if _, err := tx.Exec(ctx, `UPDATE platform.platform_refresh_tokens SET revoked_at = now()
		WHERE revoked_at IS NULL AND session_id IN (SELECT id FROM platform.platform_sessions WHERE user_id = $1)`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE platform.platform_sessions SET revoked_at = now(), revoke_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, why)
	return err
}

// queueUserNotice emails the affected user and the platform inbox.
func (s *Service) queueUserNotice(ctx context.Context, tx tenancy.PlatformTx, u user, heading string, lines []string) error {
	data, _ := json.Marshal(map[string]any{"name": u.Name, "heading": heading, "lines": lines})
	to := []string{u.Email}
	if s.cfg.InboxEmail != "" && !strings.EqualFold(s.cfg.InboxEmail, u.Email) {
		to = append(to, s.cfg.InboxEmail)
	}
	for i, addr := range to {
		if _, err := tx.Exec(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data)
			VALUES ($1, 'platform_notice', $2, $3, '`+mailtmpl.PlatformUserNoticeTemplate+`', $4::text::jsonb) ON CONFLICT (idempotency_key) DO NOTHING`,
			fmt.Sprintf("platform-user:%d:%s:%d", u.ID, security.NewToken(9), i), addr, u.Name, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) userChange(ctx context.Context, a Actor, id int64, reason string, m Meta, action string,
	removes func(user) bool, apply func(tx tenancy.PlatformTx, u user) (heading string, lines []string, before, after map[string]any, err error)) error {
	reason, err := reasonText(reason)
	if err != nil {
		return err
	}
	return s.rw(ctx, func(tx tenancy.PlatformTx) error {
		u, err := s.changeTarget(ctx, tx, a, id, removes)
		if err != nil {
			return err
		}
		heading, lines, before, after, err := apply(tx, u)
		if err != nil {
			return err
		}
		if after == nil {
			after = map[string]any{}
		}
		after["reason"] = reason
		if err := writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: action,
			Target: "platform_user:" + strconv.FormatInt(id, 10), Before: before, After: after,
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID}); err != nil {
			return err
		}
		lines = append(lines, "Changed by "+a.Email+". Reason: "+reason)
		return s.queueUserNotice(ctx, tx, u, heading, lines)
	})
}

// ChangeRole gives another platform user a new role.
func (s *Service) ChangeRole(ctx context.Context, a Actor, id int64, role, reason string, m Meta) error {
	role = strings.TrimSpace(role)
	if !platformRoles[role] {
		return apperr.Validation(map[string]string{"role": "super_admin, billing_admin or support"})
	}
	return s.userChange(ctx, a, id, reason, m, "platform_user.role",
		func(u user) bool { return isActiveSuperAdmin(u) && role != RoleSuperAdmin },
		func(tx tenancy.PlatformTx, u user) (string, []string, map[string]any, map[string]any, error) {
			if u.Role == role {
				return "", nil, nil, nil, apperr.Conflict(apperr.CodeInvalidState, "The user already has this role.")
			}
			if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET role = $2, updated_at = now() WHERE id = $1`, u.ID, role); err != nil {
				return "", nil, nil, nil, err
			}
			// New permissions apply on the next request (roles are read per
			// request); end sessions so open tabs reload with the new menu.
			if err := endSessions(ctx, tx, u.ID, "role_changed"); err != nil {
				return "", nil, nil, nil, err
			}
			return "Your console role changed", []string{"Your role is now " + strings.ReplaceAll(role, "_", " ") + " (was " + strings.ReplaceAll(u.Role, "_", " ") + "). Sign in again to continue."},
				map[string]any{"role": u.Role}, map[string]any{"role": role}, nil
		})
}

// SetActive deactivates (signing the user out everywhere and revoking open
// invites) or reactivates a platform user.
func (s *Service) SetActive(ctx context.Context, a Actor, id int64, active bool, reason string, m Meta) error {
	action, status := "platform_user.deactivate", "Disabled"
	if active {
		action, status = "platform_user.reactivate", "Active"
	}
	return s.userChange(ctx, a, id, reason, m, action,
		func(u user) bool { return !active && isActiveSuperAdmin(u) },
		func(tx tenancy.PlatformTx, u user) (string, []string, map[string]any, map[string]any, error) {
			if u.Status == status {
				return "", nil, nil, nil, apperr.Conflict(apperr.CodeInvalidState, "The user is already "+strings.ToLower(map[bool]string{true: "active", false: "deactivated"}[active])+".")
			}
			if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET status = $2, updated_at = now() WHERE id = $1`, u.ID, status); err != nil {
				return "", nil, nil, nil, err
			}
			if active {
				return "Your console account is active again", []string{"You can sign in to the console again."},
					map[string]any{"status": u.Status}, map[string]any{"status": status}, nil
			}
			if _, err := tx.Exec(ctx, `UPDATE platform.platform_invites SET revoked_at = now() WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL`, u.ID); err != nil {
				return "", nil, nil, nil, err
			}
			if err := endSessions(ctx, tx, u.ID, "deactivated"); err != nil {
				return "", nil, nil, nil, err
			}
			return "Your console account was deactivated", []string{"You were signed out and can no longer sign in to the console."},
				map[string]any{"status": u.Status}, map[string]any{"status": status}, nil
		})
}

// ResetMFA removes another user's authenticator and recovery codes and
// signs them out; they enrol again at their next sign-in.
func (s *Service) ResetMFA(ctx context.Context, a Actor, id int64, reason string, m Meta) error {
	return s.userChange(ctx, a, id, reason, m, "platform_user.mfa_reset",
		isActiveSuperAdmin,
		func(tx tenancy.PlatformTx, u user) (string, []string, map[string]any, map[string]any, error) {
			if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET mfa_enrolled = false, totp_ciphertext = NULL, totp_dek = NULL,
				totp_key_id = NULL, totp_last_step = 0, pending_totp_ciphertext = NULL, pending_totp_dek = NULL, pending_totp_key_id = NULL,
				failed_logins = 0, locked_at = NULL, updated_at = now() WHERE id = $1`, u.ID); err != nil {
				return "", nil, nil, nil, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM platform.platform_recovery_codes WHERE user_id = $1`, u.ID); err != nil {
				return "", nil, nil, nil, err
			}
			if err := endSessions(ctx, tx, u.ID, "mfa_reset"); err != nil {
				return "", nil, nil, nil, err
			}
			return "Your console sign-in code was reset", []string{"Your authenticator and recovery codes no longer work. At your next sign-in you set up a new authenticator. If you did not ask for this, contact the platform owner now."},
				map[string]any{"mfaEnrolled": u.MFAEnrolled}, map[string]any{"mfaEnrolled": false}, nil
		})
}

func errInvite() error {
	return apperr.Unauthenticated(apperr.CodeUnauthenticated, "This invite link is invalid, used or expired. Ask for a new one.")
}

// AcceptInvite sets the invited user's password and starts the mandatory
// MFA enrolment (the next step of the sign-in flow).
func (s *Service) AcceptInvite(ctx context.Context, token, password string, m Meta) (LoginResult, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 200 || len(password) > 256 {
		return LoginResult{}, errInvite()
	}
	var u user
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		var uid int64
		err := tx.QueryRow(ctx, `SELECT user_id FROM platform.platform_invites WHERE token_hash = $1 AND used_at IS NULL
			AND revoked_at IS NULL AND expires_at > $2`, security.HashToken(token), s.now()).Scan(&uid)
		if err != nil {
			return err
		}
		u, err = scanUser(tx.QueryRow(ctx, qUserByID, uid))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && u.Status != "Active") {
		return LoginResult{}, errInvite()
	}
	if err != nil {
		return LoginResult{}, err
	}
	if err := security.CheckPassword(password, MinPasswordLen, u.Email, u.Name); err != nil {
		return LoginResult{}, apperr.Validation(map[string]string{"password": err.Error()})
	}
	hash, err := s.cfg.Hasher.Hash(ctx, password)
	if err != nil {
		return LoginResult{}, err
	}
	var res LoginResult
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var inviteID, uid int64
		err := tx.QueryRow(ctx, `SELECT id, user_id FROM platform.platform_invites WHERE token_hash = $1 AND used_at IS NULL
			AND revoked_at IS NULL AND expires_at > $2 FOR UPDATE`, security.HashToken(token), s.now()).Scan(&inviteID, &uid)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && uid != u.ID) {
			return errInvite()
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_invites SET used_at = CASE WHEN id = $2 THEN now() ELSE used_at END,
			revoked_at = CASE WHEN id = $2 THEN revoked_at ELSE now() END
			WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL`, u.ID, inviteID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.platform_users SET password_hash = $2, must_change_password = false,
			failed_logins = 0, locked_at = NULL, updated_at = now() WHERE id = $1 AND status = 'Active'`, u.ID, hash); err != nil {
			return err
		}
		purpose := "enroll"
		res.MFAEnrollRequired = true
		if u.MFAEnrolled { // cannot happen through the console; kept safe
			purpose, res.MFAEnrollRequired, res.MFARequired = "mfa", false, true
		}
		if res.ChallengeID, err = s.newChallenge(ctx, tx, u.ID, purpose, m); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: u.ID, ActorEmail: u.Email, Action: "platform_user.invite_accepted",
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return res, err
}

// ------------------------------------------------------------------ HTTP

func (h *HTTP) mountPlatformUsers(p chi.Router) {
	need := h.need(PermPlatformUsersAdmin)
	p.With(need).Get("/platform-users", h.listPlatformUsers)
	p.With(need, h.stepUpRequired).Post("/platform-users", h.addPlatformUser)
	p.With(need, h.stepUpRequired).Post("/platform-users/{uid}/invite", h.reinvite)
	p.With(need, h.stepUpRequired).Post("/platform-users/{uid}/role", h.changeRole)
	p.With(need, h.stepUpRequired).Post("/platform-users/{uid}/deactivate", h.setActive(false))
	p.With(need, h.stepUpRequired).Post("/platform-users/{uid}/reactivate", h.setActive(true))
	p.With(need, h.stepUpRequired).Post("/platform-users/{uid}/reset-mfa", h.resetMFA)
}

func uidParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

type userActionBody struct {
	Reason string `json:"reason"`
	Role   string `json:"role"`
}

func (h *HTTP) listPlatformUsers(w http.ResponseWriter, r *http.Request) {
	l, err := h.svc.ListPlatformUsers(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *HTTP) addPlatformUser(w http.ResponseWriter, r *http.Request) {
	var in NewPlatformUser
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.AddPlatformUser(r.Context(), actorFrom(r.Context()), in, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *HTTP) reinvite(w http.ResponseWriter, r *http.Request) {
	id, err := uidParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in userActionBody
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.Reinvite(r.Context(), actorFrom(r.Context()), id, in.Reason, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *HTTP) changeRole(w http.ResponseWriter, r *http.Request) {
	h.userAction(w, r, func(id int64, in userActionBody) error {
		return h.svc.ChangeRole(r.Context(), actorFrom(r.Context()), id, in.Role, in.Reason, h.meta(r))
	})
}

func (h *HTTP) setActive(active bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.userAction(w, r, func(id int64, in userActionBody) error {
			return h.svc.SetActive(r.Context(), actorFrom(r.Context()), id, active, in.Reason, h.meta(r))
		})
	}
}

func (h *HTTP) resetMFA(w http.ResponseWriter, r *http.Request) {
	h.userAction(w, r, func(id int64, in userActionBody) error {
		return h.svc.ResetMFA(r.Context(), actorFrom(r.Context()), id, in.Reason, h.meta(r))
	})
}

func (h *HTTP) userAction(w http.ResponseWriter, r *http.Request, fn func(int64, userActionBody) error) {
	id, err := uidParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in userActionBody
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := fn(id, in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTP) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.svc.AcceptInvite(r.Context(), in.Token, in.Password, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}
