package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Fake is an in-memory Provider for tests and local development
// (BILLING_PROVIDER=fake). Its webhooks are real Razorpay-shaped events
// signed with its secret, so the production verification and parsing
// code runs on them.
type Fake struct {
	Secret  []byte
	PayBase string // checkout links: PayBase + subscription ID
	mu      sync.Mutex
	seq     atomic.Int64
	run     string
	subs    map[string]*Subscription
	cancels map[string]bool // subscription ID → at period end
	// FailNext makes the next call return this error (tests).
	FailNext error
}

// NewFake creates a fake provider.
func NewFake(secret []byte, payBase string) *Fake {
	if payBase == "" {
		payBase = "https://billing.example.invalid/pay/"
	}
	var tag [3]byte
	_, _ = rand.Read(tag[:])
	return &Fake{Secret: secret, PayBase: payBase, run: hex.EncodeToString(tag[:]), subs: map[string]*Subscription{}, cancels: map[string]bool{}}
}

// Name implements Provider.
func (f *Fake) Name() string { return "fake" }

// id is unique across restarts (the run tag): stored provider IDs are
// unique, and a dev stack keeps them in its database.
func (f *Fake) id(prefix string) string {
	return fmt.Sprintf("%s_fake%s%06d", prefix, f.run, f.seq.Add(1))
}

func (f *Fake) fail() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.FailNext
	f.FailNext = nil
	return err
}

// CreateCustomer implements Provider.
func (f *Fake) CreateCustomer(context.Context, TenantRef) (string, error) {
	if err := f.fail(); err != nil {
		return "", err
	}
	return f.id("cust"), nil
}

// CreateSubscription implements Provider.
func (f *Fake) CreateSubscription(_ context.Context, q CreateSubscriptionReq) (Subscription, error) {
	if err := f.fail(); err != nil {
		return Subscription{}, err
	}
	id := f.id("sub")
	s := &Subscription{ID: id, Status: "created", PlanID: q.PlanID, CustomerID: q.CustomerID, ShortURL: f.PayBase + id}
	f.mu.Lock()
	f.subs[id] = s
	f.mu.Unlock()
	return *s, nil
}

// CancelSubscription implements Provider.
func (f *Fake) CancelSubscription(_ context.Context, subID string, atPeriodEnd bool) error {
	if err := f.fail(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.subs[subID]; !ok {
		return &APIError{Status: 400, Code: "BAD_REQUEST_ERROR", Detail: "no such subscription"}
	}
	f.cancels[subID] = atPeriodEnd
	return nil
}

// Cancelled reports whether a subscription was cancelled and how (tests).
func (f *Fake) Cancelled(subID string) (cancelled, atPeriodEnd bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	end, ok := f.cancels[subID]
	return ok, end
}

// FetchSubscription implements Provider.
func (f *Fake) FetchSubscription(_ context.Context, subID string) (Subscription, error) {
	if err := f.fail(); err != nil {
		return Subscription{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.subs[subID]
	if !ok {
		return Subscription{}, &APIError{Status: 400, Code: "BAD_REQUEST_ERROR", Detail: "no such subscription"}
	}
	return *s, nil
}

// VerifyWebhook implements Provider with Razorpay's verification.
func (f *Fake) VerifyWebhook(raw []byte, h http.Header) (Event, error) {
	return verifyRazorpay(f.Secret, raw, h)
}

// FakeEvent describes a webhook for Webhook to build.
type FakeEvent struct {
	ID             string // X-Razorpay-Event-Id; "" for a new one
	Type           EventType
	CreatedAt      time.Time
	SubscriptionID string
	PlanID         string
	CustomerID     string
	Status         string // provider subscription status
	TenantID       string
	CurrentStart   *time.Time
	CurrentEnd     *time.Time
	PaidCount      int
	InvoiceID      string // charged: the payment's invoice; invoice.*: the invoice
	AmountMinor    int64
	InvoiceStatus  string
}

// Webhook builds a signed Razorpay-shaped webhook: body and headers.
func (f *Fake) Webhook(e FakeEvent) ([]byte, http.Header) {
	if e.ID == "" {
		e.ID = f.id("evt")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	sec := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.Unix()
	}
	notes := any([]any{})
	if e.TenantID != "" {
		notes = map[string]string{"tenant_id": e.TenantID}
	}
	payload := map[string]any{}
	contains := []string{}
	if strings.HasPrefix(string(e.Type), "subscription.") {
		contains = append(contains, "subscription")
		payload["subscription"] = map[string]any{"entity": map[string]any{
			"id": e.SubscriptionID, "entity": "subscription", "plan_id": e.PlanID, "customer_id": e.CustomerID,
			"status": e.Status, "current_start": sec(e.CurrentStart), "current_end": sec(e.CurrentEnd),
			"paid_count": e.PaidCount, "notes": notes}}
	}
	if e.Type == EvCharged && e.InvoiceID != "" {
		contains = append(contains, "payment")
		payload["payment"] = map[string]any{"entity": map[string]any{"id": f.id("pay"), "entity": "payment",
			"amount": e.AmountMinor, "currency": "INR", "status": "captured", "invoice_id": e.InvoiceID, "created_at": e.CreatedAt.Unix()}}
	}
	if strings.HasPrefix(string(e.Type), "invoice.") {
		contains = append(contains, "invoice")
		st := e.InvoiceStatus
		if st == "" {
			st = strings.TrimPrefix(string(e.Type), "invoice.")
		}
		payload["invoice"] = map[string]any{"entity": map[string]any{"id": e.InvoiceID, "entity": "invoice",
			"subscription_id": e.SubscriptionID, "status": st, "amount": e.AmountMinor, "amount_paid": e.AmountMinor,
			"currency": "INR", "issued_at": e.CreatedAt.Unix(), "short_url": f.PayBase + e.InvoiceID}}
	}
	body, _ := json.Marshal(map[string]any{"entity": "event", "account_id": "acc_fake", "event": e.Type,
		"contains": contains, "payload": payload, "created_at": e.CreatedAt.Unix()})
	h := http.Header{}
	h.Set(HeaderSignature, Sign(f.Secret, body))
	h.Set(HeaderEventID, e.ID)
	h.Set("Content-Type", "application/json")
	return body, h
}

// SetStatus changes a fake subscription's provider-side state (tests).
func (f *Fake) SetStatus(subID, status string, start, end *time.Time, paid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.subs[subID]; ok {
		s.Status, s.CurrentStart, s.CurrentEnd, s.PaidCount = status, start, end, paid
	}
}
