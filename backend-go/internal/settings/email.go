package settings

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Test-send limits (EMAIL_AND_RECEIPTS §3.1).
const (
	testsPerHour       = 20
	customTestsPerHour = 5
	testTimeout        = 30 * time.Second
	defaultDailyCap    = 300
	maxDailyCap        = 100000
)

// Email is a tenant's SMTP configuration with the password decrypted. Only
// the worker and the test endpoint load it; it is never serialised.
type Email struct {
	Enabled             bool
	Status              string
	Server              mailer.Server
	FromName            string
	FromEmail           string
	Fallback            bool
	DailyCap            int
	ConsecutiveFailures int
}

// ErrUndecryptable means the stored password does not open in this tenant
// (copied from another tenant, or its master key was removed).
var ErrUndecryptable = errors.New("settings: smtp password cannot be decrypted")

func binding(tenantID string) string { return "tenant:" + tenantID + "|smtp_password" }

type emailRow struct {
	enabled             bool
	host                string
	port                int
	tlsMode             string
	username            string
	sealed              secrets.Sealed
	fromName            *string
	fromEmail           string
	fallback            bool
	dailyCap            int
	status              string
	lastTestAt          *time.Time
	lastSuccessAt       *time.Time
	lastErrorCode       *string
	consecutiveFailures int
	updatedAt           time.Time
}

const (
	qEmail = `SELECT enabled, host, port, tls_mode, username, password_ciphertext, password_dek_wrapped, key_id,
		from_name, from_email::text, fallback_to_platform, daily_cap, status, last_test_at, last_success_at,
		last_error_code, consecutive_failures, updated_at FROM tenant_email_settings WHERE id = 1`
	qEmailLock = qEmail + ` FOR UPDATE`
)

func loadRow(ctx context.Context, tx tenancy.TenantTx, lock bool) (*emailRow, error) {
	var row pgx.Row
	if lock {
		row = tx.QueryRow(ctx, qEmailLock)
	} else {
		row = tx.QueryRow(ctx, qEmail)
	}
	var e emailRow
	err := row.Scan(&e.enabled, &e.host, &e.port, &e.tlsMode, &e.username, &e.sealed.Ciphertext,
		&e.sealed.WrappedDEK, &e.sealed.KeyID, &e.fromName, &e.fromEmail, &e.fallback, &e.dailyCap, &e.status,
		&e.lastTestAt, &e.lastSuccessAt, &e.lastErrorCode, &e.consecutiveFailures, &e.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// LoadEmail returns the tenant's SMTP configuration, or nil when none is
// saved. The password only decrypts in the tenant it was saved for.
func LoadEmail(ctx context.Context, tx tenancy.TenantTx, env *secrets.Envelope) (*Email, error) {
	row, err := loadRow(ctx, tx, false)
	if err != nil || row == nil {
		return nil, err
	}
	pw, err := env.Open(row.sealed, binding(tx.Tenant().ID))
	if err != nil {
		return nil, ErrUndecryptable
	}
	e := &Email{Enabled: row.enabled, Status: row.status, FromEmail: row.fromEmail, Fallback: row.fallback,
		DailyCap: row.dailyCap, ConsecutiveFailures: row.consecutiveFailures,
		Server: mailer.Server{Host: row.host, Port: row.port, TLS: mailer.TLSMode(row.tlsMode), Username: row.username, Password: secrets.Secret(pw)}}
	if row.fromName != nil {
		e.FromName = *row.fromName
	}
	return e, nil
}

// EmailJSON is GET /api/settings/email. The password is write-only.
type EmailJSON struct {
	Configured          bool       `json:"configured"`
	Enabled             bool       `json:"enabled"`
	Host                string     `json:"host"`
	Port                int        `json:"port"`
	TLSMode             string     `json:"tlsMode"`
	Username            string     `json:"username"`
	PasswordSet         bool       `json:"passwordSet"`
	FromName            *string    `json:"fromName"`
	FromEmail           string     `json:"fromEmail"`
	FallbackToPlatform  bool       `json:"fallbackToPlatform"`
	DailyCap            int        `json:"dailyCap"`
	Status              string     `json:"status"`
	LastTestAt          *time.Time `json:"lastTestAt"`
	LastSuccessAt       *time.Time `json:"lastSuccessAt"`
	LastErrorCode       *string    `json:"lastErrorCode"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	UpdatedAt           *time.Time `json:"updatedAt"`
}

func emailJSON(e *emailRow) EmailJSON {
	if e == nil {
		return EmailJSON{Port: 587, TLSMode: "starttls", FallbackToPlatform: true, DailyCap: defaultDailyCap, Status: "unverified"}
	}
	u := e.updatedAt
	return EmailJSON{Configured: true, Enabled: e.enabled, Host: e.host, Port: e.port, TLSMode: e.tlsMode,
		Username: e.username, PasswordSet: len(e.sealed.Ciphertext) > 0, FromName: e.fromName, FromEmail: e.fromEmail,
		FallbackToPlatform: e.fallback, DailyCap: e.dailyCap, Status: e.status, LastTestAt: e.lastTestAt,
		LastSuccessAt: e.lastSuccessAt, LastErrorCode: e.lastErrorCode, ConsecutiveFailures: e.consecutiveFailures,
		UpdatedAt: &u}
}

func (a *API) getEmail(w http.ResponseWriter, r *http.Request) {
	var row *emailRow
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		row, err = loadRow(r.Context(), tx, false)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, emailJSON(row))
}

type emailIn struct {
	Enabled            *bool   `json:"enabled"`
	Host               *string `json:"host"`
	Port               *int    `json:"port"`
	TLSMode            *string `json:"tlsMode"`
	Username           *string `json:"username"`
	Password           *string `json:"password"`
	FromName           *string `json:"fromName"`
	FromEmail          *string `json:"fromEmail"`
	FallbackToPlatform *bool   `json:"fallbackToPlatform"`
	DailyCap           *int    `json:"dailyCap"`
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

// internalSuffixes are names that only make sense inside a network.
var internalSuffixes = []string{"localhost", ".localhost", ".local", ".internal", ".home.arpa", ".lan", ".intranet", ".corp"}

// checkHost rejects obviously internal hosts early. The real control is
// the guard at connect time, which checks every resolved address.
func (a *API) checkHost(h string) string {
	if len(h) > 253 || !hostPattern.MatchString(h) {
		return "Enter a host name such as smtp.gmail.com."
	}
	lh := strings.ToLower(h)
	for _, s := range internalSuffixes {
		if lh == strings.TrimPrefix(s, ".") || strings.HasSuffix(lh, s) {
			return "Internal host names are not allowed."
		}
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		if _, err := a.d.Guard.Resolve(context.Background(), ip.String()); err != nil {
			return "This address is not allowed."
		}
	}
	return ""
}

func usernameIsEmail(u string) bool { return strings.Contains(u, "@") && mailer.ValidEmail(u) }

const qUpsertEmail = `INSERT INTO tenant_email_settings (id, enabled, host, port, tls_mode, username, password_ciphertext,
		password_dek_wrapped, key_id, from_name, from_email, fallback_to_platform, daily_cap, status,
		consecutive_failures, updated_by, updated_at)
	VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, now())
	ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled, host = EXCLUDED.host, port = EXCLUDED.port,
		tls_mode = EXCLUDED.tls_mode, username = EXCLUDED.username, password_ciphertext = EXCLUDED.password_ciphertext,
		password_dek_wrapped = EXCLUDED.password_dek_wrapped, key_id = EXCLUDED.key_id, from_name = EXCLUDED.from_name,
		from_email = EXCLUDED.from_email, fallback_to_platform = EXCLUDED.fallback_to_platform,
		daily_cap = EXCLUDED.daily_cap, status = EXCLUDED.status,
		consecutive_failures = EXCLUDED.consecutive_failures, updated_by = EXCLUDED.updated_by, updated_at = now()`

func (a *API) putEmail(w http.ResponseWriter, r *http.Request) {
	var in emailIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, p := tenant(r), principal(r)
	limits, err := a.d.Plans.For(r.Context(), t.PlanID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out *emailRow
	err = a.rw(r.Context(), t, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		cur, err := loadRow(ctx, tx, true)
		if err != nil {
			return err
		}
		e := emailRow{port: 587, tlsMode: "starttls", fallback: true, dailyCap: defaultDailyCap, status: "unverified"}
		if cur != nil {
			e = *cur
		}
		f := map[string]string{}
		reverify := cur == nil
		set := func(dst *string, v *string, field string, check func(string) string) {
			if v == nil {
				return
			}
			s := strings.TrimSpace(*v)
			if msg := check(s); msg != "" {
				f[field] = msg
				return
			}
			if s != *dst {
				reverify = true
			}
			*dst = s
		}
		set(&e.host, in.Host, "host", a.checkHost)
		set(&e.username, in.Username, "username", func(s string) string {
			if s == "" || len(s) > 254 || hasControl(s) {
				return "Username is required."
			}
			return ""
		})
		set(&e.fromEmail, in.FromEmail, "fromEmail", func(s string) string {
			if !mailer.ValidEmail(s) {
				return "Enter a valid email address."
			}
			return ""
		})
		if in.Port != nil {
			if *in.Port != 465 && *in.Port != 587 {
				f["port"] = "Use port 587 (STARTTLS) or 465 (TLS). Port 25 is not allowed."
			} else if *in.Port != e.port {
				e.port, reverify = *in.Port, true
				e.tlsMode = map[int]string{465: "implicit", 587: "starttls"}[e.port]
			}
		}
		if in.TLSMode != nil && *in.TLSMode != e.tlsMode {
			f["tlsMode"] = "Port 587 uses starttls and port 465 uses implicit TLS."
		}
		if in.FromName != nil {
			s := strings.TrimSpace(*in.FromName)
			switch {
			case hasControl(s) || len([]rune(s)) > mailer.MaxNameRunes:
				f["fromName"] = "At most 78 characters, no line breaks."
			case s == "":
				e.fromName = nil
			default:
				e.fromName = &s
			}
		}
		if in.Password != nil {
			pw := *in.Password
			if pw == "" || len(pw) > 500 || hasControl(pw) {
				f["password"] = "Password is required."
			} else {
				sealed, err := a.d.Envelope.Seal([]byte(pw), binding(t.ID))
				if err != nil {
					return err
				}
				e.sealed, reverify = sealed, true
			}
		}
		if in.FallbackToPlatform != nil {
			e.fallback = *in.FallbackToPlatform
		}
		if in.DailyCap != nil {
			limit := maxDailyCap
			if limits.MaxEmailsPerDay > 0 {
				limit = int(min(limits.MaxEmailsPerDay, maxDailyCap))
			}
			if *in.DailyCap < 1 || *in.DailyCap > limit {
				f["dailyCap"] = "Daily cap must be between 1 and your plan limit."
			} else {
				e.dailyCap = *in.DailyCap
			}
		}
		if in.Enabled != nil {
			e.enabled = *in.Enabled
		}
		for k, missing := range map[string]bool{"host": e.host == "", "username": e.username == "",
			"password": len(e.sealed.Ciphertext) == 0, "fromEmail": e.fromEmail == ""} {
			if missing && f[k] == "" {
				f[k] = "Required."
			}
		}
		// Gmail and Workspace rewrite a From that is not the signed-in
		// mailbox, so it must match when the username is an address.
		if usernameIsEmail(e.username) && e.fromEmail != "" && !strings.EqualFold(e.username, e.fromEmail) && f["fromEmail"] == "" {
			f["fromEmail"] = "The from address must be the SMTP username."
		}
		if len(f) > 0 {
			return apperr.Validation(f)
		}
		if reverify {
			e.status, e.consecutiveFailures = "unverified", 0
		}
		if _, err := tx.Exec(ctx, qUpsertEmail, e.enabled, e.host, e.port, e.tlsMode, e.username, e.sealed.Ciphertext,
			e.sealed.WrappedDEK, e.sealed.KeyID, e.fromName, e.fromEmail, e.fallback, e.dailyCap, e.status,
			e.consecutiveFailures, p.UserID); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "smtp.updated", Entity: "email_settings",
			EntityID: "1", After: map[string]any{"host": e.host, "port": e.port, "enabled": e.enabled,
				"password_changed": in.Password != nil, "reverify": reverify},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = loadRow(ctx, tx, false)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, emailJSON(out))
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return true
		}
	}
	return false
}

// deleteEmail disables tenant SMTP and wipes the stored password.
func (a *API) deleteEmail(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM tenant_email_settings WHERE id = 1`)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: p.UserID, Action: "smtp.deleted", Entity: "email_settings",
			EntityID: "1", RequestID: httpx.RequestID(r.Context()), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ test

type testIn struct {
	To *string `json:"to"`
}

// TestResult is the POST /api/settings/email/test response.
type TestResult struct {
	OK         bool   `json:"ok"`
	Status     string `json:"status"`
	ErrorClass string `json:"errorClass,omitempty"`
	ErrorCode  string `json:"errorCode,omitempty"`
	Message    string `json:"message"`
}

// testEmail sends one message synchronously through the tenant's SMTP
// server, to the acting user unless "to" is given (rate-limited harder),
// and records the result in status.
func (a *API) testEmail(w http.ResponseWriter, r *http.Request) {
	var in testIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, p := tenant(r), principal(r)
	var (
		cfg      *Email
		business string
		to       string
		custom   bool
	)
	err := a.ro(r.Context(), t, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var userEmail string
		if err := tx.QueryRow(ctx, `SELECT email::text FROM users WHERE id = $1`, p.UserID).Scan(&userEmail); err != nil {
			return err
		}
		to = userEmail
		if in.To != nil && strings.TrimSpace(*in.To) != "" {
			to = strings.TrimSpace(*in.To)
			if !mailer.ValidEmail(to) {
				return apperr.Invalid("to", "Enter a valid email address.")
			}
			custom = !strings.EqualFold(to, userEmail)
		}
		var all, customN int
		if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE (after->>'custom')::boolean)
			FROM audit_log WHERE action = 'smtp.test' AND at > now() - interval '1 hour'`).Scan(&all, &customN); err != nil {
			return err
		}
		if all >= testsPerHour || (custom && customN >= customTestsPerHour) {
			return apperr.RateLimited(apperr.CodeRateLimited, time.Hour)
		}
		if err := tx.QueryRow(ctx, `SELECT business_name FROM settings WHERE id = 1`).Scan(&business); err != nil {
			return err
		}
		var err error
		if cfg, err = LoadEmail(ctx, tx, a.d.Envelope); errors.Is(err, ErrUndecryptable) {
			return apperr.Conflict(apperr.CodeInvalidState, "The saved SMTP password cannot be used. Enter it again.")
		} else if err != nil {
			return err
		}
		if cfg == nil {
			return apperr.Invalid("smtp", "Save the SMTP settings first.")
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	from := cfg.FromName
	if from == "" {
		from = business
	}
	msg := mailer.Message{
		From:      mailer.Address{Name: mailer.SanitizeName(from), Email: cfg.FromEmail},
		To:        []mailer.Address{{Email: to}},
		Subject:   "SMTP test from " + mailer.SanitizeName(business),
		TextBody:  "This is a test message. Your SMTP settings work, so receipts will be sent from this mailbox.\n",
		MessageID: "smtp-test." + uuid.NewString() + "@" + a.d.MailDomain,
	}
	sender := mailer.NewSMTP(cfg.Server, mailer.Options{Guard: a.d.Guard, RootCAs: a.d.RootCAs, HELO: a.d.MailDomain, SendTimeout: testTimeout})
	ctx, cancel := context.WithTimeout(r.Context(), testTimeout)
	_, sendErr := sender.Send(ctx, msg)
	cancel()

	res := TestResult{OK: sendErr == nil, Status: "ok", Message: "Test email sent."}
	var code *string
	if sendErr != nil {
		res.Status, res.ErrorClass = "failing", mailer.Class(sendErr)
		var me *mailer.Error
		if errors.As(sendErr, &me) {
			res.ErrorCode, res.Message = me.Code, me.Detail
		} else {
			res.ErrorCode, res.Message = "invalid_message", "The message could not be built from these settings."
		}
		code = &res.ErrorCode
	}
	_, domain, _ := strings.Cut(to, "@")
	err = a.rw(context.WithoutCancel(r.Context()), t, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if sendErr == nil {
			_, err := tx.Exec(ctx, `UPDATE tenant_email_settings SET status = 'ok', last_test_at = now(), last_success_at = now(),
				last_error_code = NULL, consecutive_failures = 0 WHERE id = 1`)
			if err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE tenant_email_settings SET status = 'failing', last_test_at = now(),
				last_error_code = $1 WHERE id = 1`, code); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "smtp.test", Entity: "email_settings", EntityID: "1",
			After:     map[string]any{"ok": res.OK, "error_code": res.ErrorCode, "to_domain": domain, "custom": custom},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}
