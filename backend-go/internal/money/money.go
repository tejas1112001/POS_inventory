// Package money holds decimal helpers and lenient JSON input types. Money
// is never a float on the server: NUMERIC in Postgres, decimal.Decimal in
// Go, JSON numbers on the wire (PHASE1_DESIGN §4.3).
package money

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

func init() {
	// The API contract sends money as JSON numbers, not strings.
	decimal.MarshalJSONWithoutQuotes = true
}

// Dec is a decimal that accepts a JSON number, a numeric string, "" or null
// (the old forms send numbers as strings). Valid is false for "" / null.
type Dec struct {
	D     decimal.Decimal
	Valid bool
	Set   bool // the field was present
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Dec) UnmarshalJSON(b []byte) error {
	d.Set = true
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	s := string(b)
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return errors.New("must be a number")
	}
	d.D, d.Valid = v, true
	return nil
}

// Int accepts a JSON integer, an integer string, "" or null.
type Int struct {
	V     int64
	Valid bool
	Set   bool
}

// UnmarshalJSON implements json.Unmarshaler.
func (i *Int) UnmarshalJSON(b []byte) error {
	i.Set = true
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	s := string(b)
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f == float64(int64(f)) && !strings.ContainsAny(s, "eE") {
		i.V, i.Valid = int64(f), true
		return nil
	}
	return errors.New("must be a whole number")
}

// Ptr returns a pointer to the value or nil.
func (i Int) Ptr() *int64 {
	if !i.Valid {
		return nil
	}
	v := i.V
	return &v
}

// Date accepts "YYYY-MM-DD", an RFC 3339 timestamp, "" or null.
type Date struct {
	T     time.Time
	Valid bool
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Date) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("must be a date")
	}
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v := strings.TrimSpace(*s)
	if t, err := time.Parse("2006-01-02", v); err == nil {
		d.T, d.Valid = t, true
		return nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		d.T, d.Valid = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		return nil
	}
	return errors.New("must be a date (YYYY-MM-DD)")
}

// Ptr returns the date or nil.
func (d Date) Ptr() *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.T
	return &t
}

// Exponent returns the minor-unit exponent of an ISO 4217 currency.
func Exponent(currency string) int32 {
	switch currency {
	case "JPY", "KRW", "VND", "CLP", "ISK", "UGX", "XAF", "XOF", "PYG", "RWF":
		return 0
	case "KWD", "BHD", "OMR", "JOD", "TND", "LYD", "IQD":
		return 3
	default:
		return 2
	}
}

// Round rounds half away from zero to the currency's minor unit.
func Round(v decimal.Decimal, currency string) decimal.Decimal {
	return v.Round(Exponent(currency))
}
