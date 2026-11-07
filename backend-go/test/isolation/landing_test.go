//go:build integration

package isolation

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer/smtptest"
)

// The landing page on the main address (pos.test).

type page struct {
	Code   int
	Body   string
	Header http.Header
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func apex(t *testing.T, method, path string, form url.Values, ip string) page {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, baseURL+path, body)
	require.NoError(t, err)
	req.Host = "pos.test"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://pos.test")
	}
	req.Header.Set("X-Forwarded-For", ip)
	res, err := noRedirect.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return page{Code: res.StatusCode, Body: string(raw), Header: res.Header}
}

var ftRe = regexp.MustCompile(`name="ft" value="([^"]+)"`)

func formToken(t *testing.T, path string) string {
	t.Helper()
	p := apex(t, "GET", path, nil, "10.250.0.1")
	require.Equal(t, 200, p.Code, p.Body)
	m := ftRe.FindStringSubmatch(p.Body)
	require.Len(t, m, 2)
	return m[1]
}

// mailWith waits for a message to addr whose subject contains subj.
func mailWith(t *testing.T, srv *smtptest.Server, addr, subj string) parsed {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range srv.Messages() {
			for _, to := range m.To {
				if strings.EqualFold(to, addr) {
					if p := parseMail(t, m.Data); strings.Contains(p.Header.Get("Subject"), subj) {
						return p
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no email to %s with %q", addr, subj)
	return parsed{}
}

func TestLandingPricingShowsOnlyPublicActivePlans(t *testing.T) {
	ctx := context.Background()
	adm := admin(t)
	_, err := adm.Exec(ctx, `INSERT INTO platform.plans (code, name, price_minor, billing_interval, trial_days, limits, features, is_public, is_active) VALUES
		('landing_shown', 'Landing Shown', 149900, 'monthly', 14, '{"max_stores":2,"max_users":5}', '{"public_shop":true}', true, true),
		('landing_hidden', 'Landing Hidden', 99900, 'monthly', 0, '{}', '{}', false, true),
		('landing_retired', 'Landing Retired', 49900, 'monthly', 0, '{}', '{}', true, false)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adm.Exec(context.Background(), `DELETE FROM platform.plans WHERE code IN ('landing_shown','landing_hidden','landing_retired')`)
	})

	p := apex(t, "GET", "/", nil, "10.250.0.2")
	require.Equal(t, 200, p.Code)
	require.Contains(t, p.Body, "Landing Shown")
	require.Contains(t, p.Body, "₹1,499")
	require.Contains(t, p.Body, "Up to 2 stores")
	require.Contains(t, p.Body, "Online shop per store")
	require.Contains(t, p.Body, `"price":"1499.00","priceCurrency":"INR"`)
	require.NotContains(t, p.Body, "Landing Hidden")
	require.NotContains(t, p.Body, "Landing Retired")
	require.NotContains(t, p.Body, "Contact us for pricing")
	require.Contains(t, p.Header.Get("Content-Security-Policy"), "script-src 'self'")
}

func TestLandingFindMyShop(t *testing.T) {
	ctx := context.Background()
	adm := admin(t)
	_, err := adm.Exec(ctx, `UPDATE platform.tenants SET billing_email = 'billing-landing@iso.test' WHERE id = $1`, tenants["b"].ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adm.Exec(context.Background(), `UPDATE platform.tenants SET billing_email = NULL WHERE id = $1`, tenants["b"].ID)
	})

	tok := formToken(t, "/find-my-shop")
	time.Sleep(2100 * time.Millisecond) // the form is refused as a bot within 2 seconds

	send := func(email, ip string) page {
		return apex(t, "POST", "/find-my-shop", url.Values{"ft": {tok}, "email": {email}}, ip)
	}
	owner := send(strings.ToUpper(tenants["a"].OwnerEmail), "10.250.1.1")
	billing := send("billing-landing@iso.test", "10.250.1.2")
	nobody := send("nobody-landing@iso.test", "10.250.1.3")
	for _, r := range []page{owner, billing, nobody} {
		require.Equal(t, http.StatusSeeOther, r.Code, r.Body)
		require.Equal(t, "/find-my-shop/sent", r.Header.Get("Location"), "the same reply for every address")
		require.Equal(t, owner.Body, r.Body)
	}

	m := mailWith(t, platSrv, tenants["a"].OwnerEmail, "Your shop address")
	require.Contains(t, m.Text, "Shop A: http://iso-a.pos.test/login")
	m = mailWith(t, platSrv, "billing-landing@iso.test", "Your shop address")
	require.Contains(t, m.Text, "Shop B: http://iso-b.pos.test/login")
	require.NotContains(t, m.Text, "iso-a")

	// No shop for this address: the row is cancelled and nothing is sent.
	var status string
	eventually(t, func() bool {
		_ = adm.QueryRow(ctx, `SELECT status FROM platform.email_outbox WHERE to_addr = 'nobody-landing@iso.test'`).Scan(&status)
		return status == "cancelled"
	}, "find_shop row for an unknown address is cancelled")
	require.False(t, gotMail(platSrv, "nobody-landing@iso.test"))

	// A second request within the bucket sends no second email.
	again := send(tenants["a"].OwnerEmail, "10.250.1.4")
	require.Equal(t, http.StatusSeeOther, again.Code)
	var n int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'find_shop' AND to_addr = $1`,
		tenants["a"].OwnerEmail).Scan(&n))
	require.Equal(t, 1, n)

	// Per IP: the 6th request in an hour is refused.
	for i := range 5 {
		r := send("limit"+string(rune('a'+i))+"@iso.test", "10.250.1.9")
		require.Equal(t, http.StatusSeeOther, r.Code)
	}
	require.Equal(t, http.StatusTooManyRequests, send("limitz@iso.test", "10.250.1.9").Code)
}

func TestLandingContactRequest(t *testing.T) {
	ctx := context.Background()
	tok := formToken(t, "/contact")
	time.Sleep(2100 * time.Millisecond)
	r := apex(t, "POST", "/contact", url.Values{"ft": {tok}, "topic": {"demo"}, "name": {"Asha <Rao>"},
		"email": {"asha-landing@iso.test"}, "business": {"Rao Stores"}, "phone": {"+91 98765 43210"},
		"message": {"We run 2 stores.\nCan we see a demo?"}}, "10.250.2.1")
	require.Equal(t, http.StatusSeeOther, r.Code, r.Body)
	require.Equal(t, "/contact/sent", r.Header.Get("Location"))

	var topic, name, business, msg string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT topic, name, business, message FROM platform.contact_requests
		WHERE email = 'asha-landing@iso.test'`).Scan(&topic, &name, &business, &msg))
	require.Equal(t, "demo", topic)
	require.Equal(t, "Asha <Rao>", name)
	require.Equal(t, "Rao Stores", business)
	require.Equal(t, "We run 2 stores.\nCan we see a demo?", msg)

	m := mailWith(t, platSrv, "sales@pos.test", "Demo request from Asha <Rao> (Rao Stores)")
	require.Contains(t, m.Header.Get("Reply-To"), "asha-landing@iso.test")
	require.Contains(t, m.Text, "Can we see a demo?")
	require.Contains(t, m.HTML, "Asha &lt;Rao&gt;")
	require.NotContains(t, m.HTML, "<Rao>")

	// A bot that fills in the hidden field gets the same reply; nothing is stored.
	r = apex(t, "POST", "/contact", url.Values{"ft": {tok}, "name": {"Bot"}, "email": {"bot-landing@iso.test"}, "website": {"x"}}, "10.250.2.2")
	require.Equal(t, http.StatusSeeOther, r.Code)
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.contact_requests WHERE email = 'bot-landing@iso.test'`).Scan(&n))
	require.Zero(t, n)
}

func TestLandingHostsAndRobots(t *testing.T) {
	p := apex(t, "GET", "/login", nil, "10.250.3.1")
	require.Equal(t, 200, p.Code)
	require.Equal(t, "noindex", p.Header.Get("X-Robots-Tag"))
	require.Contains(t, p.Header.Get("Content-Security-Policy"), "form-action 'self' http://*.pos.test;")
	p = apex(t, "GET", "/login/go?shop=iso-a", nil, "10.250.3.1")
	require.Equal(t, http.StatusSeeOther, p.Code)
	require.Equal(t, "http://iso-a.pos.test/login", p.Header.Get("Location"))

	p = apex(t, "GET", "/robots.txt", nil, "10.250.3.1")
	require.Contains(t, p.Body, "Sitemap: http://pos.test/sitemap.xml")
	p = apex(t, "GET", "/sitemap.xml", nil, "10.250.3.1")
	require.Contains(t, p.Body, "<loc>http://pos.test/</loc>")

	// The console host is never indexed, API included.
	r := do(t, rq{method: "GET", host: "admin.pos.test", path: "/api/platform/auth/me"})
	require.Equal(t, "noindex, nofollow", r.Header.Get("X-Robots-Tag"))
	// Tenant hosts: the public shop API carries no noindex.
	r = do(t, rq{method: "GET", host: tenants["a"].Host, path: "/api/public/home"})
	require.Empty(t, r.Header.Get("X-Robots-Tag"))
}
