// Package pricing computes sale totals on the server (PHASE1_DESIGN §4.3).
// Clients never supply totals: the cart discount is allocated pro-rata to
// lines before tax (Q-2), tax is per line and rounded half-up to the
// currency's minor unit, and document totals are sums of rounded lines.
package pricing

import (
	"errors"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
)

// GST split modes (settings.tax_split).
const (
	SplitNone     = "none"
	SplitCGSTSGST = "cgst_sgst"
	SplitIGST     = "igst"
)

// Errors.
var (
	ErrNegative         = errors.New("pricing: amounts must not be negative")
	ErrQuantity         = errors.New("pricing: quantity must be positive")
	ErrDiscountTooLarge = errors.New("pricing: discount is larger than the subtotal")
)

// Line is one cart line.
type Line struct {
	UnitPrice decimal.Decimal
	Qty       int64
	TaxRate   decimal.Decimal // percent, e.g. 18 for 18 %
	Inclusive bool            // the unit price already contains the tax
}

// Settings are the tenant settings the engine needs.
type Settings struct {
	Currency string
	Split    string
}

// LineQuote is the priced line. Total is what the line adds to the sale
// (net plus exclusive tax; inclusive tax is already inside net).
type LineQuote struct {
	Gross, Discount, Net, Tax, Total decimal.Decimal
	CGST, SGST, IGST                 *decimal.Decimal
}

// Quote is the priced sale.
type Quote struct {
	Lines                                    []LineQuote
	Subtotal, Discount, Tax, Shipping, Total decimal.Decimal
}

var hundred = decimal.NewFromInt(100)

// Compute prices a cart. Subtotal is the sum of gross line amounts, and
// Total = sum of line totals + shipping. Shipping is not taxed.
func Compute(lines []Line, cartDiscount, shipping decimal.Decimal, s Settings) (Quote, error) {
	exp := money.Exponent(s.Currency)
	if cartDiscount.IsNegative() || shipping.IsNegative() {
		return Quote{}, ErrNegative
	}
	q := Quote{Lines: make([]LineQuote, len(lines))}
	gross := make([]decimal.Decimal, len(lines))
	for i, l := range lines {
		if l.Qty <= 0 {
			return Quote{}, ErrQuantity
		}
		if l.UnitPrice.IsNegative() || l.TaxRate.IsNegative() {
			return Quote{}, ErrNegative
		}
		gross[i] = l.UnitPrice.Mul(decimal.NewFromInt(l.Qty)).Round(exp)
		q.Subtotal = q.Subtotal.Add(gross[i])
	}
	disc := cartDiscount.Round(exp)
	if disc.GreaterThan(q.Subtotal) {
		return Quote{}, ErrDiscountTooLarge
	}
	shares := Allocate(disc, gross, exp)
	for i, l := range lines {
		lq := LineQuote{Gross: gross[i], Discount: shares[i]}
		lq.Net = lq.Gross.Sub(lq.Discount)
		rate := l.TaxRate.Div(hundred)
		if l.Inclusive {
			lq.Tax = lq.Net.Sub(lq.Net.Div(decimal.NewFromInt(1).Add(rate))).Round(exp)
			lq.Total = lq.Net
		} else {
			lq.Tax = lq.Net.Mul(rate).Round(exp)
			lq.Total = lq.Net.Add(lq.Tax)
		}
		lq.CGST, lq.SGST, lq.IGST = split(lq.Tax, s.Split, exp)
		q.Lines[i] = lq
		q.Tax = q.Tax.Add(lq.Tax)
		q.Total = q.Total.Add(lq.Total)
	}
	q.Discount = disc
	q.Shipping = shipping.Round(exp)
	q.Total = q.Total.Add(q.Shipping)
	return q, nil
}

// Allocate splits amount over weights pro-rata, in whole minor units, by
// the largest-remainder method: shares sum exactly to amount, and ties go
// to the earlier line. amount must already be rounded to exp.
func Allocate(amount decimal.Decimal, weights []decimal.Decimal, exp int32) []decimal.Decimal {
	out := make([]decimal.Decimal, len(weights))
	total := decimal.Zero
	for _, w := range weights {
		total = total.Add(w)
	}
	if amount.IsZero() || total.IsZero() {
		return out
	}
	type rem struct {
		i int
		r decimal.Decimal
	}
	rems := make([]rem, len(weights))
	given := decimal.Zero
	for i, w := range weights {
		exact := amount.Mul(w).Div(total)
		out[i] = exact.Truncate(exp)
		rems[i] = rem{i, exact.Sub(out[i])}
		given = given.Add(out[i])
	}
	unit := decimal.New(1, -exp)
	left := amount.Sub(given).Div(unit).IntPart()
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r.GreaterThan(rems[b].r) })
	for k := int64(0); k < left && k < int64(len(rems)); k++ {
		out[rems[k].i] = out[rems[k].i].Add(unit)
	}
	return out
}

// split divides a line's tax for GST receipts: CGST and SGST halves (CGST
// rounded half-up, SGST the rest) or all IGST.
func split(tax decimal.Decimal, mode string, exp int32) (cgst, sgst, igst *decimal.Decimal) {
	switch mode {
	case SplitCGSTSGST:
		c := tax.Div(decimal.NewFromInt(2)).Round(exp)
		s := tax.Sub(c)
		return &c, &s, nil
	case SplitIGST:
		i := tax
		return nil, nil, &i
	default:
		return nil, nil, nil
	}
}
