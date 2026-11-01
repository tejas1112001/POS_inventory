//go:build integration

package isolation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCrashAfterSMTPAcceptResendsWithSameMessageID (EMAIL_AND_RECEIPTS
// §5, Phase 3: two workers, one crash point). A worker that crashes after
// the SMTP server accepted a receipt but before marking the row sent leaves
// it "sending" with a lease. Once the lease expires another worker sends it
// again: an accepted at-least-once duplicate, carrying the same Message-ID
// so receiving systems can drop it.
func TestCrashAfterSMTPAcceptResendsWithSameMessageID(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "MAIL-CRASH", store, 5)
	to := uniqueAddr("crash")
	body := posSale(store, line(p, 1))
	body["receiptEmail"] = to
	s := sell(t, "a", tok, body, nil)
	require.Equal(t, 201, s.Code, s.Raw)

	first := waitMail(t, platSrv, to)
	id := first.Header.Get("Message-Id")
	require.NotEmpty(t, id)
	require.Equal(t, "sent", waitOutbox(t, "a", to, func(s outboxState) bool { return s.Status == "sent" }).Status)

	// The crash: the row looks as if a worker sent it and died before the
	// update (still "sending", its lease already expired).
	ctx := context.Background()
	_, err := admin(t).Exec(ctx, `UPDATE `+schema("a")+`.email_outbox SET status = 'sending', sent_at = NULL,
		leased_by = 'crashed-worker', lease_until = now() - interval '1 second' WHERE to_addr = $1`, to)
	require.NoError(t, err)
	// Wake the dispatcher for this tenant now instead of at its next slot.
	_, err = admin(t).Exec(ctx, `INSERT INTO platform.mail_dispatch (tenant_id) VALUES ($1)
		ON CONFLICT (tenant_id) DO UPDATE SET next_attempt_at = now()`, tenants["a"].ID)
	require.NoError(t, err)

	var copies []string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		copies = copies[:0]
		for _, m := range platSrv.Messages() {
			for _, rcpt := range m.To {
				if strings.EqualFold(rcpt, to) {
					copies = append(copies, parseMail(t, m.Data).Header.Get("Message-Id"))
				}
			}
		}
		if len(copies) >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Len(t, copies, 2, "the recovered row is sent once more, not more")
	require.Equal(t, id, copies[1], "the duplicate keeps the Message-ID")
	st := waitOutbox(t, "a", to, func(s outboxState) bool { return s.Status == "sent" })
	require.Equal(t, "sent", st.Status)
	time.Sleep(2 * time.Second)
	n := 0
	for _, m := range platSrv.Messages() {
		for _, rcpt := range m.To {
			if strings.EqualFold(rcpt, to) {
				n++
			}
		}
	}
	require.Equal(t, 2, n, "no third copy")
}
