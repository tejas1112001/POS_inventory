// Package billing is the SaaS subscription side (BILLING_DESIGN): the
// lifecycle of a tenant's subscription, the provider abstraction, webhook
// processing, scheduled lifecycle moves and notices. Only a verified
// provider event (or a provider pull) changes a subscription's status;
// nothing the browser says does.
package billing

import (
	"strconv"
	"time"
)

// Status is a subscription status (platform.subscriptions.status).
type Status string

// Subscription statuses.
const (
	Trialing  Status = "trialing"
	Active    Status = "active"
	PastDue   Status = "past_due"
	Grace     Status = "grace"
	Suspended Status = "suspended"
	Cancelled Status = "cancelled"
	Expired   Status = "expired"
	ReadOnly  Status = "read_only"
)

// AllStatuses lists every status (tests walk the full matrix).
var AllStatuses = []Status{Trialing, Active, PastDue, Grace, Suspended, Cancelled, Expired, ReadOnly}

// TenantStatus is the tenant status a subscription status gives
// (BILLING_DESIGN §3): an expired subscription leaves the tenant read-only.
func TenantStatus(s Status) string {
	if s == Expired {
		return string(ReadOnly)
	}
	return string(s)
}

// EventType is a provider event type (Razorpay's names).
type EventType string

// Provider events (BILLING_DESIGN §3).
const (
	EvAuthenticated  EventType = "subscription.authenticated"
	EvActivated      EventType = "subscription.activated"
	EvCharged        EventType = "subscription.charged"
	EvPending        EventType = "subscription.pending"
	EvHalted         EventType = "subscription.halted"
	EvCancelled      EventType = "subscription.cancelled"
	EvCompleted      EventType = "subscription.completed"
	EvPaused         EventType = "subscription.paused"
	EvResumed        EventType = "subscription.resumed"
	EvUpdated        EventType = "subscription.updated"
	EvInvoicePaid    EventType = "invoice.paid"
	EvInvoiceExpired EventType = "invoice.expired"
	EvPaymentFailed  EventType = "payment.failed"
)

// AllEvents lists every handled event type.
var AllEvents = []EventType{EvAuthenticated, EvActivated, EvCharged, EvPending, EvHalted, EvCancelled, EvCompleted,
	EvPaused, EvResumed, EvUpdated, EvInvoicePaid, EvInvoiceExpired, EvPaymentFailed}

// IsSubscriptionEvent reports whether an event carries subscription state
// (and so takes part in the out-of-order guard).
func (t EventType) IsSubscriptionEvent() bool {
	switch t {
	case EvInvoicePaid, EvInvoiceExpired, EvPaymentFailed:
		return false
	}
	return true
}

// Invoice is a provider invoice carried by an event.
type Invoice struct {
	ID              string
	SubscriptionID  string
	AmountMinor     int64
	AmountPaidMinor int64
	Currency        string
	Status          string // issued, partially_paid, paid, cancelled, expired
	IssuedAt        *time.Time
	PaidAt          *time.Time
	ShortURL        string
}

// Event is a verified, parsed provider event.
type Event struct {
	ID             string
	Type           EventType
	CreatedAt      time.Time // inside the signed body
	SubscriptionID string
	CustomerID     string
	PlanID         string // provider plan ID
	ProviderStatus string
	CurrentStart   *time.Time
	CurrentEnd     *time.Time
	PaidCount      int
	TenantID       string // notes.tenant_id
	Invoice        *Invoice
}

// Sub is the stored subscription state that Apply and Tick read and write.
type Sub struct {
	Status             Status
	ProviderSubID      string
	ProviderCustomerID string
	ProviderPlanID     string
	ProviderStatus     string
	TrialEnd           *time.Time
	PeriodStart        *time.Time
	PeriodEnd          *time.Time
	GraceEnd           *time.Time
	CancelAtPeriodEnd  bool
	CancelledAt        *time.Time
	LastEventAt        *time.Time
}

// Notice kinds (BILLING_DESIGN §6).
const (
	NoticeRenewal       = "renewal_success"
	NoticePaymentFailed = "payment_failed"
	NoticeSuspended     = "suspended"
	NoticeReactivated   = "reactivated"
	NoticeGraceEnding   = "grace_ending"
	NoticeTrialEnding   = "trial_ending" // + _7, _3, _1
	NoticeSubEnding     = "subscription_ending"
)

// Notice is a notice to send once per (kind, period key).
type Notice struct {
	Kind      string
	PeriodKey string
}

// Outcomes of Apply (billing_events.outcome).
const (
	OutcomeApplied      = "applied"
	OutcomeStaleIgnored = "stale_ignored"
	OutcomeIgnored      = "ignored"
)

// Config holds the lifecycle settings (Q-14).
type Config struct {
	GraceDays int
	// TrialChargeWait is how long a trial with a provider subscription
	// may run past trial_end before Tick makes it read-only (the first
	// charge is usually seconds away).
	TrialChargeWait time.Duration
}

// DefaultConfig is grace 7 days and a 24-hour wait for the first charge.
var DefaultConfig = Config{GraceDays: 7, TrialChargeWait: 24 * time.Hour}

// Result is what Apply decided.
type Result struct {
	Sub     Sub
	Outcome string
	Notices []Notice
	Invoice *Invoice // to upsert
}

// target is the transition matrix: the status an event leads to from each
// status. A missing entry keeps the status. halted is resolved by grace.
var target = map[EventType]map[Status]Status{
	EvActivated: all(Active),
	EvCharged:   all(Active),
	EvPending:   {Trialing: PastDue, Active: PastDue, PastDue: PastDue},
	EvHalted:    {Trialing: Grace, Active: Grace, PastDue: Grace, Grace: Grace}, // Grace, or Suspended once grace_end has passed
	EvCancelled: {Trialing: Cancelled, Active: Cancelled, PastDue: Cancelled, Grace: Cancelled},
	EvCompleted: {Trialing: Expired, Active: Expired, PastDue: Expired, Grace: Expired, Cancelled: Expired, ReadOnly: Expired},
	EvPaused:    {Trialing: ReadOnly, Active: ReadOnly, PastDue: ReadOnly, Grace: ReadOnly, Cancelled: ReadOnly},
	EvResumed:   {ReadOnly: Active},
}

func all(s Status) map[Status]Status {
	m := make(map[Status]Status, len(AllStatuses))
	for _, from := range AllStatuses {
		m[from] = s
	}
	return m
}

// Next is the status an event leads to (the matrix, before the grace rule).
func Next(from Status, ev EventType) Status {
	if to, ok := target[ev][from]; ok {
		return to
	}
	return from
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Apply applies one verified event to a subscription (BILLING_DESIGN §3).
// Subscription events older than the last applied one are ignored
// (out-of-order guard); invoice events only carry an invoice to upsert.
func Apply(cur Sub, ev Event, now time.Time, cfg Config) Result {
	if !ev.Type.IsSubscriptionEvent() {
		if ev.Invoice != nil && (ev.Type == EvInvoicePaid || ev.Type == EvInvoiceExpired) {
			return Result{Sub: cur, Outcome: OutcomeApplied, Invoice: ev.Invoice}
		}
		return Result{Sub: cur, Outcome: OutcomeIgnored}
	}
	if cur.LastEventAt != nil && ev.CreatedAt.Before(*cur.LastEventAt) {
		return Result{Sub: cur, Outcome: OutcomeStaleIgnored}
	}
	s := cur
	at := ev.CreatedAt
	s.LastEventAt = &at
	if ev.SubscriptionID != "" {
		s.ProviderSubID = ev.SubscriptionID
	}
	if ev.CustomerID != "" {
		s.ProviderCustomerID = ev.CustomerID
	}
	if ev.PlanID != "" {
		s.ProviderPlanID = ev.PlanID
	}
	if ev.ProviderStatus != "" {
		s.ProviderStatus = ev.ProviderStatus
	}
	if ev.CurrentStart != nil {
		s.PeriodStart = ev.CurrentStart
	}
	if ev.CurrentEnd != nil {
		s.PeriodEnd = ev.CurrentEnd
	}
	res := Result{Outcome: OutcomeApplied, Invoice: ev.Invoice}
	to := Next(cur.Status, ev.Type)

	switch ev.Type {
	case EvActivated, EvCharged:
		s.GraceEnd = nil
		s.CancelAtPeriodEnd = false
		switch cur.Status {
		case Suspended, ReadOnly, Expired, Cancelled:
			res.Notices = append(res.Notices, Notice{NoticeReactivated, stamp(at)})
		}
		if ev.Type == EvCharged && ev.PaidCount > 1 {
			key := ev.ID
			if ev.Invoice != nil && ev.Invoice.ID != "" {
				key = ev.Invoice.ID
			}
			res.Notices = append(res.Notices, Notice{NoticeRenewal, key})
		}
	case EvPending:
		if to == PastDue && s.GraceEnd == nil {
			g := now.Add(time.Duration(cfg.GraceDays) * 24 * time.Hour)
			s.GraceEnd = &g
			res.Notices = append(res.Notices, Notice{NoticePaymentFailed, day(now)})
		}
	case EvHalted:
		if to == Grace {
			if s.GraceEnd == nil {
				g := now.Add(time.Duration(cfg.GraceDays) * 24 * time.Hour)
				s.GraceEnd = &g
				res.Notices = append(res.Notices, Notice{NoticePaymentFailed, day(now)})
			}
			if !now.Before(*s.GraceEnd) {
				to = Suspended
				res.Notices = append(res.Notices, Notice{NoticeSuspended, stamp(now)})
			}
		}
	case EvCancelled:
		s.CancelledAt = &at
		s.CancelAtPeriodEnd = false
	case EvResumed:
		if cur.Status == ReadOnly {
			res.Notices = append(res.Notices, Notice{NoticeReactivated, stamp(at)})
		}
	}
	s.Status = to
	res.Sub = s
	return res
}

// Tick applies the time-driven moves a scheduled job makes (BILLING_DESIGN
// §3): a trial that ended unpaid becomes read-only, a payment problem whose
// grace has run out is suspended, and a cancelled subscription becomes
// read-only when its paid period ends.
func Tick(cur Sub, now time.Time, cfg Config) (Sub, []Notice, bool) {
	s := cur
	switch cur.Status {
	case Trialing:
		if cur.TrialEnd == nil {
			return cur, nil, false
		}
		end := *cur.TrialEnd
		if cur.ProviderSubID != "" {
			end = end.Add(cfg.TrialChargeWait)
		}
		if now.Before(end) {
			return cur, nil, false
		}
		s.Status = ReadOnly
		return s, nil, true
	case PastDue, Grace:
		if cur.GraceEnd == nil || now.Before(*cur.GraceEnd) {
			return cur, nil, false
		}
		s.Status = Suspended
		return s, []Notice{{NoticeSuspended, stamp(now)}}, true
	case Cancelled:
		if cur.PeriodEnd != nil && now.Before(*cur.PeriodEnd) {
			return cur, nil, false
		}
		s.Status = ReadOnly
		return s, nil, true
	}
	return cur, nil, false
}

// Reminders are the dated notices due now (BILLING_DESIGN §6): trial and
// paid-period endings 7, 3 and 1 days ahead, and grace ending 1 day ahead.
// The period key makes each one unique per date.
func Reminders(s Sub, now time.Time) []Notice {
	var out []Notice
	ahead := func(end *time.Time, kind string) {
		if end == nil || !end.After(now) {
			return
		}
		left := end.Sub(now)
		for _, d := range []int{1, 3, 7} {
			if left <= time.Duration(d)*24*time.Hour {
				out = append(out, Notice{kind + "_" + strconv.Itoa(d), day(*end)})
				return
			}
		}
	}
	switch s.Status {
	case Trialing:
		ahead(s.TrialEnd, NoticeTrialEnding)
	case Cancelled:
		ahead(s.PeriodEnd, NoticeSubEnding)
	case PastDue, Grace:
		if s.GraceEnd != nil && s.GraceEnd.After(now) && s.GraceEnd.Sub(now) <= 24*time.Hour {
			out = append(out, Notice{NoticeGraceEnding, day(*s.GraceEnd)})
		}
	}
	if s.CancelAtPeriodEnd && s.Status == Active {
		ahead(s.PeriodEnd, NoticeSubEnding)
	}
	return out
}
