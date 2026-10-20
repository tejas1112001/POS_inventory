// Package worker runs the background jobs (EMAIL_AND_RECEIPTS §4.3,
// OPERATIONS §5): tenant and platform mail delivery from the outboxes,
// receipt pre-rendering, and maintenance (mail sweep, expired POS locks,
// idempotency keys, nightly ledger check). Jobs run on River with the
// pos_jobs role; every tenant read happens in that tenant's own session.
package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/outbox"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Delivery tuning (EMAIL_AND_RECEIPTS §4.3).
const (
	batchSize         = 20
	maxBatchesPerRun  = 10
	leaseFor          = 2 * time.Minute
	backoffBase       = 30 * time.Second
	backoffCap        = 6 * time.Hour
	attachAfterTries  = 3 // after this many attempts a receipt goes out without the PDF
	tenantFailoverAt  = 3 // consecutive transient tenant-SMTP failures before falling back
	platformRetryWait = time.Hour
)

// MailConfig wires the mail jobs.
type MailConfig struct {
	Jobs         *tenancy.Sessions // pos_jobs pool
	Registry     *tenancy.Registry
	Envelope     *secrets.Envelope
	Receipts     *receipts.Service // on the jobs pool
	Plans        *plans.Service
	Platform     mailer.Sender                     // nil: platform SMTP not configured
	TenantSender func(mailer.Server) mailer.Sender // builds a guarded sender for tenant SMTP
	OwnerInvite  func(context.Context, tenancy.Tenant) (string, time.Time, error)
	PublicScheme string
	BaseDomain   string
	PlatformName string
	MailFrom     string
	MailDomain   string
	WorkerID     string
	Metrics      *obs.Metrics
	Log          *slog.Logger
}

// Mailer delivers outbox rows.
type Mailer struct{ c MailConfig }

// NewMailer creates the mail deliverer.
func NewMailer(c MailConfig) *Mailer {
	if c.WorkerID == "" {
		c.WorkerID = "w-" + uuid.NewString()[:8]
	}
	return &Mailer{c: c}
}

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Statements exist twice: tenant rows are unqualified (the session's
// search_path is the tenant schema), platform rows are schema-qualified
// (platform sessions have no platform schema on the path). They must be
// constants (constsql), so scope picks one at each call.
const (
	leaseCols = `o.id, o.idempotency_key, o.kind, o.to_addr::text, o.to_name, o.template, o.template_data,
	o.attachment_receipt_id, o.sender_policy, o.attempts, o.max_attempts`
	dueWhere = `(status IN ('pending','failed') AND next_retry_at <= now()) OR (status = 'sending' AND lease_until < now())`

	tLease = `WITH due AS (SELECT id FROM email_outbox WHERE ` + dueWhere + ` ORDER BY id LIMIT 20 FOR UPDATE SKIP LOCKED)
		UPDATE email_outbox o SET status = 'sending', lease_until = now() + interval '2 minutes', leased_by = $1, attempts = o.attempts + 1
		FROM due WHERE o.id = due.id RETURNING ` + leaseCols
	pLease = `WITH due AS (SELECT id FROM platform.email_outbox WHERE ` + dueWhere + ` ORDER BY id LIMIT 20 FOR UPDATE SKIP LOCKED)
		UPDATE platform.email_outbox o SET status = 'sending', lease_until = now() + interval '2 minutes', leased_by = $1, attempts = o.attempts + 1
		FROM due WHERE o.id = due.id RETURNING ` + leaseCols

	refreshSet = ` SET lease_until = now() + interval '2 minutes' WHERE id = $1 AND leased_by = $2 AND status = 'sending'`
	tRefresh   = `UPDATE email_outbox` + refreshSet
	pRefresh   = `UPDATE platform.email_outbox` + refreshSet

	sentSet = ` SET status = 'sent', sent_at = now(), sent_via = $3, provider_message_id = $4, lease_until = NULL,
		leased_by = NULL, last_error = NULL, last_error_class = NULL WHERE id = $1 AND leased_by = $2 AND status = 'sending'`
	tSent = `UPDATE email_outbox` + sentSet
	pSent = `UPDATE platform.email_outbox` + sentSet

	retrySet = ` SET status = 'failed', next_retry_at = $3, last_error_class = $4, last_error = $5,
		lease_until = NULL, leased_by = NULL WHERE id = $1 AND leased_by = $2 AND status = 'sending'`
	tRetry = `UPDATE email_outbox` + retrySet
	pRetry = `UPDATE platform.email_outbox` + retrySet

	deadSet = ` SET status = 'dead', last_error_class = $3, last_error = $4, lease_until = NULL, leased_by = NULL
		WHERE id = $1 AND leased_by = $2 AND status = 'sending'`
	tDead = `UPDATE email_outbox` + deadSet
	pDead = `UPDATE platform.email_outbox` + deadSet

	cancelSet = ` SET status = 'cancelled', last_error = $3, lease_until = NULL, leased_by = NULL
		WHERE id = $1 AND leased_by = $2 AND status = 'sending'`
	tCancel = `UPDATE email_outbox` + cancelSet
	pCancel = `UPDATE platform.email_outbox` + cancelSet

	tNextDue = `SELECT min(CASE WHEN status = 'sending' THEN lease_until ELSE next_retry_at END) FROM email_outbox
		WHERE status IN ('pending','failed','sending')`
)

// scope selects the tenant or the platform outbox statements.
type scope bool

const (
	tenantScope   scope = false
	platformScope scope = true
)

type row struct {
	id                    int64
	key, kind, to         string
	toName                *string
	template              string
	data                  []byte
	receiptID             *int64
	policy                string
	attempts, maxAttempts int
}

func lease(ctx context.Context, q querier, s scope, workerID string) ([]row, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if s == platformScope {
		rows, err = q.Query(ctx, pLease, workerID)
	} else {
		rows, err = q.Query(ctx, tLease, workerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key, &r.kind, &r.to, &r.toName, &r.template, &r.data, &r.receiptID, &r.policy,
			&r.attempts, &r.maxAttempts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// outcome is what happens to a row after one attempt.
type outcome struct {
	status     string // sent, retry, dead, cancel
	via        string
	providerID string
	class      string
	detail     string
	retryAt    time.Time
}

func backoff(attempt int) time.Duration {
	d := backoffCap
	if attempt < 20 {
		if b := backoffBase << attempt; b < backoffCap {
			d = b
		}
	}
	return d + time.Duration(rand.Int64N(int64(d/10)+1)) // #nosec G404 -- jitter, not security
}

func errOutcome(r row, err error) outcome {
	class := mailer.Class(err)
	detail := "send failed"
	var me *mailer.Error
	if errors.As(err, &me) {
		detail = me.Code + ": " + me.Detail
	} else if errors.Is(err, mailer.ErrInvalid) {
		detail = "invalid message"
	}
	if len(detail) > 500 {
		detail = detail[:500]
	}
	if class == "permanent" || r.attempts >= r.maxAttempts {
		return outcome{status: "dead", class: class, detail: detail}
	}
	return outcome{status: "retry", class: class, detail: detail, retryAt: time.Now().Add(backoff(r.attempts))}
}

func (m *Mailer) finish(ctx context.Context, q querier, s scope, r row, o outcome, kind string) error {
	p := s == platformScope
	var err error
	switch o.status {
	case "sent":
		if p {
			_, err = q.Exec(ctx, pSent, r.id, m.c.WorkerID, o.via, o.providerID)
		} else {
			_, err = q.Exec(ctx, tSent, r.id, m.c.WorkerID, o.via, o.providerID)
		}
		m.c.Metrics.EmailSent.WithLabelValues(o.via, kind).Inc()
	case "retry":
		if p {
			_, err = q.Exec(ctx, pRetry, r.id, m.c.WorkerID, o.retryAt, o.class, o.detail)
		} else {
			_, err = q.Exec(ctx, tRetry, r.id, m.c.WorkerID, o.retryAt, o.class, o.detail)
		}
		m.c.Metrics.EmailFailed.WithLabelValues(o.class).Inc()
	case "dead":
		if p {
			_, err = q.Exec(ctx, pDead, r.id, m.c.WorkerID, o.class, o.detail)
		} else {
			_, err = q.Exec(ctx, tDead, r.id, m.c.WorkerID, o.class, o.detail)
		}
		m.c.Metrics.EmailFailed.WithLabelValues(o.class).Inc()
		m.c.Metrics.EmailDead.Inc()
	default:
		if p {
			_, err = q.Exec(ctx, pCancel, r.id, m.c.WorkerID, o.detail)
		} else {
			_, err = q.Exec(ctx, tCancel, r.id, m.c.WorkerID, o.detail)
		}
	}
	return err
}

func domainOf(addr string) string {
	_, d, _ := strings.Cut(addr, "@")
	return d
}

var keySafe = regexp.MustCompile(`[^A-Za-z0-9._:+-]`)

func (m *Mailer) messageID(scope, key string) string {
	return keySafe.ReplaceAllString(scope+"."+key, "-") + "@" + m.c.MailDomain
}

// ------------------------------------------------------------- tenant mail

// tenantState is loaded once per batch, in the tenant's session.
type tenantState struct {
	t           tenancy.Tenant
	biz         settings.Business
	smtp        *settings.Email
	smtpErr     error
	sentTenant  int
	sentPlat    int
	platformCap int64
}

// content is a message body that does not depend on the sender.
type content struct {
	to          mailer.Address
	rendered    mailtmpl.Rendered
	attachments []mailer.Attachment
}

// DeliverTenant sends one tenant's due outbox rows. It handles exactly one
// tenant and reads only through that tenant's session.
func (m *Mailer) DeliverTenant(ctx context.Context, tenantID string) error {
	t, err := m.c.Registry.ByID(ctx, tenantID)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Kind == apperr.KindNotFound {
			return m.clearDispatch(ctx, tenantID)
		}
		return err
	}
	for range maxBatchesPerRun {
		var rows []row
		if err := m.c.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			var err error
			rows, err = lease(ctx, tx, tenantScope, m.c.WorkerID)
			return err
		}); err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		st, err := m.loadTenant(ctx, t)
		if err != nil {
			return err // leases expire; the rows are retried
		}
		for _, r := range rows {
			o := m.deliverTenantRow(ctx, st, r)
			if err := m.c.Jobs.InTenantTx(context.WithoutCancel(ctx), t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
				return m.finish(ctx, tx, tenantScope, r, o, r.kind)
			}); err != nil {
				return err
			}
			m.c.Log.InfoContext(ctx, "email processed", slog.String("tenant_id", t.ID), slog.Int64("outbox_id", r.id),
				slog.String("kind", r.kind), slog.String("status", o.status), slog.String("via", o.via),
				slog.String("error_class", o.class), slog.Int("attempt", r.attempts), slog.String("to_domain", domainOf(r.to)))
		}
	}
	return m.settleDispatch(ctx, t)
}

// settleDispatch removes the tenant's dispatch mark, then re-checks the
// outbox and puts the mark back if anything is still waiting. Deleting
// first means a row committed meanwhile re-marks the tenant itself (its
// trigger runs after our delete), so no mail is stranded.
func (m *Mailer) settleDispatch(ctx context.Context, t tenancy.Tenant) error {
	if err := m.clearDispatch(ctx, t.ID); err != nil {
		return err
	}
	var next *time.Time
	if err := m.c.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(ctx, tNextDue).Scan(&next)
	}); err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	return m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tx.Exec(ctx, `INSERT INTO platform.mail_dispatch (tenant_id, next_attempt_at) VALUES ($1, $2)
			ON CONFLICT (tenant_id) DO UPDATE SET next_attempt_at = LEAST(platform.mail_dispatch.next_attempt_at, EXCLUDED.next_attempt_at)`,
			t.ID, *next)
		return err
	})
}

func (m *Mailer) clearDispatch(ctx context.Context, tenantID string) error {
	return m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tx.Exec(ctx, `DELETE FROM platform.mail_dispatch WHERE tenant_id = $1`, tenantID)
		return err
	})
}

const qSentToday = `SELECT count(*) FILTER (WHERE sent_via = 'tenant'), count(*) FILTER (WHERE sent_via = 'platform')
	FROM email_outbox WHERE status = 'sent' AND sent_at > now() - interval '24 hours'`

func (m *Mailer) loadTenant(ctx context.Context, t tenancy.Tenant) (*tenantState, error) {
	st := &tenantState{t: t}
	err := m.c.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		var err error
		if st.biz, err = settings.LoadBusiness(ctx, tx); err != nil {
			return err
		}
		st.smtp, st.smtpErr = settings.LoadEmail(ctx, tx, m.c.Envelope)
		return tx.QueryRow(ctx, qSentToday).Scan(&st.sentTenant, &st.sentPlat)
	})
	if err != nil {
		return nil, err
	}
	limits, err := m.c.Plans.For(ctx, t.PlanID)
	if err != nil {
		return nil, err
	}
	st.platformCap = limits.MaxEmailsPerDay
	return st, nil
}

func (m *Mailer) refresh(ctx context.Context, t tenancy.Tenant, r row) bool {
	var ok bool
	err := m.c.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		tag, err := tx.Exec(ctx, tRefresh, r.id, m.c.WorkerID)
		ok = tag.RowsAffected() == 1
		return err
	})
	return err == nil && ok
}

func (m *Mailer) deliverTenantRow(ctx context.Context, st *tenantState, r row) outcome {
	if r.attempts > r.maxAttempts {
		return outcome{status: "dead", class: "transient", detail: "too many attempts"}
	}
	c, o := m.buildTenant(ctx, st, r)
	if o != nil {
		return *o
	}
	if !m.refresh(ctx, st.t, r) {
		return outcome{status: "cancel", detail: "lease lost"}
	}
	msg := mailer.Message{To: []mailer.Address{c.to}, Subject: c.rendered.Subject, TextBody: c.rendered.Text,
		HTMLBody: c.rendered.HTML, Attachments: c.attachments, MessageID: m.messageID(st.t.ID, r.key)}

	useTenant := r.policy == outbox.PolicyTenantThenPlatform && st.smtpErr == nil && st.smtp != nil &&
		st.smtp.Enabled && st.smtp.Status == "ok" && st.sentTenant < st.smtp.DailyCap
	if useTenant {
		name := st.smtp.FromName
		if name == "" {
			name = st.biz.Name
		}
		msg.From = mailer.Address{Name: mailer.SanitizeName(name), Email: st.smtp.FromEmail}
		id, err := m.c.TenantSender(st.smtp.Server).Send(ctx, msg)
		m.recordTenantSMTP(ctx, st, err)
		if err == nil {
			st.sentTenant++
			return outcome{status: "sent", via: "tenant", providerID: id}
		}
		fallback := errors.Is(err, mailer.ErrAuth) || errors.Is(err, mailer.ErrQuota) ||
			(errors.Is(err, mailer.ErrTransient) && st.smtp.ConsecutiveFailures+1 >= tenantFailoverAt)
		if !fallback || !st.smtp.Fallback {
			return errOutcome(r, err)
		}
	} else if r.policy == outbox.PolicyTenantThenPlatform && st.smtp != nil && st.smtpErr == nil && !st.smtp.Fallback {
		// The tenant asked for its own mailbox only, and it is not usable.
		return outcome{status: "retry", class: "auth", detail: "tenant SMTP is not ready and fallback is off",
			retryAt: time.Now().Add(platformRetryWait)}
	}
	return m.sendPlatform(ctx, st, r, msg)
}

// sendPlatform sends tenant mail through the platform identity:
// "<Business> via <Platform>" <MAIL_FROM_ADDRESS>, Reply-To the business.
func (m *Mailer) sendPlatform(ctx context.Context, st *tenantState, r row, msg mailer.Message) outcome {
	if m.c.Platform == nil {
		return outcome{status: "retry", class: "transient", detail: "platform SMTP is not configured", retryAt: time.Now().Add(platformRetryWait)}
	}
	if st.platformCap > 0 && int64(st.sentPlat) >= st.platformCap {
		return outcome{status: "retry", class: "quota", detail: "platform email limit of the plan reached for today", retryAt: time.Now().Add(platformRetryWait)}
	}
	msg.From = mailer.Address{Name: mailer.SanitizeName(st.biz.Name + " via " + m.c.PlatformName), Email: m.c.MailFrom}
	if st.biz.Email != nil && mailer.ValidEmail(*st.biz.Email) {
		msg.ReplyTo = mailer.Address{Name: mailer.SanitizeName(st.biz.Name), Email: *st.biz.Email}
	}
	id, err := m.c.Platform.Send(ctx, msg)
	if err != nil {
		return errOutcome(r, err)
	}
	st.sentPlat++
	return outcome{status: "sent", via: "platform", providerID: id}
}

// recordTenantSMTP keeps the tenant's SMTP status current: success clears
// failures, an auth error marks it failing, other errors count up.
func (m *Mailer) recordTenantSMTP(ctx context.Context, st *tenantState, sendErr error) {
	const (
		qOK      = `UPDATE tenant_email_settings SET status = 'ok', last_success_at = now(), consecutive_failures = 0, last_error_code = NULL WHERE id = 1`
		qFailing = `UPDATE tenant_email_settings SET status = 'failing', consecutive_failures = consecutive_failures + 1, last_error_code = $1 WHERE id = 1`
		qCount   = `UPDATE tenant_email_settings SET consecutive_failures = consecutive_failures + 1, last_error_code = $1 WHERE id = 1`
	)
	if errors.Is(sendErr, mailer.ErrPermanent) || errors.Is(sendErr, mailer.ErrInvalid) {
		return // the recipient or the message, not the server
	}
	err := m.c.Jobs.InTenantTx(context.WithoutCancel(ctx), st.t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		var err error
		switch {
		case sendErr == nil:
			_, err = tx.Exec(ctx, qOK)
		case errors.Is(sendErr, mailer.ErrAuth):
			_, err = tx.Exec(ctx, qFailing, errCode(sendErr))
		default:
			_, err = tx.Exec(ctx, qCount, errCode(sendErr))
		}
		return err
	})
	switch {
	case sendErr == nil:
		st.smtp.ConsecutiveFailures = 0
	case errors.Is(sendErr, mailer.ErrAuth):
		st.smtp.Status = "failing"
		st.smtp.ConsecutiveFailures++
	default:
		st.smtp.ConsecutiveFailures++
	}
	if err != nil {
		m.c.Log.WarnContext(ctx, "smtp status not updated", slog.String("tenant_id", st.t.ID), slog.Any("err", err))
	}
}

func errCode(err error) string {
	var me *mailer.Error
	if errors.As(err, &me) {
		return me.Code
	}
	return "error"
}

func (m *Mailer) tenantURL(t tenancy.Tenant) string {
	return m.c.PublicScheme + "://" + t.Slug + "." + m.c.BaseDomain
}

func humanize(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= time.Hour:
		return "1 hour"
	default:
		return fmt.Sprintf("%d minutes", max(1, int(d.Minutes())))
	}
}

// buildTenant renders a tenant outbox row. A non-nil outcome means the row
// is finished without sending (cancelled, dead or retried).
func (m *Mailer) buildTenant(ctx context.Context, st *tenantState, r row) (content, *outcome) {
	name := ""
	if r.toName != nil {
		name = *r.toName
	}
	c := content{to: mailer.Address{Name: mailer.SanitizeName(name), Email: r.to}}
	var err error
	switch r.template {
	case "receipt":
		err = m.buildReceipt(ctx, st, r, &c)
	case "password_reset", "user_invite":
		var o *outcome
		if o, err = m.buildLink(ctx, st, r, &c); o != nil {
			return c, o
		}
	default:
		return c, &outcome{status: "dead", class: "permanent", detail: "unknown template " + keySafe.ReplaceAllString(r.template, "?")}
	}
	if err != nil {
		var o outcome
		switch {
		case errors.Is(err, errCancel):
			o = outcome{status: "cancel", detail: err.Error()}
		case r.attempts >= r.maxAttempts:
			o = outcome{status: "dead", class: "transient", detail: "could not build the message"}
		default:
			o = outcome{status: "retry", class: "transient", detail: "could not build the message", retryAt: time.Now().Add(backoff(r.attempts))}
		}
		m.c.Log.WarnContext(ctx, "email not built", slog.String("tenant_id", st.t.ID), slog.Int64("outbox_id", r.id), slog.Any("err", err))
		return c, &o
	}
	return c, nil
}

var errCancel = errors.New("no longer needed")

type receiptData struct {
	SaleID    int64 `json:"sale_id"`
	ReceiptID int64 `json:"receipt_id"`
}

func (m *Mailer) buildReceipt(ctx context.Context, st *tenantState, r row, c *content) error {
	var d receiptData
	if err := json.Unmarshal(r.data, &d); err != nil || d.SaleID <= 0 {
		return fmt.Errorf("%w: bad receipt data", errCancel)
	}
	// Always the sale's current version: an edit after the sale changes
	// what the customer should receive.
	var rid int64
	if err := m.c.Jobs.InTenantTx(ctx, st.t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		var version int
		if err := tx.QueryRow(ctx, `SELECT version FROM sales WHERE id = $1`, d.SaleID).Scan(&version); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: sale missing", errCancel)
			}
			return err
		}
		var err error
		rid, err = receipts.Create(ctx, tx, d.SaleID, version)
		return err
	}); err != nil {
		return err
	}
	var v receipts.View
	if err := m.c.Jobs.InTenantTx(ctx, st.t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		var err error
		v, err = receipts.Load(ctx, tx, rid)
		return err
	}); err != nil {
		return err
	}
	pdf, perr := m.c.Receipts.Open(ctx, st.t, rid)
	if perr != nil && r.attempts < attachAfterTries {
		return perr
	}
	logo := ""
	if st.biz.LogoFileID != nil {
		logo = m.tenantURL(st.t) + files.URL(*st.biz.LogoFileID, files.PurposeTenantLogo)
	}
	rendered, err := mailtmpl.RenderReceipt(mailtmpl.Receipt{
		BusinessName: st.biz.Name, LogoURL: logo, CustomerName: c.to.Name, Reference: v.Reference, Date: v.Date,
		Total: v.Total, StoreName: v.StoreName, StoreAddress: v.StoreAddress, StorePhone: v.StorePhone,
		Header: v.Header, Footer: v.Footer, Attached: perr == nil, Void: v.Void,
	})
	if err != nil {
		return err
	}
	c.rendered = rendered
	if perr == nil {
		c.attachments = []mailer.Attachment{{Name: keySafe.ReplaceAllString(v.Reference, "_") + ".pdf", ContentType: "application/pdf", Data: pdf}}
	}
	return nil
}

type linkData struct {
	AuthTokenID int64             `json:"auth_token_id"`
	LinkPath    string            `json:"link_path"`
	Sealed      map[string]string `json:"token_sealed"`
}

var linkPaths = map[string]bool{"/reset-password": true, "/accept-invite": true}

// buildLink renders a password reset or invite. The raw token is opened
// from the envelope only here, and only if the token is still usable.
func (m *Mailer) buildLink(ctx context.Context, st *tenantState, r row, c *content) (*outcome, error) {
	var d linkData
	if err := json.Unmarshal(r.data, &d); err != nil || d.AuthTokenID <= 0 || !linkPaths[d.LinkPath] {
		return &outcome{status: "dead", class: "permanent", detail: "bad link data"}, nil
	}
	var (
		usable  bool
		expires time.Time
	)
	err := m.c.Jobs.InTenantTx(ctx, st.t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(ctx, `SELECT used_at IS NULL AND expires_at > now(), expires_at FROM auth_tokens WHERE id = $1`, d.AuthTokenID).
			Scan(&usable, &expires)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !usable) {
		return &outcome{status: "cancel", detail: "token used or expired"}, nil
	}
	if err != nil {
		return nil, err
	}
	ct, err1 := base64.StdEncoding.DecodeString(d.Sealed["ct"])
	dek, err2 := base64.StdEncoding.DecodeString(d.Sealed["dek"])
	if err1 != nil || err2 != nil {
		return &outcome{status: "dead", class: "permanent", detail: "bad token envelope"}, nil
	}
	raw, err := m.c.Envelope.Open(secrets.Sealed{Ciphertext: ct, WrappedDEK: dek, KeyID: d.Sealed["kid"]},
		fmt.Sprintf("tenant:%s|auth_token:%d", st.t.ID, d.AuthTokenID))
	if err != nil {
		return &outcome{status: "dead", class: "permanent", detail: "token envelope does not open"}, nil
	}
	data := mailtmpl.Link{Name: c.to.Name, BusinessName: st.biz.Name, PlatformName: m.c.PlatformName,
		Link: m.tenantURL(st.t) + d.LinkPath + "#token=" + string(raw), Expires: humanize(time.Until(expires))}
	if r.template == "password_reset" {
		c.rendered, err = mailtmpl.RenderPasswordReset(data)
	} else {
		c.rendered, err = mailtmpl.RenderUserInvite(data)
	}
	return nil, err
}

// ----------------------------------------------------------- platform mail

// DeliverPlatform sends due rows from platform.email_outbox.
func (m *Mailer) DeliverPlatform(ctx context.Context) error {
	for range maxBatchesPerRun {
		var rows []row
		if err := m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			var err error
			rows, err = lease(ctx, tx, platformScope, m.c.WorkerID)
			return err
		}); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, r := range rows {
			o := m.deliverPlatformRow(ctx, r)
			if err := m.c.Jobs.InPlatformTx(context.WithoutCancel(ctx), tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
				return m.finish(ctx, tx, platformScope, r, o, r.template)
			}); err != nil {
				return err
			}
			m.c.Log.InfoContext(ctx, "platform email processed", slog.Int64("outbox_id", r.id), slog.String("template", r.template),
				slog.String("status", o.status), slog.String("error_class", o.class), slog.Int("attempt", r.attempts),
				slog.String("to_domain", domainOf(r.to)))
		}
	}
	return nil
}

type welcomeData struct {
	TenantID  string `json:"tenant_id"`
	TenantURL string `json:"tenant_url"`
}

func (m *Mailer) deliverPlatformRow(ctx context.Context, r row) outcome {
	if r.attempts > r.maxAttempts {
		return outcome{status: "dead", class: "transient", detail: "too many attempts"}
	}
	if m.c.Platform == nil {
		return outcome{status: "retry", class: "transient", detail: "platform SMTP is not configured", retryAt: time.Now().Add(platformRetryWait)}
	}
	if r.template != "tenant_welcome" {
		return outcome{status: "dead", class: "permanent", detail: "unknown template " + keySafe.ReplaceAllString(r.template, "?")}
	}
	var d welcomeData
	if err := json.Unmarshal(r.data, &d); err != nil {
		return outcome{status: "dead", class: "permanent", detail: "bad welcome data"}
	}
	t, err := m.c.Registry.ByID(ctx, d.TenantID)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Kind == apperr.KindNotFound {
			return outcome{status: "cancel", detail: "tenant not active"}
		}
		return outcome{status: "retry", class: "transient", detail: "registry lookup failed", retryAt: time.Now().Add(backoff(r.attempts))}
	}
	data := mailtmpl.Welcome{TenantName: t.Name, TenantURL: m.tenantURL(t), PlatformName: m.c.PlatformName}
	if r.toName != nil {
		data.Name = *r.toName
	}
	if m.c.OwnerInvite != nil {
		link, exp, err := m.c.OwnerInvite(ctx, t)
		switch {
		case err == nil:
			data.Link, data.Expires = link, humanize(time.Until(exp))
		case isConflict(err):
			// The owner already has a password: a plain welcome.
		default:
			return outcome{status: "retry", class: "transient", detail: "owner invite failed", retryAt: time.Now().Add(backoff(r.attempts))}
		}
	}
	rendered, err := mailtmpl.RenderWelcome(data)
	if err != nil {
		return outcome{status: "dead", class: "permanent", detail: "template failed"}
	}
	name := ""
	if r.toName != nil {
		name = *r.toName
	}
	if !refreshPlatform(ctx, m, r) {
		return outcome{status: "cancel", detail: "lease lost"}
	}
	msg := mailer.Message{
		From:    mailer.Address{Name: mailer.SanitizeName(m.c.PlatformName), Email: m.c.MailFrom},
		To:      []mailer.Address{{Name: mailer.SanitizeName(name), Email: r.to}},
		Subject: rendered.Subject, TextBody: rendered.Text, HTMLBody: rendered.HTML,
		MessageID: m.messageID("platform", r.key),
	}
	id, err := m.c.Platform.Send(ctx, msg)
	if err != nil {
		return errOutcome(r, err)
	}
	return outcome{status: "sent", via: "platform", providerID: id}
}

func refreshPlatform(ctx context.Context, m *Mailer, r row) bool {
	var ok bool
	err := m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		tag, err := tx.Exec(ctx, pRefresh, r.id, m.c.WorkerID)
		ok = tag.RowsAffected() == 1
		return err
	})
	return err == nil && ok
}

func isConflict(err error) bool {
	e, ok := apperr.As(err)
	return ok && e.Kind == apperr.KindConflict
}
