//go:build integration

package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
)

// TestOwnerPlanChange (S10 item 2): the owner of a paying shop upgrades
// now, schedules a downgrade for the period end (refused while usage is
// over the cheaper plan) and cancels it. Staff without billing:manage get
// 403; every call touches only the shop of the host.
func TestOwnerPlanChange(t *testing.T) {
	ctx := context.Background()
	_, err := admin(t).Exec(ctx, `INSERT INTO platform.plans (code, name, price_minor, billing_interval, trial_days, limits, features, is_public, razorpay_plan_id)
		SELECT v.code, v.name, v.price, 'monthly', 14, limits || jsonb_build_object('max_stores', v.stores, 'max_users', 10), features, true, v.pp
		FROM platform.plans, (VALUES ('own_small', 'Small', 19900, 1, 'plan_fake_ownsmall'), ('own_big', 'Big', 79900, 5, 'plan_fake_ownbig'))
			AS v(code, name, price, stores, pp)
		WHERE platform.plans.code = 'trial' ON CONFLICT (code) DO NOTHING`)
	require.NoError(t, err)
	l := billingTenant(t, "plan-own")
	other := billingTenant(t, "plan-own-other")
	otherBefore := subOf(t, other)
	fake := theApp.Billing().Provider.(*billing.Fake)
	sub, err := fake.CreateSubscription(ctx, billing.CreateSubscriptionReq{TenantID: tenants[l].ID, PlanID: "plan_fake_ownsmall"})
	require.NoError(t, err)
	end := time.Now().Add(20 * 24 * time.Hour)
	_, err = admin(t).Exec(ctx, `UPDATE platform.subscriptions SET provider_subscription_id = $2, provider = 'fake', provider_status = 'active',
		status = 'active', current_period_end = $3, plan_id = (SELECT id FROM platform.plans WHERE code = 'own_small') WHERE tenant_id = $1`,
		tenants[l].ID, sub.ID, end)
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `UPDATE platform.tenants SET plan_id = (SELECT id FROM platform.plans WHERE code = 'own_small'), status = 'active' WHERE id = $1`, tenants[l].ID)
	require.NoError(t, err)

	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	preview := func(code string) resp { return get(t, host, "/api/billing/plan-change?planCode="+code, tok) }
	change := func(code string) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/billing/plan-change", token: tok, body: map[string]any{"planCode": code}})
	}

	require.Equal(t, 400, preview("trial").Code, "hidden plans cannot be chosen")
	p := preview("own_big")
	require.Equal(t, 200, p.Code, p.Raw)
	require.Equal(t, "upgrade", p.Body["kind"])
	require.Equal(t, true, p.Body["allowed"], p.Raw)
	require.NotContains(t, p.Raw, "plan_fake", "no provider IDs")
	c := change("own_big")
	require.Equal(t, 200, c.Code, c.Raw)
	require.Equal(t, "now", c.Body["when"])
	got, err := fake.FetchSubscription(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, "plan_fake_ownbig", got.PlanID)
	var planCode string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT p.code FROM platform.tenants t JOIN platform.plans p ON p.id = t.plan_id WHERE t.id = $1`,
		tenants[l].ID).Scan(&planCode))
	require.Equal(t, "own_big", planCode, "the shop's limits follow at once")

	// A second store: the small plan (1 store) is refused.
	_, err = admin(t).Exec(ctx, `INSERT INTO `+schema(l)+`.stores (name, slug) VALUES ('Second Store', 'second-store')`)
	require.NoError(t, err)
	p = preview("own_small")
	require.Equal(t, false, p.Body["allowed"])
	require.Contains(t, p.Body["refusal"], "Stores 2 of 1 allowed")
	require.Equal(t, 409, change("own_small").Code)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.stores SET deleted_at = now() WHERE name = 'Second Store'`)
	require.NoError(t, err)

	// Downgrade scheduled for the period end, then cancelled.
	c = change("own_small")
	require.Equal(t, 200, c.Code, c.Raw)
	require.Equal(t, "period_end", c.Body["when"])
	require.Equal(t, "plan_fake_ownsmall", fake.Pending(sub.ID))
	v := get(t, host, "/api/billing/subscription", tok)
	require.Equal(t, "own_big", v.Body["plan"].(map[string]any)["code"])
	require.Equal(t, "own_small", v.Body["pendingChange"].(map[string]any)["plan"].(map[string]any)["code"])

	// Staff and other shops cannot see or touch it.
	createUser(t, l, tok, "cashier@plan-own.test", "biller", "cashier-plan-own-1")
	btok, _ := login(t, l, "cashier@plan-own.test", "cashier-plan-own-1")
	require.Equal(t, 403, get(t, host, "/api/billing/plan-change?planCode=own_big", btok).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/billing/plan-change", token: btok, body: map[string]any{"planCode": "own_big"}}).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/billing/plan-change/cancel", token: btok}).Code)
	otok, _ := ownerLogin(t, other)
	oh := tenants[other].Host
	require.Equal(t, 401, do(t, rq{method: "POST", host: oh, path: "/api/billing/plan-change/cancel", token: tok}).Code, "a token of another shop")
	require.Equal(t, 409, do(t, rq{method: "POST", host: oh, path: "/api/billing/plan-change/cancel", token: otok}).Code, "nothing scheduled there")
	require.Equal(t, 409, do(t, rq{method: "POST", host: oh, path: "/api/billing/plan-change", token: otok, body: map[string]any{"planCode": "own_big"}}).Code,
		"a trial shop subscribes instead")
	require.Equal(t, "plan_fake_ownsmall", fake.Pending(sub.ID), "still scheduled")

	cc := do(t, rq{method: "POST", host: host, path: "/api/billing/plan-change/cancel", token: tok})
	require.Equal(t, 204, cc.Code, cc.Raw)
	require.Empty(t, fake.Pending(sub.ID))
	v = get(t, host, "/api/billing/subscription", tok)
	require.Nil(t, v.Body["pendingChange"])
	require.Equal(t, otherBefore, subOf(t, other), "other shop untouched")
	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action IN ('billing.plan_change','billing.plan_change_cancelled')`).Scan(&audits))
	require.Equal(t, 3, audits)
}
