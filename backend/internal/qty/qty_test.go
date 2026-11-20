package qty

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAndString(t *testing.T) {
	for in, want := range map[string]string{"2": "2", "1.5": "1.5", "0.125": "0.125", "-3.250": "-3.25", "0": "0", "-0.001": "-0.001", " 7 ": "7"} {
		q, err := Parse(in)
		require.NoError(t, err, in)
		require.Equal(t, want, q.String(), in)
	}
	for _, bad := range []string{"1.2345", "abc", "", "1e20", "100000000000000"} {
		_, err := Parse(bad)
		require.Error(t, err, bad)
	}
	require.Equal(t, "99999999999.999", MaxAbs.String())
}

func TestArithmetic(t *testing.T) {
	a, b := Must("1.5"), Int(2)
	require.Equal(t, "3.5", a.Add(b).String())
	require.Equal(t, "-0.5", a.Sub(b).String())
	require.Equal(t, "4.5", a.MulInt(3).String())
	require.True(t, a.Less(b))
	require.Equal(t, -1, a.Cmp(b))
	require.Equal(t, a, Min(a, b))
	require.Equal(t, b, Max(a, b))
	require.True(t, b.IsWhole())
	require.False(t, a.IsWhole())
	n, ok := b.Whole()
	require.True(t, ok)
	require.EqualValues(t, 2, n)
	require.Equal(t, "3.5", Sum(a, b).String())
	require.Equal(t, 1, a.Sign())
	require.True(t, Zero.IsZero())
	require.Equal(t, "1.5", a.Neg().Abs().String())
	require.Equal(t, "1.5", a.Decimal().String())
}

func TestJSON(t *testing.T) {
	var v struct {
		A Q  `json:"a"`
		B Q  `json:"b"`
		C Q  `json:"c"`
		D *Q `json:"d"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"a": 2, "b": "1.25", "c": "", "d": 0.5}`), &v))
	require.Equal(t, Int(2), v.A)
	require.Equal(t, Must("1.25"), v.B)
	require.True(t, v.C.IsZero())
	require.Equal(t, Must("0.5"), *v.D)
	out, err := json.Marshal(v)
	require.NoError(t, err)
	require.JSONEq(t, `{"a": 2, "b": 1.25, "c": 0, "d": 0.5}`, string(out))
	require.Error(t, json.Unmarshal([]byte(`{"a": 1.0001}`), &v))
	require.Error(t, json.Unmarshal([]byte(`{"a": "x"}`), &v))
}

func TestScan(t *testing.T) {
	var q Q
	require.NoError(t, q.Scan("12.500"))
	require.Equal(t, Must("12.5"), q)
	require.NoError(t, q.Scan([]byte("3")))
	require.Equal(t, Int(3), q)
	require.NoError(t, q.Scan(int64(4)))
	require.Equal(t, Int(4), q)
	require.NoError(t, q.Scan(nil))
	require.True(t, q.IsZero())
	require.Error(t, q.Scan(true))
	v, err := Must("0.25").Value()
	require.NoError(t, err)
	require.Equal(t, "0.25", v)
}

func TestTimes(t *testing.T) {
	got, err := Int(2).Times(Int(12))
	if err != nil || got != Int(24) {
		t.Fatalf("2 boxes of 12: %v %v", got, err)
	}
	got, err = Must("1.5").Times(Must("0.5"))
	if err != nil || got != Must("0.75") {
		t.Fatalf("1.5 x 0.5: %v %v", got, err)
	}
	if _, err := Must("0.001").Times(Must("0.5")); err == nil {
		t.Fatal("0.0005 needs four decimals")
	}
	if _, err := MaxAbs.Times(Int(2)); err == nil {
		t.Fatal("out of range")
	}
}
