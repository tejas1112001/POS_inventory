package billing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func tp(t time.Time) *time.Time { return &t }

// expected restates BILLING_DESIGN §3 for every (status, event) pair, with
// grace still running. Anything not listed keeps its status.
var expected = map[EventType]map[Status]Status{
	EvAuthenticated:  {},
	EvActivated:      {Trialing: Active, Active: Active, PastDue: Active, Grace: Active, Suspended: Active, Cancelled: Active, Expired: Active, ReadOnly: Active},
	EvCharged:        {Trialing: Active, Active: Active, PastDue: Active, Grace: Active, Suspended: Active, Cancelled: Active, Expired: Active, ReadOnly: Active},
	EvPending:        {Trialing: PastDue, Active: PastDue, PastDue: PastDue},
	EvHalted:         {Trialing: Grace, Active: Grace, PastDue: Grace, Grace: Grace},
	EvCancelled:      {Trialing: Cancelled, Active: Cancelled, PastDue: Cancelled, Grace: Cancelled},
	EvCompleted:      {Trialing: Expired, Active: Expired, PastDue: Expired, Grace: Expired, Cancelled: Expired, ReadOnly: Expired},
	EvPaused:         {Trialing: ReadOnly, Active: ReadOnly, PastDue: ReadOnly, Grace: ReadOnly, Cancelled: ReadOnly},
	EvResumed:        {ReadOnly: Active},
	EvUpdated:        {},
	EvInvoicePaid:    {},
	EvInvoiceExpired: {},
	EvPaymentFailed:  {},
}

func TestEveryStatusEventPair(t *testing.T) {
	require.Len(t, expected, len(AllEvents), "every event type is in the expected table")
	for _, ev := range AllEvents {
		for _, from := range AllStatuses {
			want, ok := expected[ev][from]
			if !ok {
				want = from
			}
			cur := Sub{Status: from, GraceEnd: tp(t0.Add(48 * time.Hour)), LastEventAt: tp(t0.Add(-time.Hour))}
			res := Apply(cur, Event{ID: "evt", Type: ev, CreatedAt: t0, PaidCount: 1}, t0, DefaultConfig)
			require.Equal(t, want, res.Sub.Status, "%s from %s", ev, from)
			if ev.IsSubscriptionEvent() {
				require.Equal(t, OutcomeApplied, res.Outcome, "%s from %s", ev, from)
				require.Equal(t, t0, *res.Sub.LastEventAt)
			} else {
				require.Equal(t, cur.LastEventAt, res.Sub.LastEventAt, "invoice and payment events do not move the ordering guard")
			}
		}
	}
}

func TestPendingStartsGraceOnce(t *testing.T) {
	res := Apply(Sub{Status: Active}, Event{Type: EvPending, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, PastDue, res.Sub.Status)
	require.Equal(t, t0.Add(7*24*time.Hour), *res.Sub.GraceEnd)
	require.Equal(t, []Notice{{NoticePaymentFailed, "2026-10-24"}}, res.Notices)

	later := t0.Add(48 * time.Hour)
	again := Apply(res.Sub, Event{Type: EvPending, CreatedAt: later}, later, DefaultConfig)
	require.Equal(t, *res.Sub.GraceEnd, *again.Sub.GraceEnd, "one grace period per incident")
	require.Empty(t, again.Notices)
}

func TestHaltedSuspendsOnlyAfterGrace(t *testing.T) {
	within := Apply(Sub{Status: PastDue, GraceEnd: tp(t0.Add(time.Hour))}, Event{Type: EvHalted, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, Grace, within.Sub.Status)
	require.Empty(t, within.Notices)

	after := Apply(Sub{Status: Grace, GraceEnd: tp(t0.Add(-time.Minute))}, Event{Type: EvHalted, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, Suspended, after.Sub.Status)
	require.Equal(t, []Notice{{NoticeSuspended, "2026-10-24T12:00:00Z"}}, after.Notices)

	// Halted with no earlier pending starts the grace period now.
	fresh := Apply(Sub{Status: Active}, Event{Type: EvHalted, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, Grace, fresh.Sub.Status)
	require.Equal(t, t0.Add(7*24*time.Hour), *fresh.Sub.GraceEnd)
	require.Equal(t, []Notice{{NoticePaymentFailed, "2026-10-24"}}, fresh.Notices)
}

func TestChargedClearsGraceAndNotices(t *testing.T) {
	end := t0.Add(30 * 24 * time.Hour)
	first := Apply(Sub{Status: Trialing}, Event{ID: "evt_1", Type: EvCharged, CreatedAt: t0, PaidCount: 1, CurrentStart: tp(t0), CurrentEnd: &end,
		SubscriptionID: "sub_1", CustomerID: "cust_1", PlanID: "plan_pro", ProviderStatus: "active",
		Invoice: &Invoice{ID: "inv_1", AmountMinor: 49900, Status: "paid"}}, t0, DefaultConfig)
	require.Equal(t, Active, first.Sub.Status)
	require.Empty(t, first.Notices, "no renewal notice on the first charge")
	require.Equal(t, end, *first.Sub.PeriodEnd)
	require.Equal(t, "sub_1", first.Sub.ProviderSubID)
	require.Equal(t, "cust_1", first.Sub.ProviderCustomerID)
	require.Equal(t, "plan_pro", first.Sub.ProviderPlanID)
	require.Equal(t, "inv_1", first.Invoice.ID)

	renewal := Apply(Sub{Status: Grace, GraceEnd: tp(t0)}, Event{ID: "evt_2", Type: EvCharged, CreatedAt: t0, PaidCount: 2,
		Invoice: &Invoice{ID: "inv_2"}}, t0, DefaultConfig)
	require.Nil(t, renewal.Sub.GraceEnd)
	require.Equal(t, []Notice{{NoticeRenewal, "inv_2"}}, renewal.Notices)

	for _, from := range []Status{Suspended, ReadOnly, Expired, Cancelled} {
		r := Apply(Sub{Status: from}, Event{Type: EvActivated, CreatedAt: t0}, t0, DefaultConfig)
		require.Equal(t, []Notice{{NoticeReactivated, "2026-10-24T12:00:00Z"}}, r.Notices, string(from))
	}
	r := Apply(Sub{Status: ReadOnly}, Event{Type: EvResumed, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, []Notice{{NoticeReactivated, "2026-10-24T12:00:00Z"}}, r.Notices)
}

func TestOutOfOrderEventsAreIgnored(t *testing.T) {
	cur := Sub{Status: Active, LastEventAt: tp(t0)}
	old := Apply(cur, Event{Type: EvPending, CreatedAt: t0.Add(-time.Second)}, t0, DefaultConfig)
	require.Equal(t, OutcomeStaleIgnored, old.Outcome)
	require.Equal(t, cur, old.Sub)
	require.Empty(t, old.Notices)
	same := Apply(cur, Event{Type: EvPending, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, OutcomeApplied, same.Outcome, "an event at the same instant still applies")

	// charged (t1) then a late pending (t0): stays active.
	t1 := t0.Add(time.Minute)
	a := Apply(Sub{Status: PastDue, GraceEnd: tp(t0)}, Event{Type: EvCharged, CreatedAt: t1}, t1, DefaultConfig)
	b := Apply(a.Sub, Event{Type: EvPending, CreatedAt: t0}, t1, DefaultConfig)
	require.Equal(t, Active, b.Sub.Status)
	require.Equal(t, OutcomeStaleIgnored, b.Outcome)
}

func TestInvoiceAndPaymentEvents(t *testing.T) {
	cur := Sub{Status: Active, LastEventAt: tp(t0)}
	inv := &Invoice{ID: "inv_9", Status: "paid"}
	r := Apply(cur, Event{Type: EvInvoicePaid, CreatedAt: t0.Add(-time.Hour), Invoice: inv}, t0, DefaultConfig)
	require.Equal(t, OutcomeApplied, r.Outcome, "invoice events are not subject to the ordering guard")
	require.Equal(t, inv, r.Invoice)
	require.Equal(t, cur, r.Sub)
	require.Equal(t, OutcomeIgnored, Apply(cur, Event{Type: EvPaymentFailed, CreatedAt: t0}, t0, DefaultConfig).Outcome)
	require.Equal(t, OutcomeIgnored, Apply(cur, Event{Type: EvInvoicePaid, CreatedAt: t0}, t0, DefaultConfig).Outcome, "no invoice in it")
}

func TestCancelledRecordsTime(t *testing.T) {
	r := Apply(Sub{Status: Active, CancelAtPeriodEnd: true}, Event{Type: EvCancelled, CreatedAt: t0}, t0, DefaultConfig)
	require.Equal(t, Cancelled, r.Sub.Status)
	require.Equal(t, t0, *r.Sub.CancelledAt)
	require.False(t, r.Sub.CancelAtPeriodEnd)
}

func TestTick(t *testing.T) {
	// A trial that ended unpaid becomes read-only; with a provider
	// subscription it waits for the first charge.
	s, n, ok := Tick(Sub{Status: Trialing, TrialEnd: tp(t0.Add(-time.Minute))}, t0, DefaultConfig)
	require.True(t, ok)
	require.Equal(t, ReadOnly, s.Status)
	require.Empty(t, n)
	_, _, ok = Tick(Sub{Status: Trialing, TrialEnd: tp(t0.Add(-time.Hour)), ProviderSubID: "sub_1"}, t0, DefaultConfig)
	require.False(t, ok)
	s, _, ok = Tick(Sub{Status: Trialing, TrialEnd: tp(t0.Add(-25 * time.Hour)), ProviderSubID: "sub_1"}, t0, DefaultConfig)
	require.True(t, ok)
	require.Equal(t, ReadOnly, s.Status)
	_, _, ok = Tick(Sub{Status: Trialing, TrialEnd: tp(t0.Add(time.Minute))}, t0, DefaultConfig)
	require.False(t, ok)

	// Grace runs out: suspended, with a notice.
	for _, from := range []Status{PastDue, Grace} {
		s, n, ok = Tick(Sub{Status: from, GraceEnd: tp(t0)}, t0, DefaultConfig)
		require.True(t, ok)
		require.Equal(t, Suspended, s.Status)
		require.Equal(t, []Notice{{NoticeSuspended, "2026-10-24T12:00:00Z"}}, n)
		_, _, ok = Tick(Sub{Status: from, GraceEnd: tp(t0.Add(time.Second))}, t0, DefaultConfig)
		require.False(t, ok)
	}

	// Cancelled: read-only once the paid period is over.
	_, _, ok = Tick(Sub{Status: Cancelled, PeriodEnd: tp(t0.Add(time.Hour))}, t0, DefaultConfig)
	require.False(t, ok)
	s, _, ok = Tick(Sub{Status: Cancelled, PeriodEnd: tp(t0)}, t0, DefaultConfig)
	require.True(t, ok)
	require.Equal(t, ReadOnly, s.Status)
	s, _, ok = Tick(Sub{Status: Cancelled}, t0, DefaultConfig)
	require.True(t, ok, "no period: read-only at once")
	require.Equal(t, ReadOnly, s.Status)

	for _, st := range []Status{Active, Suspended, Expired, ReadOnly} {
		_, _, ok = Tick(Sub{Status: st, TrialEnd: tp(t0.Add(-time.Hour)), GraceEnd: tp(t0.Add(-time.Hour)), PeriodEnd: tp(t0.Add(-time.Hour))}, t0, DefaultConfig)
		require.False(t, ok, string(st))
	}
}

func TestReminders(t *testing.T) {
	trial := func(left time.Duration) []Notice {
		return Reminders(Sub{Status: Trialing, TrialEnd: tp(t0.Add(left))}, t0)
	}
	require.Empty(t, trial(8*24*time.Hour))
	require.Equal(t, []Notice{{"trial_ending_7", "2026-10-31"}}, trial(7*24*time.Hour))
	require.Equal(t, []Notice{{"trial_ending_7", "2026-10-29"}}, trial(5*24*time.Hour))
	require.Equal(t, []Notice{{"trial_ending_3", "2026-10-27"}}, trial(3*24*time.Hour))
	require.Equal(t, []Notice{{"trial_ending_1", "2026-10-25"}}, trial(20*time.Hour))
	require.Empty(t, trial(-time.Hour), "already over")

	require.Equal(t, []Notice{{"subscription_ending_3", "2026-10-26"}},
		Reminders(Sub{Status: Cancelled, PeriodEnd: tp(t0.Add(50 * time.Hour))}, t0))
	require.Equal(t, []Notice{{"subscription_ending_1", "2026-10-24"}},
		Reminders(Sub{Status: Active, CancelAtPeriodEnd: true, PeriodEnd: tp(t0.Add(10 * time.Hour))}, t0))
	require.Empty(t, Reminders(Sub{Status: Active, PeriodEnd: tp(t0.Add(10 * time.Hour))}, t0), "renewing: no reminder")

	require.Equal(t, []Notice{{NoticeGraceEnding, "2026-10-25"}}, Reminders(Sub{Status: Grace, GraceEnd: tp(t0.Add(12 * time.Hour))}, t0))
	require.Empty(t, Reminders(Sub{Status: PastDue, GraceEnd: tp(t0.Add(48 * time.Hour))}, t0))
}

func TestTenantStatus(t *testing.T) {
	require.Equal(t, "read_only", TenantStatus(Expired))
	for _, s := range []Status{Trialing, Active, PastDue, Grace, Suspended, Cancelled, ReadOnly} {
		require.Equal(t, string(s), TenantStatus(s))
	}
}

func TestBillingBanner(t *testing.T) {
	require.Nil(t, BannerFor(Sub{Status: Active}, t0))
	require.Nil(t, BannerFor(Sub{Status: Trialing, TrialEnd: tp(t0.Add(10 * 24 * time.Hour))}, t0), "trial far off")
	b := BannerFor(Sub{Status: Trialing, TrialEnd: tp(t0.Add(3 * 24 * time.Hour))}, t0)
	require.Equal(t, "info", b.Level)
	require.True(t, b.Dismissible)
	require.Contains(t, b.Message, "27 Oct 2026")
	for _, s := range []Status{PastDue, Grace} {
		b = BannerFor(Sub{Status: s, GraceEnd: tp(t0.Add(48 * time.Hour))}, t0)
		require.Equal(t, "critical", b.Level)
		require.False(t, b.Dismissible)
		require.Contains(t, b.Message, "26 Oct 2026")
	}
	require.Equal(t, "warning", BannerFor(Sub{Status: Cancelled, PeriodEnd: tp(t0)}, t0).Level)
	require.Equal(t, "warning", BannerFor(Sub{Status: Active, CancelAtPeriodEnd: true, PeriodEnd: tp(t0)}, t0).Level)
	for _, s := range []Status{ReadOnly, Expired} {
		require.Equal(t, "critical", BannerFor(Sub{Status: s}, t0).Level)
	}
	require.Nil(t, BannerFor(Sub{Status: Suspended}, t0), "suspended tenants cannot sign in")
}
