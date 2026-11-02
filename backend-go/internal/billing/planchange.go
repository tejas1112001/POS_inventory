package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Plan changes (S10 item 2, docs/S10_DESIGN.md §2, B1-B4):
//   - B1 a dearer plan applies now; the provider charges the prorated
//     difference.
//   - B2 a cheaper (or same-price) plan applies at the end of the paid
//     period; any change is refused while the shop uses more than the new
//     plan allows (stores, users, products, storage).
//   - B3 a scheduled change can be cancelled before it applies.
//   - B4 the console may change the plan of a paying shop; it is changed
//     at the provider too, so the next webhook does not set it back.
//
// Without a live provider subscription (trial, comped or manual plans) a
// change is local and applies now.

// UsageCounts is what a shop uses, compared with the new plan's limits.
// Known is false when the counts are not available (console: no usage
// snapshot yet); then nothing may change.
type UsageCounts struct {
	Stores, Users, Products, StorageMB int64
	Known                              bool
	TakenAt                            *time.Time
}

// Blocker is one limit the shop is over for the new plan.
type Blocker struct {
	Resource string `json:"resource"`
	Label    string `json:"label"`
	Used     int64  `json:"used"`
	Limit    int64  `json:"limit"`
}

// PlanChangeJSON describes a plan change: the preview shown before the
// confirm, and the answer after it.
type PlanChangeJSON struct {
	From        PlanJSON   `json:"from"`
	To          PlanJSON   `json:"to"`
	Kind        string     `json:"kind"` // upgrade | downgrade
	When        string     `json:"when"` // now | period_end
	EffectiveAt *time.Time `json:"effectiveAt"`
	Provider    bool       `json:"provider"` // also changed at the payment provider
	Effect      string     `json:"effect"`   // the billing effect in plain words
	Blockers    []Blocker  `json:"blockers"`
	UsageAsOf   *time.Time `json:"usageAsOf"`
	Allowed     bool       `json:"allowed"`
	Refusal     string     `json:"refusal,omitempty"`
}

// PendingJSON is a scheduled plan change.
type PendingJSON struct {
	Plan PlanJSON  `json:"plan"`
	At   time.Time `json:"at"`
}

type planRow struct {
	id           int64
	providerPlan string
	plan         PlanJSON
	active       bool
	public       bool
}

const qPlanCols = `p.id, coalesce(p.razorpay_plan_id, ''), p.is_active, p.is_public, ` + planCols

func scanPlanRow(row pgx.Row) (planRow, error) {
	var r planRow
	var limits, features []byte
	err := row.Scan(&r.id, &r.providerPlan, &r.active, &r.public, &r.plan.Code, &r.plan.Name, &r.plan.PriceMinor, &r.plan.Currency,
		&r.plan.Interval, &r.plan.TrialDays, &limits, &features)
	r.plan.Limits, r.plan.Features = limits, features
	return r, err
}

// changeState is a subscription with its current and target plans.
type changeState struct {
	sub      subRow
	cur, to  planRow
	pending  *planRow
	pendAt   *time.Time
	live     bool
	preview  PlanChangeJSON
	atCycleE bool
}

const (
	qSubForChange     = `SELECT ` + subCols + `, pending_plan_id, pending_change_at FROM platform.subscriptions WHERE tenant_id = $1`
	qSubForChangeLock = qSubForChange + ` FOR UPDATE`
	qPlanByID         = `SELECT ` + qPlanCols + ` FROM platform.plans p WHERE p.id = $1`
	qPlanByCode       = `SELECT ` + qPlanCols + ` FROM platform.plans p WHERE p.code = $1`
	qOverrides        = `SELECT key, value FROM platform.tenant_overrides
		WHERE tenant_id = $1 AND revoked_at IS NULL AND expires_at > now() AND key LIKE 'max\_%'`
)

func liveSub(r subRow) bool {
	switch r.ProviderStatus {
	case "", "created", "cancelled", "completed", "expired":
		return false
	}
	return r.ProviderSubID != ""
}

// loadChange reads (and with lock, locks) the subscription and both plans
// and works out the preview. byOwner limits the target to the plans a
// shop can buy.
func loadChange(ctx context.Context, tx tenancy.PlatformTx, tenantID, code string, byOwner, lock bool, u UsageCounts) (changeState, error) {
	var st changeState
	var pendID *int64
	var row pgx.Row
	if lock {
		row = tx.QueryRow(ctx, qSubForChangeLock, tenantID)
	} else {
		row = tx.QueryRow(ctx, qSubForChange, tenantID)
	}
	var status string
	r := &st.sub
	err := row.Scan(&r.id, &r.tenantID, &r.planID, &status, &r.ProviderSubID, &r.ProviderCustomerID, &r.ProviderStatus,
		&r.TrialEnd, &r.PeriodStart, &r.PeriodEnd, &r.GraceEnd, &r.CancelAtPeriodEnd, &r.CancelledAt, &r.LastEventAt, &pendID, &st.pendAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, apperr.NotFound()
	}
	if err != nil {
		return st, err
	}
	r.Status = Status(status)
	st.live = liveSub(*r)
	if st.cur, err = scanPlanRow(tx.QueryRow(ctx, qPlanByID, r.planID)); err != nil {
		return st, err
	}
	if pendID != nil {
		p, err := scanPlanRow(tx.QueryRow(ctx, qPlanByID, *pendID))
		if err != nil {
			return st, err
		}
		st.pending = &p
	}
	st.to, err = scanPlanRow(tx.QueryRow(ctx, qPlanByCode, strings.TrimSpace(code)))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!st.to.active || (byOwner && (!st.to.public || st.to.providerPlan == "")))) {
		return st, apperr.Invalid("planCode", "Choose one of the available plans.")
	}
	if err != nil {
		return st, err
	}
	p := &st.preview
	p.From, p.To, p.Provider, p.Blockers = st.cur.plan, st.to.plan, st.live, []Blocker{}
	p.Kind = "downgrade"
	if st.to.plan.PriceMinor > st.cur.plan.PriceMinor {
		p.Kind = "upgrade"
	}
	p.When = "now"
	if st.live && p.Kind == "downgrade" && r.PeriodEnd != nil {
		p.When, p.EffectiveAt, st.atCycleE = "period_end", r.PeriodEnd, true
	}
	if err := blockers(ctx, tx, tenantID, st.to, u, st.live, p); err != nil {
		return st, err
	}
	p.Effect = effect(st, p)
	p.Refusal = refusal(st, byOwner)
	if p.Refusal == "" && len(p.Blockers) > 0 {
		p.Refusal = "The shop uses more than the " + st.to.plan.Name + " plan allows. Reduce first: " + blockerText(p.Blockers) + "."
	}
	p.Allowed = p.Refusal == ""
	return st, nil
}

// refusal says why the change cannot be made at all ("" = it can).
func refusal(st changeState, byOwner bool) string {
	switch {
	case st.to.id == st.cur.id && st.pending != nil:
		return "This is the current plan. Cancel the scheduled change instead."
	case st.to.id == st.cur.id:
		return "This is already the current plan."
	case st.pending != nil && st.pending.id == st.to.id:
		return "This change is already scheduled."
	}
	switch st.sub.Status {
	case PastDue, Grace, Suspended:
		return "Settle the overdue payment before changing the plan."
	case Cancelled, Expired, ReadOnly:
		if st.live || byOwner {
			return "The subscription has ended. Subscribe again to choose a plan."
		}
	}
	if st.sub.CancelAtPeriodEnd {
		return "The subscription is cancelled at the end of this period; the plan cannot change."
	}
	if byOwner && !st.live {
		return "You do not have a paid subscription yet. Choose a plan with Subscribe."
	}
	if st.live {
		if st.to.providerPlan == "" {
			return "This plan cannot be billed by the payment provider (no provider plan ID)."
		}
		if st.to.plan.Interval != st.cur.plan.Interval {
			return "Changing between monthly and yearly billing is not supported yet. Contact support."
		}
	}
	return ""
}

func money(minor int64, currency string) string {
	sym := currency + " "
	if currency == "INR" {
		sym = "₹"
	}
	if minor%100 == 0 {
		return fmt.Sprintf("%s%d", sym, minor/100)
	}
	return fmt.Sprintf("%s%d.%02d", sym, minor/100, minor%100)
}

func per(interval string) string {
	if interval == "yearly" {
		return "a year"
	}
	return "a month"
}

// effect is the billing effect in plain words.
func effect(st changeState, p *PlanChangeJSON) string {
	price := fmt.Sprintf("%s %s → %s %s.", money(p.From.PriceMinor, p.From.Currency), per(p.From.Interval),
		money(p.To.PriceMinor, p.To.Currency), per(p.To.Interval))
	switch {
	case !st.live:
		return "Price " + price + " Takes effect now. Nothing is charged through the payment provider (no paid subscription)."
	case p.When == "now" && p.Kind == "upgrade":
		return "Price " + price + " Takes effect now; the payment provider charges the pro-rated difference for the rest of this period."
	case p.When == "now":
		return "Price " + price + " Takes effect now (no billing period has started yet); the next charge is at the new price."
	default:
		return "Price " + price + " Takes effect on " + p.EffectiveAt.UTC().Format("2 Jan 2006") +
			", the end of the paid period; nothing is refunded and the next charge is at the new price."
	}
}

// blockers lists the limits of the target plan (with the shop's active
// overrides) that the shop's usage passes.
//
// Unknown usage (the console before the first usage count) blocks a change
// billed by the provider; a local change (trial, comped) goes ahead, as the
// shop's own limit checks still stop it adding more.
func blockers(ctx context.Context, tx tenancy.PlatformTx, tenantID string, to planRow, u UsageCounts, live bool, p *PlanChangeJSON) error {
	if !u.Known {
		if live {
			p.Blockers = append(p.Blockers, Blocker{Resource: "usage", Label: "Usage not measured yet (the hourly usage count has not run for this shop)"})
		}
		return nil
	}
	p.UsageAsOf = u.TakenAt
	var l plans.Limits
	if len(to.plan.Limits) > 0 {
		if err := json.Unmarshal(to.plan.Limits, &l); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, qOverrides, tenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var v float64
		if err := rows.Scan(&key, &v); err != nil {
			return err
		}
		switch key {
		case "max_stores":
			l.MaxStores = int64(v)
		case "max_users":
			l.MaxUsers = int64(v)
		case "max_products":
			l.MaxProducts = int64(v)
		case "max_storage_mb":
			l.MaxStorageMB = int64(v)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range []struct {
		res, label  string
		used, limit int64
	}{
		{plans.Stores, "Stores", u.Stores, l.MaxStores},
		{plans.Users, "Users", u.Users, l.MaxUsers},
		{plans.Products, "Products", u.Products, l.MaxProducts},
		{"storage", "Storage (MB)", u.StorageMB, l.MaxStorageMB},
	} {
		if c.limit > 0 && c.used > c.limit {
			p.Blockers = append(p.Blockers, Blocker{Resource: c.res, Label: c.label, Used: c.used, Limit: c.limit})
		}
	}
	return nil
}

func blockerText(b []Blocker) string {
	parts := make([]string, 0, len(b))
	for _, x := range b {
		if x.Resource == "usage" {
			parts = append(parts, x.Label)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d of %d allowed", x.Label, x.Used, x.Limit))
	}
	return strings.Join(parts, ", ")
}

// PreviewPlanChange describes a change without making it (read-only).
func PreviewPlanChange(ctx context.Context, tx tenancy.PlatformTx, tenantID, code string, byOwner bool, u UsageCounts) (PlanChangeJSON, error) {
	st, err := loadChange(ctx, tx, tenantID, code, byOwner, false, u)
	return st.preview, err
}

// ChangePlanTx makes the change: at the provider first (when the shop pays
// through it), then here. actor is written to the platform audit log.
func (s *Service) ChangePlanTx(ctx context.Context, tx tenancy.PlatformTx, tenantID, code string, byOwner bool, u UsageCounts,
	actor string, actorID int64, reason string) (PlanChangeJSON, error) {
	st, err := loadChange(ctx, tx, tenantID, code, byOwner, true, u)
	if err != nil {
		return st.preview, err
	}
	p := st.preview
	if !p.Allowed {
		return p, apperr.Conflict(apperr.CodeInvalidState, p.Refusal)
	}
	if st.live {
		if s.Provider == nil {
			return p, apperr.Unavailable()
		}
		// A change made now replaces a scheduled one.
		if st.pending != nil && !st.atCycleE {
			if err := s.Provider.CancelScheduledChange(ctx, st.sub.ProviderSubID); err != nil {
				return p, s.providerFail(ctx, err)
			}
		}
		if _, err := s.Provider.ChangePlan(ctx, st.sub.ProviderSubID, st.to.providerPlan, st.atCycleE); err != nil {
			return p, s.providerFail(ctx, err)
		}
	}
	if st.atCycleE {
		_, err = tx.Exec(ctx, `UPDATE platform.subscriptions SET pending_plan_id = $2, pending_change_at = $3, updated_at = now() WHERE id = $1`,
			st.sub.id, st.to.id, *p.EffectiveAt)
	} else {
		_, err = tx.Exec(ctx, `UPDATE platform.subscriptions SET plan_id = $2, pending_plan_id = NULL, pending_change_at = NULL,
			updated_at = now() WHERE id = $1`, st.sub.id, st.to.id)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE platform.tenants SET plan_id = $2, updated_at = now() WHERE id = $1 AND plan_id IS DISTINCT FROM $2`,
				tenantID, st.to.id)
		}
	}
	if err != nil {
		return p, err
	}
	after := map[string]any{"plan": st.to.plan.Code, "when": p.When, "kind": p.Kind, "provider": st.live}
	if p.EffectiveAt != nil {
		after["effective_at"] = p.EffectiveAt.UTC().Format(time.RFC3339)
	}
	if reason != "" {
		after["reason"] = reason
	}
	if actor == "" { // the caller writes its own audit row
		return p, nil
	}
	return p, Audit(ctx, tx, AuditEntry{Actor: actor, ActorID: actorID, Action: "billing.plan_change", Tenant: tenantID,
		Target: st.sub.ProviderSubID, Before: map[string]any{"plan": st.cur.plan.Code}, After: after})
}

// CancelScheduledTx drops a scheduled plan change (B3) and returns the
// plan that was scheduled.
func (s *Service) CancelScheduledTx(ctx context.Context, tx tenancy.PlatformTx, tenantID, actor string, actorID int64, reason string) (PlanJSON, error) {
	var subID int64
	var providerSub string
	var pend *int64
	err := tx.QueryRow(ctx, `SELECT id, coalesce(provider_subscription_id, ''), pending_plan_id FROM platform.subscriptions
		WHERE tenant_id = $1 FOR UPDATE`, tenantID).Scan(&subID, &providerSub, &pend)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanJSON{}, apperr.NotFound()
	}
	if err != nil {
		return PlanJSON{}, err
	}
	if pend == nil {
		return PlanJSON{}, apperr.Conflict(apperr.CodeInvalidState, "No plan change is scheduled.")
	}
	pl, err := scanPlanRow(tx.QueryRow(ctx, qPlanByID, *pend))
	if err != nil {
		return PlanJSON{}, err
	}
	if providerSub != "" {
		if s.Provider == nil {
			return PlanJSON{}, apperr.Unavailable()
		}
		if err := s.Provider.CancelScheduledChange(ctx, providerSub); err != nil {
			return PlanJSON{}, s.providerFail(ctx, err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET pending_plan_id = NULL, pending_change_at = NULL, updated_at = now()
		WHERE id = $1`, subID); err != nil {
		return PlanJSON{}, err
	}
	after := map[string]any{"cancelled_plan": pl.plan.Code}
	if reason != "" {
		after["reason"] = reason
	}
	if actor == "" {
		return pl.plan, nil
	}
	return pl.plan, Audit(ctx, tx, AuditEntry{Actor: actor, ActorID: actorID, Action: "billing.plan_change_cancelled", Tenant: tenantID,
		Target: providerSub, After: after})
}

// PendingFor is a tenant's scheduled plan change, if any.
func PendingFor(ctx context.Context, tx tenancy.PlatformTx, tenantID string) (*PendingJSON, error) {
	var id *int64
	var at *time.Time
	err := tx.QueryRow(ctx, `SELECT pending_plan_id, pending_change_at FROM platform.subscriptions WHERE tenant_id = $1`, tenantID).Scan(&id, &at)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (id == nil || at == nil)) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pl, err := scanPlanRow(tx.QueryRow(ctx, qPlanByID, *id))
	if err != nil {
		return nil, err
	}
	return &PendingJSON{Plan: pl.plan, At: *at}, nil
}

// ConsoleUsage is a shop's usage as the console sees it: the worker's
// snapshot (the console never reads tenant schemas) and stored bytes.
func ConsoleUsage(ctx context.Context, tx tenancy.PlatformTx, tenantID string) (UsageCounts, error) {
	var u UsageCounts
	var at *time.Time
	var stores, users, products *int64
	var storage int64
	err := tx.QueryRow(ctx, `SELECT u.stores, u.users, u.products, u.taken_at, t.storage_bytes
		FROM platform.tenants t LEFT JOIN platform.tenant_usage u ON u.tenant_id = t.id WHERE t.id = $1`, tenantID).
		Scan(&stores, &users, &products, &at, &storage)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, apperr.NotFound()
	}
	if err != nil || at == nil {
		return u, err
	}
	u.Stores, u.Users, u.Products, u.TakenAt, u.Known = *stores, *users, *products, at, true
	u.StorageMB = (storage + (1<<20 - 1)) >> 20
	return u, nil
}

// providerFail logs the provider's answer and hides it from the client.
func (s *Service) providerFail(ctx context.Context, err error) error {
	if s.Log != nil {
		s.Log.ErrorContext(ctx, "billing provider call failed", "err", err)
	}
	return apperr.Unavailable()
}
