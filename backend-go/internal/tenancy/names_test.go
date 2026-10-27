package tenancy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSchemaName(t *testing.T) {
	ok := []string{"t_ab12cd34ef", "t_abcdef", "t_" + "abcdefghij0123456789"}
	for _, s := range ok {
		n, err := ParseSchemaName(s)
		require.NoError(t, err, s)
		require.Equal(t, s, n.String())
		require.Equal(t, `"`+s+`", extensions`, n.SearchPath())
	}
	bad := []string{
		"", "t_", "t_abc", "T_abcdef", "t_ABCDEF", "public", "platform", "pg_catalog",
		"information_schema", "extensions", `t_abc"; DROP SCHEMA x`, "t_abcdef ", " t_abcdef",
		"t_abcdef\n", "t_аbcdef", // Cyrillic а
		"t_" + "abcdefghij0123456789x", "t_abc-def", "t_abc.def", "x_abcdef", "t_abcdef_rw",
	}
	for _, s := range bad {
		_, err := ParseSchemaName(s)
		require.ErrorIs(t, err, ErrInvalidName, "%q", s)
	}
	require.True(t, SchemaName{}.IsZero())
}

func TestParseRoleName(t *testing.T) {
	r, err := ParseRoleName("t_ab12cd34ef_rw")
	require.NoError(t, err)
	require.Equal(t, "t_ab12cd34ef_rw", r.String())
	for _, s := range []string{"", "t_ab12cd34ef", "pos_app", "pos_owner", "t_ab12cd34ef_rw;", "t_AB12cd34ef_rw"} {
		_, err := ParseRoleName(s)
		require.ErrorIs(t, err, ErrInvalidName, s)
	}
}

func TestNewTenantIDAndSchemaFor(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewTenantID()
		require.True(t, ValidTenantID(id), id)
		require.False(t, seen[id])
		seen[id] = true
		s, r, err := SchemaFor(id)
		require.NoError(t, err)
		require.Equal(t, "t_"+id, s.String())
		require.Equal(t, "t_"+id+"_rw", r.String())
	}
	_, _, err := SchemaFor("bad")
	require.ErrorIs(t, err, ErrInvalidName)
}

func FuzzParseSchemaName(f *testing.F) {
	for _, s := range []string{"t_ab12cd34ef", `t_x"; DROP`, "public", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := ParseSchemaName(s)
		if err != nil {
			return
		}
		// Anything accepted must be safe to embed quoted.
		for _, c := range n.String() {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
				t.Fatalf("accepted unsafe name %q", s)
			}
		}
	})
}
