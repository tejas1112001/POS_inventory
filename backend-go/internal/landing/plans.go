package landing

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// qPublicPlans lists the plans shown on the pricing section: public and
// active, cheapest first. Nothing about prices lives in code.
const qPublicPlans = `SELECT code, name, price_minor, currency, billing_interval, trial_days, limits, features
	FROM platform.plans WHERE is_public AND is_active ORDER BY price_minor, id LIMIT 12`

// Plan is one public plan as the pricing section shows it.
type Plan struct {
	Code, Name string
	PriceMinor int64
	Currency   string
	Interval   string // monthly | yearly
	TrialDays  int
	Limits     []string
	Features   []string
}

// Price is the price for people, e.g. "₹1,499".
func (p Plan) Price() string { return formatMoney(p.PriceMinor, p.Currency) }

// Per is the billing period, e.g. "month".
func (p Plan) Per() string {
	if p.Interval == "yearly" {
		return "year"
	}
	return "month"
}

// Decimal is the price for JSON-LD, e.g. "1499.00".
func (p Plan) Decimal() string {
	return strconv.FormatInt(p.PriceMinor/100, 10) + "." + pad2(p.PriceMinor%100)
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// limitLabels turns platform.plans.limits into lines. 0 means no limit.
var limitLabels = []struct {
	key, one, many, unlimited string
}{
	{"max_stores", "1 store", "Up to %s stores", "Unlimited stores"},
	{"max_users", "1 staff account", "Up to %s staff accounts", "Unlimited staff accounts"},
	{"max_products", "1 product", "Up to %s products", "Unlimited products"},
	{"max_storage_mb", "", "", ""}, // formatted below
	{"max_emails_per_day", "1 email a day", "Up to %s emails a day", ""},
}

// featureLabels names the plan feature flags shown when on. Unknown flags
// are not shown.
var featureLabels = []struct{ key, label string }{
	{"multi_store", "Multiple stores"},
	{"public_shop", "Online shop per store"},
	{"reports_export", "Report exports (Excel and PDF)"},
	{"tenant_smtp", "Send emails from your own mail server"},
	{"custom_domain", "Custom domain"},
}

func planFromRow(code, name string, price int64, currency, interval string, trial int, limits, features []byte) Plan {
	p := Plan{Code: code, Name: name, PriceMinor: price, Currency: strings.TrimSpace(currency), Interval: interval, TrialDays: trial}
	var l map[string]any
	dec := json.NewDecoder(bytes.NewReader(limits))
	dec.UseNumber()
	_ = dec.Decode(&l) // a CHECK keeps it an object; values that are not numbers are skipped
	for _, ll := range limitLabels {
		v, ok := l[ll.key].(json.Number)
		if !ok {
			continue
		}
		n, err := v.Int64()
		if err != nil || n < 0 {
			continue
		}
		switch {
		case ll.key == "max_storage_mb":
			if n > 0 {
				p.Limits = append(p.Limits, storageLabel(n))
			}
		case n == 0:
			if ll.unlimited != "" {
				p.Limits = append(p.Limits, ll.unlimited)
			}
		case n == 1:
			p.Limits = append(p.Limits, ll.one)
		default:
			p.Limits = append(p.Limits, strings.Replace(ll.many, "%s", groupDigits(n), 1))
		}
	}
	var f map[string]any
	_ = json.Unmarshal(features, &f)
	for _, fl := range featureLabels {
		if on, _ := f[fl.key].(bool); on {
			p.Features = append(p.Features, fl.label)
		}
	}
	return p
}

func storageLabel(mb int64) string {
	if mb >= 1000 && mb%1000 == 0 {
		return groupDigits(mb/1000) + " GB file storage"
	}
	return groupDigits(mb) + " MB file storage"
}

// formatMoney formats minor units: INR with ₹ and Indian digit grouping
// (₹1,49,999), other currencies with their code.
func formatMoney(minor int64, currency string) string {
	whole, frac := minor/100, minor%100
	s := ""
	if currency == "INR" || currency == "" {
		s = "₹" + groupIndian(whole)
	} else {
		s = currency + " " + groupDigits(whole)
	}
	if frac != 0 {
		s += "." + pad2(frac)
	}
	return s
}

// groupDigits groups by thousands: 12,345.
func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// groupIndian groups the last three digits, then by two: 1,49,999.
func groupIndian(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	head, tail := s[:len(s)-3], s[len(s)-3:]
	for i := len(head) - 2; i > 0; i -= 2 {
		head = head[:i] + "," + head[i:]
	}
	return head + "," + tail
}

// planCache keeps the public plans for a minute (a failed read for a few
// seconds), so page views do not hit the database.
type planCache struct {
	mu      sync.Mutex
	plans   []Plan
	until   time.Time
	ttl     time.Duration
	load    func(context.Context) ([]Plan, error)
	now     func() time.Time
	haveAny bool
}

func (c *planCache) get(ctx context.Context) []Plan {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.haveAny && now.Before(c.until) {
		return c.plans
	}
	p, err := c.load(ctx)
	if err != nil {
		// Keep showing the last good list; retry soon.
		c.until = now.Add(5 * time.Second)
		return c.plans
	}
	c.plans, c.until, c.haveAny = p, now.Add(c.ttl), true
	return p
}

func loadPlans(s *tenancy.Sessions) func(context.Context) ([]Plan, error) {
	return func(ctx context.Context) ([]Plan, error) {
		var out []Plan
		err := s.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
			rows, err := tx.Query(ctx, qPublicPlans)
			if err != nil {
				return err
			}
			defer rows.Close()
			out = out[:0]
			for rows.Next() {
				var (
					code, name, currency, interval string
					price                          int64
					trial                          int
					limits, features               []byte
				)
				if err := rows.Scan(&code, &name, &price, &currency, &interval, &trial, &limits, &features); err != nil {
					return err
				}
				out = append(out, planFromRow(code, name, price, currency, interval, trial, limits, features))
			}
			return rows.Err()
		})
		return out, err
	}
}
