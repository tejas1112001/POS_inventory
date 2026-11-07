// Package docdate reads the date of a business document (sale, return,
// purchase): RFC 3339, or YYYY-MM-DD in the tenant's time zone, where
// today means now. Far-future dates are refused.
package docdate

import (
	"strings"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
)

// MaxFutureBy is how far ahead a document date may be (clock skew).
const MaxFutureBy = 24 * time.Hour

// Parse reads v. Empty means now.
func Parse(v string, loc *time.Location, now time.Time) (time.Time, error) {
	if v == "" {
		return now, nil
	}
	var t time.Time
	if d, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		if d.Format("2006-01-02") == now.In(loc).Format("2006-01-02") {
			return now, nil
		}
		t = d
	} else if d, err := time.Parse(time.RFC3339, v); err == nil {
		t = d
	} else {
		return time.Time{}, apperr.Invalid("date", "Date must be YYYY-MM-DD or an RFC 3339 timestamp.")
	}
	if t.After(now.Add(MaxFutureBy)) {
		return time.Time{}, apperr.Invalid("date", "Date cannot be in the future.")
	}
	return t, nil
}

// Bound parses a startDate/endDate list filter: YYYY-MM-DD is a whole day
// in the tenant's time zone (an end date includes that whole day), or an
// RFC 3339 timestamp. Empty means no bound.
func Bound(v string, loc *time.Location, end bool) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	if d, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		if end {
			d = d.AddDate(0, 0, 1)
		}
		return &d, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		if end {
			t = t.Add(time.Microsecond)
		}
		return &t, nil
	}
	return nil, apperr.Invalid("date", "Dates must be YYYY-MM-DD or RFC 3339 timestamps.")
}
