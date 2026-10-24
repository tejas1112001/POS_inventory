//go:build integration

package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
)

const webhookSecret = "whsec-isolation-0123456789abcdef"

// hooks builds signed Razorpay-shaped webhooks with the app's secret.
var hooks = billing.NewFake([]byte(webhookSecret), "")

// billingTenant provisions a tenant of its own, so billing tests can
// suspend and expire it without touching a, b and c.
func billingTenant(t *testing.T, slug string) string {
	t.Helper()
	o := provisioning.New(tdb.MigratorDSN, "http", "pos.test", slog.New(slog.DiscardHandler))
	res, err := o.Provision(context.Background(), provisioning.Request{Slug: slug, Name: "Billing " + slug,
		OwnerEmail: "owner@" + slug + ".test", OwnerName: "Owner " + slug})
	require.NoError(t, err)
	tenants[slug] = tenantInfo{ID: res.TenantID, Slug: slug, Host: slug + ".pos.test", OwnerEmail: "owner@" + slug + ".test"}
	return slug
}

func linkSub(t *testing.T, l, subID string) {
	t.Helper()
	_, err := admin(t).Exec(context.Background(), `UPDATE platform.subscriptions SET provider_subscription_id = $2, provider = 'fake'
		WHERE tenant_id = $1`, tenants[l].ID, subID)
	require.NoError(t, err)
}

func webhook(t *testing.T, body []byte, h http.Header) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/platform/webhooks/razorpay", bytes.NewReader(body))
	require.NoError(t, err)
	req.Host = "admin.pos.test"
	req.Header = h.Clone()
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", 1+ipSeq.Add(1)%250))
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	return res.StatusCode
}

func send(t *testing.T, e billing.FakeEvent) int {
	t.Helper()
	body, h := hooks.Webhook(e)
	return webhook(t, body, h)
}

type subState struct {
	Status, Tenant, Source string
	GraceEnd, PeriodEnd    *time.Time
	PlanID, TenantPlanID   int64
}

func subOf(t *testing.T, l string) subState {
	t.Helper()
	var s subState
	var src *string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT s.status, t.status, t.suspension_source, s.grace_end,
			s.current_period_end, s.plan_id, t.plan_id
		FROM platform.subscriptions s JOIN platform.tenants t ON t.id = s.tenant_id WHERE s.tenant_id = $1`, tenants[l].ID).
		Scan(&s.Status, &s.Tenant, &src, &s.GraceEnd, &s.PeriodEnd, &s.PlanID, &s.TenantPlanID))
	if src != nil {
		s.Source = *src
	}
	return s
}

func notices(t *testing.T, l string) map[string]int {
	t.Helper()
	rows, err := admin(t).Query(context.Background(), `SELECT n.kind, count(*) FROM platform.notification_log n
		JOIN platform.email_outbox o ON o.id = n.outbox_id AND o.template = 'billing_notice'
		WHERE n.tenant_id = $1 GROUP BY n.kind`, tenants[l].ID)
	require.NoError(t, err)
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		require.NoError(t, rows.Scan(&k, &n))
		out[k] = n
	}
	return out
}

func eventOutcome(t *testing.T, id string) string {
	t.Helper()
	var o *string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT outcome FROM platform.billing_events
		WHERE event_id = $1 AND signature_valid`, id).Scan(&o))
	if o == nil {
		return ""
	}
	return *o
}

func TestWebhookSignatureChecks(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "bill-sig")
	linkSub(t, l, "sub_sig_1")
	body, h := hooks.Webhook(billing.FakeEvent{ID: "evt_sig_1", Type: billing.EvActivated, SubscriptionID: "sub_sig_1"})

	tampered := bytes.Replace(body, []byte(`"sub_sig_1"`), []byte(`"sub_sig_2"`), 1)
	require.Equal(t, 400, webhook(t, tampered, h))
	nosig := h.Clone()
	nosig.Del(billing.HeaderSignature)
	require.Equal(t, 400, webhook(t, body, nosig))
	forged := h.Clone()
	forged.Set(billing.HeaderSignature, billing.Sign([]byte("not-the-secret-000000"), body))
	require.Equal(t, 400, webhook(t, body, forged))
	var stored, withPayload int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*), count(payload) FROM platform.billing_events
		WHERE event_id = 'evt_sig_1' AND NOT signature_valid AND outcome = 'rejected'`).Scan(&stored, &withPayload))
	require.Equal(t, 3, stored)
	require.Zero(t, withPayload, "rejected deliveries keep no payload")
	require.Equal(t, "trialing", subOf(t, l).Status, "nothing applied")

	// The forged attempts did not reserve the event ID: the real one applies.
	require.Equal(t, 200, webhook(t, body, h))
	require.Equal(t, "applied", eventOutcome(t, "evt_sig_1"))
	require.Equal(t, "active", subOf(t, l).Status)

	// Only on the admin host, and bodies over 256 KiB are refused.
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/platform/webhooks/razorpay", bytes.NewReader(body))
	require.NoError(t, err)
	req.Host = tenants["a"].Host
	req.Header = h.Clone()
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	require.NotEqual(t, 200, res.StatusCode, "not served on tenant hosts")
	big := bytes.Repeat([]byte("x"), 300<<10)
	bh := http.Header{}
	bh.Set(billing.HeaderSignature, billing.Sign([]byte(webhookSecret), big))
	require.Equal(t, 413, webhook(t, big, bh))
}

func TestWebhookLifecycle(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "bill-life")
	linkSub(t, l, "sub_life_1")
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) time.Time { return now.Add(d) }
	end := at(30 * 24 * time.Hour)
	ev := func(id string, typ billing.EventType, created time.Time, extra func(*billing.FakeEvent)) {
		e := billing.FakeEvent{ID: id, Type: typ, CreatedAt: created, SubscriptionID: "sub_life_1", Status: "active"}
		if extra != nil {
			extra(&e)
		}
		require.Equal(t, 200, send(t, e), id)
		require.Equal(t, "applied", eventOutcome(t, id), id)
	}

	ev("evt_life_auth", billing.EvAuthenticated, at(-10*time.Minute), nil)
	require.Equal(t, "trialing", subOf(t, l).Status)
	ev("evt_life_act", billing.EvActivated, at(-9*time.Minute), func(e *billing.FakeEvent) { e.CurrentStart, e.CurrentEnd = &now, &end })
	s := subOf(t, l)
	require.Equal(t, "active", s.Status)
	require.Equal(t, "active", s.Tenant)
	require.Equal(t, end, s.PeriodEnd.UTC())
	ev("evt_life_chg1", billing.EvCharged, at(-8*time.Minute), func(e *billing.FakeEvent) {
		e.PaidCount, e.InvoiceID, e.AmountMinor = 1, "inv_life_1", 49900
	})
	var amount int64
	var invStatus string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT amount_minor, status FROM platform.invoices WHERE provider_invoice_id = 'inv_life_1'`).
		Scan(&amount, &invStatus))
	require.Equal(t, int64(49900), amount)
	require.Equal(t, "paid", invStatus)
	require.Empty(t, notices(t, l), "no renewal notice on the first charge")

	// A failed charge starts one grace period and one notice.
	ev("evt_life_pend", billing.EvPending, at(-7*time.Minute), nil)
	s = subOf(t, l)
	require.Equal(t, "past_due", s.Status)
	require.Equal(t, "past_due", s.Tenant)
	require.NotNil(t, s.GraceEnd)
	require.WithinDuration(t, time.Now().Add(7*24*time.Hour), *s.GraceEnd, time.Minute)
	ev("evt_life_pend2", billing.EvPending, at(-6*time.Minute), nil)
	require.Equal(t, s.GraceEnd.UTC(), subOf(t, l).GraceEnd.UTC(), "grace is set once per incident")
	ev("evt_life_halt1", billing.EvHalted, at(-5*time.Minute), nil)
	require.Equal(t, "grace", subOf(t, l).Status, "halted within grace")
	require.Equal(t, map[string]int{"payment_failed": 1}, notices(t, l))

	// Grace over: halted suspends (a billing suspension).
	_, err := admin(t).Exec(ctx, `UPDATE platform.subscriptions SET grace_end = now() - interval '1 minute' WHERE tenant_id = $1`, tenants[l].ID)
	require.NoError(t, err)
	ev("evt_life_halt2", billing.EvHalted, at(-4*time.Minute), nil)
	s = subOf(t, l)
	require.Equal(t, "suspended", s.Status)
	require.Equal(t, "suspended", s.Tenant)
	require.Equal(t, "billing", s.Source)
	require.Equal(t, 1, notices(t, l)["suspended"])

	// Paying reactivates: active again, grace cleared, notices sent.
	ev("evt_life_chg2", billing.EvCharged, at(-3*time.Minute), func(e *billing.FakeEvent) {
		e.PaidCount, e.InvoiceID, e.AmountMinor = 2, "inv_life_2", 49900
	})
	s = subOf(t, l)
	require.Equal(t, "active", s.Status)
	require.Equal(t, "active", s.Tenant)
	require.Empty(t, s.Source)
	require.Nil(t, s.GraceEnd)
	n := notices(t, l)
	require.Equal(t, 1, n["reactivated"])
	require.Equal(t, 1, n["renewal_success"])

	// Cancelled, then completed: read-only for the tenant.
	ev("evt_life_cxl", billing.EvCancelled, at(-2*time.Minute), nil)
	require.Equal(t, "cancelled", subOf(t, l).Tenant)
	ev("evt_life_done", billing.EvCompleted, at(-time.Minute), nil)
	s = subOf(t, l)
	require.Equal(t, "expired", s.Status)
	require.Equal(t, "read_only", s.Tenant)

	// Every applied subscription event is audited as the provider.
	var audited int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log
		WHERE target_tenant = $1 AND actor_email = 'system:fake' AND action LIKE 'billing.subscription.%'`, tenants[l].ID).Scan(&audited))
	require.Equal(t, 10, audited)
}

func TestWebhookDuplicatesOrderingAndStaleness(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "bill-order")
	linkSub(t, l, "sub_order_1")
	now := time.Now().UTC().Truncate(time.Second)

	// Ten concurrent deliveries of one event apply it once.
	body, h := hooks.Webhook(billing.FakeEvent{ID: "evt_order_pend", Type: billing.EvPending, CreatedAt: now.Add(-time.Hour),
		SubscriptionID: "sub_order_1"})
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for range 10 {
		wg.Go(func() { codes <- webhook(t, body, h) })
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		require.Equal(t, 200, c)
	}
	var rows int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.billing_events WHERE event_id = 'evt_order_pend'`).Scan(&rows))
	require.Equal(t, 1, rows)
	var applied int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log
		WHERE target_tenant = $1 AND after->>'event_id' = 'evt_order_pend'`, tenants[l].ID).Scan(&applied))
	require.Equal(t, 1, applied, "applied exactly once")
	require.Equal(t, 1, notices(t, l)["payment_failed"])
	require.Equal(t, 200, webhook(t, body, h), "a later redelivery is fine too")
	require.Equal(t, 1, notices(t, l)["payment_failed"])

	// Out of order: charged (newer) then a late pending (older).
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_order_chg", Type: billing.EvCharged, CreatedAt: now.Add(-10 * time.Minute),
		SubscriptionID: "sub_order_1", PaidCount: 3, InvoiceID: "inv_order_3", AmountMinor: 100}))
	require.Equal(t, "active", subOf(t, l).Status)
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_order_late", Type: billing.EvHalted, CreatedAt: now.Add(-20 * time.Minute),
		SubscriptionID: "sub_order_1"}))
	require.Equal(t, "stale_ignored", eventOutcome(t, "evt_order_late"))
	require.Equal(t, "active", subOf(t, l).Status)
	// A late invoice event still records the invoice.
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_order_inv", Type: billing.EvInvoicePaid, CreatedAt: now.Add(-30 * time.Minute),
		SubscriptionID: "sub_order_1", InvoiceID: "inv_order_2", AmountMinor: 100}))
	require.Equal(t, "applied", eventOutcome(t, "evt_order_inv"))

	// Older than 72 hours: stored as stale, not applied.
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_order_old", Type: billing.EvCancelled, CreatedAt: now.Add(-73 * time.Hour),
		SubscriptionID: "sub_order_1"}))
	require.Equal(t, "stale", eventOutcome(t, "evt_order_old"))
	require.Equal(t, "active", subOf(t, l).Status)

	// Unknown subscriptions and event types are recorded, not applied.
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_order_unknown", Type: billing.EvActivated, SubscriptionID: "sub_nobody"}))
	require.Equal(t, "unknown_subscription", eventOutcome(t, "evt_order_unknown"))
	odd := []byte(`{"event":"order.paid","created_at":` + fmt.Sprint(now.Unix()) + `,"payload":{}}`)
	oh := http.Header{}
	oh.Set(billing.HeaderSignature, billing.Sign([]byte(webhookSecret), odd))
	oh.Set(billing.HeaderEventID, "evt_order_odd")
	require.Equal(t, 200, webhook(t, odd, oh))
	require.Equal(t, "ignored", eventOutcome(t, "evt_order_odd"))
}

func TestWebhookNeverLiftsAnAdminSuspension(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "bill-admin")
	linkSub(t, l, "sub_admin_1")
	_, err := admin(t).Exec(ctx, `UPDATE platform.tenants SET status = 'suspended', suspension_source = 'admin',
		suspend_reason = 'Abuse report', suspended_at = now() WHERE id = $1`, tenants[l].ID)
	require.NoError(t, err)
	require.Equal(t, 200, send(t, billing.FakeEvent{Type: billing.EvCharged, SubscriptionID: "sub_admin_1", PaidCount: 2,
		InvoiceID: "inv_admin_2", AmountMinor: 100}))
	s := subOf(t, l)
	require.Equal(t, "active", s.Status, "the subscription is paid")
	require.Equal(t, "suspended", s.Tenant, "the Super Admin's suspension stays")
	require.Equal(t, "admin", s.Source)
}

func TestWebhookTouchesOnlyItsTenant(t *testing.T) {
	ctx := context.Background()
	x := billingTenant(t, "bill-iso-x")
	y := billingTenant(t, "bill-iso-y")
	linkSub(t, x, "sub_iso_x")
	linkSub(t, y, "sub_iso_y")
	before := subOf(t, y)

	// The subscription ID decides; notes naming another tenant change nothing.
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_iso_1", Type: billing.EvPending, SubscriptionID: "sub_iso_x",
		TenantID: tenants[y].ID}))
	require.Equal(t, "past_due", subOf(t, x).Status)
	require.Equal(t, before, subOf(t, y))
	// notes.tenant_id alone cannot reach a tenant that has its own provider
	// subscription.
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_iso_2", Type: billing.EvHalted, SubscriptionID: "sub_other",
		TenantID: tenants[y].ID}))
	require.Equal(t, "unknown_subscription", eventOutcome(t, "evt_iso_2"))
	require.Equal(t, before, subOf(t, y))
	var leaked int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.notification_log WHERE tenant_id = $1`, tenants[y].ID).Scan(&leaked))
	require.Zero(t, leaked)
	require.Equal(t, "trialing", subOf(t, "a").Status, "the shared test tenants are untouched")
	require.True(t, strings.HasPrefix(tenants[x].Host, "bill-iso-x"))
}

// setSub changes a test tenant's subscription directly (dates, status).
func setSub(t *testing.T, l, set string, args ...any) {
	t.Helper()
	_, err := admin(t).Exec(context.Background(), `UPDATE platform.subscriptions SET `+set+` WHERE tenant_id = $1`,
		append([]any{tenants[l].ID}, args...)...)
	require.NoError(t, err)
}

func setTenant(t *testing.T, l, set string, args ...any) {
	t.Helper()
	_, err := admin(t).Exec(context.Background(), `UPDATE platform.tenants SET `+set+` WHERE id = $1`,
		append([]any{tenants[l].ID}, args...)...)
	require.NoError(t, err)
}

func TestLifecycleJob(t *testing.T) {
	ctx := context.Background()
	trial := billingTenant(t, "bill-job-trial")
	setSub(t, trial, `trial_end = now() - interval '1 minute'`)
	waiting := billingTenant(t, "bill-job-wait")
	setSub(t, waiting, `trial_end = now() - interval '1 hour', provider_subscription_id = 'sub_job_wait'`)
	overdue := billingTenant(t, "bill-job-grace")
	setSub(t, overdue, `status = 'grace', grace_end = now() - interval '1 minute'`)
	setTenant(t, overdue, `status = 'grace'`)
	inGrace := billingTenant(t, "bill-job-ingrace")
	setSub(t, inGrace, `status = 'past_due', grace_end = now() + interval '2 days'`)
	setTenant(t, inGrace, `status = 'past_due'`)
	ended := billingTenant(t, "bill-job-cxl")
	setSub(t, ended, `status = 'cancelled', current_period_end = now() - interval '1 minute'`)
	setTenant(t, ended, `status = 'cancelled'`)
	held := billingTenant(t, "bill-job-held")
	setSub(t, held, `trial_end = now() - interval '1 minute'`)
	setTenant(t, held, `status = 'suspended', suspension_source = 'admin', suspend_reason = 'Review'`)

	svc := theApp.Billing()
	_, err := svc.RunLifecycle(ctx)
	require.NoError(t, err)
	_, err = svc.RunLifecycle(ctx)
	require.NoError(t, err, "a second run is harmless")

	s := subOf(t, trial)
	require.Equal(t, []string{"read_only", "read_only"}, []string{s.Status, s.Tenant}, "unpaid trial: read-only")
	require.Equal(t, "trialing", subOf(t, waiting).Status, "a provider-backed trial waits for its first charge")
	s = subOf(t, overdue)
	require.Equal(t, []string{"suspended", "suspended", "billing"}, []string{s.Status, s.Tenant, s.Source})
	require.Equal(t, 1, notices(t, overdue)["suspended"])
	require.Equal(t, "past_due", subOf(t, inGrace).Status, "grace still running")
	s = subOf(t, ended)
	require.Equal(t, []string{"read_only", "read_only"}, []string{s.Status, s.Tenant}, "cancelled period over")
	s = subOf(t, held)
	require.Equal(t, []string{"read_only", "suspended", "admin"}, []string{s.Status, s.Tenant, s.Source}, "an admin suspension stays")

	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log
		WHERE target_tenant = $1 AND actor_email = 'system:billing' AND action = 'billing.lifecycle'`, tenants[overdue].ID).Scan(&audits))
	require.Equal(t, 1, audits, "changed once")
	// A suspended tenant can no longer sign in; a read-only one can, but not write.
	require.Equal(t, 403, do(t, rq{method: "POST", host: tenants[overdue].Host, path: "/api/auth/login",
		body: map[string]any{"email": tenants[overdue].OwnerEmail, "password": "whatever-password-1"}}).Code)
}

func TestRemindersAreSentOnceEvenInParallel(t *testing.T) {
	ctx := context.Background()
	trial := billingTenant(t, "bill-rem-trial")
	setSub(t, trial, `trial_end = now() + interval '5 days'`)
	setTenant(t, trial, `billing_email = 'accounts@bill-rem-trial.test'`)
	grace := billingTenant(t, "bill-rem-grace")
	setSub(t, grace, `status = 'grace', grace_end = now() + interval '12 hours', checkout_short_url = 'https://pay.example.test/sub_rem'`)
	setTenant(t, grace, `status = 'grace', billing_email = 'accounts@bill-rem-grace.test'`)
	ending := billingTenant(t, "bill-rem-end")
	setSub(t, ending, `status = 'cancelled', current_period_end = now() + interval '2 days'`)
	setTenant(t, ending, `status = 'cancelled'`)

	svc := theApp.Billing()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			_, err := svc.RunReminders(ctx)
			require.NoError(t, err)
		})
	}
	wg.Wait()
	n, err := svc.RunReminders(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "nothing new on a later run")
	require.Equal(t, map[string]int{"trial_ending_7": 1}, notices(t, trial))
	require.Equal(t, map[string]int{"grace_ending": 1}, notices(t, grace))
	require.Equal(t, map[string]int{"subscription_ending_3": 1}, notices(t, ending))
	var outbox int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'billing_notice'
		AND template_data->>'tenant_id' = ANY($1)`, []string{tenants[trial].ID, tenants[grace].ID, tenants[ending].ID}).Scan(&outbox))
	require.Equal(t, 3, outbox, "one email per notice")

	// They go to the billing contact by platform mail, with the pay link.
	m := waitMail(t, platSrv, "accounts@bill-rem-trial.test")
	require.Equal(t, "Your Test trial for Billing bill-rem-trial ends in 7 days", m.Header.Get("Subject"))
	require.Contains(t, m.Text, "http://bill-rem-trial.pos.test")
	g := waitMail(t, platSrv, "accounts@bill-rem-grace.test")
	require.Equal(t, "Last day to pay for Billing bill-rem-grace", g.Header.Get("Subject"))
	require.Contains(t, g.Text, "Pay now: https://pay.example.test/sub_rem")
}

// buyable makes sure a public plan with a provider plan ID exists.
func buyable(t *testing.T) {
	t.Helper()
	_, err := admin(t).Exec(context.Background(), `INSERT INTO platform.plans (code, name, price_minor, billing_interval, trial_days,
			limits, features, is_public, razorpay_plan_id)
		SELECT 'pro_test', 'Pro', 49900, 'monthly', 14, limits, features, true, 'plan_fake_pro' FROM platform.plans WHERE code = 'trial'
		ON CONFLICT (code) DO NOTHING`)
	require.NoError(t, err)
}

func TestBillingSubscriptionViewAndBanners(t *testing.T) {
	ctx := context.Background()
	buyable(t)
	l := billingTenant(t, "bill-view")
	other := billingTenant(t, "bill-view-other")
	setSub(t, l, `trial_end = now() + interval '3 days'`)
	_, err := admin(t).Exec(ctx, `INSERT INTO platform.invoices (tenant_id, provider, provider_invoice_id, amount_minor, currency, status, short_url)
		VALUES ($1, 'fake', 'inv_view_mine', 49900, 'INR', 'issued', 'https://pay.test/inv_view_mine'),
		       ($2, 'fake', 'inv_view_theirs', 99900, 'INR', 'paid', NULL)`, tenants[l].ID, tenants[other].ID)
	require.NoError(t, err)
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host

	v := get(t, host, "/api/billing/subscription", tok)
	require.Equal(t, 200, v.Code, v.Raw)
	require.Equal(t, "trialing", v.Body["status"])
	require.Equal(t, "trial", v.Body["plan"].(map[string]any)["code"])
	require.Equal(t, true, v.Body["billingEnabled"])
	require.Equal(t, false, v.Body["subscribed"])
	u := v.Body["usage"].(map[string]any)
	require.Equal(t, 1.0, num(u["stores"]))
	require.Equal(t, 1.0, num(u["users"]))
	require.Contains(t, v.Raw, "inv_view_mine")
	require.NotContains(t, v.Raw, "inv_view_theirs", "only this tenant's invoices")
	require.NotContains(t, v.Raw, "plan_fake_pro", "no provider IDs")
	var codes []string
	for _, p := range v.Body["plans"].([]any) {
		codes = append(codes, p.(map[string]any)["code"].(string))
	}
	require.Contains(t, codes, "pro_test")
	require.NotContains(t, codes, "trial", "hidden plans are not offered")

	b := get(t, host, "/api/notifications/banners", tok)
	require.Equal(t, 200, b.Code)
	banners := list(t, b)
	require.Len(t, banners, 1)
	require.Equal(t, "info", banners[0]["level"])
	require.NotNil(t, banners[0]["action"])

	// Staff without billing rights: no billing data, a banner without action.
	createUser(t, l, tok, "cashier@bill-view.test", "biller", "cashier-bill-view-1")
	btok, _ := login(t, l, "cashier@bill-view.test", "cashier-bill-view-1")
	require.Equal(t, 403, get(t, host, "/api/billing/subscription", btok).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/billing/checkout", token: btok,
		body: map[string]any{"planCode": "pro_test"}}).Code)
	bb := list(t, get(t, host, "/api/notifications/banners", btok))
	require.Len(t, bb, 1)
	require.Nil(t, bb[0]["action"])

	// Another tenant sees its own state only.
	otok, _ := ownerLogin(t, other)
	ov := get(t, tenants[other].Host, "/api/billing/subscription", otok)
	require.Contains(t, ov.Raw, "inv_view_theirs")
	require.NotContains(t, ov.Raw, "inv_view_mine")
	require.Empty(t, list(t, get(t, tenants[other].Host, "/api/notifications/banners", otok)), "its trial is not ending")
	require.Equal(t, 401, get(t, tenants[other].Host, "/api/billing/subscription", tok).Code)
}

func TestCheckoutNeverActivatesUntilTheWebhook(t *testing.T) {
	ctx := context.Background()
	buyable(t)
	l := billingTenant(t, "bill-co")
	other := billingTenant(t, "bill-co-other")
	otherBefore := subOf(t, other)
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	checkout := func(plan string) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/billing/checkout", token: tok, body: map[string]any{"planCode": plan}})
	}

	require.Equal(t, 400, checkout("no_such_plan").Code)
	require.Equal(t, 400, checkout("trial").Code, "hidden plans cannot be bought")

	// The provider fails: 503 and nothing stored.
	fake := theApp.Billing().Provider.(*billing.Fake)
	fake.FailNext = fmt.Errorf("provider down")
	down := checkout("pro_test")
	require.Equal(t, 503, down.Code)
	require.NotContains(t, down.Raw, "provider down")

	c := checkout("pro_test")
	require.Equal(t, 200, c.Code, c.Raw)
	url := c.Body["url"].(string)
	require.True(t, strings.HasPrefix(url, "https://billing.example.invalid/pay/sub_fake"))
	var subID, provStatus string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT provider_subscription_id, provider_status FROM platform.subscriptions
		WHERE tenant_id = $1`, tenants[l].ID).Scan(&subID, &provStatus))
	require.Equal(t, "created", provStatus)
	s := subOf(t, l)
	require.Equal(t, "trialing", s.Status, "checkout never activates anything")
	require.Equal(t, otherBefore, subOf(t, other), "other tenants untouched")
	require.Equal(t, 200, checkout("pro_test").Code, "clicking again before paying is fine")

	// The verified webhook activates, on the plan the provider names.
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT provider_subscription_id FROM platform.subscriptions WHERE tenant_id = $1`,
		tenants[l].ID).Scan(&subID))
	require.Equal(t, 200, send(t, billing.FakeEvent{Type: billing.EvActivated, SubscriptionID: subID, PlanID: "plan_fake_pro", Status: "active"}))
	s = subOf(t, l)
	require.Equal(t, "active", s.Status)
	var planCode string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT p.code FROM platform.tenants t JOIN platform.plans p ON p.id = t.plan_id
		WHERE t.id = $1`, tenants[l].ID).Scan(&planCode))
	require.Equal(t, "pro_test", planCode, "the tenant's plan follows the provider")
	conflict := checkout("pro_test")
	require.Equal(t, 409, conflict.Code)
	require.Equal(t, "INVALID_STATE", conflict.Body["code"])

	// A read-only tenant can still pay, and nothing else.
	ro := billingTenant(t, "bill-co-ro")
	setSub(t, ro, `status = 'read_only'`)
	setTenant(t, ro, `status = 'read_only'`)
	rtok, _ := ownerLogin(t, ro)
	eventually(t, func() bool {
		return do(t, rq{method: "POST", host: tenants[ro].Host, path: "/api/stores", token: rtok,
			body: map[string]any{"name": "Blocked"}}).Code == 403
	}, "read-only applied")
	require.Equal(t, 200, do(t, rq{method: "POST", host: tenants[ro].Host, path: "/api/billing/checkout", token: rtok,
		body: map[string]any{"planCode": "pro_test"}}).Code)
	require.Equal(t, 200, get(t, tenants[ro].Host, "/api/billing/subscription", rtok).Code)
	require.Equal(t, "critical", list(t, get(t, tenants[ro].Host, "/api/notifications/banners", rtok))[0]["level"])
}
