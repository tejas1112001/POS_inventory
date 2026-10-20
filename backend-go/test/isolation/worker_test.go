//go:build integration

package isolation

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer/smtptest"
)

type attachment struct {
	Name, Type string
	Data       []byte
}

type parsed struct {
	Header      mail.Header
	Text, HTML  string
	Attachments []attachment
}

func decodePart(enc string, r io.Reader) []byte {
	switch strings.ToLower(enc) {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	}
	b, _ := io.ReadAll(r)
	return b
}

func walk(p *parsed, ctype, enc, disp string, body io.Reader) {
	mt, params, _ := mime.ParseMediaType(ctype)
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				return
			}
			walk(p, part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part.Header.Get("Content-Disposition"), part)
		}
	}
	data := decodePart(enc, body)
	_, dparams, _ := mime.ParseMediaType(disp)
	switch {
	case strings.HasPrefix(disp, "attachment") || mt == "application/pdf":
		p.Attachments = append(p.Attachments, attachment{Name: dparams["filename"], Type: mt, Data: data})
	case mt == "text/html":
		p.HTML = string(data)
	default:
		p.Text = string(data)
	}
}

func parseMail(t *testing.T, raw []byte) parsed {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)
	p := parsed{Header: m.Header}
	walk(&p, m.Header.Get("Content-Type"), m.Header.Get("Content-Transfer-Encoding"), "", m.Body)
	return p
}

// waitMail waits for a message to addr on srv.
func waitMail(t *testing.T, srv *smtptest.Server, addr string) parsed {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range srv.Messages() {
			for _, to := range m.To {
				if strings.EqualFold(to, addr) {
					return parseMail(t, m.Data)
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no email to %s", addr)
	return parsed{}
}

func gotMail(srv *smtptest.Server, addr string) bool {
	for _, m := range srv.Messages() {
		for _, to := range m.To {
			if strings.EqualFold(to, addr) {
				return true
			}
		}
	}
	return false
}

type outboxState struct {
	Status, Via, Class string
	Attempts           int
	NextRetry          time.Time
}

func outboxByTo(t *testing.T, l, to string) outboxState {
	t.Helper()
	var s outboxState
	var via, class *string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT status, sent_via, last_error_class, attempts, next_retry_at
		FROM `+schema(l)+`.email_outbox WHERE to_addr = $1 ORDER BY id DESC LIMIT 1`, to).Scan(&s.Status, &via, &class, &s.Attempts, &s.NextRetry))
	if via != nil {
		s.Via = *via
	}
	if class != nil {
		s.Class = *class
	}
	return s
}

func waitOutbox(t *testing.T, l, to string, want func(outboxState) bool) outboxState {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var s outboxState
	for time.Now().Before(deadline) {
		s = outboxByTo(t, l, to)
		if want(s) {
			return s
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("outbox row to %s stuck: %+v", to, s)
	return s
}

func uniqueAddr(prefix string) string {
	return fmt.Sprintf("%s-%d@example.test", prefix, time.Now().UnixNano())
}

func TestWelcomeEmailsAtProvisioning(t *testing.T) {
	for _, l := range []string{"a", "b", "c"} {
		m := waitMail(t, platSrv, tenants[l].OwnerEmail)
		require.Equal(t, "Your shop Shop "+strings.ToUpper(l)+" is ready on Test", m.Header.Get("Subject"))
		require.Contains(t, m.Header.Get("From"), "<no-reply@pos.test>")
		require.Contains(t, m.Text, "http://"+tenants[l].Host)
		require.Regexp(t, `http://`+regexp.QuoteMeta(tenants[l].Host)+`/accept-invite#token=[A-Za-z0-9_-]+`, m.Text, "set-password link")
		require.Equal(t, "<platform.tenant:"+tenants[l].ID+":welcome@pos.test>", m.Header.Get("Message-Id"), "stable Message-ID")
	}
	var status string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT string_agg(DISTINCT status, ',') FROM platform.email_outbox`).Scan(&status))
	require.Equal(t, "sent", status)
}

func TestReceiptEmailViaPlatformFallback(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "MAIL-FB", store, 5)
	to := uniqueAddr("fallback")
	body := posSale(store, line(p, 1))
	body["receiptEmail"] = to
	s := sell(t, "a", tok, body, nil)
	require.Equal(t, 201, s.Code, s.Raw)

	m := waitMail(t, platSrv, to)
	require.Equal(t, `"Shop A via Test" <no-reply@pos.test>`, m.Header.Get("From"), "platform identity, never the tenant's domain")
	require.Contains(t, m.Header.Get("Subject"), s.Body["reference"].(string))
	require.Contains(t, m.Header.Get("Message-Id"), tenants["a"].ID+".sale:", "Message-ID carries the tenant, so tenants never collide")
	require.Len(t, m.Attachments, 1)
	require.Equal(t, "application/pdf", m.Attachments[0].Type)
	require.Equal(t, s.Body["reference"].(string)+".pdf", m.Attachments[0].Name)
	require.True(t, bytes.HasPrefix(m.Attachments[0].Data, []byte("%PDF-")))
	require.Contains(t, m.Text, "Your receipt is attached")
	st := waitOutbox(t, "a", to, func(s outboxState) bool { return s.Status == "sent" })
	require.Equal(t, "platform", st.Via)
	require.False(t, gotMail(smtpSrv, to), "tenant A has no SMTP; B's server never sees A's mail")
}

// prepareTenantSMTP configures tenant B's SMTP and verifies it.
func prepareTenantSMTP(t *testing.T, tok string, extra map[string]any) {
	t.Helper()
	host := tenants["b"].Host
	smtpSrv.FailAt("AUTH", "")
	smtpSrv.FailAt("RCPT", "")
	smtpSrv.FailAt("DATA", "")
	b := smtpBody()
	for k, v := range extra {
		b[k] = v
	}
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: b}).Code)
	r := do(t, rq{method: "POST", host: host, path: "/api/settings/email/test", token: tok, body: map[string]any{}})
	require.Equal(t, true, r.Body["ok"], r.Raw)
}

func TestReceiptEmailViaTenantSMTPAndFallbackOnAuthFailure(t *testing.T) {
	tok, _ := ownerLogin(t, "b")
	host := tenants["b"].Host
	prepareTenantSMTP(t, tok, nil)
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"email": "replies@shop-b.test"}}).Code)
	business := get(t, host, "/api/settings", tok).Body["businessName"].(string)
	store := storeIDs(t, "b", tok)[0]
	p := stockedProduct(t, "b", tok, "MAIL-B", store, 20)
	saleTo := func(addr string) {
		body := posSale(store, line(p, 1))
		body["receiptEmail"] = addr
		require.Equal(t, 201, sell(t, "b", tok, body, nil).Code)
	}

	// Through the tenant's own mailbox.
	to := uniqueAddr("tenant")
	saleTo(to)
	m := waitMail(t, smtpSrv, to)
	require.Equal(t, `"Shop B" <shop@iso-b.test>`, m.Header.Get("From"))
	require.Len(t, m.Attachments, 1)
	require.Equal(t, "tenant", waitOutbox(t, "b", to, func(s outboxState) bool { return s.Status == "sent" }).Via)
	require.False(t, gotMail(platSrv, to))

	// The App Password stops working: SMTP is marked failing and the
	// receipt goes out through the platform, replying to the business.
	smtpSrv.FailAt("AUTH", "535 5.7.8 Username and Password not accepted")
	to = uniqueAddr("authfail")
	saleTo(to)
	m = waitMail(t, platSrv, to)
	require.Equal(t, `"`+business+` via Test" <no-reply@pos.test>`, m.Header.Get("From"))
	require.Contains(t, m.Header.Get("Reply-To"), "<replies@shop-b.test>")
	require.Equal(t, "platform", waitOutbox(t, "b", to, func(s outboxState) bool { return s.Status == "sent" }).Via)
	es := get(t, host, "/api/settings/email", tok)
	require.Equal(t, "failing", es.Body["status"])
	require.Equal(t, "smtp_535", es.Body["lastErrorCode"])

	// Without fallback the receipt waits instead.
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: "/api/settings/email", token: tok, body: map[string]any{"fallbackToPlatform": false}}).Code)
	to = uniqueAddr("nofallback")
	saleTo(to)
	st := waitOutbox(t, "b", to, func(s outboxState) bool { return s.Status == "failed" })
	require.Equal(t, "auth", st.Class)
	require.True(t, st.NextRetry.After(time.Now()))
	require.False(t, gotMail(platSrv, to))
	require.False(t, gotMail(smtpSrv, to))

	prepareTenantSMTP(t, tok, map[string]any{"fallbackToPlatform": true})
}

func TestPermanentAndTransientFailures(t *testing.T) {
	tok, _ := ownerLogin(t, "b")
	prepareTenantSMTP(t, tok, nil)
	store := storeIDs(t, "b", tok)[0]
	p := stockedProduct(t, "b", tok, "MAIL-FAIL", store, 20)
	saleTo := func(addr string) {
		body := posSale(store, line(p, 1))
		body["receiptEmail"] = addr
		require.Equal(t, 201, sell(t, "b", tok, body, nil).Code)
	}

	smtpSrv.FailAt("RCPT", "550 5.1.1 no such user")
	to := uniqueAddr("bounce")
	saleTo(to)
	st := waitOutbox(t, "b", to, func(s outboxState) bool { return s.Status == "dead" })
	require.Equal(t, "permanent", st.Class)
	require.Equal(t, 1, st.Attempts, "permanent errors are not retried")
	smtpSrv.FailAt("RCPT", "")

	smtpSrv.FailAt("DATA", "451 4.3.0 try again later")
	to = uniqueAddr("later")
	saleTo(to)
	st = waitOutbox(t, "b", to, func(s outboxState) bool { return s.Status == "failed" })
	require.Equal(t, "transient", st.Class)
	require.Equal(t, 1, st.Attempts)
	require.True(t, st.NextRetry.After(time.Now().Add(20*time.Second)), "backoff before the next try")
	smtpSrv.FailAt("DATA", "")
	es := get(t, tenants["b"].Host, "/api/settings/email", tok)
	require.Equal(t, "ok", es.Body["status"], "a 4xx does not mark the mailbox failing")

	// The retry goes through once due (moved forward for the test).
	_, err := admin(t).Exec(context.Background(), `UPDATE `+schema("b")+`.email_outbox SET next_retry_at = now() WHERE to_addr = $1`, to)
	require.NoError(t, err)
	_, err = admin(t).Exec(context.Background(), `UPDATE platform.mail_dispatch SET next_attempt_at = now() WHERE tenant_id = $1`, tenants["b"].ID)
	require.NoError(t, err)
	st = waitOutbox(t, "b", to, func(s outboxState) bool { return s.Status == "sent" })
	require.Equal(t, 2, st.Attempts)
}

func linkToken(t *testing.T, m parsed, path string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(path) + `#token=([A-Za-z0-9_-]+)`)
	got := re.FindStringSubmatch(m.Text)
	require.NotNil(t, got, "no %s link in: %s", path, m.Text)
	return got[1]
}

func TestPasswordResetAndInviteEmailsWork(t *testing.T) {
	owner, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	createUser(t, "a", owner, "mailreset@iso-a.test", "biller", "mailreset-pass-11")
	require.Equal(t, 202, do(t, rq{method: "POST", host: host, path: "/api/auth/password/forgot", body: map[string]string{"email": "mailreset@iso-a.test"}}).Code)
	m := waitMail(t, platSrv, "mailreset@iso-a.test")
	require.Equal(t, "Reset your password for Shop A", m.Header.Get("Subject"))
	require.Contains(t, m.Text, "expires in 59 minutes")
	tok := linkToken(t, m, "http://"+host+"/reset-password")
	require.Equal(t, 204, do(t, rq{method: "POST", host: host, path: "/api/auth/password/reset", body: map[string]string{"token": tok, "password": "reset-by-email-pass-7"}}).Code)
	login(t, "a", "mailreset@iso-a.test", "reset-by-email-pass-7")

	// An invite without a password.
	r := do(t, rq{method: "POST", host: host, path: "/api/users", token: owner, body: map[string]any{
		"name": "Invited Person", "email": "mailinvite@iso-a.test", "role_id": roleID(t, "a", "biller"), "status": "Active"}})
	require.Equal(t, 201, r.Code, r.Raw)
	m = waitMail(t, platSrv, "mailinvite@iso-a.test")
	require.Equal(t, "You're invited to Shop A", m.Header.Get("Subject"))
	require.Contains(t, m.Text, "Hello Invited Person")
	tok = linkToken(t, m, "http://"+host+"/accept-invite")
	require.Equal(t, 204, do(t, rq{method: "POST", host: host, path: "/api/auth/invite/accept", body: map[string]string{"token": tok, "password": "invited-by-email-pass-8"}}).Code)
	login(t, "a", "mailinvite@iso-a.test", "invited-by-email-pass-8")
}

func TestLeasedRowsAreNotStolenAndExpiredLeasesRecover(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "MAIL-LEASE", store, 5)
	s := sell(t, "a", tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s.Code)
	to := uniqueAddr("lease")
	// Another worker holds this row.
	_, err := admin(t).Exec(context.Background(), `INSERT INTO `+schema("a")+`.email_outbox
		(idempotency_key, kind, to_addr, template, template_data, status, lease_until, leased_by)
		VALUES ($1, 'receipt', $2, 'receipt', jsonb_build_object('sale_id', $3::bigint), 'sending', now() + interval '1 hour', 'ghost')`,
		fmt.Sprintf("sale:%v:receipt:test-lease", s.Body["id"]), to, int64(s.Body["id"].(float64)))
	require.NoError(t, err)
	time.Sleep(3 * time.Second) // sweeps run every second
	st := outboxByTo(t, "a", to)
	require.Equal(t, "sending", st.Status, "a live lease is respected")
	require.False(t, gotMail(platSrv, to))

	// The other worker crashed: the lease expires and the row is recovered.
	_, err = admin(t).Exec(context.Background(), `UPDATE `+schema("a")+`.email_outbox SET lease_until = now() - interval '1 second' WHERE to_addr = $1`, to)
	require.NoError(t, err)
	_, err = admin(t).Exec(context.Background(), `UPDATE platform.mail_dispatch SET next_attempt_at = now() WHERE tenant_id = $1`, tenants["a"].ID)
	require.NoError(t, err)
	waitMail(t, platSrv, to)
	require.Equal(t, "sent", waitOutbox(t, "a", to, func(s outboxState) bool { return s.Status == "sent" }).Status)
}

func TestMaintenanceJobs(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	ctx := context.Background()
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "MAINT-1", store, 5)
	m := theApp.Worker().Maintenance()

	// Expired POS locks go; live ones stay.
	var uid int64
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT id FROM `+schema("a")+`.users ORDER BY id LIMIT 1`).Scan(&uid))
	p2 := stockedProduct(t, "a", tok, "MAINT-2", store, 5)
	_, err := admin(t).Exec(ctx, `INSERT INTO `+schema("a")+`.pos_locks (store_id, product_id, user_id, expires_at) VALUES
		($1, $2, $4, now() - interval '10 minutes'), ($1, $3, $4, now() + interval '2 minutes')`, int64(store), int64(p.pid), int64(p2.pid), uid)
	require.NoError(t, err)
	n, err := m.SweepPOSLocks(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))
	var left []int64
	rows, err := admin(t).Query(ctx, `SELECT product_id FROM `+schema("a")+`.pos_locks WHERE product_id = ANY($1)`, []int64{int64(p.pid), int64(p2.pid)})
	require.NoError(t, err)
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		left = append(left, id)
	}
	rows.Close()
	require.Equal(t, []int64{int64(p2.pid)}, left)

	// Idempotency keys older than a day go.
	_, err = admin(t).Exec(ctx, `INSERT INTO `+schema("a")+`.idempotency_keys (user_id, key, request_hash, created_at) VALUES
		($1, 'maint-old', '\x00', now() - interval '25 hours'), ($1, 'maint-new', '\x00', now() - interval '1 hour')`, uid)
	require.NoError(t, err)
	_, err = m.CleanupIdempotencyKeys(ctx)
	require.NoError(t, err)
	var keys []string
	rows, err = admin(t).Query(ctx, `SELECT key FROM `+schema("a")+`.idempotency_keys WHERE key LIKE 'maint-%'`)
	require.NoError(t, err)
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		keys = append(keys, k)
	}
	rows.Close()
	require.Equal(t, []string{"maint-new"}, keys)

	// Ledger check: a quantity changed outside the ledger is reported in
	// that tenant's audit log only.
	_, err = admin(t).Exec(ctx, `UPDATE `+schema("a")+`.inventories SET qty = qty + 5 WHERE variant_id = $1`, int64(p.vid))
	require.NoError(t, err)
	defer func() {
		_, _ = admin(t).Exec(ctx, `UPDATE `+schema("a")+`.inventories SET qty = qty - 5 WHERE variant_id = $1`, int64(p.vid))
	}()
	found, err := m.ReconcileLedgers(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, found, 1)
	var after string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT after::text FROM `+schema("a")+`.audit_log
		WHERE action = 'stock.ledger_mismatch' AND actor_type = 'system' ORDER BY id DESC LIMIT 1`).Scan(&after))
	require.Contains(t, after, fmt.Sprintf(`"variant_id": %d`, int64(p.vid)))
	for _, l := range []string{"b", "c"} {
		var n int
		require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'stock.ledger_mismatch'`).Scan(&n))
		require.Zero(t, n, "tenant %s untouched", l)
	}
}

func TestJobsAreReady(t *testing.T) {
	r := get(t, tenants["a"].Host, "/readyz", "")
	require.Equal(t, 200, r.Code, r.Raw)
	require.True(t, theApp.Worker().Started())
}
