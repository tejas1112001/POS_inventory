package docdate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	now := time.Date(2026, 10, 20, 20, 0, 0, 0, time.UTC) // 01:30 on the 21st in Kolkata

	got, err := Parse("", loc, now)
	require.NoError(t, err)
	require.Equal(t, now, got)

	got, err = Parse("2026-10-21", loc, now)
	require.NoError(t, err)
	require.Equal(t, now, got, "today in the tenant's zone means now")

	got, err = Parse("2026-10-01", loc, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, loc), got)

	got, err = Parse("2026-10-20T10:00:00Z", loc, now)
	require.NoError(t, err)
	require.True(t, got.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)))

	_, err = Parse("2026-12-01", loc, now)
	require.Error(t, err, "future")
	_, err = Parse("01/10/2026", loc, now)
	require.Error(t, err)
}

func TestBound(t *testing.T) {
	loc := time.UTC
	from, err := Bound("2026-10-01", loc, false)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, loc), *from)
	to, err := Bound("2026-10-01", loc, true)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 2, 0, 0, 0, 0, loc), *to, "endDate includes the whole day")
	none, err := Bound(" ", loc, false)
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = Bound("yesterday", loc, false)
	require.Error(t, err)
}
