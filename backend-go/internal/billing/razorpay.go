package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Razorpay webhook headers.
const (
	HeaderSignature = "X-Razorpay-Signature"
	HeaderEventID   = "X-Razorpay-Event-Id"
	maxEventIDLen   = 100
)

// Sign is the Razorpay webhook signature: hex(HMAC-SHA256(secret, body)).
func Sign(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// verifyRazorpay checks the signature in constant time and parses the
// event (BILLING_DESIGN §4 steps 2–3).
func verifyRazorpay(secret, raw []byte, h http.Header) (Event, error) {
	got, err := hex.DecodeString(strings.TrimSpace(h.Get(HeaderSignature)))
	if err != nil || len(got) != sha256.Size || len(secret) == 0 {
		return Event{}, ErrSignature
	}
	m := hmac.New(sha256.New, secret)
	m.Write(raw)
	if !hmac.Equal(got, m.Sum(nil)) {
		return Event{}, ErrSignature
	}
	ev, err := parseRazorpay(raw)
	if err != nil {
		return Event{}, err
	}
	ev.ID = strings.TrimSpace(h.Get(HeaderEventID))
	if ev.ID == "" {
		// R5: if the event ID header is ever missing, an identical
		// redelivery still dedupes on the body hash.
		sum := sha256.Sum256(raw)
		ev.ID = "body:" + hex.EncodeToString(sum[:16])
	}
	if len(ev.ID) > maxEventIDLen {
		return Event{}, ErrPayload
	}
	return ev, nil
}

type rzSub struct {
	ID           string          `json:"id"`
	PlanID       string          `json:"plan_id"`
	CustomerID   string          `json:"customer_id"`
	Status       string          `json:"status"`
	CurrentStart *int64          `json:"current_start"`
	CurrentEnd   *int64          `json:"current_end"`
	PaidCount    int             `json:"paid_count"`
	ShortURL     string          `json:"short_url"`
	Notes        json.RawMessage `json:"notes"`
}

type rzInvoice struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	Amount         int64  `json:"amount"`
	AmountPaid     int64  `json:"amount_paid"`
	Currency       string `json:"currency"`
	IssuedAt       *int64 `json:"issued_at"`
	PaidAt         *int64 `json:"paid_at"`
	ShortURL       string `json:"short_url"`
}

type rzPayment struct {
	ID        string `json:"id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	InvoiceID string `json:"invoice_id"`
	CreatedAt *int64 `json:"created_at"`
}

type rzEvent struct {
	Event     string `json:"event"`
	CreatedAt int64  `json:"created_at"`
	Payload   struct {
		Subscription *struct {
			Entity rzSub `json:"entity"`
		} `json:"subscription"`
		Payment *struct {
			Entity rzPayment `json:"entity"`
		} `json:"payment"`
		Invoice *struct {
			Entity rzInvoice `json:"entity"`
		} `json:"invoice"`
	} `json:"payload"`
}

func unix(v *int64) *time.Time {
	if v == nil || *v <= 0 {
		return nil
	}
	t := time.Unix(*v, 0).UTC()
	return &t
}

// notesTenant reads notes.tenant_id. Razorpay sends empty notes as [].
func notesTenant(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	s, _ := m["tenant_id"].(string)
	return s
}

var invoiceStatus = map[string]string{
	"draft": "issued", "issued": "issued", "partially_paid": "partially_paid", "paid": "paid",
	"cancelled": "cancelled", "expired": "expired", "deleted": "cancelled",
}

func parseRazorpay(raw []byte) (Event, error) {
	var e rzEvent
	if err := json.Unmarshal(raw, &e); err != nil || e.Event == "" || len(e.Event) > 100 {
		return Event{}, ErrPayload
	}
	ev := Event{Type: EventType(e.Event)}
	if e.CreatedAt > 0 {
		ev.CreatedAt = time.Unix(e.CreatedAt, 0).UTC()
	}
	if p := e.Payload.Subscription; p != nil {
		s := p.Entity
		ev.SubscriptionID, ev.PlanID, ev.CustomerID, ev.ProviderStatus = s.ID, s.PlanID, s.CustomerID, s.Status
		ev.CurrentStart, ev.CurrentEnd, ev.PaidCount = unix(s.CurrentStart), unix(s.CurrentEnd), s.PaidCount
		ev.TenantID = notesTenant(s.Notes)
	}
	if p := e.Payload.Invoice; p != nil && p.Entity.ID != "" {
		i := p.Entity
		st, ok := invoiceStatus[i.Status]
		if !ok {
			st = "issued"
		}
		ev.Invoice = &Invoice{ID: i.ID, SubscriptionID: i.SubscriptionID, AmountMinor: i.Amount, AmountPaidMinor: i.AmountPaid,
			Currency: strings.ToUpper(i.Currency), Status: st, IssuedAt: unix(i.IssuedAt), PaidAt: unix(i.PaidAt), ShortURL: i.ShortURL}
		if ev.SubscriptionID == "" {
			ev.SubscriptionID = i.SubscriptionID
		}
	} else if p := e.Payload.Payment; p != nil && p.Entity.InvoiceID != "" && ev.Type == EvCharged {
		// subscription.charged carries the payment; its invoice is paid.
		pm := p.Entity
		paid := unix(pm.CreatedAt)
		if paid == nil && !ev.CreatedAt.IsZero() {
			t := ev.CreatedAt
			paid = &t
		}
		ev.Invoice = &Invoice{ID: pm.InvoiceID, SubscriptionID: ev.SubscriptionID, AmountMinor: pm.Amount, AmountPaidMinor: pm.Amount,
			Currency: strings.ToUpper(pm.Currency), Status: "paid", IssuedAt: paid, PaidAt: paid}
	}
	if ev.Invoice != nil && ev.Invoice.Currency == "" {
		ev.Invoice.Currency = "INR"
	}
	return ev, nil
}

// ------------------------------------------------------------- REST client

// RazorpayConfig configures the Razorpay client.
type RazorpayConfig struct {
	KeyID, KeySecret, WebhookSecret string
	BaseURL                         string // default https://api.razorpay.com
	HTTP                            *http.Client
}

// Razorpay is a thin typed client for the few Subscriptions API calls we
// need (BILLING_DESIGN §1: no razorpay-go). Secrets never appear in errors
// or logs. Request and response shapes follow Razorpay's documentation and
// are still to be confirmed in test mode (docs/spikes/RAZORPAY_TEST_MODE.md).
type Razorpay struct {
	c RazorpayConfig
}

// NewRazorpay creates the client (10 s timeout by default).
func NewRazorpay(c RazorpayConfig) *Razorpay {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.razorpay.com"
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	return &Razorpay{c: c}
}

// Name implements Provider.
func (r *Razorpay) Name() string { return "razorpay" }

// APIError is a non-2xx answer; Detail is Razorpay's description.
type APIError struct {
	Status int
	Code   string
	Detail string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("razorpay: %d %s: %s", e.Status, e.Code, e.Detail)
}

// call sends one request. Only GETs are retried (idempotent), twice, on
// network errors and 5xx answers.
func (r *Razorpay) call(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	var last error
	for i := range attempts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * 300 * time.Millisecond):
			}
		}
		// The base URL is operator configuration (api.razorpay.com by
		// default) and path IDs are escaped: no request input picks the host.
		req, err := http.NewRequestWithContext(ctx, method, r.c.BaseURL+path, bytes.NewReader(payload)) // #nosec G704 -- configured host
		if err != nil {
			return err
		}
		req.SetBasicAuth(r.c.KeyID, r.c.KeySecret)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := r.c.HTTP.Do(req) // #nosec G704 -- configured host
		if err != nil {
			last = fmt.Errorf("razorpay: %s %s: %w", method, path, err) // the URL holds no credentials
			continue
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			if out == nil {
				return nil
			}
			return json.Unmarshal(data, out)
		}
		var e struct {
			Error struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		last = &APIError{Status: res.StatusCode, Code: e.Error.Code, Detail: e.Error.Description}
		if res.StatusCode < 500 {
			return last
		}
	}
	return last
}

// CreateCustomer implements Provider (fail_existing 0 returns the existing
// customer for the same email).
func (r *Razorpay) CreateCustomer(ctx context.Context, t TenantRef) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := r.call(ctx, http.MethodPost, "/v1/customers", map[string]any{
		"name": t.Name, "email": t.Email, "fail_existing": "0", "notes": map[string]string{"tenant_id": t.ID}}, &out)
	return out.ID, err
}

func (s rzSub) toSubscription() Subscription {
	return Subscription{ID: s.ID, Status: s.Status, PlanID: s.PlanID, CustomerID: s.CustomerID, ShortURL: s.ShortURL,
		CurrentStart: unix(s.CurrentStart), CurrentEnd: unix(s.CurrentEnd), PaidCount: s.PaidCount}
}

// CreateSubscription implements Provider.
func (r *Razorpay) CreateSubscription(ctx context.Context, q CreateSubscriptionReq) (Subscription, error) {
	body := map[string]any{"plan_id": q.PlanID, "customer_id": q.CustomerID, "total_count": q.TotalCount,
		"customer_notify": 1, "notes": map[string]string{"tenant_id": q.TenantID}}
	if q.StartAt != nil {
		body["start_at"] = q.StartAt.Unix()
	}
	var out rzSub
	if err := r.call(ctx, http.MethodPost, "/v1/subscriptions", body, &out); err != nil {
		return Subscription{}, err
	}
	return out.toSubscription(), nil
}

// CancelSubscription implements Provider.
func (r *Razorpay) CancelSubscription(ctx context.Context, subID string, atPeriodEnd bool) error {
	end := 0
	if atPeriodEnd {
		end = 1
	}
	return r.call(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(subID)+"/cancel",
		map[string]any{"cancel_at_cycle_end": end}, nil)
}

// FetchSubscription implements Provider.
func (r *Razorpay) FetchSubscription(ctx context.Context, subID string) (Subscription, error) {
	var out rzSub
	if err := r.call(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(subID), nil, &out); err != nil {
		return Subscription{}, err
	}
	return out.toSubscription(), nil
}

// VerifyWebhook implements Provider.
func (r *Razorpay) VerifyWebhook(raw []byte, h http.Header) (Event, error) {
	return verifyRazorpay([]byte(r.c.WebhookSecret), raw, h)
}
