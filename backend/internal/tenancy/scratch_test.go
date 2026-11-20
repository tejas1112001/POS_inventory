package tenancy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScratchAndOldNamesAreNeverTenantNames(t *testing.T) {
	at := time.Date(2026, 10, 24, 18, 5, 59, 0, time.FixedZone("IST", 19800))
	s, err := ScratchFor("abcdefghij", at)
	require.NoError(t, err)
	require.Equal(t, "r_abcdefghij_202610241235", s.String(), "UTC minute")
	require.Equal(t, "r_abcdefghij_202610241235_rw", s.Role())
	require.Equal(t, "abcdefghij", s.TenantID())
	o, err := OldSchemaFor("abcdefghij", at)
	require.NoError(t, err)
	require.Equal(t, "t_abcdefghij_old_202610241235", o.String())
	require.Equal(t, "abcdefghij", o.TenantID())

	for _, name := range []string{s.String(), s.Role(), o.String()} {
		_, err := ParseSchemaName(name)
		require.ErrorIs(t, err, ErrInvalidName, "%s is not a tenant schema", name)
		_, err = ParseRoleName(name)
		require.ErrorIs(t, err, ErrInvalidName, "%s is not a tenant role", name)
	}
	for _, bad := range []string{"t_abcdefghij", "r_abcdefghij", "r_ABCDEFGHIJ_202610241235", "r_abcdefghij_20261024123", "r_abcdefghij_202610241235;x", ""} {
		_, err := ParseScratchSchema(bad)
		require.ErrorIs(t, err, ErrInvalidName, bad)
	}
	_, err = ParseOldSchema("t_abcdefghij")
	require.ErrorIs(t, err, ErrInvalidName)
	_, err = ScratchFor("Bad!", at)
	require.ErrorIs(t, err, ErrInvalidName)
}
