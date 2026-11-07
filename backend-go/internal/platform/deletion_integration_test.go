//go:build integration

package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

// TestFinaliseDeletion (S10 item 4): a tenant pending deletion is
// finalised only after its deletion date, with a final export whose
// restore test passed, by a super admin with step-up, a reason and the
// slug typed. It cancels the provider subscription at once and marks the
// tenant deleted; nothing is dropped. Warnings go out 14/7/1 days before.
func TestFinaliseDeletion(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	admin := tdb.Admin(t)
	call := func(method, path string, body any) resp {
		return e.do(req{method: method, path: path, token: s.token, body: body})
	}
	id := e.newTenant(t, s, "final-delete")
	other := e.newTenant(t, s, "final-delete-other")
	_, err := admin.Exec(ctx, `UPDATE platform.tenants SET billing_email = 'accounts@final-delete.test' WHERE id = $1`, id)
	require.NoError(t, err)

	// Console delete: pending deletion, deletion date 30 days out.
	e.stepUp(t, s)
	d := call("DELETE", "/tenants/"+id, map[string]any{"reason": "Owner closed the shop"})
	require.Equal(t, 200, d.Code, d.Raw)
	var due time.Time
	require.NoError(t, admin.QueryRow(ctx, `SELECT purge_after FROM platform.tenants WHERE id = $1`, id).Scan(&due))
	require.WithinDuration(t, time.Now().Add(30*24*time.Hour), due, time.Minute)

	// Warnings: the nearest one, to the billing contact and the owner, once.
	bs := &billing.Service{Sessions: e.sessions, Provider: e.fake, Cfg: billing.DefaultConfig,
		Now: func() time.Time { return due.Add(-10 * 24 * time.Hour) }}
	_, err = bs.RunDeletionWarnings(ctx)
	require.NoError(t, err)
	_, err = bs.RunDeletionWarnings(ctx)
	require.NoError(t, err)
	warned := func(kind string) []string {
		rows, err := admin.Query(ctx, `SELECT o.to_addr::text FROM platform.email_outbox o WHERE o.template = 'billing_notice'
			AND o.template_data->>'tenant_id' = $1 AND o.template_data->>'kind' = $2 ORDER BY o.to_addr`, id, kind)
		require.NoError(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var a string
			require.NoError(t, rows.Scan(&a))
			out = append(out, a)
		}
		return out
	}
	require.Equal(t, []string{"accounts@final-delete.test", "owner@final-delete.test"}, warned("deletion_warning_14"))
	bs.Now = func() time.Time { return due.Add(-6 * 24 * time.Hour) }
	_, err = bs.RunDeletionWarnings(ctx)
	require.NoError(t, err)
	require.Len(t, warned("deletion_warning_7"), 2)
	require.Empty(t, warned("deletion_warning_1"))

	// Not yet: before the deletion date; then no verified final export.
	v := call("GET", "/tenants/"+id+"/deletion", nil)
	require.Equal(t, 200, v.Code, v.Raw)
	require.Equal(t, false, v.Body["canFinalise"])
	require.Contains(t, v.Body["blocker"], "The deletion date is")
	require.Equal(t, []any{"14 days", "7 days"}, v.Body["warningsSent"])
	fin := func(body map[string]any) resp { return call("POST", "/tenants/"+id+"/finalise-deletion", body) }
	ok := map[string]any{"reason": "Owner asked to close for good", "confirm": "final-delete"}
	require.Equal(t, 409, fin(ok).Code)
	_, err = admin.Exec(ctx, `UPDATE platform.tenants SET purge_after = now() - interval '1 minute' WHERE id = $1`, id)
	require.NoError(t, err)
	r := fin(ok)
	require.Equal(t, 409, r.Code)
	require.Contains(t, r.Raw, "final export has not succeeded")
	exportID := uuid.New()
	_, err = admin.Exec(ctx, `INSERT INTO platform.tenant_exports (id, tenant_id, kind, status, requested_by, object_key, size_bytes, sha256, manifest_sha256, finished_at)
		VALUES ($1, $2, 'final', 'succeeded', 'system:deletion', 'exports/x.age', 10, repeat('a', 64), repeat('b', 64), now())`, exportID, id)
	require.NoError(t, err)
	r = fin(ok)
	require.Equal(t, 409, r.Code)
	require.Contains(t, r.Raw, "restore test has not passed")
	_, err = admin.Exec(ctx, `INSERT INTO platform.tenant_restores (id, export_id, tenant_id, mode, status, requested_by, finished_at)
		VALUES ($1, $2, $3, 'test', 'succeeded', 'system:deletion', now())`, uuid.New(), exportID, id)
	require.NoError(t, err)

	// A live provider subscription is cancelled at once.
	sub, err := e.fake.CreateSubscription(ctx, billing.CreateSubscriptionReq{TenantID: id, PlanID: "plan_fake_x"})
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `UPDATE platform.subscriptions SET provider_subscription_id = $2, provider = 'fake', provider_status = 'active'
		WHERE tenant_id = $1`, id, sub.ID)
	require.NoError(t, err)
	var otherBefore string
	require.NoError(t, admin.QueryRow(ctx, `SELECT row_to_json(t)::text FROM platform.tenants t WHERE id = $1`, other).Scan(&otherBefore))

	// Step-up has expired by now; then a wrong slug and a short reason.
	e.advance(6 * time.Minute)
	r = fin(ok)
	require.Equal(t, 403, r.Code)
	e.advance(31 * time.Second)
	require.Equal(t, 200, e.do(req{method: "POST", path: "/auth/step-up", token: s.token,
		body: map[string]string{"code": security.TOTPCode(root.secret, e.clock())}}).Code)
	require.Equal(t, 400, fin(map[string]any{"reason": "Owner asked to close for good", "confirm": "final-delete-other"}).Code)
	require.Equal(t, 400, fin(map[string]any{"reason": "short", "confirm": "final-delete"}).Code)
	r = fin(ok)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "deleted", r.Body["status"])
	cancelled, atEnd := e.fake.Cancelled(sub.ID)
	require.True(t, cancelled)
	require.False(t, atEnd, "cancelled at once, not at period end")
	var status, subStatus string
	var deletedBy *int64
	require.NoError(t, admin.QueryRow(ctx, `SELECT t.status, t.deleted_by, s.status FROM platform.tenants t
		JOIN platform.subscriptions s ON s.tenant_id = t.id WHERE t.id = $1`, id).Scan(&status, &deletedBy, &subStatus))
	require.Equal(t, "deleted", status)
	require.NotNil(t, deletedBy)
	require.Equal(t, "cancelled", subStatus)
	var schemaLeft bool
	require.NoError(t, admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 't_' || $1)`, id).Scan(&schemaLeft))
	require.True(t, schemaLeft, "the schema is not dropped")
	require.Equal(t, 409, fin(ok).Code, "only once")
	var audits int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log WHERE target_tenant = $1 AND action = 'tenant.finalise_deletion'`, id).Scan(&audits))
	require.Equal(t, 1, audits)
	var otherAfter string
	require.NoError(t, admin.QueryRow(ctx, `SELECT row_to_json(t)::text FROM platform.tenants t WHERE id = $1`, other).Scan(&otherAfter))
	require.Equal(t, otherBefore, otherAfter, "other tenant untouched")
}
