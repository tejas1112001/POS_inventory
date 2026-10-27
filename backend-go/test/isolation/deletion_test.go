//go:build integration

package isolation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
)

func restrictedAt(t *testing.T, l string) *time.Time {
	t.Helper()
	var at *time.Time
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT restricted_at FROM platform.tenants WHERE id = $1`,
		tenants[l].ID).Scan(&at))
	return at
}

func tenantStatus(t *testing.T, l string) string {
	t.Helper()
	var s string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT status FROM platform.tenants WHERE id = $1`,
		tenants[l].ID).Scan(&s))
	return s
}

// Day 0 of the deletion clock follows billing: set when billing makes the
// tenant read-only, cleared when it is live again.
func TestRestrictedAtFollowsBilling(t *testing.T) {
	l := billingTenant(t, "bill-clock-sync")
	linkSub(t, l, "sub_clock_sync")
	now := time.Now().UTC().Truncate(time.Second)
	require.Nil(t, restrictedAt(t, l))

	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_clock_pause", Type: billing.EvPaused, SubscriptionID: "sub_clock_sync",
		CreatedAt: now.Add(-3 * time.Minute)}))
	require.Equal(t, "read_only", tenantStatus(t, l))
	day0 := restrictedAt(t, l)
	require.NotNil(t, day0)
	require.WithinDuration(t, time.Now(), *day0, time.Minute)

	// Staying restricted keeps day 0.
	setTenant(t, l, `restricted_at = now() - interval '3 days'`)
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_clock_done", Type: billing.EvCompleted, SubscriptionID: "sub_clock_sync",
		CreatedAt: now.Add(-2 * time.Minute)}))
	require.Equal(t, "read_only", tenantStatus(t, l))
	require.WithinDuration(t, time.Now().Add(-72*time.Hour), *restrictedAt(t, l), time.Minute)

	// Paying clears it.
	require.Equal(t, 200, send(t, billing.FakeEvent{ID: "evt_clock_paid", Type: billing.EvCharged, SubscriptionID: "sub_clock_sync",
		CreatedAt: now.Add(-time.Minute), PaidCount: 2, InvoiceID: "inv_clock_2", AmountMinor: 100}))
	require.Equal(t, "active", tenantStatus(t, l))
	require.Nil(t, restrictedAt(t, l))
}

func TestDeletionClock(t *testing.T) {
	ctx := context.Background()
	due := billingTenant(t, "bill-clock-due")
	setSub(t, due, `status = 'read_only'`)
	setTenant(t, due, `status = 'read_only', restricted_at = now() - interval '31 days', billing_email = 'accounts@bill-clock-due.test'`)
	dueSusp := billingTenant(t, "bill-clock-susp")
	setSub(t, dueSusp, `status = 'suspended'`)
	setTenant(t, dueSusp, `status = 'suspended', suspension_source = 'billing', suspend_reason = 'Payment overdue',
		suspended_at = now() - interval '20 days', restricted_at = now() - interval '45 days'`)
	early := billingTenant(t, "bill-clock-early")
	setSub(t, early, `status = 'read_only'`)
	setTenant(t, early, `status = 'read_only', restricted_at = now() - interval '29 days'`)
	held := billingTenant(t, "bill-clock-admin")
	setTenant(t, held, `status = 'suspended', suspension_source = 'admin', suspend_reason = 'Review',
		restricted_at = now() - interval '60 days'`)

	svc := theApp.Billing()
	var wg sync.WaitGroup
	moved := make(chan int, 3)
	for range 3 {
		wg.Go(func() {
			n, err := svc.RunDeletionClock(ctx)
			require.NoError(t, err)
			moved <- n
		})
	}
	wg.Wait()
	close(moved)
	total := 0
	for n := range moved {
		total += n
	}
	require.Equal(t, 2, total, "each due tenant moves once, also in parallel")
	n, err := svc.RunDeletionClock(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "a later run is harmless")

	require.Equal(t, "pending_deletion", tenantStatus(t, due))
	require.Equal(t, "pending_deletion", tenantStatus(t, dueSusp))
	require.Equal(t, "read_only", tenantStatus(t, early), "day 29: not yet")
	require.Equal(t, "suspended", tenantStatus(t, held), "an admin suspension does not run the clock")

	// Eligible for deletion 90 days after day 0; nothing is dropped.
	var purge, day0 time.Time
	var schemaExists bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT purge_after, restricted_at,
			EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = schema_name)
		FROM platform.tenants WHERE id = $1`, tenants[due].ID).Scan(&purge, &day0, &schemaExists))
	require.Equal(t, day0.Add(90*24*time.Hour).UTC(), purge.UTC())
	require.True(t, schemaExists, "no automatic drop (O-4)")

	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log
		WHERE target_tenant = $1 AND actor_email = 'system:billing' AND action = 'tenant.pending_deletion'`, tenants[due].ID).Scan(&audits))
	require.Equal(t, 1, audits)
	require.Equal(t, map[string]int{"pending_deletion": 1}, notices(t, due))
	require.Empty(t, notices(t, early))
	require.Empty(t, notices(t, held))

	// Nobody can sign in, and the owner is told until when the data is kept.
	eventually(t, func() bool {
		return do(t, rq{method: "POST", host: tenants[due].Host, path: "/api/auth/login",
			body: map[string]any{"email": tenants[due].OwnerEmail, "password": "whatever-password-1"}}).Code == 403
	}, "pending_deletion blocks sign-in")
	m := waitMail(t, platSrv, "accounts@bill-clock-due.test")
	require.Equal(t, "Billing bill-clock-due is scheduled for deletion", m.Header.Get("Subject"))
	require.Contains(t, m.Text, purge.UTC().Format("02 Jan 2006"))
	require.NotContains(t, m.Text, "http://bill-clock-due.pos.test", "no sign-in link: nobody can sign in")
}
