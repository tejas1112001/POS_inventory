package money

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestDec(t *testing.T) {
	var v struct {
		A, B, C, D Dec
	}
	require.NoError(t, json.Unmarshal([]byte(`{"A": 12.5, "B": "7.25", "C": "", "D": null}`), &v))
	require.True(t, v.A.Valid && v.A.D.Equal(decimal.RequireFromString("12.5")))
	require.True(t, v.B.Valid && v.B.D.Equal(decimal.RequireFromString("7.25")))
	require.False(t, v.C.Valid)
	require.True(t, v.C.Set)
	require.False(t, v.D.Valid)
	require.Error(t, json.Unmarshal([]byte(`{"A": "abc"}`), &v))
	b, _ := json.Marshal(decimal.RequireFromString("10.50"))
	require.Equal(t, "10.5", string(b), "money is a JSON number")
}

func TestInt(t *testing.T) {
	var v struct{ A, B, C Int }
	require.NoError(t, json.Unmarshal([]byte(`{"A": 3, "B": "42", "C": ""}`), &v))
	require.Equal(t, int64(3), v.A.V)
	require.Equal(t, int64(42), v.B.V)
	require.False(t, v.C.Valid)
	require.Error(t, json.Unmarshal([]byte(`{"A": 1.5}`), &v))
	require.Error(t, json.Unmarshal([]byte(`{"A": "1e3"}`), &v))
}

func TestDateAndRound(t *testing.T) {
	var v struct{ A, B, C Date }
	require.NoError(t, json.Unmarshal([]byte(`{"A": "2026-10-19", "B": "2026-10-19T10:00:00Z", "C": ""}`), &v))
	require.True(t, v.A.Valid && v.B.Valid && !v.C.Valid)
	require.Equal(t, "10.13", Round(decimal.RequireFromString("10.125"), "INR").String())
	require.Equal(t, "10", Round(decimal.RequireFromString("9.5"), "JPY").String())
	require.Equal(t, "1.235", Round(decimal.RequireFromString("1.2345"), "KWD").String())
}
