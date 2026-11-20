//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDashboardExtras (UI S7): items sold, discounts, the previous period
// of the same length, the chart series (hours for one day, days for a
// short range) and action-card counts limited by permission.
func TestDashboardExtras(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "DASH-X", store, 50)
	// 2003: a year no other test uses.
	for _, s := range []struct {
		day string
		qty int
	}{{"2003-06-09", 1}, {"2003-06-10", 2}, {"2003-06-11", 1}} {
		r := sell(t, "a", tok, dated(store, s.day, nil, line(p, s.qty)), nil)
		require.Equal(t, 201, r.Code, r.Raw)
	}
	disc := dated(store, "2003-06-11", nil, line(p, 1))
	disc["discount"] = 5
	require.Equal(t, 201, sell(t, "a", tok, disc, nil).Code)

	d := dash(t, "a", tok, "?startDate=2003-06-10&endDate=2003-06-11")
	x := sub(d, "extra")
	cur := sub(x, "current")
	require.EqualValues(t, 3, num(cur["orders"]))
	require.EqualValues(t, 4, num(cur["itemsSold"]))
	require.EqualValues(t, 5, num(cur["discounts"]))
	require.Equal(t, num(sub(d, "stats")["totalRevenue"]), num(cur["revenue"]), "same figure as the old tile")
	prev := sub(x, "previous")
	require.EqualValues(t, 1, num(prev["orders"]), "2003-06-08 and 09")
	require.EqualValues(t, 1, num(prev["itemsSold"]))
	require.Equal(t, "day", x["granularity"])
	require.Len(t, x["series"], 2)

	one := sub(dash(t, "a", tok, "?startDate=2003-06-10&endDate=2003-06-10"), "extra")
	require.Equal(t, "hour", one["granularity"])
	require.Len(t, one["series"], 24)
	var orders float64
	for _, s := range one["series"].([]any) {
		orders += num(s.(map[string]any)["orders"])
	}
	require.EqualValues(t, 1, orders)

	all := sub(dash(t, "a", tok, ""), "extra")
	require.Equal(t, "month", all["granularity"])
	require.Nil(t, all["previous"], "no previous period without dates")

	// Counts: the owner sees all three; a cashier only stock.
	require.NotNil(t, x["lowStock"])
	require.NotNil(t, x["purchasesWaiting"])
	require.NotNil(t, x["customersWithDues"])
	createUser(t, "a", tok, "dash-x-cashier@iso-a.test", "biller", "dash-x-cashier-1")
	ctok, _ := login(t, "a", "dash-x-cashier@iso-a.test", "dash-x-cashier-1")
	cx := sub(dash(t, "a", ctok, "?startDate=2003-06-10&endDate=2003-06-11"), "extra")
	require.NotNil(t, cx["lowStock"])
	require.Nil(t, cx["purchasesWaiting"])
	require.Nil(t, cx["customersWithDues"])
	require.EqualValues(t, 0, num(sub(cx, "current")["orders"]), "a cashier sees only own sales")
}
