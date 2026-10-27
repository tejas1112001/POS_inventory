package pricing

import (
	"math/rand/v2"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func eq(t *testing.T, want string, got decimal.Decimal, msg string) {
	t.Helper()
	require.True(t, d(want).Equal(got), "%s: want %s, got %s", msg, want, got)
}

var inr = Settings{Currency: "INR", Split: SplitCGSTSGST}

func TestExclusiveTaxWithCGSTSGST(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("100"), Qty: 2, TaxRate: d("18")}}, decimal.Zero, decimal.Zero, inr)
	require.NoError(t, err)
	eq(t, "200", q.Subtotal, "subtotal")
	eq(t, "36", q.Tax, "tax")
	eq(t, "236", q.Total, "total")
	eq(t, "18", *q.Lines[0].CGST, "cgst")
	eq(t, "18", *q.Lines[0].SGST, "sgst")
	require.Nil(t, q.Lines[0].IGST)
}

func TestInclusiveTaxIsNotAddedAgain(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("118"), Qty: 1, TaxRate: d("18"), Inclusive: true}}, decimal.Zero, decimal.Zero, inr)
	require.NoError(t, err)
	eq(t, "18", q.Tax, "tax inside the price")
	eq(t, "118", q.Total, "total")
	eq(t, "118", q.Lines[0].Total, "line total")
}

func TestDiscountIsAllocatedBeforeTax(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("100"), Qty: 1, TaxRate: d("10")}}, d("10"), decimal.Zero, inr)
	require.NoError(t, err)
	eq(t, "90", q.Lines[0].Net, "net")
	eq(t, "9", q.Tax, "tax on the discounted amount")
	eq(t, "99", q.Total, "total")
	eq(t, "10", q.Discount, "discount")
}

func TestProRataAndLargestRemainder(t *testing.T) {
	got := Allocate(d("10"), []decimal.Decimal{d("100"), d("200"), d("700")}, 2)
	for i, w := range []string{"1", "2", "7"} {
		eq(t, w, got[i], "pro-rata")
	}
	got = Allocate(d("0.10"), []decimal.Decimal{d("1"), d("1"), d("1")}, 2)
	for i, w := range []string{"0.04", "0.03", "0.03"} {
		eq(t, w, got[i], "tie goes to the first line")
	}
	// Exact shares 0.01666 and 0.03333 truncate to 0.01 and 0.03; the spare
	// cent goes to the larger remainder (0.00666, the first line).
	got = Allocate(d("0.05"), []decimal.Decimal{d("1"), d("2")}, 2)
	eq(t, "0.02", got[0], "larger remainder gets the extra unit")
	eq(t, "0.03", got[1], "smaller remainder")
	require.Len(t, Allocate(d("1"), []decimal.Decimal{decimal.Zero}, 2), 1)
}

func TestCurrencyExponents(t *testing.T) {
	jpy := Settings{Currency: "JPY", Split: SplitNone}
	q, err := Compute([]Line{
		{UnitPrice: d("333.3333"), Qty: 3},
		{UnitPrice: d("1000"), Qty: 1},
		{UnitPrice: d("1000"), Qty: 1},
	}, d("100"), decimal.Zero, jpy)
	require.NoError(t, err)
	eq(t, "1000", q.Lines[0].Gross, "gross rounded to whole yen")
	eq(t, "34", q.Lines[0].Discount, "extra yen to the first tied line")
	eq(t, "33", q.Lines[1].Discount, "")
	eq(t, "2900", q.Total, "")
	require.Nil(t, q.Lines[0].CGST)

	kwd := Settings{Currency: "KWD", Split: SplitIGST}
	q, err = Compute([]Line{{UnitPrice: d("1.2345"), Qty: 1, TaxRate: d("5")}}, decimal.Zero, decimal.Zero, kwd)
	require.NoError(t, err)
	eq(t, "1.235", q.Subtotal, "three decimals, half-up")
	eq(t, "0.062", q.Tax, "0.06175 -> 0.062")
	eq(t, "0.062", *q.Lines[0].IGST, "igst")
}

func TestHalfUpAndShipping(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("0.10"), Qty: 1, TaxRate: d("5")}}, decimal.Zero, d("40"), inr)
	require.NoError(t, err)
	eq(t, "0.01", q.Tax, "0.005 rounds up")
	eq(t, "0.01", *q.Lines[0].CGST, "cgst rounds up")
	eq(t, "0", *q.Lines[0].SGST, "sgst gets the rest")
	eq(t, "40", q.Shipping, "")
	eq(t, "40.11", q.Total, "shipping is added untaxed")
}

func TestErrors(t *testing.T) {
	one := []Line{{UnitPrice: d("10"), Qty: 1}}
	_, err := Compute(one, d("10.01"), decimal.Zero, inr)
	require.ErrorIs(t, err, ErrDiscountTooLarge)
	_, err = Compute(one, d("-1"), decimal.Zero, inr)
	require.ErrorIs(t, err, ErrNegative)
	_, err = Compute([]Line{{UnitPrice: d("10"), Qty: 0}}, decimal.Zero, decimal.Zero, inr)
	require.ErrorIs(t, err, ErrQuantity)
	_, err = Compute([]Line{{UnitPrice: d("-1"), Qty: 1}}, decimal.Zero, decimal.Zero, inr)
	require.ErrorIs(t, err, ErrNegative)
	q, err := Compute(one, d("10"), decimal.Zero, inr)
	require.NoError(t, err, "a 100 % discount is allowed by the engine (the cap is a permission rule)")
	eq(t, "0", q.Total, "")
}

// Random carts: shares always sum to the discount, never exceed a line's
// gross, and the document totals are the sums of the lines.
func TestRandomCartsAreConsistent(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 2000; n++ {
		var lines []Line
		for i := 0; i < 1+r.IntN(6); i++ {
			lines = append(lines, Line{
				UnitPrice: decimal.New(r.Int64N(1_000_000), -4),
				Qty:       1 + r.Int64N(5),
				TaxRate:   []decimal.Decimal{d("0"), d("5"), d("12"), d("18"), d("28")}[r.IntN(5)],
				Inclusive: r.IntN(2) == 0,
			})
		}
		var sub decimal.Decimal
		for _, l := range lines {
			sub = sub.Add(l.UnitPrice.Mul(decimal.NewFromInt(l.Qty)).Round(2))
		}
		disc := sub.Mul(decimal.NewFromFloat(r.Float64())).Round(2)
		q, err := Compute(lines, disc, decimal.Zero, inr)
		require.NoError(t, err)
		var dsum, tsum, total decimal.Decimal
		for _, l := range q.Lines {
			require.False(t, l.Discount.GreaterThan(l.Gross))
			require.False(t, l.Net.IsNegative())
			require.True(t, l.CGST.Add(*l.SGST).Equal(l.Tax))
			dsum, tsum, total = dsum.Add(l.Discount), tsum.Add(l.Tax), total.Add(l.Total)
		}
		require.True(t, dsum.Equal(disc), "shares sum to the discount")
		require.True(t, tsum.Equal(q.Tax))
		require.True(t, total.Equal(q.Total))
	}
}
