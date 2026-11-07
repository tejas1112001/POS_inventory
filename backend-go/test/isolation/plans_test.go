//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// limitPlan creates (or resets) a hidden plan with the given limits.
func limitPlan(t *testing.T, code, limits string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `INSERT INTO platform.plans (code, name, price_minor, billing_interval,
			trial_days, limits, features, is_public)
		SELECT $1, 'Limits ' || $1, 0, 'monthly', 14, $2::jsonb, features, false FROM platform.plans WHERE code = 'trial'
		ON CONFLICT (code) DO UPDATE SET limits = EXCLUDED.limits RETURNING id`, code, limits).Scan(&id))
	return id
}

func onPlan(t *testing.T, l string, plan int64) {
	t.Helper()
	setSub(t, l, `plan_id = $2`, plan)
	setTenant(t, l, `plan_id = $2`, plan)
}

func count(t *testing.T, l, table string) int {
	t.Helper()
	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema(l)+`.`+table+` WHERE deleted_at IS NULL`).Scan(&n))
	return n
}

// race runs n requests at once and counts creations and limit refusals;
// any other answer fails the test.
func race(t *testing.T, n int, fn func(i int) resp) (created, limited int) {
	t.Helper()
	out := make([]resp, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	for _, r := range out {
		switch {
		case r.Code == 201:
			created++
		case r.Code == 403 && r.Body["code"] == "PLAN_LIMIT_REACHED":
			limited++
		default:
			t.Errorf("unexpected answer %d: %s", r.Code, r.Raw)
		}
	}
	return created, limited
}

// BILLING_DESIGN §8: parallel creates at the limit cannot both take the
// last slot (plans.Check holds a per-tenant advisory lock).
func TestPlanLimitsHoldUnderParallelCreates(t *testing.T) {
	l := billingTenant(t, "bill-limits")
	onPlan(t, l, limitPlan(t, "limits_race", `{"max_stores":2,"max_users":3,"max_products":4,"max_emails_per_day":300,"max_storage_mb":1000}`))
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	biller := roleID(t, l, "biller")

	c, lim := race(t, 8, func(i int) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": fmt.Sprintf("Race %d", i)}})
	})
	require.Equal(t, [2]int{1, 7}, [2]int{c, lim}, "the seeded store plus one")
	require.Equal(t, 2, count(t, l, "stores"))

	c, lim = race(t, 8, func(i int) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/users", token: tok, body: map[string]any{
			"name": fmt.Sprintf("Racer %d", i), "email": fmt.Sprintf("racer%d@bill-limits.test", i),
			"password": "racer-pass-0001", "role_id": biller, "status": "Active"}})
	})
	require.Equal(t, [2]int{2, 6}, [2]int{c, lim}, "the owner plus two")
	require.Equal(t, 3, count(t, l, "users"))

	c, lim = race(t, 10, func(i int) resp {
		return createProduct(t, l, tok, addFormSingle(fmt.Sprintf("LIM-RACE-%d", i), store, 1))
	})
	require.Equal(t, [2]int{4, 6}, [2]int{c, lim})
	require.Equal(t, 4, count(t, l, "products"))
}

// A downgrade below current use blocks new records and keeps the
// existing ones; going back up allows creation again.
func TestPlanDowngradeBlocksCreation(t *testing.T) {
	l := billingTenant(t, "bill-downgrade")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	for i := range 3 {
		require.Equal(t, 201, createProduct(t, l, tok, addFormSingle(fmt.Sprintf("LIM-DOWN-%d", i), store, 1)).Code)
	}
	var trial int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT plan_id FROM platform.tenants WHERE id = $1`, tenants[l].ID).Scan(&trial))

	onPlan(t, l, limitPlan(t, "limits_small", `{"max_stores":1,"max_users":1,"max_products":2,"max_emails_per_day":300,"max_storage_mb":1000}`))
	n := 3
	eventually(t, func() bool {
		r := createProduct(t, l, tok, addFormSingle(fmt.Sprintf("LIM-DOWN-%d", n), store, 1))
		n++
		return r.Code == 403 && r.Body["code"] == "PLAN_LIMIT_REACHED"
	}, "downgrade applied")
	before := count(t, l, "products")
	require.GreaterOrEqual(t, before, 3, "nothing is deleted")
	require.Len(t, list(t, get(t, host, "/api/products", tok)), before, "and all can be read")
	st := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Second"}})
	require.Equal(t, 403, st.Code)
	require.Equal(t, "PLAN_LIMIT_REACHED", st.Body["code"])
	sub := get(t, host, "/api/billing/subscription", tok)
	require.EqualValues(t, before, sub.Body["usage"].(map[string]any)["products"], "usage shows the overage")

	onPlan(t, l, trial)
	eventually(t, func() bool {
		r := createProduct(t, l, tok, addFormSingle(fmt.Sprintf("LIM-DOWN-%d", n), store, 1))
		n++
		return r.Code == 201
	}, "upgrade applied")
}
