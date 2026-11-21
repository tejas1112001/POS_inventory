package reports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseGSTPeriod(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	p, err := ParseGSTPeriod("2026-11", "", ist)
	require.NoError(t, err)
	require.Equal(t, "2026-11", p.Label)
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, ist), p.From)
	require.Equal(t, time.Date(2026, 12, 1, 0, 0, 0, 0, ist), p.To)
	p, err = ParseGSTPeriod("2026-10", "2026-12", ist) // a quarter
	require.NoError(t, err)
	require.Equal(t, "2026-10 to 2026-12", p.Label)
	require.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, ist), p.To)
	for _, c := range [][2]string{{"2026-13", ""}, {"2026-11", "2026-10"}, {"2026-01", "2027-01"}, {"nov", ""}} {
		_, err := ParseGSTPeriod(c[0], c[1], ist)
		require.Error(t, err, "%v", c)
	}
}
