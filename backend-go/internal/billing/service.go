package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Service processes provider webhooks and runs the lifecycle jobs
// (BILLING_DESIGN §4, §6). It works in platform transactions only.
type Service struct {
	Sessions *tenancy.Sessions
	Provider Provider // nil: billing is off (BILLING_PROVIDER=none)
	Cfg      Config
	MaxAge   time.Duration // webhook staleness window (72 h)
	Metrics  *obs.Metrics  // optional
	Log      *slog.Logger
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) count(outcome string) {
	if s.Metrics != nil {
		s.Metrics.BillingEvents.WithLabelValues(outcome).Inc()
	}
}

// Outcomes beyond Apply's (billing_events.outcome; duplicate is not stored).
const (
	OutcomeStale       = "stale"
	OutcomeUnknownSub  = "unknown_subscription"
	OutcomeRejected    = "rejected"
	OutcomeDuplicate   = "duplicate"
	providerSystemUser = "system:"
)

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

const qInsertEvent = `INSERT INTO platform.billing_events (provider, event_id, event_type, provider_created_at, signature_valid,
		payload, processed_at, outcome, error)
	VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''))
	ON CONFLICT (provider, event_id) WHERE signature_valid DO NOTHING`

// HandleWebhook verifies, stores, dedupes and applies one delivery
// (BILLING_DESIGN §4 steps 2–7) and returns the HTTP status to answer.
// 400 for a bad signature; 200 once an event is stored and handled, also
// for duplicates and events that are not applied; 500 when processing
// failed, so the provider retries and the stored event is processed then.
func (s *Service) HandleWebhook(ctx context.Context, raw []byte, h http.Header) (int, string) {
	if s.Provider == nil {
		return http.StatusNotFound, "billing off"
	}
	prov := s.Provider.Name()
	now := s.now()
	ev, err := s.Provider.VerifyWebhook(raw, h)
	switch {
	case errors.Is(err, ErrSignature):
		if s.Metrics != nil {
			s.Metrics.WebhookSignatureFailures.Inc()
		}
		// Kept without payload, outside the dedupe index.
		_ = s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			_, err := tx.Exec(ctx, qInsertEvent, prov, clip(strings.TrimSpace(h.Get(HeaderEventID)), 100), "unknown", nil,
				false, nil, now, OutcomeRejected, "invalid signature")
			return err
		})
		return http.StatusBadRequest, OutcomeRejected
	case errors.Is(err, ErrPayload):
		sum := sha256.Sum256(raw)
		id := "unreadable:" + hex.EncodeToString(sum[:16])
		_ = s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			_, err := tx.Exec(ctx, qInsertEvent, prov, id, "unknown", nil, true, nil, now, OutcomeIgnored, "unreadable payload")
			return err
		})
		s.count(OutcomeIgnored)
		return http.StatusOK, OutcomeIgnored
	case err != nil:
		return http.StatusInternalServerError, "error"
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = now // R5: if created_at is ever missing, order by arrival
	}
	// The stored payload is our parsed event: no customer emails, phones
	// or card data from the provider's entities.
	payload, err := json.Marshal(ev)
	if err != nil {
		return http.StatusInternalServerError, "error"
	}
	// 1. Store first, in its own transaction, so a failed apply leaves the
	// event for the provider's retry.
	if err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tx.Exec(ctx, qInsertEvent, prov, ev.ID, clip(string(ev.Type), 100), ev.CreatedAt, true, string(payload), nil, "", "")
		return err
	}); err != nil {
		s.log(ctx, "billing event not stored", ev, "", err)
		return http.StatusInternalServerError, "error"
	}
	// 2. Process under the event row's lock: a concurrent duplicate waits
	// and then sees it processed.
	var outcome string
	err = s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var id int64
		var processed *time.Time
		if err := tx.QueryRow(ctx, `SELECT id, processed_at FROM platform.billing_events
			WHERE provider = $1 AND event_id = $2 AND signature_valid FOR UPDATE`, prov, ev.ID).Scan(&id, &processed); err != nil {
			return err
		}
		if processed != nil {
			outcome = OutcomeDuplicate
			return nil
		}
		var tenantID string
		var err error
		outcome, tenantID, err = s.apply(ctx, tx, ev, now)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE platform.billing_events SET processed_at = now(), outcome = $2, tenant_id = NULLIF($3, ''), error = NULL
			WHERE id = $1`, id, outcome, tenantID)
		return err
	})
	if err != nil {
		s.log(ctx, "billing event failed; the provider will retry", ev, "", err)
		return http.StatusInternalServerError, "error"
	}
	s.count(outcome)
	s.log(ctx, "billing event", ev, outcome, nil)
	return http.StatusOK, outcome
}

func (s *Service) log(ctx context.Context, msg string, ev Event, outcome string, err error) {
	if s.Log == nil {
		return
	}
	attrs := []any{slog.String("event_id", ev.ID), slog.String("event_type", string(ev.Type)), slog.String("outcome", outcome)}
	if err != nil {
		s.Log.ErrorContext(ctx, msg, append(attrs, slog.Any("err", err))...)
		return
	}
	s.Log.InfoContext(ctx, msg, attrs...)
}

// subRow is a locked subscription.
type subRow struct {
	id       int64
	tenantID string
	planID   int64
	Sub
}

const subCols = `id, tenant_id, plan_id, status, coalesce(provider_subscription_id, ''), coalesce(provider_customer_id, ''),
	coalesce(provider_status, ''), trial_end, current_period_start, current_period_end, grace_end, cancel_at_period_end,
	cancelled_at, last_event_at`

const (
	qLockSubByProvider = `SELECT ` + subCols + ` FROM platform.subscriptions WHERE provider_subscription_id = $1 FOR UPDATE`
	qLockSubByTenant   = `SELECT ` + subCols + ` FROM platform.subscriptions WHERE tenant_id = $1 FOR UPDATE`
)

func scanSub(row pgx.Row) (subRow, error) {
	var r subRow
	var status string
	err := row.Scan(&r.id, &r.tenantID, &r.planID, &status, &r.ProviderSubID, &r.ProviderCustomerID, &r.ProviderStatus,
		&r.TrialEnd, &r.PeriodStart, &r.PeriodEnd, &r.GraceEnd, &r.CancelAtPeriodEnd, &r.CancelledAt, &r.LastEventAt)
	r.Status = Status(status)
	return r, err
}

// lockSub finds the subscription an event is about: by provider
// subscription ID, else (before the ID is stored) by notes.tenant_id.
func lockSub(ctx context.Context, tx tenancy.PlatformTx, ev Event) (subRow, bool, error) {
	if ev.SubscriptionID != "" {
		r, err := scanSub(tx.QueryRow(ctx, qLockSubByProvider, ev.SubscriptionID))
		if err == nil {
			return r, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return r, false, err
		}
	}
	if !tenancy.ValidTenantID(ev.TenantID) {
		return subRow{}, false, nil
	}
	r, err := scanSub(tx.QueryRow(ctx, qLockSubByTenant, ev.TenantID))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && r.ProviderSubID != "" && r.ProviderSubID != ev.SubscriptionID) {
		return subRow{}, false, nil
	}
	return r, err == nil, err
}

const qSaveSub = `UPDATE platform.subscriptions SET status = $2, provider_subscription_id = NULLIF($3, ''),
		provider_customer_id = NULLIF($4, ''), provider_status = NULLIF($5, ''), trial_end = $6, current_period_start = $7,
		current_period_end = $8, grace_end = $9, cancel_at_period_end = $10, cancelled_at = $11, last_event_at = $12,
		plan_id = coalesce((SELECT p.id FROM platform.plans p WHERE p.razorpay_plan_id = NULLIF($13, '')), plan_id),
		updated_at = now()
	WHERE id = $1 RETURNING plan_id`

func saveSub(ctx context.Context, tx tenancy.PlatformTx, r subRow, s Sub) (int64, error) {
	var plan int64
	err := tx.QueryRow(ctx, qSaveSub, r.id, string(s.Status), s.ProviderSubID, s.ProviderCustomerID, s.ProviderStatus,
		s.TrialEnd, s.PeriodStart, s.PeriodEnd, s.GraceEnd, s.CancelAtPeriodEnd, s.CancelledAt, s.LastEventAt,
		s.ProviderPlanID).Scan(&plan)
	return plan, err
}

// syncTenant gives the tenant the status its subscription implies and
// the subscription's plan. It never touches tenants being provisioned,
// failed, pending deletion or deleted, and never lifts a Super Admin's
// suspension (only billing suspensions come and go with payments).
func syncTenant(ctx context.Context, tx tenancy.PlatformTx, tenantID string, s Status, planID int64) (string, string, error) {
	var before string
	var source *string
	if err := tx.QueryRow(ctx, `SELECT status, suspension_source FROM platform.tenants WHERE id = $1 FOR UPDATE`, tenantID).
		Scan(&before, &source); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.tenants SET plan_id = $2, updated_at = now() WHERE id = $1 AND plan_id IS DISTINCT FROM $2`,
		tenantID, planID); err != nil {
		return "", "", err
	}
	after := TenantStatus(s)
	switch {
	case before == "provisioning", before == "failed", before == "pending_deletion", before == "deleted":
		return before, before, nil
	case before == "suspended" && (source == nil || *source != "billing"):
		return before, before, nil
	case before == after:
		return before, before, nil
	}
	_, err := tx.Exec(ctx, `UPDATE platform.tenants SET status = $2,
			suspension_source = CASE WHEN $2 = 'suspended' THEN 'billing' END,
			suspended_at = CASE WHEN $2 = 'suspended' THEN now() END,
			suspend_reason = CASE WHEN $2 = 'suspended' THEN 'Payment overdue' END,
			updated_at = now()
		WHERE id = $1`, tenantID, after)
	return before, after, err
}

const qUpsertInvoice = `INSERT INTO platform.invoices (tenant_id, subscription_id, provider, provider_invoice_id, amount_minor,
		amount_paid_minor, currency, status, issued_at, paid_at, short_url)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''))
	ON CONFLICT (provider, provider_invoice_id) DO UPDATE SET
		status = CASE WHEN array_position(ARRAY['issued','partially_paid','expired','cancelled','paid'], EXCLUDED.status)
			>= array_position(ARRAY['issued','partially_paid','expired','cancelled','paid'], platform.invoices.status)
			THEN EXCLUDED.status ELSE platform.invoices.status END,
		amount_minor = greatest(platform.invoices.amount_minor, EXCLUDED.amount_minor),
		amount_paid_minor = greatest(platform.invoices.amount_paid_minor, EXCLUDED.amount_paid_minor),
		issued_at = coalesce(platform.invoices.issued_at, EXCLUDED.issued_at),
		paid_at = coalesce(platform.invoices.paid_at, EXCLUDED.paid_at),
		short_url = coalesce(EXCLUDED.short_url, platform.invoices.short_url),
		updated_at = now()`

func (s *Service) upsertInvoice(ctx context.Context, tx tenancy.PlatformTx, r subRow, inv *Invoice) error {
	cur := inv.Currency
	if cur == "" {
		cur = "INR"
	}
	_, err := tx.Exec(ctx, qUpsertInvoice, r.tenantID, r.id, s.Provider.Name(), clip(inv.ID, 100), inv.AmountMinor,
		inv.AmountPaidMinor, cur, inv.Status, inv.IssuedAt, inv.PaidAt, clip(inv.ShortURL, 500))
	return err
}

// apply runs Apply on the locked subscription and writes the results.
func (s *Service) apply(ctx context.Context, tx tenancy.PlatformTx, ev Event, now time.Time) (string, string, error) {
	if s.MaxAge > 0 && now.Sub(ev.CreatedAt) > s.MaxAge {
		return OutcomeStale, "", nil
	}
	if !ev.Type.Known() {
		return OutcomeIgnored, "", nil
	}
	r, ok, err := lockSub(ctx, tx, ev)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return OutcomeUnknownSub, "", nil
	}
	res := Apply(r.Sub, ev, now, s.Cfg)
	if res.Outcome != OutcomeApplied {
		return res.Outcome, r.tenantID, nil
	}
	if res.Invoice != nil && res.Invoice.ID != "" {
		if err := s.upsertInvoice(ctx, tx, r, res.Invoice); err != nil {
			return "", "", err
		}
	}
	if !ev.Type.IsSubscriptionEvent() {
		return res.Outcome, r.tenantID, nil
	}
	plan, err := saveSub(ctx, tx, r, res.Sub)
	if err != nil {
		return "", "", err
	}
	before, after, err := syncTenant(ctx, tx, r.tenantID, res.Sub.Status, plan)
	if err != nil {
		return "", "", err
	}
	if err := Audit(ctx, tx, AuditEntry{Actor: providerSystemUser + s.Provider.Name(), Action: "billing." + string(ev.Type),
		Tenant: r.tenantID, Target: res.Sub.ProviderSubID,
		Before: map[string]any{"subscription": r.Status, "tenant": before},
		After:  map[string]any{"subscription": res.Sub.Status, "tenant": after, "event_id": ev.ID}}); err != nil {
		return "", "", err
	}
	for _, n := range res.Notices {
		if _, err := Notify(ctx, tx, r.tenantID, n, noticeData(res.Sub, ev)); err != nil {
			return "", "", err
		}
	}
	return res.Outcome, r.tenantID, nil
}

// noticeData is the template data a notice needs (dates and the link to
// pay); no secrets, no card data.
func noticeData(s Sub, ev Event) map[string]any {
	d := map[string]any{}
	if s.TrialEnd != nil {
		d["trial_end"] = s.TrialEnd.UTC().Format(time.RFC3339)
	}
	if s.PeriodEnd != nil {
		d["period_end"] = s.PeriodEnd.UTC().Format(time.RFC3339)
	}
	if s.GraceEnd != nil {
		d["grace_end"] = s.GraceEnd.UTC().Format(time.RFC3339)
	}
	if ev.Invoice != nil && ev.Invoice.ShortURL != "" {
		d["pay_url"] = ev.Invoice.ShortURL
	}
	return d
}

// AuditEntry is one platform_audit_log row written by billing.
type AuditEntry struct {
	Actor          string // "system:razorpay", "system:billing", or a platform user's email
	ActorID        int64
	Action, Tenant string
	Target         string
	Before, After  map[string]any
}

// Audit writes a platform audit row (BILLING_DESIGN §4 step 6, §7).
func Audit(ctx context.Context, tx tenancy.PlatformTx, e AuditEntry) error {
	before, _ := json.Marshal(e.Before)
	after, _ := json.Marshal(e.After)
	_, err := tx.Exec(ctx, `INSERT INTO platform.platform_audit_log (actor_id, actor_email, action, target_tenant, target, before, after)
		VALUES (NULLIF($1, 0), $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7)`,
		e.ActorID, e.Actor, e.Action, e.Tenant, clip(e.Target, 200), string(before), string(after))
	return err
}

// NoticeTemplate is the platform outbox template for billing notices.
const NoticeTemplate = "billing_notice"

// payLink is where an overdue tenant can pay without signing in: the
// newest unpaid invoice's link, else the subscription's checkout link
// (which one Razorpay honours for a halted subscription is R1).
const qPayLink = `SELECT coalesce(
		(SELECT short_url FROM platform.invoices WHERE tenant_id = $1 AND status IN ('issued','partially_paid')
			AND short_url IS NOT NULL ORDER BY created_at DESC LIMIT 1),
		(SELECT checkout_short_url FROM platform.subscriptions WHERE tenant_id = $1), '')`

var needsPayLink = map[string]bool{NoticePaymentFailed: true, NoticeGraceEnding: true, NoticeSuspended: true}

// Notify records a notice once per (tenant, kind, period key) and queues
// its email to the tenant's billing contact (or owner) in the same
// transaction (BILLING_DESIGN §6). It reports whether the notice is new;
// a second call is a no-op.
func Notify(ctx context.Context, tx tenancy.PlatformTx, tenantID string, n Notice, data map[string]any) (bool, error) {
	var logID int64
	err := tx.QueryRow(ctx, `INSERT INTO platform.notification_log (tenant_id, kind, period_key) VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, kind, period_key) DO NOTHING RETURNING id`, tenantID, n.Kind, clip(n.PeriodKey, 100)).Scan(&logID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // already sent
	}
	if err != nil {
		return false, err
	}
	var to, name string
	if err := tx.QueryRow(ctx, `SELECT coalesce(billing_email, owner_email)::text, name FROM platform.tenants WHERE id = $1`, tenantID).
		Scan(&to, &name); err != nil {
		return false, err
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["pay_url"]; !ok && needsPayLink[n.Kind] {
		var link string
		if err := tx.QueryRow(ctx, qPayLink, tenantID).Scan(&link); err != nil {
			return false, err
		}
		if link != "" {
			data["pay_url"] = link
		}
	}
	data["tenant_id"], data["kind"], data["period_key"] = tenantID, n.Kind, n.PeriodKey
	body, err := json.Marshal(data)
	if err != nil {
		return false, err
	}
	var outboxID int64
	err = tx.QueryRow(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data)
		VALUES ($1, 'platform_notice', $2, $3, $4, $5) ON CONFLICT (idempotency_key) DO NOTHING RETURNING id`,
		clip("billing:"+tenantID+":"+n.Kind+":"+n.PeriodKey, 200), to, name, NoticeTemplate, string(body)).Scan(&outboxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE platform.notification_log SET outbox_id = $2 WHERE id = $1`, logID, outboxID)
	return true, err
}
