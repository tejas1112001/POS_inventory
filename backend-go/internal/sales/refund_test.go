package sales

import (
	"math/rand/v2"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestRefundShareSplitsWithoutLosingCents(t *testing.T) {
	// 3 units for 100.00: one at a time gives 33.33 + 33.34 + 33.33.
	var got []string
	before := int64(0)
	for range 3 {
		got = append(got, refundShare(d("100.00"), 3, before, 1, "INR").StringFixed(2))
		before++
	}
	require.Equal(t, []string{"33.33", "33.34", "33.33"}, got)

	require.Equal(t, "100.00", refundShare(d("100.00"), 3, 0, 3, "INR").StringFixed(2), "full return is exact")
	require.Equal(t, "66.67", refundShare(d("100.00"), 3, 1, 2, "INR").StringFixed(2), "rest after one returned")
	require.True(t, refundShare(d("100.00"), 3, 0, 0, "INR").IsZero())
	require.True(t, refundShare(d("100.00"), 0, 0, 1, "INR").IsZero())
}

func TestRefundShareCurrencyExponents(t *testing.T) {
	require.Equal(t, "333", refundShare(d("1000"), 3, 0, 1, "JPY").String())
	require.Equal(t, "1000", refundShare(d("1000"), 3, 0, 3, "JPY").String())
	require.Equal(t, "0.333", refundShare(d("1.000"), 3, 0, 1, "KWD").String())
	require.Equal(t, "0.334", refundShare(d("1.000"), 3, 1, 1, "KWD").String())
}

func TestRefundSharePartsAlwaysAddUp(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		sold := int64(1 + r.IntN(50))
		amount := decimal.New(int64(r.IntN(10_000_000)), -2)
		sum := decimal.Zero
		for before := int64(0); before < sold; {
			k := min(int64(1+r.IntN(5)), sold-before)
			part := refundShare(amount, sold, before, k, "INR")
			require.False(t, part.IsNegative())
			sum = sum.Add(part)
			before += k
		}
		require.True(t, sum.Equal(amount), "sold %d of %s: parts add to %s", sold, amount, sum)
	}
}

func TestRefundSharePtr(t *testing.T) {
	require.Nil(t, refundSharePtr(nil, 2, 0, 1, "INR"))
	v := d("9.00")
	require.Equal(t, "4.50", refundSharePtr(&v, 2, 0, 1, "INR").StringFixed(2))
}
