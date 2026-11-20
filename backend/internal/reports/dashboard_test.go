package reports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChartWindow(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	ptr := func(t time.Time) *time.Time { return &t }

	// No filter: the 12 months up to now, in the tenant's zone (19:00 UTC
	// on 31 Oct is already 1 Nov in Kolkata).
	first, last, ok := chartWindow(nil, nil, kolkata, time.Date(2026, 10, 31, 19, 0, 0, 0, time.UTC))
	require.True(t, ok)
	require.Equal(t, d(2025, 12, 1), first)
	require.Equal(t, d(2026, 11, 1), last)

	// A range shorter than a year starts at the start date's month; the end
	// bound is exclusive (the day after the end date).
	from := time.Date(2001, 3, 1, 0, 0, 0, 0, kolkata)
	to := time.Date(2002, 1, 1, 0, 0, 0, 0, kolkata)
	first, last, ok = chartWindow(&from, &to, kolkata, time.Now())
	require.True(t, ok)
	require.Equal(t, d(2001, 3, 1), first)
	require.Equal(t, d(2001, 12, 1), last)

	// A longer range shows its last 12 months.
	first, last, _ = chartWindow(ptr(time.Date(1999, 1, 1, 0, 0, 0, 0, kolkata)), &to, kolkata, time.Now())
	require.Equal(t, d(2001, 1, 1), first)
	require.Equal(t, d(2001, 12, 1), last)

	// A start after the end gives no months.
	_, _, ok = chartWindow(ptr(time.Date(2003, 1, 1, 0, 0, 0, 0, kolkata)), &to, kolkata, time.Now())
	require.False(t, ok)
}
