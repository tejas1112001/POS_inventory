package pricing

import (
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
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
	q, err := Compute([]Line{{UnitPrice: d("100"), Qty: qty.Int(2), TaxRate: d("18")}}, decimal.Zero, decimal.Zero, inr)
	require.NoError(t, err)
	eq(t, "200", q.Subtotal, "subtotal")
	eq(t, "36", q.Tax, "tax")
	eq(t, "236", q.Total, "total")
	eq(t, "18", *q.Lines[0].CGST, "cgst")
	eq(t, "18", *q.Lines[0].SGST, "sgst")
	require.Nil(t, q.Lines[0].IGST)
}

func TestInclusiveTaxIsNotAddedAgain(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("118"), Qty: qty.Int(1), TaxRate: d("18"), Inclusive: true}}, decimal.Zero, decimal.Zero, inr)
	require.NoError(t, err)
	eq(t, "18", q.Tax, "tax inside the price")
	eq(t, "118", q.Total, "total")
	eq(t, "118", q.Lines[0].Total, "line total")
}

func TestDiscountIsAllocatedBeforeTax(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("100"), Qty: qty.Int(1), TaxRate: d("10")}}, d("10"), decimal.Zero, inr)
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
		{UnitPrice: d("333.3333"), Qty: qty.Int(3)},
		{UnitPrice: d("1000"), Qty: qty.Int(1)},
		{UnitPrice: d("1000"), Qty: qty.Int(1)},
	}, d("100"), decimal.Zero, jpy)
	require.NoError(t, err)
	eq(t, "1000", q.Lines[0].Gross, "gross rounded to whole yen")
	eq(t, "34", q.Lines[0].Discount, "extra yen to the first tied line")
	eq(t, "33", q.Lines[1].Discount, "")
	eq(t, "2900", q.Total, "")
	require.Nil(t, q.Lines[0].CGST)

	kwd := Settings{Currency: "KWD", Split: SplitIGST}
	q, err = Compute([]Line{{UnitPrice: d("1.2345"), Qty: qty.Int(1), TaxRate: d("5")}}, decimal.Zero, decimal.Zero, kwd)
	require.NoError(t, err)
	eq(t, "1.235", q.Subtotal, "three decimals, half-up")
	eq(t, "0.062", q.Tax, "0.06175 -> 0.062")
	eq(t, "0.062", *q.Lines[0].IGST, "igst")
}

func TestHalfUpAndShipping(t *testing.T) {
	q, err := Compute([]Line{{UnitPrice: d("0.10"), Qty: qty.Int(1), TaxRate: d("5")}}, decimal.Zero, d("40"), inr)
	require.NoError(t, err)
	eq(t, "0.01", q.Tax, "0.005 rounds up")
	eq(t, "0.01", *q.Lines[0].CGST, "cgst rounds up")
	eq(t, "0", *q.Lines[0].SGST, "sgst gets the rest")
	eq(t, "40", q.Shipping, "")
	eq(t, "40.11", q.Total, "shipping is added untaxed")
}

func TestErrors(t *testing.T) {
	one := []Line{{UnitPrice: d("10"), Qty: qty.Int(1)}}
	_, err := Compute(one, d("10.01"), decimal.Zero, inr)
	require.ErrorIs(t, err, ErrDiscountTooLarge)
	_, err = Compute(one, d("-1"), decimal.Zero, inr)
	require.ErrorIs(t, err, ErrNegative)
	_, err = Compute([]Line{{UnitPrice: d("10"), Qty: qty.Int(0)}}, decimal.Zero, decimal.Zero, inr)
	require.ErrorIs(t, err, ErrQuantity)
	_, err = Compute([]Line{{UnitPrice: d("-1"), Qty: qty.Int(1)}}, decimal.Zero, decimal.Zero, inr)
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
				Qty:       qty.Int(1 + r.Int64N(5)),
				TaxRate:   []decimal.Decimal{d("0"), d("5"), d("12"), d("18"), d("28")}[r.IntN(5)],
				Inclusive: r.IntN(2) == 0,
			})
		}
		var sub decimal.Decimal
		for _, l := range lines {
			sub = sub.Add(l.UnitPrice.Mul(l.Qty.Decimal()).Round(2))
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

func TestAboveMRP(t *testing.T) {
	one := d("1")
	m := d("118")
	require.False(t, AboveMRP(d("100"), d("18"), false, &m, one), "exclusive 100 + 18% = 118 is at MRP")
	require.True(t, AboveMRP(d("100.01"), d("18"), false, &m, one), "exclusive 100.01 + 18% is above MRP")
	require.False(t, AboveMRP(d("118"), d("18"), true, &m, one), "inclusive 118 is at MRP")
	require.True(t, AboveMRP(d("118.01"), d("18"), true, &m, one), "inclusive 118.01 is above MRP")
	require.False(t, AboveMRP(d("1000"), d("18"), true, nil, one), "no MRP")
	eq(t, "59", TaxInclusive(d("50"), d("18"), false), "tax added")

	// Pharmacy plan §6.2: MRP printed per pack.
	strip := d("35")
	require.False(t, AboveMRP(d("3.5"), d("0"), true, &strip, d("10")), "₹3.50 a tablet: strip ₹35.00 = MRP")
	require.True(t, AboveMRP(d("3.51"), d("0"), true, &strip, d("10")), "₹3.51 a tablet: strip ₹35.10")
	require.True(t, AboveMRP(d("33"), d("12"), false, &strip, d("1")), "₹33 + 12% = ₹36.96 above ₹35")
	fifteen := d("25")
	require.False(t, AboveMRP(d("1.66"), d("0"), true, &fifteen, d("15")), "₹1.66 a tablet of a ₹25/15 strip")
	require.True(t, AboveMRP(d("1.6667"), d("0"), true, &fifteen, d("15")), "one loose tablet would cost ₹1.67, its share is ₹1.66")
	require.True(t, AboveMRP(d("1.67"), d("0"), true, &fifteen, d("15")), "strip of 15 at ₹1.67 = ₹25.05")
}

// TestLineMRP is the examples table of the pharmacy plan §6.2.
func TestLineMRP(t *testing.T) {
	part := func(mrp, pack, q string) MRPPart {
		return MRPPart{MRP: d(mrp), Pack: d(pack), Qty: qty.Must(q)}
	}
	cases := []struct {
		name  string
		parts []MRPPart
		want  string
	}{
		{"1 tablet of a ₹35/10 strip", []MRPPart{part("35", "10", "1")}, "3.5"},
		{"3 tablets", []MRPPart{part("35", "10", "3")}, "10.5"},
		{"1 strip", []MRPPart{part("35", "10", "10")}, "35"},
		{"1 box of 100", []MRPPart{part("35", "10", "100")}, "350"},
		{"1 tablet of a ₹25/15 strip rounds down", []MRPPart{part("25", "15", "1")}, "1.66"},
		{"2 tablets of a ₹25/15 strip", []MRPPart{part("25", "15", "2")}, "3.33"},
		{"a whole ₹25/15 strip is exact", []MRPPart{part("25", "15", "15")}, "25"},
		{"two batches, different MRPs", []MRPPart{part("35", "10", "6"), part("38", "10", "4")}, "36.2"},
		{"two batches, different packs", []MRPPart{part("25", "15", "5"), part("25", "15", "10"), part("35", "10", "10")}, "60"},
		{"thirds that sum to a whole", []MRPPart{part("1", "3", "1"), part("2", "3", "1")}, "1"},
		{"pack 0 means 1", []MRPPart{part("12.5", "0", "2")}, "25"},
		{"half a kilo", []MRPPart{part("80", "1", "0.5")}, "40"},
		{"nothing", nil, "0"},
	}
	for _, c := range cases {
		eq(t, c.want, LineMRP(c.parts), c.name)
	}
}

// M3: CGST + UTGST splits like CGST + SGST (the UTGST half in SGST).
func TestSplitUTGST(t *testing.T) {
	c, s, i := split(decimal.RequireFromString("1.81"), SplitCGSTUTGST, 2)
	require.Equal(t, "0.91", c.String())
	require.Equal(t, "0.9", s.String())
	require.Nil(t, i)
}
