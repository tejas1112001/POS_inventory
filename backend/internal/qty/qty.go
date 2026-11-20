// Package qty is the stock quantity type (ARCHITECTURE_REVIEW H3).
// Quantities are numeric(14,3) in Postgres: up to three decimals, so a
// shop can sell 0.25 kg, 1.5 m or 3 tablets of a strip. In Go a Q is a
// fixed-point count of thousandths. It is a struct, not an integer type,
// so an untyped constant can never be mistaken for thousandths: write
// qty.Int(1), never qty.Q(1).
//
// On the wire a Q is a JSON number (2, 1.5, 0.125), so clients that send
// and read whole numbers are unchanged.
package qty

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// Scale is the number of decimals a quantity keeps.
const Scale = 3

const unit = 1000 // thousandths in one

// MaxAbs is the largest magnitude numeric(14,3) holds.
var MaxAbs = Q{m: 99_999_999_999_999}

// Q is a quantity in thousandths.
type Q struct{ m int64 }

// Zero is no quantity.
var Zero = Q{}

// ErrPrecision means more than Scale decimals.
var ErrPrecision = errors.New("qty: at most 3 decimals")

// ErrRange means the value does not fit numeric(14,3).
var ErrRange = errors.New("qty: out of range")

// Int is n whole units.
func Int(n int64) Q { return Q{m: n * unit} }

// Milli is n thousandths.
func Milli(n int64) Q { return Q{m: n} }

// Parse reads "12", "-1.5" or "0.125". More than three decimals is an
// error, never rounded: a quantity is counted, not measured, at this point.
func Parse(s string) (Q, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return Q{}, errors.New("qty: not a number")
	}
	return FromDecimal(d)
}

// FromDecimal converts an exact decimal.
func FromDecimal(d decimal.Decimal) (Q, error) {
	m := d.Shift(Scale)
	if !m.Equal(m.Truncate(0)) {
		return Q{}, ErrPrecision
	}
	if m.Abs().GreaterThan(decimal.NewFromInt(MaxAbs.m)) {
		return Q{}, ErrRange
	}
	return Q{m: m.IntPart()}, nil
}

// Must is Parse for constants in code and tests.
func Must(s string) Q {
	q, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return q
}

// Decimal is the exact value.
func (q Q) Decimal() decimal.Decimal { return decimal.New(q.m, -Scale) }

// Thousandths is the raw count (exports, sorting keys).
func (q Q) Thousandths() int64 { return q.m }

// Add is q + o.
func (q Q) Add(o Q) Q { return Q{m: q.m + o.m} }

// Sub is q - o.
func (q Q) Sub(o Q) Q { return Q{m: q.m - o.m} }

// Neg is -q.
func (q Q) Neg() Q { return Q{m: -q.m} }

// Abs is |q|.
func (q Q) Abs() Q {
	if q.m < 0 {
		return q.Neg()
	}
	return q
}

// MulInt is q × n.
func (q Q) MulInt(n int64) Q { return Q{m: q.m * n} }

// Cmp is -1, 0 or +1 as q is less than, equal to or greater than o.
func (q Q) Cmp(o Q) int {
	switch {
	case q.m < o.m:
		return -1
	case q.m > o.m:
		return 1
	}
	return 0
}

// Less reports q < o.
func (q Q) Less(o Q) bool { return q.m < o.m }

// Greater reports q > o.
func (q Q) Greater(o Q) bool { return q.m > o.m }

// Sign is -1, 0 or +1.
func (q Q) Sign() int { return q.Cmp(Zero) }

// IsZero reports q == 0.
func (q Q) IsZero() bool { return q.m == 0 }

// IsPositive reports q > 0.
func (q Q) IsPositive() bool { return q.m > 0 }

// IsNegative reports q < 0.
func (q Q) IsNegative() bool { return q.m < 0 }

// IsWhole reports that q has no fraction.
func (q Q) IsWhole() bool { return q.m%unit == 0 }

// Whole returns the whole part when q has no fraction.
func (q Q) Whole() (int64, bool) { return q.m / unit, q.IsWhole() }

// Float is for display arithmetic only (charts, PDFs); never store it.
func (q Q) Float() float64 { return float64(q.m) / unit }

// Min is the smaller of a and b.
func Min(a, b Q) Q {
	if a.m < b.m {
		return a
	}
	return b
}

// Max is the larger of a and b.
func Max(a, b Q) Q {
	if a.m > b.m {
		return a
	}
	return b
}

// Sum adds qs.
func Sum(qs ...Q) Q {
	var t Q
	for _, q := range qs {
		t.m += q.m
	}
	return t
}

// String is the shortest exact form: "2", "1.5", "-0.125".
func (q Q) String() string {
	neg := q.m < 0
	m := q.m
	if neg {
		m = -m
	}
	s := strconv.FormatInt(m/unit, 10)
	if f := m % unit; f != 0 {
		s += "." + strings.TrimRight(fmt.Sprintf("%03d", f), "0")
	}
	if neg {
		return "-" + s
	}
	return s
}

// MarshalJSON writes a JSON number.
func (q Q) MarshalJSON() ([]byte, error) { return []byte(q.String()), nil }

// UnmarshalJSON accepts a JSON number or a numeric string ("" is zero).
func (q *Q) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*q = Q{}
		return nil
	}
	s := string(b)
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if strings.TrimSpace(s) == "" {
			*q = Q{}
			return nil
		}
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*q = v
	return nil
}

// Value implements driver.Valuer: the exact decimal text.
func (q Q) Value() (driver.Value, error) { return q.String(), nil }

// Scan implements sql.Scanner for numeric, integer and text columns.
func (q *Q) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*q = Q{}
		return nil
	case int64:
		*q = Int(v)
		return nil
	case int32:
		*q = Int(int64(v))
		return nil
	case string:
		p, err := Parse(v)
		if err != nil {
			return err
		}
		*q = p
		return nil
	case []byte:
		p, err := Parse(string(v))
		if err != nil {
			return err
		}
		*q = p
		return nil
	case float64:
		p, err := FromDecimal(decimal.NewFromFloat(v))
		if err != nil {
			return err
		}
		*q = p
		return nil
	}
	return fmt.Errorf("qty: cannot scan %T", src)
}

// In is a request quantity: a JSON number, a numeric string, "" or null
// (the old forms send numbers as strings). Valid is false for "" / null
// and for a value that is not a quantity (Err says why).
type In struct {
	V     Q
	Valid bool
	Set   bool  // the field was present
	Err   error // not a number, or more than three decimals
}

// UnmarshalJSON implements json.Unmarshaler. A bad value is kept in Err so
// the handler can name the field, instead of failing the whole body.
func (i *In) UnmarshalJSON(b []byte) error {
	*i = In{Set: true}
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) || bytes.Equal(b, []byte(`""`)) {
		return nil
	}
	var q Q
	if err := q.UnmarshalJSON(b); err != nil {
		i.Err = err
		return nil
	}
	i.V, i.Valid = q, true
	return nil
}

// Of is a valid In holding q (tests and internal callers).
func Of(q Q) In { return In{V: q, Valid: true, Set: true} }

// Times is q units of something that holds f base units each (a unit's
// factor, H3). It fails when the result needs more than three decimals or
// is out of range.
func (q Q) Times(f Q) (Q, error) {
	return FromDecimal(q.Decimal().Mul(f.Decimal()))
}
