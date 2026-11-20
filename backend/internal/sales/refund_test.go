package sales

import (
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
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
		got = append(got, shareInt(d("100.00"), 3, before, 1, "INR").StringFixed(2))
		before++
	}
	require.Equal(t, []string{"33.33", "33.34", "33.33"}, got)

	require.Equal(t, "100.00", shareInt(d("100.00"), 3, 0, 3, "INR").StringFixed(2), "full return is exact")
	require.Equal(t, "66.67", shareInt(d("100.00"), 3, 1, 2, "INR").StringFixed(2), "rest after one returned")
	require.True(t, shareInt(d("100.00"), 3, 0, 0, "INR").IsZero())
	require.True(t, shareInt(d("100.00"), 0, 0, 1, "INR").IsZero())
}

func TestRefundShareCurrencyExponents(t *testing.T) {
	require.Equal(t, "333", shareInt(d("1000"), 3, 0, 1, "JPY").String())
	require.Equal(t, "1000", shareInt(d("1000"), 3, 0, 3, "JPY").String())
	require.Equal(t, "0.333", shareInt(d("1.000"), 3, 0, 1, "KWD").String())
	require.Equal(t, "0.334", shareInt(d("1.000"), 3, 1, 1, "KWD").String())
}

func TestRefundSharePartsAlwaysAddUp(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		sold := int64(1 + r.IntN(50))
		amount := decimal.New(int64(r.IntN(10_000_000)), -2)
		sum := decimal.Zero
		for before := int64(0); before < sold; {
			k := min(int64(1+r.IntN(5)), sold-before)
			part := shareInt(amount, sold, before, k, "INR")
			require.False(t, part.IsNegative())
			sum = sum.Add(part)
			before += k
		}
		require.True(t, sum.Equal(amount), "sold %d of %s: parts add to %s", sold, amount, sum)
	}
}

func TestRefundSharePtr(t *testing.T) {
	require.Nil(t, sharePtrInt(nil, 2, 0, 1, "INR"))
	v := d("9.00")
	require.Equal(t, "4.50", sharePtrInt(&v, 2, 0, 1, "INR").StringFixed(2))
}

// shareInt and sharePtrInt call the refund split with whole quantities.
func shareInt(a decimal.Decimal, sold, before, k int64, cur string) decimal.Decimal {
	return refundShare(a, qty.Int(sold), qty.Int(before), qty.Int(k), cur)
}

func sharePtrInt(a *decimal.Decimal, sold, before, k int64, cur string) *decimal.Decimal {
	return refundSharePtr(a, qty.Int(sold), qty.Int(before), qty.Int(k), cur)
}

// H3: a line sold by weight is refunded by the weight returned.
func TestRefundShareFractional(t *testing.T) {
	sold := qty.Must("1.5")
	require.Equal(t, "50.00", refundShare(d("150.00"), sold, qty.Zero, qty.Must("0.5"), "INR").StringFixed(2))
	require.Equal(t, "100.00", refundShare(d("150.00"), sold, qty.Must("0.5"), qty.Int(1), "INR").StringFixed(2), "the rest is exact")
	require.Equal(t, "33.33", refundShare(d("100.00"), qty.Must("0.003"), qty.Zero, qty.Must("0.001"), "INR").StringFixed(2))
}
