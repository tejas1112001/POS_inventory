package gst

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckGSTIN(t *testing.T) {
	// Published examples with a correct check character.
	require.Empty(t, CheckGSTIN("27AAPFU0939F1ZV", ""))
	require.Empty(t, CheckGSTIN("29AAGCB7383J1Z4", "29"))

	require.Contains(t, CheckGSTIN("27AAPFU0939F1ZW", ""), "check character", "one character off")
	require.Contains(t, CheckGSTIN("27AAPFU0939F1Z", ""), "15 characters")
	require.Contains(t, CheckGSTIN("27aapfu0939f1zv", ""), "15 characters", "callers upper-case first (NormGSTIN)")
	require.Contains(t, CheckGSTIN("27AAPFU0939F1AV", ""), "15 characters", "the 14th character is always Z")
	require.Contains(t, CheckGSTIN("27AAPFU0939F1ZV", "29"), "another state", "M3 Q6: refused for Karnataka")
	require.Equal(t, "27AAPFU0939F1ZV", NormGSTIN("  27aapfu0939f1zv "))

	// A well-formed GSTIN with an unknown state code.
	g := "99AAPFU0939F1Z"
	require.Contains(t, CheckGSTIN(g+string(checkChar(g)), ""), "unknown state")
}

func TestStates(t *testing.T) {
	seen := map[string]bool{}
	for i, s := range States {
		require.Len(t, s.Code, 2)
		require.False(t, seen[s.Code], "unique codes")
		seen[s.Code] = true
		if i > 0 {
			require.Less(t, States[i-1].Code, s.Code, "in code order")
		}
	}
	ch, ok := StateOf("04")
	require.True(t, ok)
	require.True(t, ch.UT, "Chandigarh levies UTGST")
	dl, _ := StateOf("07")
	require.False(t, dl.UT, "Delhi has a legislature: SGST")
	_, ok = StateOf("7")
	require.False(t, ok)
	code, ok := NormState(" 27 ")
	require.True(t, ok)
	require.Equal(t, "27", code)
}

func TestParty(t *testing.T) {
	g, s, f := Party(" 27aapfu0939f1zv ", "")
	require.Empty(t, f)
	require.Equal(t, "27AAPFU0939F1ZV", g)
	require.Equal(t, "27", s, "the state comes from the GSTIN")
	_, _, f = Party("27AAPFU0939F1ZV", "29")
	require.Contains(t, f["gstin"], "another state")
	_, s, f = Party("", "29")
	require.Empty(t, f)
	require.Equal(t, "29", s, "a state without a GSTIN (an unregistered buyer)")
	_, _, f = Party("", "7")
	require.Contains(t, f["stateCode"], "two digits")
	g, s, f = Party("", "")
	require.Empty(t, f)
	require.Empty(t, g+s)
}
