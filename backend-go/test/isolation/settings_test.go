//go:build integration

package isolation

import (
	"context"
	"image/color"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Settings tests use tenant B so sales tests on A keep the default prefix.

func TestSettingsReadUpdateAndValidation(t *testing.T) {
	tok, _ := ownerLogin(t, "b")
	host := tenants["b"].Host
	g := get(t, host, "/api/settings", tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Equal(t, "Shop B", g.Body["businessName"])
	require.Equal(t, "INR", g.Body["currency"])
	require.Equal(t, "en-IN", g.Body["locale"])
	require.Equal(t, "POS-INV", g.Body["invoicePrefix"])
	require.NotNil(t, g.Body["defaultCustomerId"], "walk-in customer")

	put := func(body map[string]any) resp {
		return do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: body})
	}
	r := put(map[string]any{"businessName": "Shop B Ltd", "legalName": "Shop B Private Limited", "taxRegistrationNo": "27abcde1234f1z5",
		"receiptHeader": "Thank you!\nVisit again", "receiptFooter": "No refunds after 7 days", "taxSplit": "igst",
		"maxDiscountPct": "12.5", "phone": "+91 98765 43210", "email": "hello@shop-b.test", "logo": pngDataURI(t, color.RGBA{G: 200, A: 255})})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "Shop B Ltd", r.Body["businessName"])
	require.Equal(t, "27ABCDE1234F1Z5", r.Body["taxRegistrationNo"], "upper-cased")
	require.Equal(t, "Thank you!\nVisit again", r.Body["receiptHeader"])
	require.Equal(t, "igst", r.Body["taxSplit"])
	require.Equal(t, 12.5, r.Body["maxDiscountPct"])
	require.Regexp(t, `^/files/t/[0-9a-f-]{36}$`, r.Body["logo"], "the tenant logo is public")
	require.Equal(t, 200, get(t, host, r.Body["logo"].(string), "").Code)

	bad := map[string]map[string]any{
		"empty name":        {"businessName": " "},
		"name with CRLF":    {"businessName": "Shop\r\nBcc: x@evil.test"},
		"currency":          {"currency": "RUPEE"},
		"unknown currency":  {"currency": "XQQ"},
		"locale":            {"locale": "fr-FR"},
		"timezone":          {"timezone": "Mars/Base"},
		"local timezone":    {"timezone": "Local"},
		"prefix":            {"invoicePrefix": "INV/2026"},
		"header NUL":        {"receiptHeader": "a\x00b"},
		"footer too long":   {"receiptFooter": strings.Repeat("x", 501)},
		"email":             {"email": "not-an-email"},
		"email injection":   {"email": "a@b.test\r\nBcc: c@d.test"},
		"discount":          {"maxDiscountPct": 150},
		"split":             {"taxSplit": "vat"},
		"unknown customer":  {"defaultCustomerId": 999999},
		"unknown field":     {"language": "hi"},
		"tax reg injection": {"taxRegistrationNo": "27AB\nX"},
	}
	for name, body := range bad {
		require.Equal(t, 400, put(body).Code, name)
	}
	r = put(map[string]any{"currency": "usd", "locale": "en-US", "timezone": "America/New_York", "invoicePrefix": "B-INV-"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "USD", r.Body["currency"])
	require.Equal(t, "B-INV", r.Body["invoicePrefix"], "trailing dash trimmed")

	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("b")+`.audit_log WHERE action = 'settings.update'`).Scan(&n))
	require.GreaterOrEqual(t, n, 2)

	// Restore what other tests on B expect.
	require.Equal(t, 200, put(map[string]any{"currency": "INR", "locale": "en-IN", "timezone": "Asia/Kolkata", "invoicePrefix": "POS-INV", "taxSplit": "cgst_sgst"}).Code)

	createUser(t, "b", tok, "biller-settings@iso-b.test", "biller", "biller-settings-pass-1")
	btok, _ := login(t, "b", "biller-settings@iso-b.test", "biller-settings-pass-1")
	require.Equal(t, 200, get(t, host, "/api/settings", btok).Code, "billers see settings (receipts)")
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: "/api/settings", token: btok, body: map[string]any{"businessName": "x"}}).Code)
	require.Equal(t, 403, get(t, host, "/api/settings/email", btok).Code)

	// Tenant A is untouched.
	aTok, _ := ownerLogin(t, "a")
	require.Equal(t, "Shop A", get(t, tenants["a"].Host, "/api/settings", aTok).Body["businessName"])
}

func smtpBody() map[string]any {
	return map[string]any{"host": smtpHost, "port": 587, "username": smtpUser, "password": smtpPass,
		"fromEmail": smtpUser, "fromName": "Shop B", "enabled": true}
}

func TestEmailSettingsWriteOnlyPasswordAndSSRFInput(t *testing.T) {
	tok, _ := ownerLogin(t, "b")
	host := tenants["b"].Host
	put := func(body map[string]any) resp {
		return do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: body})
	}
	g := get(t, host, "/api/settings/email", tok)
	require.Equal(t, 200, g.Code)
	require.Equal(t, false, g.Body["configured"])

	missing := put(map[string]any{"enabled": true})
	require.Equal(t, 400, missing.Code)
	for _, k := range []string{"host", "username", "password", "fromEmail"} {
		require.Contains(t, missing.Body["fields"], k)
	}

	bad := map[string]func(b map[string]any){
		"port 25":          func(b map[string]any) { b["port"] = 25 },
		"port 2525":        func(b map[string]any) { b["port"] = 2525 },
		"tls mismatch":     func(b map[string]any) { b["tlsMode"] = "implicit" },
		"localhost":        func(b map[string]any) { b["host"] = "localhost" },
		"loopback IP":      func(b map[string]any) { b["host"] = "127.0.0.1" },
		"metadata IP":      func(b map[string]any) { b["host"] = "169.254.169.254" },
		"private IP":       func(b map[string]any) { b["host"] = "10.0.0.1" },
		"internal name":    func(b map[string]any) { b["host"] = "smtp.internal" },
		"dot local":        func(b map[string]any) { b["host"] = "printer.local" },
		"IPv6 literal":     func(b map[string]any) { b["host"] = "::1" },
		"host CRLF":        func(b map[string]any) { b["host"] = "smtp.example.test\r\nX: y" },
		"host with port":   func(b map[string]any) { b["host"] = "smtp.example.test:25" },
		"from name CRLF":   func(b map[string]any) { b["fromName"] = "Shop\r\nBcc: x@evil.test" },
		"from email CRLF":  func(b map[string]any) { b["fromEmail"] = "shop@iso-b.test\r\nBcc: x@evil.test" },
		"username CRLF":    func(b map[string]any) { b["username"] = "shop@iso-b.test\r\nX-Evil: 1" },
		"password NUL":     func(b map[string]any) { b["password"] = "a\x00b" },
		"from not mailbox": func(b map[string]any) { b["fromEmail"] = "ceo@iso-b.test" },
		"daily cap":        func(b map[string]any) { b["dailyCap"] = 0 },
	}
	for name, mutate := range bad {
		b := smtpBody()
		mutate(b)
		require.Equal(t, 400, put(b).Code, name)
	}

	ok := put(smtpBody())
	require.Equal(t, 200, ok.Code, ok.Raw)
	require.Equal(t, true, ok.Body["passwordSet"])
	require.Equal(t, "unverified", ok.Body["status"])
	require.Equal(t, "starttls", ok.Body["tlsMode"])
	require.NotContains(t, ok.Raw, smtpPass, "the password is write-only")
	g = get(t, host, "/api/settings/email", tok)
	require.NotContains(t, g.Raw, smtpPass)
	require.NotContains(t, g.Raw, "password\":")

	// Omitting the password keeps it; changing the host forces re-verification.
	keep := put(map[string]any{"fromName": "Shop B Billing"})
	require.Equal(t, 200, keep.Code, keep.Raw)
	require.Equal(t, true, keep.Body["passwordSet"])

	// Stored encrypted and bound to tenant B.
	var ct, dek []byte
	var kid string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT password_ciphertext, password_dek_wrapped, key_id
		FROM `+schema("b")+`.tenant_email_settings`).Scan(&ct, &dek, &kid))
	require.NotContains(t, string(ct), smtpPass)
	var audits string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT string_agg(after::text, ' ') FROM `+schema("b")+`.audit_log
		WHERE action LIKE 'smtp.%'`).Scan(&audits))
	require.NotContains(t, audits, smtpPass)

	// The same ciphertext copied into tenant C does not decrypt there.
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	_, err := admin(t).Exec(context.Background(), `INSERT INTO `+schema("c")+`.tenant_email_settings
		(host, port, tls_mode, username, password_ciphertext, password_dek_wrapped, key_id, from_email)
		VALUES ($1, 587, 'starttls', $2, $3, $4, $5, $6)`, smtpHost, smtpUser, ct, dek, kid, smtpUser)
	require.NoError(t, err)
	before := len(smtpSrv.Messages())
	stolen := do(t, rq{method: "POST", host: tenants["c"].Host, path: "/api/settings/email/test", token: cTok, body: map[string]any{}})
	require.Equal(t, 409, stolen.Code, stolen.Raw)
	require.Len(t, smtpSrv.Messages(), before, "nothing sent with another tenant's password")
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: tenants["c"].Host, path: "/api/settings/email", token: cTok}).Code)

	// Tenant A still has nothing configured.
	aTok, _ := ownerLogin(t, "a")
	require.Equal(t, false, get(t, tenants["a"].Host, "/api/settings/email", aTok).Body["configured"])
	require.Equal(t, 400, do(t, rq{method: "POST", host: tenants["a"].Host, path: "/api/settings/email/test", token: aTok, body: map[string]any{}}).Code)
}

func TestSMTPTestSendSSRFAndHeaderInjection(t *testing.T) {
	tok, _ := ownerLogin(t, "b")
	host := tenants["b"].Host
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: smtpBody()}).Code)
	test := func(body map[string]any) resp {
		return do(t, rq{method: "POST", host: host, path: "/api/settings/email/test", token: tok, body: body})
	}
	smtpSrv.Reset()

	r := test(map[string]any{})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, true, r.Body["ok"], r.Raw)
	require.Equal(t, "ok", r.Body["status"])
	got := smtpSrv.Messages()
	require.Len(t, got, 1)
	require.Equal(t, []string{tenants["b"].OwnerEmail}, got[0].To, "defaults to the acting user")
	data := string(got[0].Data)
	require.Contains(t, data, "From: \"Shop B\" <shop@iso-b.test>")
	require.Contains(t, data, "Subject: SMTP test from Shop B")
	require.NotContains(t, data, smtpPass)
	require.Equal(t, "ok", get(t, host, "/api/settings/email", tok).Body["status"])

	// Wrong credentials: failing, classified, nothing leaked.
	smtpSrv.FailAt("AUTH", "535 5.7.8 Username and Password not accepted")
	r = test(map[string]any{})
	require.Equal(t, false, r.Body["ok"])
	require.Equal(t, "auth", r.Body["errorClass"])
	require.Equal(t, "smtp_535", r.Body["errorCode"])
	require.Equal(t, "failing", get(t, host, "/api/settings/email", tok).Body["status"])
	smtpSrv.FailAt("AUTH", "")

	// SSRF: names that resolve to internal addresses are refused at
	// connect time, before any connection.
	for _, h := range []string{"metadata.evil.test", "private.evil.test", "mapped.evil.test", "mixed.evil.test"} {
		b := smtpBody()
		b["host"] = h
		require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: b}).Code, h)
		conns := smtpSrv.Connections()
		r = test(map[string]any{})
		require.Equal(t, false, r.Body["ok"], h)
		require.Equal(t, "blocked_address", r.Body["errorCode"], h)
		require.Equal(t, conns, smtpSrv.Connections(), "no connection for %s", h)
	}
	b := smtpBody()
	b["host"] = "nx.evil.test"
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: b}).Code)
	require.Equal(t, "dns_not_found", test(map[string]any{}).Body["errorCode"])
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: smtpBody()}).Code)

	// Header injection through the recipient.
	for _, to := range []string{"x@evil.test\r\nBcc: y@evil.test", "x@evil.test, y@evil.test", "X <x@evil.test>"} {
		require.Equal(t, 400, test(map[string]any{"to": to}).Code, to)
	}

	// A custom recipient is limited to 5 per hour.
	smtpSrv.Reset()
	for i := range 5 {
		r = test(map[string]any{"to": "someone@elsewhere.test"})
		require.Equal(t, 200, r.Code, "custom test %d: %s", i, r.Raw)
	}
	require.Equal(t, 429, test(map[string]any{"to": "someone@elsewhere.test"}).Code)
	require.Equal(t, 200, test(map[string]any{}).Code, "the user's own address still works")
	require.Len(t, smtpSrv.Messages(), 6)

	// A biller cannot run tests; another tenant's token is refused.
	createUser(t, "b", tok, "biller-smtp@iso-b.test", "biller", "biller-smtp-pass-1")
	btok, _ := login(t, "b", "biller-smtp@iso-b.test", "biller-smtp-pass-1")
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/settings/email/test", token: btok, body: map[string]any{}}).Code)
	aTok, _ := ownerLogin(t, "a")
	require.Equal(t, 401, do(t, rq{method: "POST", host: host, path: "/api/settings/email/test", token: aTok, body: map[string]any{}}).Code)
}
