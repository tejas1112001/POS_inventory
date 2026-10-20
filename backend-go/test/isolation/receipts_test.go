//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func outboxRows(t *testing.T, l, keyPrefix string) []map[string]any {
	t.Helper()
	rows, err := admin(t).Query(context.Background(), `SELECT idempotency_key, kind, to_addr::text, template, sender_policy,
		coalesce(attachment_receipt_id, 0), template_data::text FROM `+schema(l)+`.email_outbox WHERE idempotency_key LIKE $1 ORDER BY id`, keyPrefix+"%")
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var key, kind, to, tmpl, policy, data string
		var rid int64
		require.NoError(t, rows.Scan(&key, &kind, &to, &tmpl, &policy, &rid, &data))
		out = append(out, map[string]any{"key": key, "kind": kind, "to": to, "template": tmpl, "policy": policy, "receipt": rid, "data": data})
	}
	return out
}

func TestReceiptDownloadVersionsAndVoid(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RCPT-1", store, 10)
	s := sell(t, "a", tok, posSale(store, line(p, 2)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	id := s.Body["id"]
	require.Equal(t, fmt.Sprintf("/api/sales/%v/receipt", id), s.Body["receiptUrl"])

	r := do(t, rq{method: "GET", host: host, path: s.Body["receiptUrl"].(string), token: tok})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "application/pdf", r.Header.Get("Content-Type"))
	require.True(t, strings.HasPrefix(r.Raw, "%PDF-"))
	require.Contains(t, r.Header.Get("Content-Disposition"), s.Body["reference"].(string)+".pdf")
	require.Equal(t, "private, no-store", r.Header.Get("Cache-Control"))

	versions := func() map[int]string {
		rows, err := admin(t).Query(context.Background(), `SELECT sale_version, status FROM `+schema("a")+`.receipts WHERE sale_id = $1`, int64(id.(float64)))
		require.NoError(t, err)
		defer rows.Close()
		out := map[int]string{}
		for rows.Next() {
			var v int
			var st string
			require.NoError(t, rows.Scan(&v, &st))
			out[v] = st
		}
		return out
	}
	require.Equal(t, map[int]string{1: "rendered"}, versions())
	var key string
	var sum []byte
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT file_key, sha256 FROM `+schema("a")+`.receipts WHERE sale_id = $1`,
		int64(id.(float64))).Scan(&key, &sum))
	require.Equal(t, fmt.Sprintf("tenants/%s/receipts/", tenants["a"].ID), key[:len("tenants/")+10+len("/receipts/")], "stored under the tenant's prefix")
	require.Len(t, sum, 32)
	again := do(t, rq{method: "GET", host: host, path: s.Body["receiptUrl"].(string), token: tok})
	require.Equal(t, r.Raw, again.Raw, "the stored PDF is served")

	// An edit adds version 2; a void adds version 3. Old versions stay.
	e := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/sales/%v", id), token: tok, body: posSale(store, line(p, 3))})
	require.Equal(t, 200, e.Code, e.Raw)
	require.Equal(t, 200, do(t, rq{method: "GET", host: host, path: s.Body["receiptUrl"].(string), token: tok}).Code)
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", id), token: tok}).Code)
	v := do(t, rq{method: "GET", host: host, path: s.Body["receiptUrl"].(string), token: tok})
	require.Equal(t, 200, v.Code)
	require.NotEqual(t, r.Raw, v.Raw)
	require.Equal(t, map[int]string{1: "rendered", 2: "rendered", 3: "rendered"}, versions())

	require.Equal(t, 404, get(t, host, "/api/sales/999999/receipt", tok).Code)
}

func TestReceiptEmailQueuedWithTheSale(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RCPT-MAIL", store, 10)

	// Walk-in, no address: no email.
	s := sell(t, "a", tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s.Code)
	require.Empty(t, outboxRows(t, "a", fmt.Sprintf("sale:%v:", s.Body["id"])))

	// The cashier types an address.
	body := posSale(store, line(p, 1))
	body["receiptEmail"] = "walkin@example.test"
	s = sell(t, "a", tok, body, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	rows := outboxRows(t, "a", fmt.Sprintf("sale:%v:", s.Body["id"]))
	require.Len(t, rows, 1)
	require.Equal(t, fmt.Sprintf("sale:%v:receipt", s.Body["id"]), rows[0]["key"])
	require.Equal(t, "receipt", rows[0]["kind"])
	require.Equal(t, "walkin@example.test", rows[0]["to"])
	require.Equal(t, "tenant_then_platform", rows[0]["policy"])
	require.NotZero(t, rows[0]["receipt"])
	require.NotContains(t, rows[0]["data"], "total", "template data holds IDs only")
	require.Equal(t, "walkin@example.test", s.Body["receipt_email"])

	// A customer with an email gets it without typing.
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Meera", "email": "meera@example.test"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	body = posSale(store, line(p, 1))
	body["customerId"] = cu.Body["id"]
	s = sell(t, "a", tok, body, nil)
	require.Equal(t, 201, s.Code)
	rows = outboxRows(t, "a", fmt.Sprintf("sale:%v:", s.Body["id"]))
	require.Len(t, rows, 1)
	require.Equal(t, "meera@example.test", rows[0]["to"])

	// Header injection through the receipt address is refused.
	body = posSale(store, line(p, 1))
	body["receiptEmail"] = "x@example.test\r\nBcc: y@evil.test"
	require.Equal(t, 400, sell(t, "a", tok, body, nil).Code)

	// The trigger marked tenant A, and only A, for the mail worker.
	var marked []string
	rs, err := admin(t).Query(context.Background(), `SELECT tenant_id FROM platform.mail_dispatch`)
	require.NoError(t, err)
	for rs.Next() {
		var id string
		require.NoError(t, rs.Scan(&id))
		marked = append(marked, id)
	}
	rs.Close()
	require.Contains(t, marked, tenants["a"].ID)
	require.NotContains(t, marked, tenants["c"].ID)
}

func TestReceiptResendRules(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RCPT-RESEND", store, 20)
	s := sell(t, "a", tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, s.Code)
	path := fmt.Sprintf("/api/sales/%v/receipt/resend", s.Body["id"])
	resend := func(tk string, body map[string]any) resp {
		return do(t, rq{method: "POST", host: host, path: path, token: tk, body: body})
	}

	require.Equal(t, 400, resend(tok, map[string]any{}).Code, "no address anywhere")
	require.Equal(t, 400, resend(tok, map[string]any{"email": "a@b.test\r\nBcc: c@evil.test"}).Code)
	r := resend(tok, map[string]any{"email": "later@example.test"})
	require.Equal(t, 202, r.Code, r.Raw)
	one := get(t, host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), tok)
	require.Equal(t, "later@example.test", one.Body["receipt_email"], "the new address is saved")
	rows := outboxRows(t, "a", fmt.Sprintf("sale:%v:receipt:resend:", s.Body["id"]))
	require.Len(t, rows, 1)
	require.Equal(t, "receipt_resend", rows[0]["kind"])

	// The saved address is used; 5 per address per day.
	for i := 2; i <= 5; i++ {
		require.Equal(t, 202, resend(tok, map[string]any{}).Code, "resend %d", i)
	}
	require.Equal(t, 429, resend(tok, map[string]any{}).Code)
	require.Len(t, outboxRows(t, "a", fmt.Sprintf("sale:%v:receipt:resend:", s.Body["id"])), 5)
	var audits int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.audit_log
		WHERE entity = 'sale' AND entity_id = $1 AND action IN ('receipt.resend', 'sale.receipt_email')`, fmt.Sprint(s.Body["id"])).Scan(&audits))
	require.Equal(t, 6, audits)
}

func TestReceiptsOwnOnlyAndIsolation(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "RCPT-OWN", store, 10)
	createUser(t, "a", tok, "biller-rcpt1@iso-a.test", "biller", "biller-rcpt1-pass-1")
	createUser(t, "a", tok, "biller-rcpt2@iso-a.test", "biller", "biller-rcpt2-pass-1")
	b1, _ := login(t, "a", "biller-rcpt1@iso-a.test", "biller-rcpt1-pass-1")
	b2, _ := login(t, "a", "biller-rcpt2@iso-a.test", "biller-rcpt2-pass-1")
	body := posSale(store, line(p, 1))
	body["receiptEmail"] = "own@example.test"
	s1 := sell(t, "a", b1, body, nil)
	require.Equal(t, 201, s1.Code)
	r1 := s1.Body["receiptUrl"].(string)
	require.Equal(t, 200, do(t, rq{method: "GET", host: host, path: r1, token: b1}).Code, "own receipt")
	require.Equal(t, 202, do(t, rq{method: "POST", host: host, path: r1 + "/resend", token: b1, body: map[string]any{}}).Code)
	require.Equal(t, 404, do(t, rq{method: "GET", host: host, path: r1, token: b2}).Code, "another biller's receipt")
	require.Equal(t, 404, do(t, rq{method: "POST", host: host, path: r1 + "/resend", token: b2, body: map[string]any{}}).Code)

	// Tenant C: A's sale IDs mean nothing, A's token is refused.
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	ch := tenants["c"].Host
	var cHas bool
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM `+schema("c")+`.sales WHERE id = $1)`,
		int64(s1.Body["id"].(float64))).Scan(&cHas))
	if !cHas {
		require.Equal(t, 404, do(t, rq{method: "GET", host: ch, path: r1, token: cTok}).Code)
		require.Equal(t, 404, do(t, rq{method: "POST", host: ch, path: r1 + "/resend", token: cTok, body: map[string]any{"email": "x@example.test"}}).Code)
	}
	require.Equal(t, 401, do(t, rq{method: "GET", host: ch, path: r1, token: tok}).Code)
	var cOutbox int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("c")+`.email_outbox WHERE kind LIKE 'receipt%'`).Scan(&cOutbox))
	require.Zero(t, cOutbox)
}
