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
