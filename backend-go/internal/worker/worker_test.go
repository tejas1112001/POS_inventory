package worker

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
)

func TestBackoff(t *testing.T) {
	for attempt, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 5: 16 * time.Minute, 10: backoffCap, 40: backoffCap} {
		d := backoff(attempt)
		require.GreaterOrEqual(t, d, want, "attempt %d", attempt)
		require.LessOrEqual(t, d, want+want/10, "jitter is at most 10%%")
	}
}

func TestHumanize(t *testing.T) {
	require.Equal(t, "7 days", humanize(7*24*time.Hour-time.Second+time.Second))
	require.Equal(t, "59 minutes", humanize(59*time.Minute+30*time.Second))
	require.Equal(t, "1 hour", humanize(90*time.Minute))
	require.Equal(t, "5 hours", humanize(5*time.Hour))
	require.Equal(t, "1 minutes", humanize(10*time.Second))
}

func TestDailyAt(t *testing.T) {
	d := dailyAt{hour: 21}
	require.Equal(t, time.Date(2026, 10, 20, 21, 0, 0, 0, time.UTC), d.Next(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)))
	require.Equal(t, time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC), d.Next(time.Date(2026, 10, 20, 21, 0, 0, 0, time.UTC)), "not the same instant again")
	kol, _ := time.LoadLocation("Asia/Kolkata")
	require.Equal(t, time.Date(2026, 10, 20, 21, 0, 0, 0, time.UTC), d.Next(time.Date(2026, 10, 20, 23, 0, 0, 0, kol)), "UTC schedule")
}

func TestErrOutcome(t *testing.T) {
	r := row{attempts: 1, maxAttempts: 8}
	o := errOutcome(r, &mailer.Error{Class: mailer.ErrPermanent, Code: "smtp_550", Detail: "no such user"})
	require.Equal(t, "dead", o.status)
	require.Equal(t, "smtp_550: no such user", o.detail)
	o = errOutcome(r, &mailer.Error{Class: mailer.ErrTransient, Code: "timeout", Detail: "timed out"})
	require.Equal(t, "retry", o.status)
	require.True(t, o.retryAt.After(time.Now()))
	r.attempts = 8
	require.Equal(t, "dead", errOutcome(r, &mailer.Error{Class: mailer.ErrTransient}).status, "max attempts reached")
	require.Equal(t, "dead", errOutcome(row{attempts: 1, maxAttempts: 8}, mailer.ErrInvalid).status)
}

func TestMessageIDCarriesTheTenant(t *testing.T) {
	m := NewMailer(MailConfig{MailDomain: "mail.pos.test"})
	id := m.messageID("abcd234567", "sale:12:receipt")
	require.Equal(t, "abcd234567.sale:12:receipt@mail.pos.test", id)
	msg := mailer.Message{From: mailer.Address{Email: "a@b.test"}, To: []mailer.Address{{Email: "c@d.test"}}, Subject: "s", TextBody: "t", MessageID: id}
	require.NoError(t, msg.Validate())
	require.Equal(t, "platform.x-y@mail.pos.test", m.messageID("platform", "x y"), "unsafe characters replaced")
}

func TestRiverLogsOnlyWarnings(t *testing.T) {
	var b bytes.Buffer
	l := slog.New(minLevel{h: slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug}), min: slog.LevelWarn}).With("component", "river")
	l.Info("producer job counts")
	l.Warn("leader lost")
	require.NotContains(t, b.String(), "producer job counts")
	require.Contains(t, b.String(), "leader lost")
	require.Contains(t, b.String(), "component=river")
}
