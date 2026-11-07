package landing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

type fakeStore struct {
	mu       sync.Mutex
	finds    []string // keys
	emails   []string
	contacts []contactInput
	notify   []string
}

func (f *fakeStore) queueFindShop(_ context.Context, key, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finds, f.emails = append(f.finds, key), append(f.emails, email)
	return nil
}

func (f *fakeStore) saveContact(_ context.Context, in contactInput, notify string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts, f.notify = append(f.contacts, in), append(f.notify, notify)
	return int64(len(f.contacts)), nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type harness struct {
	site    *Site
	h       http.Handler
	store   *fakeStore
	clock   *clock
	queued  int
	queueMu sync.Mutex
}

func newSite(t *testing.T, mutate func(*Deps), plans []Plan) *harness {
	t.Helper()
	hs := &harness{store: &fakeStore{}, clock: &clock{t: time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)}}
	d := Deps{
		Network: httpx.Network{BaseDomain: "pos.test"}, PublicScheme: "https", BaseDomain: "pos.test",
		PlatformName: "Shopline", ContactEmail: "sales@pos.test", FormKey: FormKey([]byte("0123456789abcdef0123456789abcdef")),
		Now: hs.clock.now, Metrics: obs.NewMetrics(),
		MailQueued: func(context.Context) { hs.queueMu.Lock(); hs.queued++; hs.queueMu.Unlock() },
	}
	if mutate != nil {
		mutate(&d)
	}
	s, err := New(d)
	require.NoError(t, err)
	s.store = hs.store
	s.plans.load = func(context.Context) ([]Plan, error) { return plans, nil }
	hs.site = s
	hs.h = httpx.NewRouter(httpx.RouterDeps{
		Logger: obs.NewLogger(io.Discard, "error", "json"), Network: d.Network, Health: httpx.NewHealth(time.Second),
		RequestTimeout: 5 * time.Second, Apex: s.Mount, ApexNotFound: http.HandlerFunc(s.NotFound),
	})
	return hs
}

func (hs *harness) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://pos.test"+path, nil)
	req.RemoteAddr = "203.0.113.7:4000"
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	return rec
}

func (hs *harness) post(t *testing.T, path string, form url.Values, origin, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://pos.test"+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if ip == "" {
		ip = "203.0.113.7"
	}
	req.RemoteAddr = ip + ":4000"
	req.TLS = nil
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)
	return rec
}

// token reads the form's signed time stamp from a page.
var tokenRe = regexp.MustCompile(`name="ft" value="([^"]+)"`)

func (hs *harness) token(t *testing.T, path string) string {
	t.Helper()
	m := tokenRe.FindStringSubmatch(hs.get(t, path).Body.String())
	require.Len(t, m, 2)
	return m[1]
}

const origin = "http://pos.test" // httptest requests have no TLS, so the scheme is http

func count(s, sub string) int { return strings.Count(s, sub) }

var proPlan = Plan{Code: "pro", Name: "Pro", PriceMinor: 149900, Currency: "INR", Interval: "monthly", TrialDays: 14,
	Limits: []string{"Up to 3 stores"}, Features: []string{"Online shop per store"}}

func TestHomePage(t *testing.T) {
	hs := newSite(t, nil, nil)
	rec := hs.get(t, "/")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	h := rec.Header()
	require.Equal(t, "text/html; charset=utf-8", h.Get("Content-Type"))
	require.Equal(t, httpx.CSPStrict, h.Get("Content-Security-Policy"), "strict CSP on the main address")
	require.Empty(t, h.Get("X-Robots-Tag"), "the home page is indexable")
	require.Equal(t, "public, max-age=300", h.Get("Cache-Control"))

	require.Equal(t, 1, count(body, "<h1"), "one H1")
	require.Contains(t, body, "<title>Shopline: POS, stock and online shop for retail stores</title>")
	require.Contains(t, body, `<link rel="canonical" href="https://pos.test/">`)
	for _, tag := range []string{`name="description"`, `property="og:title"`, `property="og:description"`, `property="og:url" content="https://pos.test/"`,
		`property="og:type"`, `name="twitter:card"`, `name="twitter:title"`, `name="twitter:description"`} {
		require.Contains(t, body, tag)
	}
	require.NotContains(t, body, `name="robots"`)
	require.Contains(t, body, ">Request a demo</a>")
	require.Contains(t, body, `href="/login">Staff login</a>`)
	require.Contains(t, body, "Contact us for pricing", "no public plans yet")
	require.Contains(t, body, "your-shop.pos.test", "BASE_DOMAIN fills the copy")
	require.NotContains(t, body, "{{domain}}")
	require.NotContains(t, body, "signup")
	require.NotContains(t, body, "Sign up")

	// Strict CSP: no inline scripts, styles or handlers; only JSON-LD.
	require.NotContains(t, body, " style=")
	require.NotRegexp(t, ` on[a-z]+=`, body)
	require.Equal(t, 1, count(body, "<script"))
	require.Equal(t, 1, count(body, `<script type="application/ld+json">`))

	// Every img has alt text (there are none today; icons are inline SVG
	// marked aria-hidden).
	require.Equal(t, count(body, "<img"), count(body, " alt="))
	require.Equal(t, count(body, "<svg"), count(body, `aria-hidden="true" focusable="false"`))

	ld := jsonLD(t, body)
	types := map[string]map[string]any{}
	for _, n := range ld["@graph"].([]any) {
		m := n.(map[string]any)
		types[m["@type"].(string)] = m
	}
	require.Contains(t, types, "Organization")
	require.Equal(t, "https://pos.test/", types["Organization"]["url"])
	require.Contains(t, types, "SoftwareApplication")
	require.NotContains(t, types["SoftwareApplication"], "offers", "no offers without public plans")
	qa := types["FAQPage"]["mainEntity"].([]any)
	require.Len(t, qa, len(faqs))
	require.Equal(t, len(faqs), count(body, "<details>"), "JSON-LD FAQ matches the page")
	require.Equal(t, faqs[0].Q, qa[0].(map[string]any)["name"])

	// Every form carries the honeypot and a token.
	require.Equal(t, 2, count(body, `name="website"`))
	require.Equal(t, 2, count(body, `name="ft"`))

	css := hs.get(t, hs.site.cssPath)
	require.Equal(t, http.StatusOK, css.Code)
	require.Contains(t, css.Header().Get("Cache-Control"), "immutable")
	require.Contains(t, body, `href="`+hs.site.cssPath+`"`)
	require.Equal(t, "image/svg+xml", hs.get(t, "/favicon.svg").Header().Get("Content-Type"))

	head := httptest.NewRequest(http.MethodHead, "https://pos.test/", nil)
	hrec := httptest.NewRecorder()
	hs.h.ServeHTTP(hrec, head)
	require.Equal(t, http.StatusOK, hrec.Code, "HEAD works")
}

func jsonLD(t *testing.T, body string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(body)
	require.Len(t, m, 2)
	var v map[string]any
	require.NoError(t, json.Unmarshal([]byte(m[1]), &v), m[1])
	require.Equal(t, "https://schema.org", v["@context"])
	return v
}

func TestPricingFromPlans(t *testing.T) {
	yearly := Plan{Code: "big", Name: "Business <b>", PriceMinor: 1499900, Currency: "INR", Interval: "yearly"}
	hs := newSite(t, nil, []Plan{proPlan, yearly})
	body := hs.get(t, "/").Body.String()
	require.NotContains(t, body, "Contact us for pricing")
	require.Contains(t, body, "₹1,499")
	require.Contains(t, body, "/ month")
	require.Contains(t, body, "₹14,999")
	require.Contains(t, body, "/ year")
	require.Contains(t, body, "14-day trial")
	require.Contains(t, body, "Up to 3 stores")
	require.Contains(t, body, "Business &lt;b&gt;", "plan names are escaped")

	ld := jsonLD(t, body)
	var app map[string]any
	for _, n := range ld["@graph"].([]any) {
		if m := n.(map[string]any); m["@type"] == "SoftwareApplication" {
			app = m
		}
	}
	offers := app["offers"].([]any)
	require.Len(t, offers, 2)
	o := offers[0].(map[string]any)
	require.Equal(t, "1499.00", o["price"])
	require.Equal(t, "INR", o["priceCurrency"])
	require.Equal(t, "Pro", o["name"])
	require.Equal(t, "Business <b>", offers[1].(map[string]any)["name"], "JSON keeps the text; the HTML escaping is json.Marshal's")
	require.NotContains(t, body, "<b>\"")
}

func TestPlanFormatting(t *testing.T) {
	require.Equal(t, "₹0", formatMoney(0, "INR"))
	require.Equal(t, "₹999", formatMoney(99900, "INR"))
	require.Equal(t, "₹1,49,999", formatMoney(14999900, "INR"))
	require.Equal(t, "₹1,499.50", formatMoney(149950, "INR"))
	require.Equal(t, "USD 12,345", formatMoney(1234500, "USD"))
	require.Equal(t, "1499.05", Plan{PriceMinor: 149905}.Decimal())

	p := planFromRow("pro", "Pro", 149900, "INR", "monthly", 14,
		[]byte(`{"max_stores":3,"max_users":0,"max_products":5000,"max_storage_mb":1000,"max_emails_per_day":300,"odd":"x"}`),
		[]byte(`{"public_shop":true,"custom_domain":false,"reports_export":true,"unknown_flag":true}`))
	require.Equal(t, []string{"Up to 3 stores", "Unlimited staff accounts", "Up to 5,000 products", "1 GB file storage", "Up to 300 emails a day"}, p.Limits)
	require.Equal(t, []string{"Online shop per store", "Report exports (Excel and PDF)"}, p.Features, "only known flags that are on")
	require.Equal(t, "month", p.Per())

	bad := planFromRow("x", "X", 0, "INR", "yearly", 0, []byte(`{"max_stores":"three"}`), []byte(`[]`))
	require.Empty(t, bad.Limits)
	require.Empty(t, bad.Features)
	require.Equal(t, "year", bad.Per())
}

func TestPlanCache(t *testing.T) {
	now := time.Unix(0, 0)
	calls := 0
	fail := false
	c := &planCache{ttl: time.Minute, now: func() time.Time { return now }, load: func(context.Context) ([]Plan, error) {
		calls++
		if fail {
			return nil, context.DeadlineExceeded
		}
		return []Plan{proPlan}, nil
	}}
	require.Len(t, c.get(t.Context()), 1)
	require.Len(t, c.get(t.Context()), 1)
	require.Equal(t, 1, calls, "cached")
	now = now.Add(2 * time.Minute)
	fail = true
	require.Len(t, c.get(t.Context()), 1, "a failed reload keeps the last list")
	require.Equal(t, 2, calls)
}

func TestFindMyShop(t *testing.T) {
	hs := newSite(t, nil, nil)
	tok := hs.token(t, "/find-my-shop")
	form := url.Values{"ft": {tok}, "email": {"Owner@Example.test"}}

	rec := hs.post(t, "/find-my-shop", form, "", "")
	require.Equal(t, http.StatusForbidden, rec.Code, "no Origin header")
	rec = hs.post(t, "/find-my-shop", form, "https://evil.test", "198.51.100.1")
	require.Equal(t, http.StatusForbidden, rec.Code, "another origin")

	rec = hs.post(t, "/find-my-shop", form, origin, "198.51.100.2")
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/find-my-shop/sent", rec.Header().Get("Location"))
	require.Empty(t, hs.store.finds, "sent within 2 seconds of the page: a bot, dropped quietly")

	hs.clock.add(3 * time.Second)
	rec = hs.post(t, "/find-my-shop", form, origin, "198.51.100.3")
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/find-my-shop/sent", rec.Header().Get("Location"))
	require.Equal(t, []string{"owner@example.test"}, hs.store.emails)
	require.Regexp(t, `^find_shop:[0-9a-f]{32}:\d+$`, hs.store.finds[0])
	require.NotContains(t, hs.store.finds[0], "example", "the key holds a hash, not the address")
	require.Equal(t, 1, hs.queued)

	// Any other address takes exactly the same path: a row is queued and
	// the reply is the same (the worker decides whether to send).
	other := url.Values{"ft": {tok}, "email": {"nobody@example.test"}}
	rec2 := hs.post(t, "/find-my-shop", other, origin, "198.51.100.4")
	require.Equal(t, rec.Code, rec2.Code)
	require.Equal(t, rec.Header().Get("Location"), rec2.Header().Get("Location"))
	require.Equal(t, rec.Body.String(), rec2.Body.String())
	require.Len(t, hs.store.finds, 2)

	sent := hs.get(t, "/find-my-shop/sent")
	require.Equal(t, http.StatusOK, sent.Code)
	require.Equal(t, "noindex", sent.Header().Get("X-Robots-Tag"))
	require.Contains(t, sent.Body.String(), "If the email you entered is the owner or billing email of a shop")

	// Honeypot: dropped quietly.
	hp := url.Values{"ft": {tok}, "email": {"bot@example.test"}, "website": {"http://spam.test"}}
	rec = hs.post(t, "/find-my-shop", hp, origin, "198.51.100.5")
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Len(t, hs.store.finds, 2)

	// Forged token: dropped quietly.
	rec = hs.post(t, "/find-my-shop", url.Values{"ft": {"1.abc"}, "email": {"x@example.test"}}, origin, "198.51.100.6")
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Len(t, hs.store.finds, 2)

	// Bad address: shown again with the error.
	rec = hs.post(t, "/find-my-shop", url.Values{"ft": {tok}, "email": {"not-an-email"}}, origin, "198.51.100.7")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), `aria-invalid="true"`)
	require.Contains(t, rec.Body.String(), `value="not-an-email"`)
	require.Equal(t, "noindex", rec.Header().Get("X-Robots-Tag"))

	// A page left open for over a day asks to send again.
	hs.clock.add(25 * time.Hour)
	rec = hs.post(t, "/find-my-shop", form, origin, "198.51.100.8")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "send the form again")
}

func TestFindMyShopLimits(t *testing.T) {
	hs := newSite(t, func(d *Deps) { d.Limits = Limits{FindPerIP: 3, FindPerEmail: 2} }, nil)
	tok := hs.token(t, "/find-my-shop")
	hs.clock.add(3 * time.Second)

	// One address, many IPs: after 2 the rest are dropped quietly.
	for i := range 4 {
		rec := hs.post(t, "/find-my-shop", url.Values{"ft": {tok}, "email": {"owner@example.test"}}, origin, "198.51.100."+string(rune('1'+i)))
		require.Equal(t, http.StatusSeeOther, rec.Code)
		require.Equal(t, "/find-my-shop/sent", rec.Header().Get("Location"))
	}
	require.Len(t, hs.store.finds, 2)

	// One IP: the 4th request is refused.
	for i := range 3 {
		rec := hs.post(t, "/find-my-shop", url.Values{"ft": {tok}, "email": {"a" + string(rune('a'+i)) + "@example.test"}}, origin, "192.0.2.9")
		require.Equal(t, http.StatusSeeOther, rec.Code)
	}
	rec := hs.post(t, "/find-my-shop", url.Values{"ft": {tok}, "email": {"z@example.test"}}, origin, "192.0.2.9")
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
	require.Contains(t, rec.Body.String(), "Too many requests")
}

func TestContact(t *testing.T) {
	hs := newSite(t, nil, nil)
	tok := hs.token(t, "/contact")
	hs.clock.add(3 * time.Second)

	ok := url.Values{"ft": {tok}, "topic": {"demo"}, "name": {"Asha Rao"}, "email": {"asha@example.test"},
		"business": {"Rao Stores"}, "phone": {"+91 98765-43210"}, "message": {"Two stores.\r\nDemo please."}}
	rec := hs.post(t, "/contact", ok, origin, "")
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/contact/sent", rec.Header().Get("Location"))
	require.Len(t, hs.store.contacts, 1)
	c := hs.store.contacts[0]
	require.Equal(t, contactInput{topic: "demo", name: "Asha Rao", email: "asha@example.test", business: "Rao Stores",
		phone: "+91 98765-43210", message: "Two stores.\nDemo please."}, c)
	require.Equal(t, "sales@pos.test", hs.store.notify[0])
	require.Equal(t, 1, hs.queued)

	demo := url.Values{"ft": {tok}, "name": {"B"}, "email": {"b@example.test"}}
	require.Equal(t, http.StatusSeeOther, hs.post(t, "/contact", demo, origin, "198.51.100.20").Code, "a demo request needs no message")

	bad := url.Values{"ft": {tok}, "topic": {"question"}, "name": {""}, "email": {"nope"}, "phone": {"call me"},
		"business": {strings.Repeat("x", 121)}, "message": {""}}
	rec = hs.post(t, "/contact", bad, origin, "198.51.100.21")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	for _, id := range []string{"contact-name-err", "contact-email-err", "contact-phone-err", "contact-business-err", "contact-message-err"} {
		require.Contains(t, body, `aria-describedby="`+id+`"`, id)
	}
	require.Contains(t, body, `role="alert"`)
	require.Contains(t, body, `value="question" checked`, "the choice is kept")
	require.Len(t, hs.store.contacts, 2)

	hostile := url.Values{"ft": {tok}, "name": {"<script>x</script>"}, "email": {"bad"}}
	rec = hs.post(t, "/contact", hostile, origin, "198.51.100.22")
	require.NotContains(t, rec.Body.String(), "<script>x")

	ctrl := url.Values{"ft": {tok}, "name": {"A\x00B"}, "email": {"c@example.test"}}
	require.Equal(t, http.StatusUnprocessableEntity, hs.post(t, "/contact", ctrl, origin, "198.51.100.23").Code)

	sent := hs.get(t, "/contact/sent")
	require.Equal(t, "noindex", sent.Header().Get("X-Robots-Tag"))

	rec = hs.post(t, "/contact", url.Values{"ft": {tok}}, origin, "198.51.100.24")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Wrong content type.
	req := httptest.NewRequest(http.MethodPost, "https://pos.test/contact", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.RemoteAddr = "198.51.100.25:1"
	req.TLS = nil
	r2 := httptest.NewRecorder()
	hs.h.ServeHTTP(r2, req)
	require.Equal(t, http.StatusUnsupportedMediaType, r2.Code)
}

func TestContactWithoutInboxAndLimits(t *testing.T) {
	hs := newSite(t, func(d *Deps) { d.ContactEmail = ""; d.Limits = Limits{ContactPerIP: 1, ContactTotal: 2} }, nil)
	tok := hs.token(t, "/contact")
	hs.clock.add(3 * time.Second)
	f := func(n string) url.Values {
		return url.Values{"ft": {tok}, "name": {n}, "email": {n + "@example.test"}}
	}
	require.Equal(t, http.StatusSeeOther, hs.post(t, "/contact", f("a"), origin, "198.51.100.30").Code)
	require.Equal(t, "", hs.store.notify[0], "stored only")
	require.Equal(t, 0, hs.queued)
	require.Equal(t, http.StatusTooManyRequests, hs.post(t, "/contact", f("b"), origin, "198.51.100.30").Code, "per IP")
	require.Equal(t, http.StatusSeeOther, hs.post(t, "/contact", f("c"), origin, "198.51.100.31").Code)
	require.Equal(t, http.StatusTooManyRequests, hs.post(t, "/contact", f("d"), origin, "198.51.100.32").Code, "total")
}

func TestStaffLogin(t *testing.T) {
	hs := newSite(t, func(d *Deps) { d.PublicPort = "8443" }, nil)
	rec := hs.get(t, "/login")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "noindex", rec.Header().Get("X-Robots-Tag"))
	require.Contains(t, rec.Body.String(), `<meta name="robots" content="noindex">`)
	csp := rec.Header().Get("Content-Security-Policy")
	require.Contains(t, csp, "form-action 'self' https://*.pos.test:8443;")
	require.Contains(t, csp, "script-src 'self'")
	require.NotContains(t, csp, "unsafe")

	for in, want := range map[string]string{
		"my-shop":                             "https://my-shop.pos.test:8443/login",
		"  My-Shop  ":                         "https://my-shop.pos.test:8443/login",
		"my-shop.pos.test":                    "https://my-shop.pos.test:8443/login",
		"https://my-shop.pos.test:8443/login": "https://my-shop.pos.test:8443/login",
	} {
		rec := hs.get(t, "/login/go?shop="+url.QueryEscape(in))
		require.Equal(t, http.StatusSeeOther, rec.Code, in)
		require.Equal(t, want, rec.Header().Get("Location"), in)
	}
	for _, in := range []string{"", "admin", "a", "bad shop!", "evil.test/x", "x--y", strings.Repeat("é", 300)} {
		rec := hs.get(t, "/login/go?shop="+url.QueryEscape(in))
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, in)
		require.Contains(t, rec.Body.String(), `aria-invalid="true"`)
		require.NotContains(t, rec.Header().Get("Location"), "evil")
	}
	// evil.test/x keeps only "evil.test", which is not a slug: no redirect
	// ever leaves <slug>.<BASE_DOMAIN>.
}

func TestPagesAndSEOFiles(t *testing.T) {
	hs := newSite(t, nil, nil)
	titles := map[string]string{}
	for _, p := range []string{"/", "/find-my-shop", "/contact", "/terms", "/privacy", "/login"} {
		rec := hs.get(t, p)
		require.Equal(t, http.StatusOK, rec.Code, p)
		body := rec.Body.String()
		require.Equal(t, 1, count(body, "<h1"), p)
		title := regexp.MustCompile(`<title>(.*?)</title>`).FindStringSubmatch(body)[1]
		require.NotContains(t, titles, title, "unique title: "+p)
		titles[title] = p
		desc := regexp.MustCompile(`<meta name="description" content="([^"]*)">`).FindStringSubmatch(body)
		require.NotEmpty(t, desc[1], p)
		require.Contains(t, body, `<html lang="en">`)
		require.Contains(t, body, `<main id="main"`)
		require.Contains(t, body, `href="#main"`, "skip link")
		if p != "/login" {
			require.Contains(t, body, `<link rel="canonical" href="https://pos.test`+p+`">`, p)
		}
	}
	for _, p := range []string{"/terms", "/privacy"} {
		require.Contains(t, hs.get(t, p).Body.String(), "Placeholder text.", "legal pages are marked as placeholders")
	}

	robots := hs.get(t, "/robots.txt")
	require.Equal(t, "text/plain; charset=utf-8", robots.Header().Get("Content-Type"))
	require.Contains(t, robots.Body.String(), "Sitemap: https://pos.test/sitemap.xml")
	require.NotContains(t, robots.Body.String(), "Disallow: /\n")

	sm := hs.get(t, "/sitemap.xml")
	require.Equal(t, "application/xml; charset=utf-8", sm.Header().Get("Content-Type"))
	for _, p := range sitemapPaths {
		require.Contains(t, sm.Body.String(), "<loc>https://pos.test"+p+"</loc>")
	}
	require.NotContains(t, sm.Body.String(), "/login")

	nf := hs.get(t, "/no-such-page")
	require.Equal(t, http.StatusNotFound, nf.Code)
	require.Equal(t, "noindex", nf.Header().Get("X-Robots-Tag"))
	require.Contains(t, nf.Body.String(), "Page not found")
	require.Equal(t, http.StatusNotFound, hs.get(t, "/api/anything").Code, "no API on the main address")
}

func TestFormKey(t *testing.T) {
	a, b := FormKey([]byte("k1")), FormKey([]byte("k2"))
	require.Len(t, a, 32)
	require.NotEqual(t, a, b)
	_, err := New(Deps{FormKey: []byte("short")})
	require.Error(t, err)
}
