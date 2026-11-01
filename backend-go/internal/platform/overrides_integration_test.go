//go:build integration

package platform_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
)

// TestTenantOverrides (S9 security note): super_admin only, step-up,
// reason, fixed keys and bounds, replace and revoke (never edit), the
// database refuses rewrites, expiring-soon flags, the list filter, the
// inbox notice and the audit trail.
func TestTenantOverrides(t *testing.T) {
	var mu sync.Mutex
	var forgot []string
	e := newEnvWith(t, func(c *platform.Config) {
		c.InboxEmail = "ops@platform.test"
		c.OverridesChanged = func(id string) {
			mu.Lock()
			forgot = append(forgot, id)
			mu.Unlock()
		}
	})
	root := e.mfaLogin(t, rootEmail, newPW, rootSession(t, e).secret)
	id := e.newTenant(t, root, "override-shop")
	base := "/tenants/" + id + "/overrides"
	post := func(s session, body map[string]any) resp {
		return e.do(req{method: "POST", path: base, token: s.token, body: body})
	}
	ok := map[string]any{"key": "max_stores", "value": 5, "reason": "Pilot with three branches", "days": 30}

	// Step-up first.
	require.Equal(t, 403, post(root, ok).Code, "step-up required")
	e.stepUp(t, root)

	// Validation: key, reason, days, value type and bounds.
	for _, bad := range []map[string]any{
		{"key": "max_cashiers", "value": 2, "reason": "Unknown key is refused"},
		{"key": "max_stores", "value": 5, "reason": "short"},
		{"key": "max_stores", "value": 5, "reason": "Too long an expiry", "days": 400},
		{"key": "max_stores", "value": 0, "reason": "Zero would mean no limit"},
		{"key": "max_stores", "value": 2.5, "reason": "Whole numbers only"},
		{"key": "max_stores", "value": 101, "reason": "Above the hard cap"},
		{"key": "public_shop", "value": 1, "reason": "A flag needs true or false"},
	} {
		r := post(root, bad)
		require.Equal(t, 400, r.Code, "%v: %s", bad, r.Raw)
	}

	a := post(root, ok)
	require.Equal(t, 201, a.Code, a.Raw)
	require.Equal(t, true, a.Body["active"])
	require.Equal(t, false, a.Body["expiringSoon"])
	// Replacing: the old row is revoked as "replaced".
	b := post(root, map[string]any{"key": "max_stores", "value": 6, "reason": "Fourth branch opening soon", "days": 3})
	require.Equal(t, 201, b.Code, b.Raw)
	require.Equal(t, true, b.Body["expiringSoon"], "ends within 7 days")
	flag := post(root, map[string]any{"key": "csv_import", "value": false, "reason": "Import misuse under review"})
	require.Equal(t, 201, flag.Code, flag.Raw)

	l := e.do(req{method: "GET", path: base, token: root.token})
	require.Equal(t, 200, l.Code)
	require.Contains(t, l.Raw, `"revokeReason":"replaced"`)

	// Tenants list: counts, expiring soon, and the filter.
	list := e.do(req{method: "GET", path: "/tenants?overrides=1", token: root.token})
	require.Equal(t, []string{id}, ids(t, list))
	require.Contains(t, list.Raw, `"activeOverrides":2`)
	require.Contains(t, list.Raw, `"overridesExpiringSoon":true`)
	one := e.do(req{method: "GET", path: "/tenants/" + id, token: root.token})
	require.EqualValues(t, 2, one.Body["activeOverrides"])

	// Revoke: reason needed, once only.
	rev := fmt.Sprintf("%s/%v/revoke", base, flag.Body["id"])
	require.Equal(t, 400, e.do(req{method: "POST", path: rev, token: root.token, body: map[string]any{"reason": "no"}}).Code)
	require.Equal(t, 204, e.do(req{method: "POST", path: rev, token: root.token, body: map[string]any{"reason": "Review finished, all good"}}).Code)
	require.Equal(t, 409, e.do(req{method: "POST", path: rev, token: root.token, body: map[string]any{"reason": "Review finished, all good"}}).Code)

	// The app's database role cannot rewrite, reopen or delete overrides.
	ctx := context.Background()
	pool := tdb.AppPool(t, 1)
	for _, tc := range []struct{ name string }{{"value"}, {"reopen"}, {"delete"}} {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `SET LOCAL ROLE pos_platform`)
		require.NoError(t, err)
		switch tc.name {
		case "value":
			_, err = tx.Exec(ctx, `UPDATE platform.tenant_overrides SET value = '100'`)
		case "reopen":
			_, err = tx.Exec(ctx, `UPDATE platform.tenant_overrides SET revoked_at = NULL, revoked_by = NULL WHERE revoked_at IS NOT NULL`)
		case "delete":
			_, err = tx.Exec(ctx, `DELETE FROM platform.tenant_overrides`)
		}
		require.Error(t, err, tc.name)
		require.NoError(t, tx.Rollback(ctx))
	}

	// Only super_admin: billing_admin and support are refused.
	for _, role := range []string{"billing_admin", "support"} {
		email := role + "-override@platform.test"
		sec := addUser(t, e, email, role, role+"-override-pass-1")
		s := e.mfaLogin(t, email, role+"-override-pass-1", sec)
		e.stepUp(t, s)
		require.Equal(t, 403, post(s, ok).Code, role)
		require.Equal(t, 200, e.do(req{method: "GET", path: base, token: s.token}).Code, role+" may read")
	}

	// Inbox notices (one per change), cache refresh, audit rows.
	admin := tdb.Admin(t)
	var notices int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'override_notice' AND to_addr = 'ops@platform.test'`).Scan(&notices))
	require.Equal(t, 4, notices, "set, set, set, revoke")
	mu.Lock()
	require.Len(t, forgot, 4)
	mu.Unlock()
	var audits int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log WHERE target_tenant = $1
		AND action IN ('tenant.override_set','tenant.override_revoke') AND after IS NOT NULL`, id).Scan(&audits))
	require.Equal(t, 4, audits)
	require.Equal(t, 200, e.do(req{method: "GET", path: "/override-keys", token: root.token}).Code)
}
