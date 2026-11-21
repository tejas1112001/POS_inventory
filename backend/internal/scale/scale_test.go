package scale

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckDigit(t *testing.T) {
	// 4006381333931 is a published EAN-13.
	require.Equal(t, byte('1'), CheckDigit("400638133393"))
}

func code(first12 string) string { return first12 + string(CheckDigit(first12)) }

func TestParse(t *testing.T) {
	rules := []Rule{
		{Prefix: "21", ItemDigits: 5, Value: ValueWeight, Decimals: 3}, // 21 PPPPP WWWWW C, grams
		{Prefix: "22", ItemDigits: 4, Value: ValuePrice, Decimals: 2},  // 22 PPPP VVVVVV C, paise
	}
	r, ok := Parse(code("210004201250"), rules)
	require.True(t, ok)
	require.Equal(t, "00042", r.Item)
	require.Equal(t, "1.25", r.Value.String(), "1250 g = 1.25 kg")
	require.Equal(t, ValueWeight, r.Rule.Value)

	r, ok = Parse(code("220042014950"), rules)
	require.True(t, ok)
	require.Equal(t, "0042", r.Item)
	require.Equal(t, "149.5", r.Value.String(), "14950 paise")

	good := code("210004201250")
	bad := good[:12] + string('0'+(good[12]-'0'+1)%10)
	_, ok = Parse(bad, rules)
	require.False(t, ok, "a wrong check digit")
	_, ok = Parse(code("230004201250"), rules)
	require.False(t, ok, "no rule for 23")
	_, ok = Parse("21000420125", rules)
	require.False(t, ok, "not 13 digits")
	_, ok = Parse(code("210004201250"), nil)
	require.False(t, ok, "no rules: off")
}

func TestCheckRules(t *testing.T) {
	require.Empty(t, CheckRules([]Rule{{Prefix: "21", ItemDigits: 5, Value: ValueWeight, Decimals: 3}}))
	f := CheckRules([]Rule{
		{Prefix: "19", ItemDigits: 5, Value: ValueWeight, Decimals: 3},
		{Prefix: "21", ItemDigits: 7, Value: ValueWeight, Decimals: 3},
		{Prefix: "21", ItemDigits: 5, Value: "count", Decimals: 3},
		{Prefix: "22", ItemDigits: 5, Value: ValuePrice, Decimals: 4},
		{Prefix: "23", ItemDigits: 5, Value: ValuePrice, Decimals: 2},
		{Prefix: "23", ItemDigits: 5, Value: ValuePrice, Decimals: 2},
	})
	require.Contains(t, f["scaleBarcodes[0]"], "20 to 29")
	require.Contains(t, f["scaleBarcodes[1]"], "4 to 6")
	require.Contains(t, f["scaleBarcodes[2]"], "weight or the price")
	require.Contains(t, f["scaleBarcodes[3]"], "0 to 3")
	require.Contains(t, f["scaleBarcodes[5]"], "already rule 5")
}
