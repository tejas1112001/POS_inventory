//go:build integration

package platform_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

func TestConsoleBilling(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	admin := tdb.Admin(t)
	call := func(method, path string, body any) resp {
		return e.do(req{method: method, path: path, token: s.token, body: body})
	}

	// Plans: created and edited by the owner of the platform, validated.
	plan := map[string]any{"code": "starter_test", "name": "Starter", "priceMinor": 29900, "interval": "monthly",
		"razorpayPlanId": "plan_fake_starter", "trialDays": 14, "isPublic": true,
		"limits":   map[string]any{"max_stores": 1, "max_users": 3, "max_products": 500, "max_emails_per_day": 100, "max_storage_mb": 200},
		"features": map[string]any{"reports_export": false, "public_shop": true}}
	c := call("POST", "/plans", plan)
	require.Equal(t, 201, c.Code, c.Raw)
	planID := c.Body["id"]
	require.Equal(t, 409, call("POST", "/plans", plan).Code, "codes are unique")
	for name, bad := range map[string]map[string]any{
		"unknown limit":   {"limits": map[string]any{"max_planets": 1}},
		"negative limit":  {"limits": map[string]any{"max_users": -1}},
		"feature value":   {"features": map[string]any{"public_shop": "yes"}},
		"unknown feature": {"features": map[string]any{"teleport": true}},
		"bad code":        {"code": "Bad Code"},
		"missing limit":   {"limits": map[string]any{"max_stores": 1}},
		"bad interval":    {"interval": "weekly"},
		"bad provider id": {"razorpayPlanId": "not-a-plan"},
		"price":           {"priceMinor": -1},
	} {
		b := map[string]any{}
		for k, v := range plan {
			b[k] = v
		}
		b["code"] = "other_test"
		b["razorpayPlanId"] = "plan_fake_other"
		for k, v := range bad {
			b[k] = v
		}
		r := call("POST", "/plans", b)
		require.Equal(t, 400, r.Code, "%s: %s", name, r.Raw)
	}
	path := "/plans/" + jsonNum(planID)
	p := call("PATCH", path, map[string]any{"name": "Starter Plus", "limits": map[string]any{"max_stores": 2}})
	require.Equal(t, 200, p.Code, p.Raw)
	require.Equal(t, "Starter Plus", p.Body["name"])
	lim := p.Body["limits"].(map[string]any)
	require.EqualValues(t, 2, lim["max_stores"])
	require.EqualValues(t, 500, lim["max_products"], "a PATCH of one limit keeps the others")
	require.Equal(t, 400, call("PATCH", path, map[string]any{"priceMinor": 39900}).Code, "a provider-linked price is fixed")
	require.Equal(t, 400, call("PATCH", path, map[string]any{"razorpayPlanId": "plan_fake_changed"}).Code, "set once")
	require.Equal(t, 400, call("PATCH", path, map[string]any{"code": "renamed"}).Code)
	l := e.do(req{method: "GET", path: "/plans", token: s.token})
	require.Equal(t, 200, l.Code)
	require.Contains(t, l.Raw, "starter_test")

	// A tenant to manage.
	created := call("POST", "/tenants", map[string]string{"name": "Billing Desk Shop", "slug": "billing-desk",
		"ownerEmail": "owner@billing-desk.test", "ownerName": "Mira"})
	require.Equal(t, 202, created.Code, created.Raw)
	id := created.Body["id"].(string)
	bystander := call("POST", "/tenants", map[string]string{"name": "Billing Bystander", "slug": "billing-bystander",
		"ownerEmail": "owner@billing-bystander.test", "ownerName": "Anu"})
	require.Equal(t, 202, bystander.Code, bystander.Raw)
	snapshot := func() string {
		var row string
		require.NoError(t, admin.QueryRow(ctx, `SELECT row_to_json(x)::text FROM (SELECT s.*, t.status AS tenant_status, t.plan_id AS tenant_plan
			FROM platform.subscriptions s JOIN platform.tenants t ON t.id = s.tenant_id WHERE s.tenant_id = $1) x`,
			bystander.Body["id"]).Scan(&row))
		return row
	}
	before := snapshot()
	tb := call("GET", "/tenants/"+id+"/billing", nil)
	require.Equal(t, 200, tb.Code, tb.Raw)
	require.Equal(t, "trialing", tb.Body["subscription"].(map[string]any)["status"])
	require.Empty(t, tb.Body["invoices"])

	// Assign a plan (with a reason), extend the trial.
	require.Equal(t, 400, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "starter_test"}).Code, "reason required")
	a := call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "starter_test", "reason": "Pilot customer"})
	require.Equal(t, 200, a.Code, a.Raw)
	require.Equal(t, "starter_test", a.Body["planCode"])
	var tenantPlan string
	require.NoError(t, admin.QueryRow(ctx, `SELECT p.code FROM platform.tenants t JOIN platform.plans p ON p.id = t.plan_id WHERE t.id = $1`, id).Scan(&tenantPlan))
	require.Equal(t, "starter_test", tenantPlan, "the tenant's limits follow")
	require.Equal(t, 400, call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "nope", "reason": "Pilot customer"}).Code)

	var trialEnd time.Time
	require.NoError(t, admin.QueryRow(ctx, `SELECT trial_end FROM platform.subscriptions WHERE tenant_id = $1`, id).Scan(&trialEnd))
	x := call("POST", "/tenants/"+id+"/subscription/extend-trial", map[string]any{"days": 7, "reason": "Asked at onboarding"})
	require.Equal(t, 200, x.Code, x.Raw)
	var newEnd time.Time
	require.NoError(t, admin.QueryRow(ctx, `SELECT trial_end FROM platform.subscriptions WHERE tenant_id = $1`, id).Scan(&newEnd))
	require.WithinDuration(t, trialEnd.Add(7*24*time.Hour), newEnd, time.Second)
	// A read-only tenant whose trial ended gets its trial back.
	_, err := admin.Exec(ctx, `UPDATE platform.subscriptions SET status = 'read_only', trial_end = now() - interval '1 day' WHERE tenant_id = $1`, id)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `UPDATE platform.tenants SET status = 'read_only' WHERE id = $1`, id)
	require.NoError(t, err)
	x = call("POST", "/tenants/"+id+"/subscription/extend-trial", map[string]any{"days": 3, "reason": "Needs more time"})
	require.Equal(t, 200, x.Code, x.Raw)
	require.Equal(t, "trialing", x.Body["status"])
	require.Equal(t, "trialing", x.Body["tenantStatus"])
	require.Equal(t, 400, call("POST", "/tenants/"+id+"/subscription/extend-trial", map[string]any{"days": 0, "reason": "Needs more time"}).Code)

	// Grace: only for payment problems; it lifts a billing suspension.
	require.Equal(t, 409, call("POST", "/tenants/"+id+"/subscription/grace", map[string]any{"days": 5, "reason": "Bank holiday"}).Code)
	_, err = admin.Exec(ctx, `UPDATE platform.subscriptions SET status = 'suspended' WHERE tenant_id = $1`, id)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `UPDATE platform.tenants SET status = 'suspended', suspension_source = 'billing' WHERE id = $1`, id)
	require.NoError(t, err)
	g := call("POST", "/tenants/"+id+"/subscription/grace", map[string]any{"days": 5, "reason": "Bank holiday"})
	require.Equal(t, 200, g.Code, g.Raw)
	require.Equal(t, "grace", g.Body["status"])
	require.Equal(t, "grace", g.Body["tenantStatus"])
	require.Nil(t, g.Body["suspensionSource"])
	pd := call("GET", "/billing/past-due", nil)
	require.Equal(t, 200, pd.Code)
	require.Contains(t, pd.Raw, id)

	// Cancel needs step-up and cancels at the provider too.
	sub, err := e.fake.CreateSubscription(ctx, billing.CreateSubscriptionReq{TenantID: id, PlanID: "plan_fake_starter"})
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `UPDATE platform.subscriptions SET provider_subscription_id = $2, provider = 'fake', provider_status = 'active',
		status = 'active' WHERE tenant_id = $1`, id, sub.ID)
	require.NoError(t, err)
	assign := call("POST", "/tenants/"+id+"/subscription", map[string]any{"planCode": "starter_test", "reason": "Pilot customer"})
	require.Equal(t, 409, assign.Code, "no local plan while the provider bills")
	cx := call("POST", "/tenants/"+id+"/subscription/cancel", map[string]any{"atPeriodEnd": true, "reason": "Closing the shop"})
	require.Equal(t, 403, cx.Code)
	require.Equal(t, "STEP_UP_REQUIRED", cx.Body["code"])
	e.advance(31 * time.Second)
	require.Equal(t, 200, e.do(req{method: "POST", path: "/auth/step-up", token: s.token,
		body: map[string]string{"code": security.TOTPCode(root.secret, e.clock())}}).Code)
	cx = call("POST", "/tenants/"+id+"/subscription/cancel", map[string]any{"atPeriodEnd": true, "reason": "Closing the shop"})
	require.Equal(t, 200, cx.Code, cx.Raw)
	require.Equal(t, true, cx.Body["cancelAtPeriodEnd"])
	cancelled, atEnd := e.fake.Cancelled(sub.ID)
	require.True(t, cancelled)
	require.True(t, atEnd)
	now := call("POST", "/tenants/"+id+"/subscription/cancel", map[string]any{"atPeriodEnd": false, "reason": "Closing the shop"})
	require.Equal(t, 200, now.Code, now.Raw)
	require.Equal(t, "cancelled", now.Body["status"])
	require.Equal(t, 409, call("POST", "/tenants/"+id+"/subscription/cancel", map[string]any{"reason": "Closing the shop"}).Code)

	// Lists and the audit trail.
	_, err = admin.Exec(ctx, `UPDATE platform.subscriptions SET current_period_end = now() + interval '2 days' WHERE tenant_id = $1`, id)
	require.NoError(t, err)
	ex := call("GET", "/billing/expiring?days=7", nil)
	require.Equal(t, 200, ex.Code)
	require.Contains(t, ex.Raw, id)
	require.Equal(t, 400, call("GET", "/billing/expiring?days=0", nil).Code)
	require.Equal(t, 200, call("GET", "/billing/invoices?tenantId="+id, nil).Code)
	require.Equal(t, 400, call("GET", "/billing/invoices?tenantId=../x", nil).Code)
	var audits int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log WHERE target_tenant = $1
		AND action IN ('billing.assign_plan','billing.extend_trial','billing.grace','billing.cancel')`, id).Scan(&audits))
	require.Equal(t, 6, audits)

	require.Equal(t, before, snapshot(), "console actions touch only the named tenant")

	// Support may look, not change; nobody may without signing in, and a
	// tenant token is not a console token.
	supportSecret := addUser(t, e, "support-billing@pos.test", platform.RoleSupport, "support-billing-pass-1")
	sup := e.mfaLogin(t, "support-billing@pos.test", "support-billing-pass-1", supportSecret)
	require.Equal(t, 200, e.do(req{method: "GET", path: "/plans", token: sup.token}).Code)
	require.Equal(t, 200, e.do(req{method: "GET", path: "/billing/past-due", token: sup.token}).Code)
	require.Equal(t, 403, e.do(req{method: "POST", path: "/plans", token: sup.token, body: plan}).Code)
	require.Equal(t, 403, e.do(req{method: "POST", path: "/tenants/" + id + "/subscription/grace", token: sup.token,
		body: map[string]any{"days": 5, "reason": "Bank holiday"}}).Code)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/billing/expiring"}).Code)
	tt, _, err := e.tenSign.Sign("admin.pos.test", "1", authtoken.Claims{SessionID: "x"})
	require.NoError(t, err)
	for _, path := range []string{"/plans", "/billing/past-due", "/billing/invoices", "/tenants/" + id + "/billing"} {
		require.Equal(t, 401, e.do(req{method: "GET", path: path, token: tt}).Code, path)
	}
	require.Equal(t, 404, call("GET", "/tenants/zzzzzzzzzz/billing", nil).Code)
}

func jsonNum(v any) string {
	return strconv.FormatInt(int64(v.(float64)), 10)
}
