// Package docdate reads the date of a business document (sale, return,
// purchase): RFC 3339, or YYYY-MM-DD in the tenant's time zone, where
// today means now. Far-future dates are refused.
package docdate

import (
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
