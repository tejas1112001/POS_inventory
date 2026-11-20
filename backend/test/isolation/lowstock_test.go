//go:build integration

package isolation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLowStockAlerts (S10 item 6, D8): from 08:00 shop time the owner gets
// one email a day listing the items at or below their alert level, only
// when there are any and only while the setting is on; users who can see
// stock get a banner. Another shop's stock never shows.
func TestLowStockAlerts(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "low-stock-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	p := stockedProduct(t, l, tok, "LOW-1", store, 3)
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.inventories SET quantity_alert = 5 WHERE variant_id = $1`, int64(p.vid))
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET timezone = 'Asia/Kolkata'`)
	require.NoError(t, err)
	m := theApp.Worker().Maintenance()
	count := func() int {
		var n int
		require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.email_outbox WHERE kind = 'low_stock'`).Scan(&n))
		return n
	}
	ist := time.FixedZone("IST", 5*3600+1800)

	_, err = m.LowStockEmails(ctx, time.Date(2030, 1, 5, 7, 30, 0, 0, ist))
	require.NoError(t, err)
	require.Zero(t, count(), "not before 08:00 shop time")
	_, err = m.LowStockEmails(ctx, time.Date(2030, 1, 5, 9, 0, 0, 0, ist))
	require.NoError(t, err)
	require.Equal(t, 1, count(), "one for the owner")
	_, err = m.LowStockEmails(ctx, time.Date(2030, 1, 5, 15, 0, 0, 0, ist))
	require.NoError(t, err)
	require.Equal(t, 1, count(), "once a day")
	var to, data string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT to_addr::text, template_data::text FROM `+schema(l)+`.email_outbox WHERE kind = 'low_stock'`).Scan(&to, &data))
	require.Equal(t, tenants[l].OwnerEmail, to)
	require.Contains(t, data, "LOW-1")

	// The email goes out (platform sender: the shop has no SMTP of its own).
	require.NoError(t, theApp.Worker().EnqueueMail(ctx, nil, false))
	msg := waitMailSubject(t, tenants[l].OwnerEmail, "low on stock")
	require.Contains(t, msg.Text, "LOW-1")

	// Turned off in Settings: nothing the next day.
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"lowStockEmail": false}}).Code)
	require.Equal(t, false, get(t, host, "/api/settings", tok).Body["lowStockEmail"])
	_, err = m.LowStockEmails(ctx, time.Date(2030, 1, 6, 9, 0, 0, 0, ist))
	require.NoError(t, err)
	require.Equal(t, 1, count())
	// On again but nothing low: nothing.
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"lowStockEmail": true}}).Code)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.inventories SET quantity_alert = 0 WHERE variant_id = $1`, int64(p.vid))
	require.NoError(t, err)
	_, err = m.LowStockEmails(ctx, time.Date(2030, 1, 7, 9, 0, 0, 0, ist))
	require.NoError(t, err)
	require.Equal(t, 1, count(), "only when something is low")

	// The banner, for users who can see stock.
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.inventories SET quantity_alert = 5 WHERE variant_id = $1`, int64(p.vid))
	require.NoError(t, err)
	b := get(t, host, "/api/notifications/banners", tok)
	require.Equal(t, 200, b.Code)
	require.Contains(t, b.Raw, `"kind":"stock"`)
	require.Contains(t, b.Raw, "1 item is at or below its low-stock level.")
	// Shop A's banner never counts this shop's stock.
	atok, _ := ownerLogin(t, "a")
	require.NotContains(t, get(t, tenants["a"].Host, "/api/notifications/banners", atok).Raw, "1 item is at or below")
}

// waitMailSubject waits for an email to addr whose subject contains part.
func waitMailSubject(t *testing.T, addr, part string) parsed {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range platSrv.Messages() {
			for _, to := range m.To {
				if strings.EqualFold(to, addr) {
					if p := parseMail(t, m.Data); strings.Contains(p.Header.Get("Subject"), part) {
						return p
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no email to %s about %q", addr, part)
	return parsed{}
}
