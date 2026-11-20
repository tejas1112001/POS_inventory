//go:build integration

package isolation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOnboardingChecklist (S10 item 8, D10): ticks come from the shop's
// data, only settings:edit users see the checklist, hiding it is for good
// and audited, and another shop's checklist is its own.
func TestOnboardingChecklist(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "onboard-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	done := func() map[string]bool {
		r := get(t, host, "/api/onboarding", tok)
		require.Equal(t, 200, r.Code, r.Raw)
		out := map[string]bool{}
		for _, s := range r.Body["steps"].([]any) {
			m := s.(map[string]any)
			out[m["key"].(string)] = m["done"].(bool)
		}
		return out
	}
	d := done()
	for _, k := range []string{"profile", "logo", "store", "product", "staff", "email", "sale"} {
		require.Contains(t, d, k)
		require.False(t, d[k], "%s is not done in a new shop", k)
	}

	// Real data ticks the steps.
	store := storeIDs(t, l, tok)[0]
	p := stockedProduct(t, l, tok, "ONB-1", store, 5)
	createUser(t, l, tok, "onboard-cashier@"+l+".test", "biller", "onboard-cashier-1")
	require.Equal(t, 201, sell(t, l, tok, map[string]any{"orderItems": []map[string]any{line(p, 1)}, "storeId": store}, nil).Code)
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET address = '12 MG Road', phone = '020 1234', tax_registration_no = '27ABCDE1234F1Z5'`)
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.stores SET address = '12 MG Road, Pune' WHERE id = $1`, int64(store))
	require.NoError(t, err)
	d = done()
	for _, k := range []string{"profile", "store", "product", "staff", "sale"} {
		require.True(t, d[k], k)
	}
	require.False(t, d["logo"])

	// Billers cannot see or hide it.
	btok, _ := login(t, l, "onboard-cashier@"+l+".test", "onboard-cashier-1")
	require.Equal(t, 403, get(t, host, "/api/onboarding", btok).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/onboarding/hide", token: btok}).Code)

	// Hide for good (twice is fine), audited once.
	require.Equal(t, 204, do(t, rq{method: "POST", host: host, path: "/api/onboarding/hide", token: tok}).Code)
	require.Equal(t, 204, do(t, rq{method: "POST", host: host, path: "/api/onboarding/hide", token: tok}).Code)
	require.Equal(t, true, get(t, host, "/api/onboarding", tok).Body["hidden"])
	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'settings.onboarding_hidden'`).Scan(&audits))
	require.Equal(t, 1, audits)

	// Shop A's checklist is its own (not hidden by this shop).
	atok, _ := ownerLogin(t, "a")
	require.Equal(t, false, get(t, tenants["a"].Host, "/api/onboarding", atok).Body["hidden"])
	require.Equal(t, 401, get(t, tenants["a"].Host, "/api/onboarding", tok).Code)
}
