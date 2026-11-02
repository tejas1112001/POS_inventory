//go:build integration

package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
)

// TestBillingReconcile (S10 item 3): the hourly reconcile records each
// difference with the provider (status, plan, period end, a subscription
// the provider does not know), alerts the inbox once when a difference is
// seen twice, resolves it when both sides agree again, never changes the
// subscription, and alerts when webhooks have gone quiet.
func TestBillingReconcile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.mfaLogin(t, rootEmail, newPW, rootSession(t, e).secret)
	admin := tdb.Admin(t)
	_, err := admin.Exec(ctx, `INSERT INTO platform.plans (code, name, price_minor, billing_interval, trial_days, limits, features, is_public, razorpay_plan_id)
		SELECT v.code, v.code, 10000, 'monthly', 14, limits, features, true, v.pp FROM platform.plans,
			(VALUES ('rc_one', 'plan_fake_rcone'), ('rc_two', 'plan_fake_rctwo')) AS v(code, pp)
		WHERE platform.plans.code = 'trial' ON CONFLICT (code) DO NOTHING`)
	require.NoError(t, err)
	id := e.newTenant(t, s, "reconcile-shop")
	gone := e.newTenant(t, s, "reconcile-gone")
	sub, err := e.fake.CreateSubscription(ctx, billing.CreateSubscriptionReq{TenantID: id, PlanID: "plan_fake_rcone"})
	require.NoError(t, err)
	end := time.Now().Add(15 * 24 * time.Hour).UTC().Truncate(time.Second)
	start := end.Add(-30 * 24 * time.Hour)
	e.fake.SetStatus(sub.ID, "active", &start, &end, 1)
	link := `UPDATE platform.subscriptions SET provider_subscription_id = $2, provider = 'fake', provider_status = 'active', status = 'active',
		current_period_end = $3, plan_id = (SELECT id FROM platform.plans WHERE code = 'rc_one') WHERE tenant_id = $1`
	_, err = admin.Exec(ctx, link, id, sub.ID, end)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, link, gone, "sub_fake_unknown_to_provider", end)
	require.NoError(t, err)

	bs := &billing.Service{Sessions: e.sessions, Provider: e.fake, Cfg: billing.DefaultConfig, Inbox: "ops-reconcile@platform.test",
		ReconcileSpacing: time.Nanosecond}
	run := func() billing.ReconcileResult {
		t.Helper()
		r, err := bs.RunReconcile(ctx)
		require.NoError(t, err)
		return r
	}
	open := func(tenant string) map[string][2]string {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT field, coalesce(ours, ''), coalesce(theirs, '') FROM platform.billing_drift WHERE tenant_id = $1 AND resolved_at IS NULL`, tenant)
		require.NoError(t, err)
		defer rows.Close()
		out := map[string][2]string{}
		for rows.Next() {
			var f, o, th string
			require.NoError(t, rows.Scan(&f, &o, &th))
			out[f] = [2]string{o, th}
		}
		return out
	}
	alerts := func() int {
		var n int
		require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'ops_alert'
			AND to_addr = 'ops-reconcile@platform.test' AND template_data->>'heading' LIKE '%reconcile-%'`).Scan(&n))
		return n
	}
	before := func() string {
		var row string
		require.NoError(t, admin.QueryRow(ctx, `SELECT row_to_json(x)::text FROM (SELECT status, provider_status, plan_id, current_period_end
			FROM platform.subscriptions WHERE tenant_id = $1) x`, id).Scan(&row))
		return row
	}

	run()
	require.Empty(t, open(id), "both sides agree")
	require.Equal(t, [2]string{"exists", "provider answered 400 BAD_REQUEST_ERROR"}, open(gone)["fetch"])

	// The provider halted it and moved the period: two differences.
	later := end.Add(3 * 24 * time.Hour)
	e.fake.SetStatus(sub.ID, "halted", &start, &later, 1)
	ours := before()
	run()
	d := open(id)
	require.Equal(t, [2]string{"active", "halted"}, d["status"])
	require.Equal(t, end.Format(time.RFC3339), d["period_end"][0])
	require.Equal(t, 1, alerts(), "only the unknown subscription was seen twice so far")
	run()
	require.Equal(t, 3, alerts(), "seen twice: one alert per difference (status, period end, and the unknown subscription)")
	run()
	require.Equal(t, 3, alerts(), "alerted once only")
	require.Equal(t, ours, before(), "report only: nothing changed")

	// Plan drift; then everything agrees again and resolves.
	_, err = e.fake.ChangePlan(ctx, sub.ID, "plan_fake_rctwo", false)
	require.NoError(t, err)
	run()
	require.Equal(t, [2]string{"plan_fake_rcone", "plan_fake_rctwo"}, open(id)["plan"])
	_, err = e.fake.ChangePlan(ctx, sub.ID, "plan_fake_rcone", false)
	require.NoError(t, err)
	e.fake.SetStatus(sub.ID, "active", &start, &end, 1)
	r := run()
	require.Empty(t, open(id))
	require.Positive(t, r.Resolved)

	// The console report (billing:view).
	rep := e.do(req{method: "GET", path: "/billing/drift", token: s.token})
	require.Equal(t, 200, rep.Code, rep.Raw)
	require.Contains(t, rep.Raw, `"slug":"reconcile-gone"`)
	require.Contains(t, rep.Raw, `"slug":"reconcile-shop"`, "in the recently resolved list")
	require.EqualValues(t, 24, rep.Body["quietAfterHours"])

	// Webhooks quiet for longer than the threshold: one alert a day.
	bs.Now = func() time.Time { return time.Now().Add(72 * time.Hour) }
	r = run()
	require.True(t, r.WebhooksQuiet)
	run()
	var quiet int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'ops_alert'
		AND to_addr = 'ops-reconcile@platform.test' AND template_data->>'heading' = 'No billing webhooks received'`).Scan(&quiet))
	require.Equal(t, 1, quiet)
}
