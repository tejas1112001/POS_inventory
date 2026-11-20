package db

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Known vector for password "pencil" with RFC 7677's example salt and 4096
// iterations. The integration tests also log in to Postgres 18 with a
// verifier produced here, which is the real proof.
func TestSCRAMVerifierKnownVector(t *testing.T) {
	salt, err := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
	require.NoError(t, err)
	got, err := scramVerifierWithSalt("pencil", salt, 4096)
	require.NoError(t, err)
	require.Equal(t,
		"SCRAM-SHA-256$4096:W22ZaJ0SNY7soEsUEjb6gQ==$WG5d8oPm3OtcPnkdi4Uo7BkeZkBFzpcXkuLmtbsT4qY=:wfPLwcE6nTWhTAmQ7tl2KeoiWGPlZqQxSrmfPwDl2dU=",
		got)
}

func TestSCRAMVerifierRandomSalt(t *testing.T) {
	a, err := SCRAMVerifier("same-password")
	require.NoError(t, err)
	b, err := SCRAMVerifier("same-password")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	require.True(t, strings.HasPrefix(a, "SCRAM-SHA-256$4096:"))
	require.NotContains(t, a, "same-password")
}

func TestQuoteRejectsBadIdentifiers(t *testing.T) {
	for _, bad := range []string{"", "T_abc", `t_abc"; DROP`, "1abc", "a-b", "a b", "ä", strings.Repeat("a", 64)} {
		_, err := quote(bad)
		require.ErrorIs(t, err, ErrBadIdentifier, bad)
	}
	q, err := quote("t_ab12cd34ef_rw")
	require.NoError(t, err)
	require.Equal(t, `"t_ab12cd34ef_rw"`, q)
}
