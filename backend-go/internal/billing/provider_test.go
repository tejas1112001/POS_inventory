package billing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var secret = []byte("whsec_test_0123456789abcdef")

func TestWebhookSignature(t *testing.T) {
	f := NewFake(secret, "")
	end := t0.Add(30 * 24 * time.Hour)
	body, h := f.Webhook(FakeEvent{ID: "evt_1", Type: EvCharged, CreatedAt: t0, SubscriptionID: "sub_1", PlanID: "plan_pro",
		CustomerID: "cust_1", Status: "active", TenantID: "abcdefghij", CurrentStart: tp(t0), CurrentEnd: &end, PaidCount: 2,
		InvoiceID: "inv_1", AmountMinor: 49900})

	ev, err := f.VerifyWebhook(body, h)
	require.NoError(t, err)
	require.Equal(t, "evt_1", ev.ID)
	require.Equal(t, EvCharged, ev.Type)
	require.Equal(t, t0, ev.CreatedAt)
	require.Equal(t, "sub_1", ev.SubscriptionID)
	require.Equal(t, "plan_pro", ev.PlanID)
	require.Equal(t, "abcdefghij", ev.TenantID)
	require.Equal(t, end, *ev.CurrentEnd)
	require.Equal(t, 2, ev.PaidCount)
	require.Equal(t, &Invoice{ID: "inv_1", SubscriptionID: "sub_1", AmountMinor: 49900, AmountPaidMinor: 49900, Currency: "INR",
		Status: "paid", IssuedAt: tp(t0), PaidAt: tp(t0)}, ev.Invoice)

	// Tampered body, missing, malformed or wrong signatures, wrong secret.
	tampered := []byte(strings.Replace(string(body), `"paid_count":2`, `"paid_count":3`, 1))
	_, err = f.VerifyWebhook(tampered, h)
	require.ErrorIs(t, err, ErrSignature)
	for _, sig := range []string{"", "zz", strings.Repeat("0", 64), Sign([]byte("other-secret-000000"), body)} {
		hh := h.Clone()
		hh.Set(HeaderSignature, sig)
		_, err = f.VerifyWebhook(body, hh)
		require.ErrorIs(t, err, ErrSignature, sig)
	}
	_, err = NewFake(nil, "").VerifyWebhook(body, h)
	require.ErrorIs(t, err, ErrSignature, "no secret, nothing verifies")
	_, err = NewRazorpay(RazorpayConfig{WebhookSecret: string(secret)}).VerifyWebhook(body, h)
	require.NoError(t, err, "the real provider verifies the same way")
}

func TestWebhookPayloadQuirks(t *testing.T) {
	f := NewFake(secret, "")
	// Missing event ID header: dedupe falls back to the body hash.
	body, h := f.Webhook(FakeEvent{Type: EvPending, CreatedAt: t0, SubscriptionID: "sub_2"})
	h.Del(HeaderEventID)
	ev, err := f.VerifyWebhook(body, h)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ev.ID, "body:"))
	again, _ := f.VerifyWebhook(body, h)
	require.Equal(t, ev.ID, again.ID)
	require.Empty(t, ev.TenantID, "empty notes come as []")

	// Too long an event ID, unreadable JSON, unknown event type.
	h.Set(HeaderEventID, strings.Repeat("x", 101))
	_, err = f.VerifyWebhook(body, h)
	require.ErrorIs(t, err, ErrPayload)
	junk := []byte(`{"event":`)
	hh := http.Header{}
	hh.Set(HeaderSignature, Sign(secret, junk))
	_, err = f.VerifyWebhook(junk, hh)
	require.ErrorIs(t, err, ErrPayload)
	other := []byte(`{"event":"order.paid","created_at":1,"payload":{}}`)
	hh.Set(HeaderSignature, Sign(secret, other))
	ev, err = f.VerifyWebhook(other, hh)
	require.NoError(t, err)
	require.False(t, ev.Type.Known())

	// Invoice events map Razorpay's statuses.
	body, h = f.Webhook(FakeEvent{Type: EvInvoiceExpired, CreatedAt: t0, SubscriptionID: "sub_3", InvoiceID: "inv_3", AmountMinor: 100})
	ev, err = f.VerifyWebhook(body, h)
	require.NoError(t, err)
	require.Equal(t, "expired", ev.Invoice.Status)
	require.Equal(t, "sub_3", ev.SubscriptionID)
	require.Equal(t, "https://billing.example.invalid/pay/inv_3", ev.Invoice.ShortURL)
	require.Equal(t, "cancelled", invoiceStatus["deleted"])
}

func TestFakeProvider(t *testing.T) {
	ctx := context.Background()
	f := NewFake(secret, "https://pay.test/")
	cust, err := f.CreateCustomer(ctx, TenantRef{ID: "t1"})
	require.NoError(t, err)
	s, err := f.CreateSubscription(ctx, CreateSubscriptionReq{TenantID: "t1", CustomerID: cust, PlanID: "plan_a", TotalCount: 12})
	require.NoError(t, err)
	require.Equal(t, "https://pay.test/"+s.ID, s.ShortURL)
	got, err := f.FetchSubscription(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, "created", got.Status)
	require.NoError(t, f.CancelSubscription(ctx, s.ID, true))
	c, end := f.Cancelled(s.ID)
	require.True(t, c)
	require.True(t, end)
	var apiErr *APIError
	require.ErrorAs(t, f.CancelSubscription(ctx, "sub_none", false), &apiErr)
	f.FailNext = errors.New("down")
	_, err = f.CreateCustomer(ctx, TenantRef{})
	require.EqualError(t, err, "down")
	_, err = f.CreateCustomer(ctx, TenantRef{})
	require.NoError(t, err, "only the next call fails")
}

func TestRazorpayClient(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "rzp_test_key" || pass != "key-secret-xyz" {
			w.WriteHeader(401)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/customers":
			if body["email"] != "owner@shop.test" || body["fail_existing"] != "0" {
				w.WriteHeader(400)
				return
			}
			_, _ = io.WriteString(w, `{"id":"cust_1","entity":"customer"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions":
			if body["plan_id"] != "plan_pro" || body["total_count"] != float64(120) || body["start_at"] != float64(t0.Unix()) ||
				body["notes"].(map[string]any)["tenant_id"] != "abcdefghij" {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"code":"BAD_REQUEST_ERROR","description":"bad body"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"sub_1","status":"created","plan_id":"plan_pro","customer_id":"cust_1","short_url":"https://rzp.io/i/abc","paid_count":0,"notes":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions/sub_1/cancel":
			if body["cancel_at_cycle_end"] != float64(1) {
				w.WriteHeader(400)
				return
			}
			_, _ = io.WriteString(w, `{"id":"sub_1","status":"active"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/sub_1":
			if gets.Add(1) == 1 {
				w.WriteHeader(503)
				return
			}
			_, _ = io.WriteString(w, `{"id":"sub_1","status":"active","current_start":1761300000,"current_end":1763978400,"paid_count":1}`)
		case r.URL.Path == "/v1/subscriptions/sub_bad":
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"code":"BAD_REQUEST_ERROR","description":"The id provided does not exist"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	rz := NewRazorpay(RazorpayConfig{KeyID: "rzp_test_key", KeySecret: "key-secret-xyz", BaseURL: srv.URL})
	ctx := context.Background()

	cust, err := rz.CreateCustomer(ctx, TenantRef{ID: "abcdefghij", Name: "Shop", Email: "owner@shop.test"})
	require.NoError(t, err)
	require.Equal(t, "cust_1", cust)
	s, err := rz.CreateSubscription(ctx, CreateSubscriptionReq{TenantID: "abcdefghij", CustomerID: cust, PlanID: "plan_pro", TotalCount: 120, StartAt: tp(t0)})
	require.NoError(t, err)
	require.Equal(t, "https://rzp.io/i/abc", s.ShortURL)
	require.NoError(t, rz.CancelSubscription(ctx, "sub_1", true))
	got, err := rz.FetchSubscription(ctx, "sub_1")
	require.NoError(t, err, "GET retried after a 503")
	require.Equal(t, int32(2), gets.Load())
	require.Equal(t, "active", got.Status)
	require.Equal(t, time.Unix(1763978400, 0).UTC(), *got.CurrentEnd)

	_, err = rz.FetchSubscription(ctx, "sub_bad")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "BAD_REQUEST_ERROR", apiErr.Code)
	require.NotContains(t, err.Error(), "key-secret-xyz")
	_, err = rz.CreateSubscription(ctx, CreateSubscriptionReq{PlanID: "plan_other"})
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "bad body", apiErr.Detail)

	wrong := NewRazorpay(RazorpayConfig{KeyID: "rzp_test_key", KeySecret: "nope", BaseURL: srv.URL})
	_, err = wrong.CreateCustomer(ctx, TenantRef{Email: "owner@shop.test"})
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 401, apiErr.Status)
	require.NotContains(t, err.Error(), "nope")
	srv.Close()
	_, err = rz.FetchSubscription(ctx, "sub_1")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "key-secret-xyz")
}
