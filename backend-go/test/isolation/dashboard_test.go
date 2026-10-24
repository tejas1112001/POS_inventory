//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// dated is a POS sale on a given day (YYYY-MM-DD in the tenant's zone).
func dated(store float64, day string, paid any, lines ...map[string]any) map[string]any {
	b := posSale(store, lines...)
	b["date"] = day
	if paid != nil {
		b["paid"] = paid
	}
	return b
}

func dash(t *testing.T, l, tok, query string) map[string]any {
	t.Helper()
	r := get(t, tenants[l].Host, "/api/dashboard"+query, tok)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "no-store", r.Header.Get("Cache-Control"))
	return r.Body
}

func sub(m map[string]any, k string) map[string]any { return m[k].(map[string]any) }

func monthsOf(m map[string]any) map[string][2]float64 {
	out := map[string][2]float64{}
	for _, x := range m["revenueData"].([]any) {
		p := x.(map[string]any)
		out[p["month"].(string)] = [2]float64{num(p["revenue"]), num(p["orders"])}
	}
	return out
}

func TestDashboardFiltersScopeAndChart(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "DASH-P", store, 50)
	bid := createUser(t, "a", tok, "dash-biller@iso-a.test", "biller", "dash-biller-pass-1")
	btok, _ := login(t, "a", "dash-biller@iso-a.test", "dash-biller-pass-1")

	// Sales in 2001, a year no other test uses.
	march := sell(t, "a", tok, dated(store, "2001-03-15", nil, line(p, 2)), nil)
	require.Equal(t, 201, march.Code, march.Raw)
	april := sell(t, "a", btok, dated(store, "2001-04-02", nil, line(p, 1)), nil)
	require.Equal(t, 201, april.Code, april.Raw)
	unpaid := sell(t, "a", tok, dated(store, "2001-04-10", 0, line(p, 1)), nil)
	require.Equal(t, 201, unpaid.Code)
	require.Equal(t, "Unpaid", unpaid.Body["payment_status"])
	voided := sell(t, "a", tok, dated(store, "2001-04-11", nil, line(p, 5)), nil)
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", voided.Body["id"]), token: tok}).Code)
	rb := retBody(march.Body["id"], store, retItem(p, 1))
	rb["date"] = "2001-04-05"
	ret := postReturn(t, "a", tok, rb, nil)
	require.Equal(t, 201, ret.Code, ret.Raw)

	t1, t2, t3 := num(march.Body["total"]), num(april.Body["total"]), num(unpaid.Body["total"])
	year := "?startDate=2001-01-01&endDate=2001-12-31"
	d := dash(t, "a", tok, year)
	st, sc := sub(d, "stats"), sub(d, "summaryCards")
	require.Equal(t, 3.0, num(st["totalOrders"]), "voided sales are left out")
	require.InDelta(t, t1+t2+t3, num(st["totalRevenue"]), 0.001)
	require.InDelta(t, num(ret.Body["total"]), num(st["totalReturns"]), 0.001)
	require.InDelta(t, t1+t2+t3-num(ret.Body["total"]), num(st["netRevenue"]), 0.001)
	require.Positive(t, num(st["totalCustomers"]))
	require.Positive(t, num(st["totalProducts"]))
	require.InDelta(t, t1+t2+t3, num(sc["totalAmount"]), 0.001)
	require.InDelta(t, t1+t2, num(sc["totalPaid"]), 0.001)
	require.InDelta(t, t3, num(sc["totalUnpaid"]), 0.001)
	require.Equal(t, 0.0, num(sc["totalOverdue"]))

	// Calendar months of that year, zero-filled, "Mon YYYY", in order.
	pts := d["revenueData"].([]any)
	require.Len(t, pts, 12, "January to December 2001")
	require.Equal(t, "Jan 2001", pts[0].(map[string]any)["month"])
	require.Equal(t, "Dec 2001", pts[11].(map[string]any)["month"])
	require.Equal(t, [2]float64{0, 0}, monthsOf(d)["Feb 2001"])
	late := dash(t, "a", tok, "?startDate=2001-03-10&endDate=2001-12-31")
	require.Len(t, late["revenueData"].([]any), 10, "the chart starts at the start date's month")
	require.Equal(t, "Mar 2001", late["revenueData"].([]any)[0].(map[string]any)["month"])
	m := monthsOf(d)
	require.InDelta(t, t1, m["Mar 2001"][0], 0.001)
	require.Equal(t, 1.0, m["Mar 2001"][1])
	require.InDelta(t, t2+t3, m["Apr 2001"][0], 0.001)
	require.Equal(t, 2.0, m["Apr 2001"][1])
	require.Equal(t, [2]float64{0, 0}, m["May 2001"])
	top := d["salesDistribution"].([]any)
	require.Len(t, top, 1)
	require.Equal(t, "Product DASH-P", top[0].(map[string]any)["category"])
	require.InDelta(t, t1+t2+t3, num(top[0].(map[string]any)["value"]), 0.001)
	require.NotEmpty(t, top[0].(map[string]any)["color"])

	// A biller sees only own sales; userId cannot widen that.
	bd := dash(t, "a", btok, year+"&userId=0")
	require.Equal(t, 1.0, num(sub(bd, "stats")["totalOrders"]))
	require.InDelta(t, t2, num(sub(bd, "stats")["totalRevenue"]), 0.001)
	require.Equal(t, 0.0, num(sub(bd, "stats")["totalReturns"]), "the return is of the owner's sale")
	require.Equal(t, true, sub(bd, "filters")["ownOnly"])
	// The owner can narrow to one biller, or to a store.
	od := dash(t, "a", tok, fmt.Sprintf("%s&userId=%d", year, bid))
	require.Equal(t, 1.0, num(sub(od, "stats")["totalOrders"]))
	none := dash(t, "a", tok, year+"&storeId=99999999")
	require.Equal(t, 0.0, num(sub(none, "stats")["totalOrders"]))
	require.Len(t, none["revenueData"].([]any), 12)

	// Without filters: the latest 12 months, this month last.
	now := dash(t, "a", tok, "")
	require.Len(t, now["revenueData"].([]any), 12)
	require.Equal(t, 400, get(t, host, "/api/dashboard?startDate=soon", tok).Code)
	nodash := userWithPerms(t, "a", tok, "no-dash@iso-a.test", "sales:view")
	require.Equal(t, 403, get(t, host, "/api/dashboard", nodash).Code)
}

func TestDashboardIsolationAcrossTenants(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	aStore := storeIDs(t, "a", aTok)[0]
	p := stockedProduct(t, "a", aTok, "DASH-ISO-A", aStore, 5)
	require.Equal(t, 201, sell(t, "a", aTok, dated(aStore, "2002-06-15", nil, line(p, 1)), nil).Code)

	c := dash(t, "c", cTok, "?startDate=2002-01-01&endDate=2002-12-31")
	require.Equal(t, 0.0, num(sub(c, "stats")["totalOrders"]))
	require.Equal(t, 0.0, num(sub(c, "stats")["totalRevenue"]))
	require.Empty(t, c["salesDistribution"].([]any))
	for _, v := range monthsOf(c) {
		require.Equal(t, [2]float64{0, 0}, v)
	}
	require.Equal(t, 401, get(t, tenants["c"].Host, "/api/dashboard", aTok).Code)
}
