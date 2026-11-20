package receipts

import (
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Formatter formats money and dates for one tenant. The locale is an
// English number-format tag (en-IN, en-US, ...): it picks grouping,
// separators and the currency symbol, never a language. No message
// catalogs are used.
type Formatter struct {
	p      *message.Printer
	symbol string
	scale  int
	loc    *time.Location
	usDate bool
}

// NewFormatter builds a formatter; unknown values fall back to en-IN, INR
// and UTC.
func NewFormatter(locale, cur string, loc *time.Location) Formatter {
	tag, err := language.Parse(locale)
	if err != nil {
		tag = language.MustParse("en-IN")
	}
	u, err := currency.ParseISO(cur)
	if err != nil {
		u = currency.INR
	}
	p := message.NewPrinter(tag)
	scale, _ := currency.Standard.Rounding(u)
	sym := p.Sprint(currency.NarrowSymbol(u))
	if sym != "" && unicode.IsLetter([]rune(sym)[len([]rune(sym))-1]) {
		sym += " " // "KWD 1,234.500", but "₹1,234.00"
	}
	if loc == nil {
		loc = time.UTC
	}
	region, _ := tag.Region()
	return Formatter{p: p, symbol: sym, scale: scale, loc: loc, usDate: region.String() == "US"}
}

// Number formats a decimal with the currency's minor digits, no symbol.
// It rounds half-up first, like the pricing engine (x/text would round
// half-to-even).
func (f Formatter) Number(d decimal.Decimal) string {
	d = d.Round(int32(f.scale)) // #nosec G115 -- ISO 4217 scales are 0..4
	return f.p.Sprint(number.Decimal(d.InexactFloat64(), number.Scale(f.scale)))
}

// Money formats an amount with the currency symbol.
func (f Formatter) Money(d decimal.Decimal) string {
	if d.IsNegative() {
		return "-" + f.symbol + f.Number(d.Neg())
	}
	return f.symbol + f.Number(d)
}

// Percent formats a tax rate such as 18 or 2.5.
func (f Formatter) Percent(d decimal.Decimal) string {
	return strings.TrimSuffix(strings.TrimRight(d.StringFixed(3), "0"), ".") + "%"
}

// DateTime formats an instant in the tenant's time zone.
func (f Formatter) DateTime(t time.Time) string {
	t = t.In(f.loc)
	if f.usDate {
		return t.Format("Jan 2, 2006, 3:04 PM")
	}
	return t.Format("02 Jan 2006, 15:04")
}

// Date formats a day in the tenant's time zone.
func (f Formatter) Date(t time.Time) string {
	t = t.In(f.loc)
	if f.usDate {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("02 Jan 2006")
}
