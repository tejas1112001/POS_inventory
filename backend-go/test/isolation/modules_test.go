//go:build integration

package isolation

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
)

// Industry Phase 1 item 1 (docs/INDUSTRY_PHASE1_DESIGN.md): business type,
// module registry, entitlements.

func strList(v any) []string {
	out := []string{}
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func putBusiness(t *testing.T, l, tok string, body map[string]any) resp {
	t.Helper()
	return do(t, rq{method: "PUT", host: tenants[l].Host, path: "/api/settings/business", token: tok, body: body})
}

// setModules switches a shop's type and modules as its owner would.
func setModules(t *testing.T, l, businessType string, mods ...string) {
	t.Helper()
	tok, _ := ownerLogin(t, l)
	body := map[string]any{"businessType": businessType}
	if mods != nil {
		body["modules"] = mods
	}
	r := putBusiness(t, l, tok, body)
	require.Equal(t, 200, r.Code, r.Raw)
}

// An existing (retail) shop is unchanged: type retail, no modules, every
// Phase 1 module entitled (E1), on /api/settings and /api/auth/me.
func TestModulesDefaultsForAnExistingShop(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "mod-default")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	all := []string{"product_attributes", "barcode_labels", "stock_counts"}

	s := get(t, host, "/api/settings", tok)
	require.Equal(t, 200, s.Code, s.Raw)
	require.Equal(t, "retail", s.Body["businessType"])
	require.Empty(t, strList(s.Body["modules"]))
	require.Equal(t, all, strList(s.Body["entitledModules"]))

	me := get(t, host, "/api/auth/me", tok)
	require.Equal(t, 200, me.Code, me.Raw)
	tn := me.Body["tenant"].(map[string]any)
	require.Equal(t, "retail", tn["businessType"])
	require.Empty(t, strList(tn["modules"]))
	require.Equal(t, all, strList(tn["entitledModules"]))

	m := get(t, host, "/api/modules", tok)
	require.Equal(t, 200, m.Code, m.Raw)
	require.Len(t, m.Body["modules"], 3)
	for _, x := range m.Body["modules"].([]any) {
		row := x.(map[string]any)
		require.Equal(t, true, row["entitled"], row["key"])
		require.Equal(t, false, row["enabled"], row["key"])
	}
	require.Equal(t, all, strList(m.Body["recommended"].(map[string]any)["clothing"]))

	// The column CHECK refuses an unknown module, and the type CHECK an
	// unknown type.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET modules = '{batches}'`)
	require.ErrorContains(t, err, "settings_modules_check")
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET business_type = 'bakery'`)
	require.ErrorContains(t, err, "settings_business_type_check")
	var bt string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT business_type FROM platform.tenants WHERE id = $1`, tenants[l].ID).Scan(&bt))
	require.Equal(t, "retail", bt)
}

// Only the owner (modules:manage) changes the type; choosing a type turns
// on its recommended modules, the console copy follows, and the change is
// audited. Unticking a module keeps the type.
func TestBusinessTypeChange(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "mod-change")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	createUser(t, l, tok, "mod-admin@"+l+".test", "admin", "mod-admin-pass-123")
	createUser(t, l, tok, "mod-biller@"+l+".test", "biller", "mod-biller-pass-123")
	atok, _ := login(t, l, "mod-admin@"+l+".test", "mod-admin-pass-123")
	btok, _ := login(t, l, "mod-biller@"+l+".test", "mod-biller-pass-123")

	for _, tk := range []string{atok, btok} {
		r := putBusiness(t, l, tk, map[string]any{"businessType": "clothing"})
		require.Equal(t, 403, r.Code, r.Raw)
		require.Equal(t, "FORBIDDEN", codeOf(r))
	}
	require.Equal(t, 200, get(t, host, "/api/modules", btok).Code, "billers have settings:view")

	require.Equal(t, 400, putBusiness(t, l, tok, map[string]any{"businessType": "bakery"}).Code)
	require.Equal(t, 400, putBusiness(t, l, tok, map[string]any{"modules": []string{"batches"}}).Code)

	r := putBusiness(t, l, tok, map[string]any{"businessType": "clothing"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "clothing", r.Body["businessType"])
	require.Equal(t, []string{"product_attributes", "barcode_labels", "stock_counts"}, strList(r.Body["modules"]))

	var bt string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT business_type FROM platform.tenants WHERE id = $1`, tenants[l].ID).Scan(&bt))
	require.Equal(t, "clothing", bt)

	// The owner unticks one module: the type stays.
	r = putBusiness(t, l, tok, map[string]any{"modules": []string{"stock_counts", "barcode_labels"}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "clothing", r.Body["businessType"])
	require.Equal(t, []string{"barcode_labels", "stock_counts"}, strList(r.Body["modules"]), "registry order")

	// The same request again changes nothing and writes no audit row.
	require.Equal(t, 200, putBusiness(t, l, tok, map[string]any{"modules": []string{"barcode_labels", "stock_counts"}}).Code)
	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'settings.business_update'`).Scan(&audits))
	require.Equal(t, 2, audits)

	// Back to retail with no list: retail recommends nothing.
	r = putBusiness(t, l, tok, map[string]any{"businessType": "retail"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Empty(t, strList(r.Body["modules"]))

	// The admin role never holds modules:manage, the owner always does.
	var adminHas, ownerHas bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM `+schema(l)+`.role_permissions rp JOIN `+schema(l)+`.roles r ON r.id = rp.role_id
			JOIN `+schema(l)+`.permissions p ON p.id = rp.permission_id WHERE r.system_key = 'admin' AND p.module = 'modules'),
		EXISTS (SELECT 1 FROM `+schema(l)+`.role_permissions rp JOIN `+schema(l)+`.roles r ON r.id = rp.role_id
			JOIN `+schema(l)+`.permissions p ON p.id = rp.permission_id WHERE r.system_key = 'owner' AND p.module = 'modules')`).Scan(&adminHas, &ownerHas))
	require.False(t, adminHas)
	require.True(t, ownerHas)
}

// Entitlement (E1, E2): a plan that names a module false, or a console
// override, takes it away; a module that is not entitled cannot be turned
// on and is left out of the usable list even when switched on.
func TestModuleEntitlement(t *testing.T) {
	// Override (E2).
	l := billingTenant(t, "mod-override")
	setModules(t, l, "clothing")
	override(t, l, "stock_counts", "false", 0, 30, false)
	theApp.Plans().Forget(tenants[l].ID)
	tok, _ := ownerLogin(t, l)
	s := get(t, tenants[l].Host, "/api/settings", tok)
	require.Equal(t, []string{"product_attributes", "barcode_labels"}, strList(s.Body["entitledModules"]))
	require.Equal(t, []string{"product_attributes", "barcode_labels"}, strList(s.Body["modules"]), "switched on but not entitled: not usable")
	require.Equal(t, []string{"product_attributes", "barcode_labels", "stock_counts"}, strList(s.Body["enabledModules"]))
	// Already on stays listed (data is kept); unticking then re-ticking is refused.
	require.Equal(t, 200, putBusiness(t, l, tok, map[string]any{"modules": []string{"product_attributes"}}).Code)
	r := putBusiness(t, l, tok, map[string]any{"modules": []string{"product_attributes", "stock_counts"}})
	require.Equal(t, 403, r.Code, r.Raw)
	require.Equal(t, "FEATURE_NOT_IN_PLAN", codeOf(r))

	// Plan (E1): a plan that names a module false.
	p := limitPlan(t, "modules_no_labels", `{"max_stores":3,"max_users":1000,"max_products":1000,"max_emails_per_day":300,"max_storage_mb":1000}`)
	_, err := admin(t).Exec(context.Background(), `UPDATE platform.plans SET features = features || '{"barcode_labels": false}' WHERE id = $1`, p)
	require.NoError(t, err)
	l2 := billingTenant(t, "mod-plan")
	onPlan(t, l2, p)
	tok2, _ := ownerLogin(t, l2)
	s = get(t, tenants[l2].Host, "/api/settings", tok2)
	require.Equal(t, []string{"product_attributes", "stock_counts"}, strList(s.Body["entitledModules"]))
	r = putBusiness(t, l2, tok2, map[string]any{"businessType": "pharmacy"})
	require.Equal(t, 403, r.Code, "pharmacy recommends barcode labels, which the plan excludes: %s", r.Raw)
	r = putBusiness(t, l2, tok2, map[string]any{"businessType": "pharmacy", "modules": []string{"stock_counts"}})
	require.Equal(t, 200, r.Code, r.Raw)
}

// Isolation (A2, A3): one shop's change never shows on another, and
// another shop's token is refused.
func TestModulesIsolation(t *testing.T) {
	l := billingTenant(t, "mod-iso")
	setModules(t, l, "footwear")
	other, _ := ownerLogin(t, "a")
	s := get(t, tenants["a"].Host, "/api/settings", other)
	require.Equal(t, "retail", s.Body["businessType"])
	require.Empty(t, strList(s.Body["modules"]))

	tok, _ := ownerLogin(t, l)
	require.Equal(t, 401, get(t, tenants["a"].Host, "/api/modules", tok).Code)
	require.Equal(t, 401, putBusiness(t, "a", tok, map[string]any{"businessType": "clothing"}).Code)
	require.Equal(t, "retail", get(t, tenants["a"].Host, "/api/settings", other).Body["businessType"])
}

// Provisioning with a type (console, Q7): preset modules on, console copy
// set, and the onboarding checklist starts from the catalogue template.
func TestProvisionWithBusinessType(t *testing.T) {
	ctx := context.Background()
	o := provisioning.New(tdb.MigratorDSN, "http", "pos.test", slog.New(slog.DiscardHandler))
	res, err := o.Provision(ctx, provisioning.Request{Slug: "mod-prov-clothing", Name: "Clothing Shop",
		OwnerEmail: "owner@mod-prov-clothing.test", OwnerName: "Owner", BusinessType: "clothing"})
	require.NoError(t, err)
	tenants["mod-prov-clothing"] = tenantInfo{ID: res.TenantID, Slug: "mod-prov-clothing", Host: "mod-prov-clothing.pos.test", OwnerEmail: "owner@mod-prov-clothing.test"}
	var bt string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT business_type FROM platform.tenants WHERE id = $1`, res.TenantID).Scan(&bt))
	require.Equal(t, "clothing", bt)

	tok, _ := ownerLogin(t, "mod-prov-clothing")
	s := get(t, "mod-prov-clothing.pos.test", "/api/settings", tok)
	require.Equal(t, "clothing", s.Body["businessType"])
	require.Equal(t, []string{"product_attributes", "barcode_labels", "stock_counts"}, strList(s.Body["modules"]))

	ob := get(t, "mod-prov-clothing.pos.test", "/api/onboarding", tok)
	require.Equal(t, 200, ob.Code, ob.Raw)
	var product map[string]any
	for _, x := range ob.Body["steps"].([]any) {
		if x.(map[string]any)["key"] == "product" {
			product = x.(map[string]any)
		}
	}
	require.Equal(t, "Import your catalogue (clothing template)", product["label"])

	// A retail shop keeps "First product".
	atok, _ := ownerLogin(t, "a")
	for _, x := range get(t, tenants["a"].Host, "/api/onboarding", atok).Body["steps"].([]any) {
		if x.(map[string]any)["key"] == "product" {
			require.Equal(t, "First product", x.(map[string]any)["label"])
		}
	}

	_, err = o.Provision(ctx, provisioning.Request{Slug: "mod-prov-bad", Name: "Bad", OwnerEmail: "o@mod-prov-bad.test", BusinessType: "bakery"})
	require.ErrorContains(t, err, "")
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.tenants WHERE slug = 'mod-prov-bad'`).Scan(&n))
	require.Zero(t, n, "an unknown type is refused before anything is created")
}
