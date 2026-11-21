package sales

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The financial year turns on 1 April in the shop's time zone.
func TestFiscalYear(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	at := func(s string) time.Time {
		d, err := time.ParseInLocation("2006-01-02 15:04", s, ist)
		require.NoError(t, err)
		return d
	}
	require.Equal(t, "2627", fiscalYear(at("2026-04-01 00:00"), ist))
	require.Equal(t, "2526", fiscalYear(at("2026-03-31 23:59"), ist))
	require.Equal(t, "2627", fiscalYear(at("2027-03-31 23:59"), ist))
	require.Equal(t, "9900", fiscalYear(at("2099-12-31 10:00"), ist), "the century wraps")
	// 31 March 20:00 UTC is already 1 April in India.
	require.Equal(t, "2627", fiscalYear(time.Date(2026, 3, 31, 20, 0, 0, 0, time.UTC), ist))
}
