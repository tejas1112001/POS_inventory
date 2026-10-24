package billing

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// TenantRef identifies a tenant to the provider.
type TenantRef struct {
	ID, Name, Email string
}

// CreateSubscriptionReq asks the provider for a subscription.
type CreateSubscriptionReq struct {
	TenantID   string
	CustomerID string
	PlanID     string     // provider plan ID
	TotalCount int        // billing cycles the mandate covers
	StartAt    *time.Time // first charge (trial end); nil: now
}

// Subscription is the provider's view of a subscription.
type Subscription struct {
	ID           string
	Status       string
	PlanID       string
	CustomerID   string
	ShortURL     string // hosted checkout / payment link
	CurrentStart *time.Time
	CurrentEnd   *time.Time
	PaidCount    int
}

// Provider is the SaaS billing provider (BILLING_DESIGN §1). Razorpay in
// production; the fake in tests and local development.
type Provider interface {
	Name() string
	CreateCustomer(ctx context.Context, t TenantRef) (string, error)
	CreateSubscription(ctx context.Context, req CreateSubscriptionReq) (Subscription, error)
	CancelSubscription(ctx context.Context, subID string, atPeriodEnd bool) error
	FetchSubscription(ctx context.Context, subID string) (Subscription, error)
	// VerifyWebhook checks the signature over the raw body and parses the
	// event. ErrSignature means the request is not from the provider.
	VerifyWebhook(raw []byte, h http.Header) (Event, error)
}

// Webhook errors.
var (
	ErrSignature = errors.New("billing: invalid webhook signature")
	ErrPayload   = errors.New("billing: unreadable webhook payload")
)

// Known reports whether an event type is handled at all; other types are
// stored and marked ignored.
func (t EventType) Known() bool {
	for _, e := range AllEvents {
		if e == t {
			return true
		}
	}
	return false
}
