//go:build integration

package isolation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// platformActor returns a platform user id to sign override rows.
func platformActor(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `INSERT INTO platform.platform_users (email, name, role, password_hash, must_change_password)
		VALUES ('override-actor@platform.test', 'Override Actor', 'super_admin', 'x', true)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name RETURNING id`).Scan(&id))
	return id
}

// override writes a row as the console would; offset/days are relative to now.
func override(t *testing.T, l, key, value string, startDaysAgo, days int, revoked bool) {
	t.Helper()
	actor := platformActor(t)
	_, err := admin(t).Exec(context.Background(), `INSERT INTO platform.tenant_overrides
		(tenant_id, key, value, reason, created_at, expires_at, created_by, revoked_at, revoked_by, revoke_reason)
		VALUES ($1, $2, $3::text::jsonb, 'integration test override', now() - make_interval(days => $4),
			now() - make_interval(days => $4) + make_interval(days => $5), $6,
			CASE WHEN $7 THEN now() END, CASE WHEN $7 THEN $6::bigint END, CASE WHEN $7 THEN 'test' END)`,
		tenants[l].ID, key, value, startDaysAgo, days, actor, revoked)
	require.NoError(t, err)
}

func addStore(t *testing.T, l, tok, name string) int {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/stores", token: tok, body: map[string]any{"name": name}}).Code
}

// TestOverridesAreEnforced (S9): every limit check uses plan + active
// overrides; expired and revoked overrides do not count; flags switch CSV
// import and the new checkout off; the worker writes the usage snapshot.
func TestOverridesAreEnforced(t *testing.T) {
	one := limitPlan(t, "limits_override_one", `{"max_stores":1,"max_users":5,"max_products":50,"max_emails_per_day":300,"max_storage_mb":1000}`)

	// Raised: plan 1 store, override 2.
	up := billingTenant(t, "ovr-raise")
	onPlan(t, up, one)
	override(t, up, "max_stores", "2", 0, 30, false)
	tok, _ := ownerLogin(t, up)
	require.Equal(t, 201, addStore(t, up, tok, "Second"), "the override allows a second store")
	require.Equal(t, 403, addStore(t, up, tok, "Third"))

	// Lowered: trial plan (3 stores), override 1.
	down := billingTenant(t, "ovr-lower")
	override(t, down, "max_stores", "1", 0, 30, false)
	tok, _ = ownerLogin(t, down)
	require.Equal(t, 403, addStore(t, down, tok, "Second"), "the override lowers the limit")

	// Expired and revoked overrides do not count.
	for _, c := range []struct {
		slug      string
		ago, days int
		revoked   bool
	}{{"ovr-expired", 40, 30, false}, {"ovr-revoked", 0, 30, true}} {
		l := billingTenant(t, c.slug)
		onPlan(t, l, one)
		override(t, l, "max_stores", "5", c.ago, c.days, c.revoked)
		tk, _ := ownerLogin(t, l)
		require.Equal(t, 403, addStore(t, l, tk, "Second"), c.slug)
	}

	// Flags: CSV import and the new checkout off.
	f := billingTenant(t, "ovr-flags")
	override(t, f, "csv_import", "false", 0, 30, false)
	override(t, f, "new_checkout", "false", 0, 30, false)
	tok, _ = ownerLogin(t, f)
	host := tenants[f].Host
	imp := do(t, rq{method: "POST", host: host, path: "/api/products/import", token: tok, body: map[string]any{}})
	require.Equal(t, 403, imp.Code, imp.Raw)
	require.Contains(t, imp.Raw, "FEATURE_NOT_IN_PLAN")
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"newCheckout": true}}).Code)
	s := get(t, host, "/api/settings", tok)
	require.Equal(t, false, s.Body["newCheckoutAllowed"])
	require.Equal(t, false, s.Body["newCheckout"], "the shop's choice does not count while the flag is off")

	// Without overrides both stay on.
	plain := get(t, tenants["a"].Host, "/api/settings", func() string { tk, _ := ownerLogin(t, "a"); return tk }())
	require.Equal(t, true, plain.Body["newCheckoutAllowed"])

	// Usage snapshot: counts only, per tenant.
	n, err := theApp.Worker().Maintenance().SnapshotUsage(context.Background())
	require.NoError(t, err)
	require.Positive(t, n)
	var users, stores int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT users, stores FROM platform.tenant_usage WHERE tenant_id = $1`, tenants[up].ID).Scan(&users, &stores))
	require.Equal(t, 1, users)
	require.Equal(t, 2, stores)
}
