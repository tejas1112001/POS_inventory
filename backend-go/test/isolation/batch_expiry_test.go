//go:build integration

package isolation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 7: expiry report, dashboard tile, settings and the
// daily digest (design §7, A33, A34).

func TestBatchExpiryReport(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "bx-rep")
	host := tenants[l].Host
	a := stockedProduct(t, l, tok, "BX-PARA", store, 0)
	receiveBatches(t, l, tok, a, store, bat("E1", day(20), 5), bat("E2", day(200), 4), bat("E3", day(60), 3), bat("E4", day(300), 2))
	e1, _ := batchByNo(t, l, a.vid, "E1")
	e2, _ := batchByNo(t, l, a.vid, "E2")
	e3, _ := batchByNo(t, l, a.vid, "E3")
	e4, _ := batchByNo(t, l, a.vid, "E4")
	// E1 expires in the past (receiving refuses expired stock, so set it here).
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = current_date - 30, mrp = 10, cost = 4 WHERE id = $1`, e1)
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET mrp = 20 WHERE id = $1`, e3)
	require.NoError(t, err)
	// E2: one unit quarantined; E4 recalled.
	require.Equal(t, 200, batchAction(t, l, tok, e2, "quarantine", map[string]any{"storeId": store, "quantity": 1, "reason": "damaged pack"}).Code)
	require.Equal(t, 200, batchAction(t, l, tok, e4, "recall", map[string]any{"reason": "maker recall notice"}).Code)

	r := get(t, host, "/api/reports/expiry", tok)
	require.Equal(t, 200, r.Code, r.Raw)
	require.EqualValues(t, 90, r.Body["windowDays"])
	sum := r.Body["summary"].(map[string]any)
	sec := func(name string) map[string]any { return sum[name].(map[string]any) }
	require.EqualValues(t, 5, sec("expired")["units"])
	require.EqualValues(t, 20, sec("expired")["costValue"])
	require.EqualValues(t, 50, sec("expired")["mrpValue"])
	require.EqualValues(t, 3, sec("expiring")["units"], "E3 only: E2 is outside 90 days")
	require.EqualValues(t, 60, sec("expiring")["mrpValue"])
	require.EqualValues(t, 1, sec("quarantined")["units"])
	require.EqualValues(t, 2, sec("recalled")["units"])
	// Totals equal the rows.
	units := map[string]int{}
	cost := map[string]decimal.Decimal{}
	for _, x := range r.Body["items"].([]any) {
		it := x.(map[string]any)
		s := it["section"].(string)
		units[s] += int(it["quantity"].(float64))
		cost[s] = cost[s].Add(decimal.NewFromFloat(it["costValue"].(float64)))
	}
	for s, n := range units {
		require.EqualValues(t, n, sec(s)["units"], s)
		require.True(t, cost[s].Equal(decimal.NewFromFloat(sec(s)["costValue"].(float64))), s)
	}
	require.Equal(t, []string{"recalled", "expired", "expiring", "quarantined"}, func() []string {
		var out []string
		for _, x := range r.Body["items"].([]any) {
			if s := x.(map[string]any)["section"].(string); len(out) == 0 || out[len(out)-1] != s {
				out = append(out, s)
			}
		}
		return out
	}())

	// Window and section filters.
	r = get(t, host, "/api/reports/expiry?windowDays=365&section=expiring", tok)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Len(t, r.Body["items"], 2, "E3 and E2's sellable units")
	require.Equal(t, 400, get(t, host, "/api/reports/expiry?windowDays=0", tok).Code)
	require.Equal(t, 400, get(t, host, "/api/reports/expiry?section=sold", tok).Code)

	// Export.
	x := do(t, rq{method: "GET", host: host, path: "/api/reports/expiry/export?format=xlsx", token: tok})
	require.Equal(t, 200, x.Code, x.Raw)

	// The dashboard tile.
	d := get(t, host, "/api/dashboard", tok)
	require.Equal(t, 200, d.Code, d.Raw)
	b := d.Body["batches"].(map[string]any)
	require.EqualValues(t, 5, b["expired"])
	require.EqualValues(t, 3, b["expiring"])
	require.EqualValues(t, 1, b["quarantined"])
	require.EqualValues(t, 2, b["recalled"])

	// Time zone boundary: a batch expiring "yesterday" in Kiritimati
	// (UTC+14) is expired there and not yet expired at UTC-12.
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = (now() AT TIME ZONE 'Pacific/Kiritimati')::date - 1 WHERE id = $1`, e3)
	require.NoError(t, err)
	inSection := func(tz string) string {
		_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET timezone = $1`, tz)
		require.NoError(t, err)
		for _, x := range get(t, host, "/api/reports/expiry", tok).Body["items"].([]any) {
			if it := x.(map[string]any); int64(it["batchId"].(float64)) == e3 {
				return it["section"].(string)
			}
		}
		return ""
	}
	require.Equal(t, "expired", inSection("Pacific/Kiritimati"))
	require.Equal(t, "expiring", inSection("Etc/GMT+12"))

	// Another pharmacy shop never sees these batches.
	l2, tok2, _ := pharmacyShop(t, "bx-rep2")
	require.NotContains(t, get(t, tenants[l2].Host, "/api/reports/expiry", tok2).Raw, "BX-PARA")

	// Module off: the report is refused and the tile is null; settings stay.
	require.Equal(t, 200, putBusiness(t, l2, tok2, map[string]any{"modules": []string{}}).Code)
	require.Equal(t, 403, get(t, tenants[l2].Host, "/api/reports/expiry", tok2).Code)
	require.Nil(t, get(t, tenants[l2].Host, "/api/dashboard", tok2).Body["batches"])
	require.Equal(t, 200, get(t, tenants[l2].Host, "/api/settings/batches", tok2).Code)
}

func TestBatchSettings(t *testing.T) {
	l, tok, _ := pharmacyShop(t, "bx-set")
	host := tenants[l].Host
	g := get(t, host, "/api/settings/batches", tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.EqualValues(t, 90, g.Body["nearExpiryDays"])
	require.EqualValues(t, 0, g.Body["minDaysToSell"])
	require.Equal(t, false, g.Body["expiryDigest"])

	put := func(tk string, body map[string]any) resp {
		return do(t, rq{method: "PUT", host: host, path: "/api/settings/batches", token: tk, body: body})
	}
	r := put(tok, map[string]any{"nearExpiryDays": 0, "minDaysToSell": 400})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "nearExpiryDays")
	require.Contains(t, r.Raw, "minDaysToSell")
	r = put(tok, map[string]any{"nearExpiryDays": 60, "expiryDigest": true})
	require.Equal(t, 200, r.Code, r.Raw)
	require.EqualValues(t, 60, r.Body["nearExpiryDays"])
	require.EqualValues(t, 0, r.Body["minDaysToSell"], "unchanged")
	require.Equal(t, true, r.Body["expiryDigest"])
	require.EqualValues(t, 60, get(t, host, "/api/reports/expiry", tok).Body["windowDays"])

	// settings:edit is needed.
	viewer := userWithPerms(t, l, tok, "bx-set-viewer@example.com", "settings:view")
	require.Equal(t, 200, get(t, host, "/api/settings/batches", viewer).Code)
	require.Equal(t, 403, put(viewer, map[string]any{"nearExpiryDays": 30}).Code)
}

func TestBatchExpiryDigest(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "bx-dig")
	host := tenants[l].Host
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET timezone = 'Asia/Kolkata'`)
	require.NoError(t, err)
	a := stockedProduct(t, l, tok, "BX-DIG", store, 0)
	receiveBatches(t, l, tok, a, store, bat("D1", day(10), 3))
	d1, _ := batchByNo(t, l, a.vid, "D1")
	m := theApp.Worker().Maintenance()
	count := func() int {
		var n int
		require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.email_outbox WHERE kind = 'expiry_digest'`).Scan(&n))
		return n
	}
	ist := time.FixedZone("IST", 5*3600+1800)
	run := func(d, h int) {
		_, err := m.ExpiryDigests(ctx, time.Date(2030, 2, d, h, 0, 0, 0, ist))
		require.NoError(t, err)
	}

	run(1, 9)
	require.Zero(t, count(), "off by default")
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/batches", token: tok, body: map[string]any{"expiryDigest": true}}).Code)
	run(1, 7)
	require.Zero(t, count(), "not before 08:00 shop time")
	run(1, 9)
	require.Equal(t, 1, count(), "one for the owner")
	run(1, 15)
	require.Equal(t, 1, count(), "once a day")
	var data string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT template_data::text FROM `+schema(l)+`.email_outbox WHERE kind = 'expiry_digest'`).Scan(&data))
	require.Contains(t, data, "BX-DIG")

	require.NoError(t, theApp.Worker().EnqueueMail(ctx, nil, false))
	msg := waitMailSubject(t, tenants[l].OwnerEmail, "Expiry check")
	require.Contains(t, msg.Text, "D1")

	// Nothing to report: no email.
	require.Equal(t, 201, postWriteOff(t, l, tok, store, "other", nil, woLine(d1, 3, "stock")).Code)
	run(2, 9)
	require.Equal(t, 1, count(), "not on empty days")
	// Off again: nothing, even with stock to report.
	receiveBatches(t, l, tok, a, store, bat("D2", day(5), 1))
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/batches", token: tok, body: map[string]any{"expiryDigest": false}}).Code)
	run(3, 9)
	require.Equal(t, 1, count())
	_ = fmt.Sprint
}
