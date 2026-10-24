package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Super Admin billing (BILLING_DESIGN §7, API_CONTRACT §5.2): plans,
// assign plan, extend trial, apply grace, cancel, expiring and past-due
// lists, invoices. Every change is audited with before and after.

// mountBilling registers the routes in the signed-in console group.
func (h *HTTP) mountBilling(p chi.Router) {
	p.With(h.needAny(PermPlansManage, PermBillingView)).Get("/plans", h.listPlans)
	p.With(h.need(PermPlansManage)).Post("/plans", h.createPlan)
	p.With(h.need(PermPlansManage)).Patch("/plans/{id}", h.patchPlan)
	p.With(h.need(PermBillingView)).Get("/tenants/{id}/billing", h.tenantBilling)
	p.With(h.need(PermBillingManage)).Post("/tenants/{id}/subscription", h.assignPlan)
	p.With(h.need(PermBillingManage)).Post("/tenants/{id}/subscription/extend-trial", h.extendTrial)
	p.With(h.need(PermBillingManage)).Post("/tenants/{id}/subscription/grace", h.applyGrace)
	p.With(h.need(PermBillingManage), h.stepUpRequired).Post("/tenants/{id}/subscription/cancel", h.cancelSub)
	p.With(h.need(PermBillingView)).Get("/billing/expiring", h.expiring)
	p.With(h.need(PermBillingView)).Get("/billing/past-due", h.pastDue)
	p.With(h.need(PermBillingView)).Get("/billing/invoices", h.invoices)
}

// ------------------------------------------------------------------ plans

// Plan limit and feature keys (BILLING_DESIGN §2).
var (
	limitKeys   = map[string]bool{"max_stores": true, "max_users": true, "max_products": true, "max_emails_per_day": true, "max_storage_mb": true}
	featureKeys = map[string]bool{"reports_export": true, "public_shop": true, "tenant_smtp": true, "custom_domain": true, "multi_store": true}
	planCodeRe  = regexp.MustCompile(`^[a-z0-9_]{2,40}$`)
	provPlanRe  = regexp.MustCompile(`^plan_[A-Za-z0-9_]{1,40}$`)
)

// PlanView is a plan in the console.
type PlanView struct {
	ID             int64           `json:"id"`
	Code           string          `json:"code"`
	Name           string          `json:"name"`
	PriceMinor     int64           `json:"priceMinor"`
	Currency       string          `json:"currency"`
	Interval       string          `json:"interval"`
	RazorpayPlanID *string         `json:"razorpayPlanId"`
	TrialDays      int             `json:"trialDays"`
	Limits         json.RawMessage `json:"limits"`
	Features       json.RawMessage `json:"features"`
	IsPublic       bool            `json:"isPublic"`
	IsActive       bool            `json:"isActive"`
	Tenants        int             `json:"tenants"`
}

// PlanInput creates or edits a plan. Pointers: omitted means unchanged.
type PlanInput struct {
	Code           *string         `json:"code"`
	Name           *string         `json:"name"`
	PriceMinor     *int64          `json:"priceMinor"`
	Currency       *string         `json:"currency"`
	Interval       *string         `json:"interval"`
	RazorpayPlanID *string         `json:"razorpayPlanId"`
	TrialDays      *int            `json:"trialDays"`
	Limits         json.RawMessage `json:"limits"`
	Features       json.RawMessage `json:"features"`
	IsPublic       *bool           `json:"isPublic"`
	IsActive       *bool           `json:"isActive"`
}

// checkLimits validates limits. A missing limit means no limit, so a new
// plan must name all of them (0 for no limit).
func checkLimits(raw json.RawMessage, all bool, f map[string]string) {
	var m map[string]json.Number
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err := d.Decode(&m); err != nil || m == nil {
		f["limits"] = "Limits must be an object of whole numbers."
		return
	}
	if all {
		for k := range limitKeys {
			if _, ok := m[k]; !ok {
				f["limits"] = "Set every limit (" + k + " is missing); 0 means no limit."
			}
		}
	}
	for k, v := range m {
		n, err := strconv.ParseInt(v.String(), 10, 64)
		switch {
		case !limitKeys[k]:
			f["limits"] = "Unknown limit " + strconv.Quote(k) + "."
		case err != nil || n < 0:
			f["limits"] = "Limit " + k + " must be a whole number of 0 or more (0: no limit)."
		}
	}
}

func checkFeatures(raw json.RawMessage, f map[string]string) {
	var m map[string]bool
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		f["features"] = "Features must be an object of true/false values."
		return
	}
	for k := range m {
		if !featureKeys[k] {
			f["features"] = "Unknown feature " + strconv.Quote(k) + "."
		}
	}
}

const planViewCols = `p.id, p.code, p.name, p.price_minor, p.currency, p.billing_interval, p.razorpay_plan_id, p.trial_days,
	p.limits, p.features, p.is_public, p.is_active, (SELECT count(*) FROM platform.tenants t WHERE t.plan_id = p.id)`

func scanPlanView(row pgx.Row) (PlanView, error) {
	var v PlanView
	var limits, features []byte
	err := row.Scan(&v.ID, &v.Code, &v.Name, &v.PriceMinor, &v.Currency, &v.Interval, &v.RazorpayPlanID, &v.TrialDays,
		&limits, &features, &v.IsPublic, &v.IsActive, &v.Tenants)
	v.Limits, v.Features = limits, features
	return v, err
}

func (s *Service) loadPlan(ctx context.Context, tx tenancy.PlatformTx, id int64) (PlanView, error) {
	v, err := scanPlanView(tx.QueryRow(ctx, `SELECT `+planViewCols+` FROM platform.plans p WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, apperr.NotFound()
	}
	return v, err
}

// ListPlans returns every plan.
func (s *Service) ListPlans(ctx context.Context) ([]PlanView, error) {
	out := []PlanView{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, `SELECT `+planViewCols+` FROM platform.plans p ORDER BY p.price_minor, p.code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanPlanView(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// mergeJSON overlays the keys of patch on base (both JSON objects; both
// are validated before this is used).
func mergeJSON(base, patch json.RawMessage) json.RawMessage {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(base, &m)
	var p map[string]json.RawMessage
	if json.Unmarshal(patch, &p) != nil {
		return base
	}
	for k, v := range p {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		return base
	}
	return out
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// CreatePlan adds a plan (prices are set by the owner, BILLING_DESIGN §9).
func (s *Service) CreatePlan(ctx context.Context, a Actor, in PlanInput, m Meta) (PlanView, error) {
	f := map[string]string{}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	code, name, cur, interval := str(in.Code), str(in.Name), strings.ToUpper(str(in.Currency)), str(in.Interval)
	if cur == "" {
		cur = "INR"
	}
	if !planCodeRe.MatchString(code) {
		f["code"] = "2-40 lowercase letters, digits or underscores."
	}
	if l := len([]rune(name)); l < 1 || l > 120 {
		f["name"] = "1-120 characters."
	}
	if in.PriceMinor == nil || *in.PriceMinor < 0 {
		f["priceMinor"] = "A price in paise, 0 or more."
	}
	if cur != "INR" {
		f["currency"] = "Plans are in INR."
	}
	if interval != "monthly" && interval != "yearly" {
		f["interval"] = "monthly or yearly."
	}
	prov := str(in.RazorpayPlanID)
	if prov != "" && !provPlanRe.MatchString(prov) {
		f["razorpayPlanId"] = "A provider plan ID such as plan_ABC123."
	}
	trial := 14
	if in.TrialDays != nil {
		trial = *in.TrialDays
	}
	if trial < 0 || trial > 365 {
		f["trialDays"] = "0-365 days."
	}
	if len(in.Limits) == 0 {
		in.Limits = json.RawMessage(`{}`)
	}
	if len(in.Features) == 0 {
		in.Features = json.RawMessage(`{}`)
	}
	checkLimits(in.Limits, true, f)
	checkFeatures(in.Features, f)
	if err := apperr.Fields(f); err != nil {
		return PlanView{}, err
	}
	public, active := in.IsPublic != nil && *in.IsPublic, in.IsActive == nil || *in.IsActive
	var out PlanView
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO platform.plans (code, name, price_minor, currency, billing_interval, razorpay_plan_id,
				trial_days, limits, features, is_public, is_active)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11) RETURNING id`,
			code, name, *in.PriceMinor, cur, interval, prov, trial, string(in.Limits), string(in.Features), public, active).Scan(&id)
		if isUnique(err) {
			return apperr.Conflict(apperr.CodeConflict, "A plan with this code or provider plan ID already exists.")
		}
		if err != nil {
			return err
		}
		if out, err = s.loadPlan(ctx, tx, id); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "plan.create", Target: code,
			After: map[string]any{"plan": out}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// PatchPlan edits a plan. A plan linked to a provider plan keeps its
// price and interval (the provider's plan is fixed); its provider plan ID
// can be set once.
func (s *Service) PatchPlan(ctx context.Context, a Actor, id int64, in PlanInput, m Meta) (PlanView, error) {
	var out PlanView
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM platform.plans WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		before, err := s.loadPlan(ctx, tx, id)
		if err != nil {
			return err
		}
		f := map[string]string{}
		next := before
		if in.Code != nil && strings.TrimSpace(*in.Code) != before.Code {
			f["code"] = "The code cannot change."
		}
		if in.Name != nil {
			next.Name = strings.TrimSpace(*in.Name)
			if l := len([]rune(next.Name)); l < 1 || l > 120 {
				f["name"] = "1-120 characters."
			}
		}
		linked := before.RazorpayPlanID != nil
		if in.PriceMinor != nil && *in.PriceMinor != before.PriceMinor {
			switch {
			case linked:
				f["priceMinor"] = "The price of a plan linked to the provider cannot change; create a new plan."
			case *in.PriceMinor < 0:
				f["priceMinor"] = "A price in paise, 0 or more."
			}
			next.PriceMinor = *in.PriceMinor
		}
		if in.Interval != nil && strings.TrimSpace(*in.Interval) != before.Interval {
			iv := strings.TrimSpace(*in.Interval)
			switch {
			case linked:
				f["interval"] = "The interval of a plan linked to the provider cannot change."
			case iv != "monthly" && iv != "yearly":
				f["interval"] = "monthly or yearly."
			}
			next.Interval = iv
		}
		if in.Currency != nil && strings.ToUpper(strings.TrimSpace(*in.Currency)) != before.Currency {
			f["currency"] = "Plans are in INR."
		}
		if in.RazorpayPlanID != nil {
			prov := strings.TrimSpace(*in.RazorpayPlanID)
			switch {
			case linked && prov != *before.RazorpayPlanID:
				f["razorpayPlanId"] = "The provider plan ID is set once."
			case !provPlanRe.MatchString(prov):
				f["razorpayPlanId"] = "A provider plan ID such as plan_ABC123."
			default:
				next.RazorpayPlanID = &prov
			}
		}
		if in.TrialDays != nil {
			if *in.TrialDays < 0 || *in.TrialDays > 365 {
				f["trialDays"] = "0-365 days."
			}
			next.TrialDays = *in.TrialDays
		}
		// Limits and features merge: a PATCH naming one limit keeps the rest.
		if len(in.Limits) > 0 {
			checkLimits(in.Limits, false, f)
			next.Limits = mergeJSON(before.Limits, in.Limits)
		}
		if len(in.Features) > 0 {
			checkFeatures(in.Features, f)
			next.Features = mergeJSON(before.Features, in.Features)
		}
		if in.IsPublic != nil {
			next.IsPublic = *in.IsPublic
		}
		if in.IsActive != nil {
			next.IsActive = *in.IsActive
		}
		if err := apperr.Fields(f); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE platform.plans SET name = $2, price_minor = $3, billing_interval = $4, razorpay_plan_id = $5,
				trial_days = $6, limits = $7, features = $8, is_public = $9, is_active = $10 WHERE id = $1`,
			id, next.Name, next.PriceMinor, next.Interval, next.RazorpayPlanID, next.TrialDays, string(next.Limits),
			string(next.Features), next.IsPublic, next.IsActive)
		if isUnique(err) {
			return apperr.Conflict(apperr.CodeConflict, "Another plan already uses this provider plan ID.")
		}
		if err != nil {
			return err
		}
		if out, err = s.loadPlan(ctx, tx, id); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "plan.update", Target: before.Code,
			Before: map[string]any{"plan": before}, After: map[string]any{"plan": out}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// --------------------------------------------------------- subscriptions

// SubscriptionView is a tenant's subscription in the console.
type SubscriptionView struct {
	TenantID          string     `json:"tenantId"`
	Slug              string     `json:"slug"`
	Name              string     `json:"name"`
	TenantStatus      string     `json:"tenantStatus"`
	SuspensionSource  *string    `json:"suspensionSource"`
	Status            string     `json:"status"`
	PlanCode          string     `json:"planCode"`
	Provider          string     `json:"provider"`
	ProviderSubID     *string    `json:"providerSubscriptionId"`
	ProviderStatus    *string    `json:"providerStatus"`
	TrialEnd          *time.Time `json:"trialEnd"`
	CurrentPeriodEnd  *time.Time `json:"currentPeriodEnd"`
	GraceEnd          *time.Time `json:"graceEnd"`
	CancelAtPeriodEnd bool       `json:"cancelAtPeriodEnd"`
	LastEventAt       *time.Time `json:"lastEventAt"`
}

const subViewCols = `t.id, t.slug::text, t.name, t.status, t.suspension_source, s.status, p.code, s.provider,
	s.provider_subscription_id, s.provider_status, s.trial_end, s.current_period_end, s.grace_end, s.cancel_at_period_end,
	s.last_event_at`

const subViewFrom = ` FROM platform.subscriptions s JOIN platform.tenants t ON t.id = s.tenant_id JOIN platform.plans p ON p.id = s.plan_id`

func scanSubView(row pgx.Row) (SubscriptionView, error) {
	var v SubscriptionView
	err := row.Scan(&v.TenantID, &v.Slug, &v.Name, &v.TenantStatus, &v.SuspensionSource, &v.Status, &v.PlanCode, &v.Provider,
		&v.ProviderSubID, &v.ProviderStatus, &v.TrialEnd, &v.CurrentPeriodEnd, &v.GraceEnd, &v.CancelAtPeriodEnd, &v.LastEventAt)
	return v, err
}

// EventView is one billing event (no payload).
type EventView struct {
	EventID    string     `json:"eventId"`
	EventType  string     `json:"eventType"`
	CreatedAt  *time.Time `json:"providerCreatedAt"`
	ReceivedAt time.Time  `json:"receivedAt"`
	Outcome    *string    `json:"outcome"`
}

// InvoiceView is one SaaS invoice in the console.
type InvoiceView struct {
	TenantID          string     `json:"tenantId"`
	ProviderInvoiceID string     `json:"providerInvoiceId"`
	AmountMinor       int64      `json:"amountMinor"`
	AmountPaidMinor   int64      `json:"amountPaidMinor"`
	Currency          string     `json:"currency"`
	Status            string     `json:"status"`
	IssuedAt          *time.Time `json:"issuedAt"`
	PaidAt            *time.Time `json:"paidAt"`
}

// TenantBilling is GET /tenants/{id}/billing.
type TenantBilling struct {
	Subscription SubscriptionView `json:"subscription"`
	Invoices     []InvoiceView    `json:"invoices"`
	Events       []EventView      `json:"events"`
}

const qInvoiceView = `SELECT tenant_id, provider_invoice_id, amount_minor, amount_paid_minor, currency, status, issued_at, paid_at
	FROM platform.invoices WHERE ($1 = '' OR tenant_id = $1) ORDER BY created_at DESC, id DESC LIMIT $2`

func listInvoices(ctx context.Context, tx tenancy.PlatformTx, tenantID string, limit int) ([]InvoiceView, error) {
	rows, err := tx.Query(ctx, qInvoiceView, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvoiceView{}
	for rows.Next() {
		var v InvoiceView
		if err := rows.Scan(&v.TenantID, &v.ProviderInvoiceID, &v.AmountMinor, &v.AmountPaidMinor, &v.Currency, &v.Status,
			&v.IssuedAt, &v.PaidAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// TenantBilling returns a tenant's subscription, invoices and recent
// billing events.
func (s *Service) TenantBilling(ctx context.Context, id string) (TenantBilling, error) {
	if !tenancy.ValidTenantID(id) {
		return TenantBilling{}, apperr.NotFound()
	}
	out := TenantBilling{Events: []EventView{}}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		v, err := scanSubView(tx.QueryRow(ctx, `SELECT `+subViewCols+subViewFrom+` WHERE s.tenant_id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		out.Subscription = v
		if out.Invoices, err = listInvoices(ctx, tx, id, 100); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT event_id, event_type, provider_created_at, received_at, outcome FROM platform.billing_events
			WHERE tenant_id = $1 ORDER BY received_at DESC, id DESC LIMIT 50`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e EventView
			if err := rows.Scan(&e.EventID, &e.EventType, &e.CreatedAt, &e.ReceivedAt, &e.Outcome); err != nil {
				return err
			}
			out.Events = append(out.Events, e)
		}
		return rows.Err()
	})
	return out, err
}

// lockSub locks a tenant's subscription for a console action.
func lockConsoleSub(ctx context.Context, tx tenancy.PlatformTx, id string) (SubscriptionView, error) {
	v, err := scanSubView(tx.QueryRow(ctx, `SELECT `+subViewCols+subViewFrom+` WHERE s.tenant_id = $1 FOR UPDATE OF s`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, apperr.NotFound()
	}
	return v, err
}

// liveProvider: the tenant has a provider subscription past the
// checkout link (it was authorised or paid at least once).
func liveProvider(v SubscriptionView) bool {
	return v.ProviderSubID != nil && v.ProviderStatus != nil && *v.ProviderStatus != "created" &&
		*v.ProviderStatus != "cancelled" && *v.ProviderStatus != "completed" && *v.ProviderStatus != "expired"
}

func reasonOK(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if l := len([]rune(reason)); l < 5 || l > 500 {
		return "", apperr.Invalid("reason", "5-500 characters.")
	}
	return reason, nil
}

// consoleChange runs a subscription change, syncs the tenant's status
// and plan (billing.SyncTenant: an admin suspension stays) and audits it.
func (s *Service) consoleChange(ctx context.Context, a Actor, id, action string, m Meta, extra map[string]any,
	change func(tx tenancy.PlatformTx, v SubscriptionView) error) (SubscriptionView, error) {
	if !tenancy.ValidTenantID(id) {
		return SubscriptionView{}, apperr.NotFound()
	}
	var out SubscriptionView
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		before, err := lockConsoleSub(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := change(tx, before); err != nil {
			return err
		}
		var status string
		var plan int64
		if err := tx.QueryRow(ctx, `SELECT status, plan_id FROM platform.subscriptions WHERE tenant_id = $1`, id).Scan(&status, &plan); err != nil {
			return err
		}
		if _, _, err := billing.SyncTenant(ctx, tx, id, billing.Status(status), plan); err != nil {
			return err
		}
		if out, err = scanSubView(tx.QueryRow(ctx, `SELECT `+subViewCols+subViewFrom+` WHERE s.tenant_id = $1`, id)); err != nil {
			return err
		}
		after := map[string]any{"subscription": out}
		for k, v := range extra {
			after[k] = v
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: action, TargetTenant: id,
			Before: map[string]any{"subscription": before}, After: after, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if err == nil {
		s.flush()
	}
	return out, err
}

// AssignPlan puts a tenant on a plan locally (a comped or manual plan).
// Refused while the tenant pays through a live provider subscription:
// its next event would set the provider's plan back. Plan changes at the
// provider come later (step 16 gate report).
func (s *Service) AssignPlan(ctx context.Context, a Actor, id, code, reason string, m Meta) (SubscriptionView, error) {
	reason, err := reasonOK(reason)
	if err != nil {
		return SubscriptionView{}, err
	}
	return s.consoleChange(ctx, a, id, "billing.assign_plan", m, map[string]any{"reason": reason, "plan": code},
		func(tx tenancy.PlatformTx, v SubscriptionView) error {
			if liveProvider(v) {
				return apperr.Conflict(apperr.CodeInvalidState, "This tenant pays through the provider; cancel that subscription first.")
			}
			tag, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET plan_id = (SELECT id FROM platform.plans WHERE code = $2 AND is_active),
				updated_at = now() WHERE tenant_id = $1 AND EXISTS (SELECT 1 FROM platform.plans WHERE code = $2 AND is_active)`, id, code)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return apperr.Invalid("planCode", "No active plan with this code.")
			}
			return nil
		})
}

// ExtendTrial adds days to a trial; a tenant made read-only by an
// expired trial is trialing again (BILLING_DESIGN §7).
func (s *Service) ExtendTrial(ctx context.Context, a Actor, id string, days int, reason string, m Meta) (SubscriptionView, error) {
	reason, err := reasonOK(reason)
	if err != nil {
		return SubscriptionView{}, err
	}
	if days < 1 || days > 90 {
		return SubscriptionView{}, apperr.Invalid("days", "1-90 days.")
	}
	return s.consoleChange(ctx, a, id, "billing.extend_trial", m, map[string]any{"reason": reason, "days": days},
		func(tx tenancy.PlatformTx, v SubscriptionView) error {
			trial := v.Status == string(billing.Trialing)
			trialOver := v.Status == string(billing.ReadOnly) && !liveProvider(v)
			if !trial && !trialOver {
				return apperr.Conflict(apperr.CodeInvalidState, "Only a trial, or a read-only tenant whose trial ended, can get more trial days.")
			}
			_, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET status = 'trialing',
				trial_end = greatest(coalesce(trial_end, now()), now()) + make_interval(days => $2), updated_at = now()
				WHERE tenant_id = $1`, id, days)
			return err
		})
}

// ApplyGrace gives a payment problem (or a billing suspension) more days:
// the subscription is in grace until then.
func (s *Service) ApplyGrace(ctx context.Context, a Actor, id string, days int, reason string, m Meta) (SubscriptionView, error) {
	reason, err := reasonOK(reason)
	if err != nil {
		return SubscriptionView{}, err
	}
	if days < 1 || days > 60 {
		return SubscriptionView{}, apperr.Invalid("days", "1-60 days.")
	}
	return s.consoleChange(ctx, a, id, "billing.grace", m, map[string]any{"reason": reason, "days": days},
		func(tx tenancy.PlatformTx, v SubscriptionView) error {
			switch billing.Status(v.Status) {
			case billing.PastDue, billing.Grace, billing.Suspended:
			default:
				return apperr.Conflict(apperr.CodeInvalidState, "Grace applies to past-due or suspended subscriptions.")
			}
			_, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET status = 'grace', grace_end = now() + make_interval(days => $2),
				updated_at = now() WHERE tenant_id = $1`, id, days)
			return err
		})
}

// CancelSubscription cancels at the provider (when there is a provider
// subscription) and locally: at period end, or at once (then the
// lifecycle job makes the tenant read-only when the paid period is over).
func (s *Service) CancelSubscription(ctx context.Context, a Actor, id string, atPeriodEnd bool, reason string, m Meta) (SubscriptionView, error) {
	reason, err := reasonOK(reason)
	if err != nil {
		return SubscriptionView{}, err
	}
	return s.consoleChange(ctx, a, id, "billing.cancel", m, map[string]any{"reason": reason, "at_period_end": atPeriodEnd},
		func(tx tenancy.PlatformTx, v SubscriptionView) error {
			switch billing.Status(v.Status) {
			case billing.Cancelled, billing.Expired, billing.ReadOnly:
				return apperr.Conflict(apperr.CodeInvalidState, "The subscription is already over.")
			}
			if v.ProviderSubID != nil && s.cfg.Billing != nil && s.cfg.Billing.Provider != nil {
				if err := s.cfg.Billing.Provider.CancelSubscription(ctx, *v.ProviderSubID, atPeriodEnd); err != nil {
					if s.cfg.Log != nil {
						s.cfg.Log.ErrorContext(ctx, "provider cancel failed", "err", err)
					}
					return apperr.Unavailable()
				}
			}
			if atPeriodEnd {
				_, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET cancel_at_period_end = true, updated_at = now() WHERE tenant_id = $1`, id)
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET status = 'cancelled', cancelled_at = now(), cancel_at_period_end = false,
				updated_at = now() WHERE tenant_id = $1`, id)
			return err
		})
}

// ------------------------------------------------------------------ lists

const (
	qExpiring = `SELECT ` + subViewCols + subViewFrom + `
		WHERE (s.status = 'trialing' AND s.trial_end <= now() + make_interval(days => $1))
		   OR ((s.status = 'cancelled' OR s.cancel_at_period_end) AND s.current_period_end <= now() + make_interval(days => $1))
		ORDER BY coalesce(CASE WHEN s.status = 'trialing' THEN s.trial_end END, s.current_period_end), t.slug LIMIT 500`
	qPastDue = `SELECT ` + subViewCols + subViewFrom + `
		WHERE s.status IN ('past_due','grace') OR (s.status = 'suspended' AND t.suspension_source = 'billing')
		ORDER BY s.grace_end NULLS LAST, t.slug LIMIT 500`
)

func (s *Service) subList(ctx context.Context, past bool, days int) ([]SubscriptionView, error) {
	out := []SubscriptionView{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		var rows pgx.Rows
		var err error
		if past {
			rows, err = tx.Query(ctx, qPastDue)
		} else {
			rows, err = tx.Query(ctx, qExpiring, days)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanSubView(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// ----------------------------------------------------------------- HTTP

func (h *HTTP) listPlans(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListPlans(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *HTTP) createPlan(w http.ResponseWriter, r *http.Request) {
	var in PlanInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.CreatePlan(r.Context(), actorFrom(r.Context()), in, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *HTTP) patchPlan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, r, apperr.NotFound())
		return
	}
	var in PlanInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out, err := h.svc.PatchPlan(r.Context(), actorFrom(r.Context()), id, in, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *HTTP) tenantBilling(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.TenantBilling(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *HTTP) respondSub(w http.ResponseWriter, r *http.Request) func(SubscriptionView, error) {
	return func(v SubscriptionView, err error) {
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, v)
	}
}

type subChange struct {
	PlanCode    string `json:"planCode"`
	Days        int    `json:"days"`
	Reason      string `json:"reason"`
	AtPeriodEnd bool   `json:"atPeriodEnd"`
}

func (h *HTTP) decodeChange(w http.ResponseWriter, r *http.Request) (subChange, bool) {
	var in subChange
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return in, false
	}
	return in, true
}

func (h *HTTP) assignPlan(w http.ResponseWriter, r *http.Request) {
	if in, ok := h.decodeChange(w, r); ok {
		h.respondSub(w, r)(h.svc.AssignPlan(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), strings.TrimSpace(in.PlanCode), in.Reason, h.meta(r)))
	}
}

func (h *HTTP) extendTrial(w http.ResponseWriter, r *http.Request) {
	if in, ok := h.decodeChange(w, r); ok {
		h.respondSub(w, r)(h.svc.ExtendTrial(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.Days, in.Reason, h.meta(r)))
	}
}

func (h *HTTP) applyGrace(w http.ResponseWriter, r *http.Request) {
	if in, ok := h.decodeChange(w, r); ok {
		h.respondSub(w, r)(h.svc.ApplyGrace(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.Days, in.Reason, h.meta(r)))
	}
}

func (h *HTTP) cancelSub(w http.ResponseWriter, r *http.Request) {
	if in, ok := h.decodeChange(w, r); ok {
		h.respondSub(w, r)(h.svc.CancelSubscription(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.AtPeriodEnd, in.Reason, h.meta(r)))
	}
}

func (h *HTTP) expiring(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 90 {
			httpx.WriteError(w, r, apperr.Invalid("days", "1-90."))
			return
		}
		days = n
	}
	out, err := h.svc.subList(r.Context(), false, days)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *HTTP) pastDue(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.subList(r.Context(), true, 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *HTTP) invoices(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenantId"))
	if tenantID != "" && !tenancy.ValidTenantID(tenantID) {
		httpx.WriteError(w, r, apperr.Invalid("tenantId", "Not a tenant ID."))
		return
	}
	var out []InvoiceView
	err := h.svc.ro(r.Context(), func(tx tenancy.PlatformTx) error {
		var err error
		out, err = listInvoices(r.Context(), tx, tenantID, 200)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
