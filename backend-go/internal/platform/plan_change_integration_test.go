//go:build integration

package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
)

// TestConsolePlanChange (S10 item 2, B1-B4): for a shop paying through the
// provider, a dearer plan applies now at the provider and here; a cheaper
// one is scheduled for the period end and refused while usage is over its
// limits; a scheduled change can be cancelled; the webhook that carries
// the new plan completes it. The preview shows the price and effect.
func TestConsolePlanChange(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.mfaLogin(t, rootEmail, newPW, rootSession(t, e).secret)
	admin := tdb.Admin(t)
	call := func(method, path string, body any) resp {
		return e.do(req{method: method, path: path, token: s.token, body: body})
	}
	mk := func(code, providerPlan, interval string, price int, stores int) {
		r := call("POST", "/plans", map[string]any{"code": code, "name": code, "priceMinor": price, "interval": interval,
			"razorpayPlanId": providerPlan, "trialDays": 14, "isPublic": true,
			"limits":   map[string]any{"max_stores": stores, "max_users": 10, "max_products": 1000, "max_emails_per_day": 100, "max_storage_mb": 500},
			"features": map[string]any{"reports_export": true, "public_shop": true}})
		require.Equal(t, 201, r.Code, r.Raw)
	}
	mk("pc_basic", "plan_fake_pcbasic", "monthly", 29900, 1)
	mk("pc_pro", "plan_fake_pcpro", "monthly", 59900, 5)
	mk("pc_yearly", "plan_fake_pcyear", "yearly", 299000, 5)
	id := e.newTenant(t, s, "plan-change-shop")
	other := e.newTenant(t, s, "plan-change-other")
	var otherSub string
	require.NoError(t, admin.QueryRow(ctx, `SELECT row_to_json(s)::text FROM platform.subscriptions s WHERE tenant_id = $1`, other).Scan(&otherSub))

	sub, err := e.fake.CreateSubscription(ctx, billing.CreateSubscriptionReq{TenantID: id, PlanID: "plan_fake_pcbasic"})
	require.NoError(t, err)
	end := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	_, err = admin.Exec(ctx, `UPDATE platform.subscriptions SET provider_subscription_id = $2, provider = 'fake', provider_status = 'active',
		status = 'active', current_period_end = $3, plan_id = (SELECT id FROM platform.plans WHERE code = 'pc_basic') WHERE tenant_id = $1`, id, sub.ID, end)
	require.NoError(t, err)

	// Without a usage count, a provider-billed change is blocked.
	pv := call("GET", "/tenants/"+id+"/subscription/preview?planCode=pc_pro", nil)
	require.Equal(t, 200, pv.Code, pv.Raw)
	require.Equal(t, false, pv.Body["allowed"])
	_, err = admin.Exec(ctx, `INSERT INTO platform.tenant_usage (tenant_id, users, stores, products) VALUES ($1, 2, 2, 10)`, id)
	require.NoError(t, err)

	// Upgrade preview, then apply (step-up first).
	pv = call("GET", "/tenants/"+id+"/subscription/preview?planCode=pc_pro", nil)
	require.Equal(t, 200, pv.Code, pv.Raw)
	require.Equal(t, true, pv.Body["allowed"], pv.Raw)
	require.Equal(t, "upgrade", pv.Body["kind"])
	require.Equal(t, "now", pv.Body["when"])
	require.Contains(t, pv.Body["effect"], "₹299 a month → ₹599 a month")
	require.Contains(t, pv.Body["effect"], "pro-rated")
	require.Equal(t, 403, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_pro", "reason": "Customer asked for more stores"}).Code)
	e.stepUp(t, s)
	up := call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_pro", "reason": "Customer asked for more stores"})
	require.Equal(t, 200, up.Code, up.Raw)
	require.Equal(t, "pc_pro", up.Body["planCode"])
	got, err := e.fake.FetchSubscription(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, "plan_fake_pcpro", got.PlanID, "changed at the provider too")

	// Interval changes are refused.
	require.Equal(t, 409, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_yearly", "reason": "Wants yearly billing"}).Code)

	// Downgrade: refused while 2 stores are in use (basic allows 1).
	pv = call("GET", "/tenants/"+id+"/subscription/preview?planCode=pc_basic", nil)
	require.Equal(t, false, pv.Body["allowed"])
	require.Contains(t, pv.Raw, `"resource":"stores"`)
	require.Contains(t, pv.Body["refusal"], "Stores 2 of 1 allowed")
	require.Equal(t, 409, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_basic", "reason": "Customer wants to save"}).Code)
	_, err = admin.Exec(ctx, `UPDATE platform.tenant_usage SET stores = 1 WHERE tenant_id = $1`, id)
	require.NoError(t, err)
	pv = call("GET", "/tenants/"+id+"/subscription/preview?planCode=pc_basic", nil)
	require.Equal(t, true, pv.Body["allowed"], pv.Raw)
	require.Equal(t, "period_end", pv.Body["when"])
	require.Contains(t, pv.Body["effect"], end.Format("2 Jan 2006"))
	down := call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_basic", "reason": "Customer wants to save"})
	require.Equal(t, 200, down.Code, down.Raw)
	require.Equal(t, "pc_pro", down.Body["planCode"], "stays until the period end")
	require.Equal(t, "pc_basic", down.Body["pendingPlanCode"])
	require.Equal(t, "plan_fake_pcbasic", e.fake.Pending(sub.ID))
	require.Equal(t, 409, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_basic", "reason": "Customer wants to save"}).Code, "already scheduled")

	// Cancel the scheduled change (B3).
	cc := call("POST", "/tenants/"+id+"/subscription/cancel-change", map[string]any{"reason": "Customer changed their mind"})
	require.Equal(t, 200, cc.Code, cc.Raw)
	require.Nil(t, cc.Body["pendingPlanCode"])
	require.Empty(t, e.fake.Pending(sub.ID))
	require.Equal(t, 409, call("POST", "/tenants/"+id+"/subscription/cancel-change", map[string]any{"reason": "Customer changed their mind"}).Code)

	// Schedule again; the provider applies it and its webhook completes it.
	require.Equal(t, 200, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "pc_basic", "reason": "Customer wants to save"}).Code)
	require.Equal(t, "plan_fake_pcbasic", e.fake.ApplyPending(sub.ID))
	bs := &billing.Service{Sessions: e.sessions, Provider: e.fake, Cfg: billing.DefaultConfig}
	body, h := e.fake.Webhook(billing.FakeEvent{Type: billing.EvUpdated, SubscriptionID: sub.ID, PlanID: "plan_fake_pcbasic", Status: "active",
		TenantID: id, CurrentEnd: &end})
	code, _ := bs.HandleWebhook(ctx, body, h)
	require.Equal(t, 200, code)
	tb := call("GET", "/tenants/"+id+"/billing", nil)
	sv := tb.Body["subscription"].(map[string]any)
	require.Equal(t, "pc_basic", sv["planCode"])
	require.Nil(t, sv["pendingPlanCode"], "the scheduled change is done")

	// Audit; the other tenant was not touched.
	var audits int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log WHERE target_tenant = $1
		AND action IN ('billing.assign_plan','billing.plan_change_cancelled')`, id).Scan(&audits))
	require.Equal(t, 4, audits)
	var after string
	require.NoError(t, admin.QueryRow(ctx, `SELECT row_to_json(s)::text FROM platform.subscriptions s WHERE tenant_id = $1`, other).Scan(&after))
	require.Equal(t, otherSub, after)
}
