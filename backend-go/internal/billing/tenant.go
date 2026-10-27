package billing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/httprate"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// TenantAPI serves the tenant side of billing (API_CONTRACT §5.1):
// subscription view, checkout and the billing banner. Everything is read
// for the tenant of the host only.
type TenantAPI struct {
	Svc *Service
}

// Mount registers the routes. Checkout passes the read-only gate so a
// read-only tenant can pay to come back (C-21).
func (a *TenantAPI) Mount(rt *rbac.Router) {
	perUser := httprate.NewRateLimiter(10, time.Minute, httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
		p, _ := auth.PrincipalFrom(r.Context())
		t, _ := tenancy.TenantFrom(r.Context())
		return "checkout:" + t.ID + ":" + strconv.FormatInt(p.UserID, 10), nil
	}), httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, apperr.RateLimited(apperr.CodeRateLimited, time.Minute))
	})).Handler
	rt.Handle(http.MethodGet, "/billing/subscription", rbac.Require("billing", "view"), a.subscription)
	rt.Handle(http.MethodPost, "/billing/checkout", rbac.Require("billing", "manage").ReadOnlyOK(), perUser(http.HandlerFunc(a.checkout)).ServeHTTP)
	rt.Handle(http.MethodGet, "/notifications/banners", rbac.Authenticated(), a.banners)
}

func tenantOf(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

// PlanJSON is a plan as tenants see it (no provider IDs).
type PlanJSON struct {
	Code       string          `json:"code"`
	Name       string          `json:"name"`
	PriceMinor int64           `json:"priceMinor"`
	Currency   string          `json:"currency"`
	Interval   string          `json:"interval"`
	TrialDays  int             `json:"trialDays"`
	Limits     json.RawMessage `json:"limits"`
	Features   json.RawMessage `json:"features"`
}

// InvoiceJSON is one SaaS invoice.
type InvoiceJSON struct {
	ID              string     `json:"id"`
	AmountMinor     int64      `json:"amountMinor"`
	AmountPaidMinor int64      `json:"amountPaidMinor"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	IssuedAt        *time.Time `json:"issuedAt"`
	PaidAt          *time.Time `json:"paidAt"`
	URL             *string    `json:"url"`
}

// Usage is the tenant's use of its plan limits.
type Usage struct {
	Stores      int   `json:"stores"`
	Users       int   `json:"users"`
	Products    int   `json:"products"`
	StorageMB   int64 `json:"storageMb"`
	EmailsToday int   `json:"emailsToday"`
}

// SubscriptionJSON is GET /api/billing/subscription.
type SubscriptionJSON struct {
	Status            string        `json:"status"`
	TenantStatus      string        `json:"tenantStatus"`
	Plan              PlanJSON      `json:"plan"`
	TrialEnd          *time.Time    `json:"trialEnd"`
	CurrentPeriodEnd  *time.Time    `json:"currentPeriodEnd"`
	GraceEnd          *time.Time    `json:"graceEnd"`
	CancelAtPeriodEnd bool          `json:"cancelAtPeriodEnd"`
	CancelledAt       *time.Time    `json:"cancelledAt"`
	Subscribed        bool          `json:"subscribed"`
	CheckoutURL       *string       `json:"checkoutUrl"`
	BillingEnabled    bool          `json:"billingEnabled"`
	Usage             Usage         `json:"usage"`
	Invoices          []InvoiceJSON `json:"invoices"`
	Plans             []PlanJSON    `json:"plans"`
}

const (
	planCols = `p.code, p.name, p.price_minor, p.currency, p.billing_interval, p.trial_days, p.limits, p.features`
	qMySub   = `SELECT s.status, t.status, ` + planCols + `, s.trial_end, s.current_period_end, s.grace_end,
			s.cancel_at_period_end, s.cancelled_at, s.provider_subscription_id IS NOT NULL, s.checkout_short_url,
			t.storage_bytes
		FROM platform.subscriptions s JOIN platform.plans p ON p.id = s.plan_id JOIN platform.tenants t ON t.id = s.tenant_id
		WHERE s.tenant_id = $1`
	qMyInvoices = `SELECT provider_invoice_id, amount_minor, amount_paid_minor, currency, status, issued_at, paid_at, short_url
		FROM platform.invoices WHERE tenant_id = $1 ORDER BY created_at DESC, id DESC LIMIT 24`
	qBuyable = `SELECT ` + planCols + ` FROM platform.plans p
		WHERE p.is_public AND p.is_active AND p.razorpay_plan_id IS NOT NULL ORDER BY p.price_minor, p.code`
	qUsage = `SELECT (SELECT count(*) FROM stores WHERE deleted_at IS NULL), (SELECT count(*) FROM users WHERE deleted_at IS NULL),
		(SELECT count(*) FROM products WHERE deleted_at IS NULL),
		(SELECT count(*) FROM email_outbox WHERE status = 'sent' AND sent_at > now() - interval '24 hours')`
)

func scanPlan(row interface{ Scan(...any) error }, p *PlanJSON) error {
	var limits, features []byte
	if err := row.Scan(&p.Code, &p.Name, &p.PriceMinor, &p.Currency, &p.Interval, &p.TrialDays, &limits, &features); err != nil {
		return err
	}
	p.Limits, p.Features = limits, features
	return nil
}

// subscription is GET /api/billing/subscription.
func (a *TenantAPI) subscription(w http.ResponseWriter, r *http.Request) {
	t := tenantOf(r)
	ctx := r.Context()
	out := SubscriptionJSON{Invoices: []InvoiceJSON{}, Plans: []PlanJSON{}, BillingEnabled: a.Svc.Provider != nil}
	err := a.Svc.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		var limits, features []byte
		var storage int64
		p := &out.Plan
		err := tx.QueryRow(ctx, qMySub, t.ID).Scan(&out.Status, &out.TenantStatus, &p.Code, &p.Name, &p.PriceMinor, &p.Currency,
			&p.Interval, &p.TrialDays, &limits, &features, &out.TrialEnd, &out.CurrentPeriodEnd, &out.GraceEnd,
			&out.CancelAtPeriodEnd, &out.CancelledAt, &out.Subscribed, &out.CheckoutURL, &storage)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		p.Limits, p.Features = limits, features
		out.Usage.StorageMB = (storage + (1<<20 - 1)) >> 20
		rows, err := tx.Query(ctx, qMyInvoices, t.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var i InvoiceJSON
			if err := rows.Scan(&i.ID, &i.AmountMinor, &i.AmountPaidMinor, &i.Currency, &i.Status, &i.IssuedAt, &i.PaidAt, &i.URL); err != nil {
				rows.Close()
				return err
			}
			out.Invoices = append(out.Invoices, i)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, qBuyable)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var pl PlanJSON
			if err := scanPlan(rows, &pl); err != nil {
				return err
			}
			out.Plans = append(out.Plans, pl)
		}
		return rows.Err()
	})
	if err == nil {
		err = a.Svc.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
			return tx.QueryRow(ctx, qUsage).Scan(&out.Usage.Stores, &out.Usage.Users, &out.Usage.Products, &out.Usage.EmailsToday)
		})
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

type checkoutIn struct {
	PlanCode string `json:"planCode"`
}

// CheckoutJSON is the answer to a checkout: where to pay.
type CheckoutJSON struct {
	URL string `json:"url"`
}

// totalCount is the number of billing cycles a mandate covers.
func totalCount(interval string) int {
	if interval == "yearly" {
		return 10
	}
	return 120
}

// checkout is POST /api/billing/checkout {planCode}: it creates the
// provider customer if missing and a provider subscription for the plan,
// stores its ID and link, and returns the link. It never changes the
// subscription status: only a verified webhook does (BILLING_DESIGN §5).
// A trial keeps running; the first charge is at its end.
func (a *TenantAPI) checkout(w http.ResponseWriter, r *http.Request) {
	if a.Svc.Provider == nil {
		httpx.WriteError(w, r, apperr.Unavailable())
		return
	}
	var in checkoutIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	code := strings.TrimSpace(in.PlanCode)
	t := tenantOf(r)
	p, _ := auth.PrincipalFrom(r.Context())
	ctx := r.Context()
	now := a.Svc.now()
	var out CheckoutJSON
	var providerSub string
	err := a.Svc.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var planID int64
		var providerPlan, interval string
		err := tx.QueryRow(ctx, `SELECT id, razorpay_plan_id, billing_interval FROM platform.plans
			WHERE code = $1 AND is_public AND is_active AND razorpay_plan_id IS NOT NULL`, code).Scan(&planID, &providerPlan, &interval)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Invalid("planCode", "Choose one of the available plans.")
		}
		if err != nil {
			return err
		}
		r, err := scanSub(tx.QueryRow(ctx, qLockSubByTenant, t.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		live := r.ProviderSubID != "" && r.ProviderStatus != "" && r.ProviderStatus != "created"
		switch {
		case live && (r.Status == Active || r.Status == PastDue || r.Status == Grace || r.Status == Trialing):
			return apperr.Conflict(apperr.CodeInvalidState, "You already have a subscription. Contact support to change your plan.")
		case r.Status == Suspended:
			return apperr.Conflict(apperr.CodeInvalidState, "This account is suspended. Use the payment link in the email.")
		}
		var email, name string
		if err := tx.QueryRow(ctx, `SELECT coalesce(billing_email, owner_email)::text, name FROM platform.tenants WHERE id = $1`, t.ID).
			Scan(&email, &name); err != nil {
			return err
		}
		customer := r.ProviderCustomerID
		if customer == "" {
			if customer, err = a.Svc.Provider.CreateCustomer(ctx, TenantRef{ID: t.ID, Name: name, Email: email}); err != nil {
				return a.providerErr(ctx, err)
			}
		}
		req := CreateSubscriptionReq{TenantID: t.ID, CustomerID: customer, PlanID: providerPlan, TotalCount: totalCount(interval)}
		if r.Status == Trialing && r.TrialEnd != nil && r.TrialEnd.After(now.Add(time.Hour)) {
			start := *r.TrialEnd
			req.StartAt = &start
		}
		sub, err := a.Svc.Provider.CreateSubscription(ctx, req)
		if err != nil {
			return a.providerErr(ctx, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET provider = $2, provider_customer_id = $3,
				provider_subscription_id = $4, provider_status = 'created', checkout_short_url = NULLIF($5, ''), updated_at = now()
			WHERE id = $1`, r.id, a.Svc.Provider.Name(), customer, sub.ID, sub.ShortURL); err != nil {
			return err
		}
		out.URL, providerSub = sub.ShortURL, sub.ID
		return Audit(ctx, tx, AuditEntry{Actor: "tenant_user:" + strconv.FormatInt(p.UserID, 10), Action: "billing.checkout", Tenant: t.ID, Target: sub.ID,
			After: map[string]any{"plan": code, "plan_id": planID, "start_at": req.StartAt}})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = a.Svc.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "billing.checkout", Entity: "subscription",
			EntityID: providerSub, After: map[string]any{"plan": code}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	httpx.WriteJSON(w, http.StatusOK, out)
}

// providerErr logs the provider's answer and hides it from the client.
func (a *TenantAPI) providerErr(ctx context.Context, err error) error {
	if a.Svc.Log != nil {
		a.Svc.Log.ErrorContext(ctx, "billing provider call failed", slog.Any("err", err))
	}
	return apperr.Unavailable()
}

// Banner is one in-app banner.
type Banner struct {
	ID          string        `json:"id"`
	Kind        string        `json:"kind"`  // billing
	Level       string        `json:"level"` // info, warning, critical
	Message     string        `json:"message"`
	Action      *BannerAction `json:"action"`
	Dismissible bool          `json:"dismissible"`
}

// BannerAction is the banner's button.
type BannerAction struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// BannerFor derives the billing banner from the subscription state
// (BILLING_DESIGN §6); nil when there is nothing to say.
func BannerFor(s Sub, now time.Time) *Banner {
	date := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format("02 Jan 2006")
	}
	b := &Banner{ID: "billing", Kind: "billing"}
	switch {
	case s.Status == Trialing && s.TrialEnd != nil && s.TrialEnd.After(now) && s.TrialEnd.Sub(now) <= 7*24*time.Hour:
		b.Level, b.Message, b.Dismissible = "info", "Your trial ends on "+date(s.TrialEnd)+". Choose a plan to keep selling.", true
		b.Action = &BannerAction{Label: "Choose a plan", Path: "/billing"}
	case s.Status == PastDue || s.Status == Grace:
		b.Level, b.Message = "critical", "Your last payment failed. Pay by "+date(s.GraceEnd)+" to avoid suspension."
		b.Action = &BannerAction{Label: "Pay now", Path: "/billing"}
	case s.Status == Cancelled || (s.Status == Active && s.CancelAtPeriodEnd):
		b.Level, b.Message = "warning", "Your subscription ends on "+date(s.PeriodEnd)+"."
		b.Action = &BannerAction{Label: "Subscribe again", Path: "/billing"}
	case s.Status == ReadOnly || s.Status == Expired:
		b.Level, b.Message = "critical", "Your shop is read-only. Choose a plan to continue selling."
		b.Action = &BannerAction{Label: "Choose a plan", Path: "/billing"}
	default:
		return nil
	}
	return b
}

// banners is GET /api/notifications/banners. The action is only offered
// to users who may manage billing.
func (a *TenantAPI) banners(w http.ResponseWriter, r *http.Request) {
	t := tenantOf(r)
	p, _ := auth.PrincipalFrom(r.Context())
	out := []Banner{}
	err := a.Svc.Sessions.InPlatformTx(r.Context(), tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		s, err := scanSub(tx.QueryRow(r.Context(), `SELECT `+subCols+` FROM platform.subscriptions WHERE tenant_id = $1`, t.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if b := BannerFor(s.Sub, a.Svc.now()); b != nil {
			if !p.Has("billing:manage") {
				b.Action = nil
			}
			out = append(out, *b)
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}
