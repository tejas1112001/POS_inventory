package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Reconcile (S10 item 3, docs/S10_DESIGN.md §3): every hour each live
// subscription is fetched from the provider and compared with ours:
// provider status, plan and period end. Differences are recorded in
// platform.billing_drift — report only, nothing is fixed here (an operator
// uses the console's actions). The platform inbox gets one email per
// difference seen in two runs in a row, and one a day while live
// subscriptions exist but no verified webhook arrived for WebhookQuietAfter.

// Drift fields.
const (
	DriftStatus    = "status"
	DriftPlan      = "plan"
	DriftPeriodEnd = "period_end"
	DriftFetch     = "fetch"
)

// ReconcileResult sums up one run.
type ReconcileResult struct {
	Checked, Drift, Resolved, FetchErrors, Alerts int
	WebhooksQuiet                                 bool
}

// Subscriptions to check: every one with a provider subscription, except
// those over on both sides.
const qReconcileSubs = `SELECT s.tenant_id, s.provider_subscription_id, coalesce(s.provider_status, ''),
		coalesce(p.razorpay_plan_id, ''), s.current_period_end
	FROM platform.subscriptions s JOIN platform.plans p ON p.id = s.plan_id
	JOIN platform.tenants t ON t.id = s.tenant_id
	WHERE s.provider_subscription_id IS NOT NULL AND t.status <> 'deleted'
	  AND NOT (coalesce(s.provider_status, '') IN ('cancelled','completed','expired') AND s.status IN ('cancelled','expired','read_only'))
	ORDER BY s.tenant_id LIMIT 20000`

type reconcileSub struct {
	tenantID, subID, status, plan string
	periodEnd                     *time.Time
}

func (s *Service) quietAfter() time.Duration {
	if s.WebhookQuietAfter > 0 {
		return s.WebhookQuietAfter
	}
	return 24 * time.Hour
}

// RunReconcile runs one reconcile. It needs a provider.
func (s *Service) RunReconcile(ctx context.Context) (ReconcileResult, error) {
	var res ReconcileResult
	if s.Provider == nil {
		return res, nil
	}
	var subs []reconcileSub
	err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qReconcileSubs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r reconcileSub
			if err := rows.Scan(&r.tenantID, &r.subID, &r.status, &r.plan, &r.periodEnd); err != nil {
				return err
			}
			subs = append(subs, r)
		}
		return rows.Err()
	})
	if err != nil {
		return res, err
	}
	spacing := s.ReconcileSpacing
	if spacing == 0 && len(subs) > 0 {
		spacing = min(20*time.Minute/time.Duration(len(subs)), time.Second)
	}
	for i, r := range subs {
		if i > 0 && spacing > 0 {
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(spacing):
			}
		}
		diffs, ok := s.compare(ctx, r)
		if !ok {
			res.FetchErrors++
			if s.Metrics != nil {
				s.Metrics.BillingFetchErrors.Inc()
			}
			continue // transient: no conclusion this run
		}
		res.Checked++
		n, resolved, alerts, err := s.recordDrift(ctx, r.tenantID, diffs)
		if err != nil {
			return res, err
		}
		res.Drift += n
		res.Resolved += resolved
		res.Alerts += alerts
	}
	quiet, err := s.checkWebhooks(ctx, len(subs) > 0)
	if err != nil {
		return res, err
	}
	res.WebhooksQuiet = quiet
	if quiet {
		res.Alerts++
	}
	err = s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		var open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.billing_drift WHERE resolved_at IS NULL`).Scan(&open); err != nil {
			return err
		}
		if s.Metrics != nil {
			s.Metrics.BillingDriftOpen.Set(float64(open))
		}
		return nil
	})
	return res, err
}

type diff struct{ field, ours, theirs string }

// compare fetches one subscription and lists every field that differs
// (nil when all agree). ok is false after a transient error.
func (s *Service) compare(ctx context.Context, r reconcileSub) ([]diff, bool) {
	p, err := s.Provider.FetchSubscription(ctx, r.subID)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		return []diff{{DriftFetch, "exists", fmt.Sprintf("provider answered %d %s", apiErr.Status, apiErr.Code)}}, true
	}
	if err != nil {
		if s.Log != nil {
			s.Log.WarnContext(ctx, "reconcile fetch failed", "tenant_id", r.tenantID, "err", err)
		}
		return nil, false
	}
	var out []diff
	if p.Status != r.status {
		out = append(out, diff{DriftStatus, r.status, p.Status})
	}
	if p.PlanID != "" && p.PlanID != r.plan {
		out = append(out, diff{DriftPlan, r.plan, p.PlanID})
	}
	if p.CurrentEnd != nil && (r.periodEnd == nil || absDur(p.CurrentEnd.Sub(*r.periodEnd)) > time.Minute) {
		ours := ""
		if r.periodEnd != nil {
			ours = r.periodEnd.UTC().Format(time.RFC3339)
		}
		out = append(out, diff{DriftPeriodEnd, ours, p.CurrentEnd.UTC().Format(time.RFC3339)})
	}
	return out, true
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// recordDrift opens or refreshes a row per difference, resolves the rows
// that agree again, and queues one alert for a difference seen twice.
func (s *Service) recordDrift(ctx context.Context, tenantID string, diffs []diff) (open, resolved, alerts int, err error) {
	err = s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		seen := []string{}
		for _, d := range diffs {
			seen = append(seen, d.field)
			var id int64
			var persistent bool
			err := tx.QueryRow(ctx, `INSERT INTO platform.billing_drift (tenant_id, field, ours, theirs)
				VALUES ($1, $2, left($3, 200), left($4, 200))
				ON CONFLICT (tenant_id, field) WHERE resolved_at IS NULL
				DO UPDATE SET ours = EXCLUDED.ours, theirs = EXCLUDED.theirs, last_seen = now()
				RETURNING id, last_seen > first_seen AND alerted_at IS NULL`, tenantID, d.field, d.ours, d.theirs).Scan(&id, &persistent)
			if err != nil {
				return err
			}
			open++
			if persistent {
				if err := s.alertDrift(ctx, tx, tenantID, d); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE platform.billing_drift SET alerted_at = now() WHERE id = $1`, id); err != nil {
					return err
				}
				alerts++
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE platform.billing_drift SET resolved_at = now()
			WHERE tenant_id = $1 AND resolved_at IS NULL AND NOT (field = ANY($2::text[]))`, tenantID, seen)
		resolved = int(tag.RowsAffected())
		return err
	})
	return open, resolved, alerts, err
}

func (s *Service) alertDrift(ctx context.Context, tx tenancy.PlatformTx, tenantID string, d diff) error {
	var slug string
	if err := tx.QueryRow(ctx, `SELECT slug::text FROM platform.tenants WHERE id = $1`, tenantID).Scan(&slug); err != nil {
		return err
	}
	return s.opsAlert(ctx, tx, fmt.Sprintf("billing-drift:%s:%s:%d", tenantID, d.field, s.now().Unix()),
		"Billing differs from the payment provider: "+slug, []string{
			fmt.Sprintf("Shop %s: %s is %q here and %q at the provider, in two hourly checks in a row.", slug, d.field, d.ours, d.theirs),
			"Nothing was changed automatically. Check Billing → Drift in the console and fix it with the tenant's billing actions.",
		})
}

// checkWebhooks updates the webhook-age gauge and queues (once a day) an
// alert when live subscriptions exist but webhooks have gone quiet.
func (s *Service) checkWebhooks(ctx context.Context, live bool) (bool, error) {
	quiet := false
	err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var last *time.Time
		if err := tx.QueryRow(ctx, `SELECT max(received_at) FROM platform.billing_events WHERE signature_valid`).Scan(&last); err != nil {
			return err
		}
		now := s.now()
		age := time.Duration(0)
		if live {
			if last == nil {
				age = s.quietAfter() + time.Second // live subscriptions and never a webhook
			} else {
				age = now.Sub(*last)
			}
		}
		if s.Metrics != nil {
			s.Metrics.BillingWebhookAge.Set(age.Seconds())
		}
		if !live || age <= s.quietAfter() {
			return nil
		}
		quiet = true
		since := "never"
		if last != nil {
			since = last.UTC().Format("2 Jan 2006 15:04 UTC")
		}
		return s.opsAlert(ctx, tx, "billing-webhooks-quiet:"+now.UTC().Format("2006-01-02"), "No billing webhooks received", []string{
			fmt.Sprintf("There are live subscriptions but no verified webhook from the payment provider since %s.", since),
			"Check the webhook URL and secret in the provider dashboard and the webhook logs. Payments may not be reaching the shops.",
		})
	})
	return quiet, err
}

// opsAlert queues an email to the platform inbox (deduplicated by key).
func (s *Service) opsAlert(ctx context.Context, tx tenancy.PlatformTx, key, heading string, lines []string) error {
	if s.Log != nil {
		s.Log.WarnContext(ctx, "billing alert", "heading", heading)
	}
	if s.Inbox == "" {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"name": "Platform team", "heading": heading, "lines": lines})
	_, err := tx.Exec(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data)
		VALUES ($1, 'platform_notice', $2, 'Platform team', '`+mailtmpl.OpsAlertTemplate+`', $3::text::jsonb)
		ON CONFLICT (idempotency_key) DO NOTHING`, key, s.Inbox, string(data))
	return err
}

// DriftRow is one difference, for the console.
type DriftRow struct {
	ID         int64      `json:"id"`
	TenantID   string     `json:"tenantId"`
	Slug       string     `json:"slug"`
	Field      string     `json:"field"`
	Ours       *string    `json:"ours"`
	Theirs     *string    `json:"theirs"`
	FirstSeen  time.Time  `json:"firstSeen"`
	LastSeen   time.Time  `json:"lastSeen"`
	AlertedAt  *time.Time `json:"alertedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

// DriftReport is GET /api/platform/billing/drift.
type DriftReport struct {
	Open          []DriftRow `json:"open"`
	Resolved      []DriftRow `json:"resolved"` // the last 7 days
	LastWebhookAt *time.Time `json:"lastWebhookAt"`
	QuietAfterH   int        `json:"quietAfterHours"`
}

const qDriftRows = `SELECT d.id, d.tenant_id, t.slug::text, d.field, d.ours, d.theirs, d.first_seen, d.last_seen, d.alerted_at, d.resolved_at
	FROM platform.billing_drift d JOIN platform.tenants t ON t.id = d.tenant_id`

// Drift reads the report (platform schema only).
func (s *Service) Drift(ctx context.Context, tx tenancy.PlatformTx) (DriftReport, error) {
	out := DriftReport{Open: []DriftRow{}, Resolved: []DriftRow{}, QuietAfterH: int(s.quietAfter() / time.Hour)}
	read := func(rows pgx.Rows, err error, into *[]DriftRow) error {
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DriftRow
			if err := rows.Scan(&d.ID, &d.TenantID, &d.Slug, &d.Field, &d.Ours, &d.Theirs, &d.FirstSeen, &d.LastSeen, &d.AlertedAt, &d.ResolvedAt); err != nil {
				return err
			}
			*into = append(*into, d)
		}
		return rows.Err()
	}
	rows, err := tx.Query(ctx, qDriftRows+` WHERE d.resolved_at IS NULL ORDER BY d.first_seen, t.slug LIMIT 500`)
	if err := read(rows, err, &out.Open); err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, qDriftRows+` WHERE d.resolved_at > now() - interval '7 days' ORDER BY d.resolved_at DESC LIMIT 200`)
	if err := read(rows, err, &out.Resolved); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT max(received_at) FROM platform.billing_events WHERE signature_valid`).Scan(&out.LastWebhookAt)
	return out, err
}
