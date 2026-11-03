package settings

import (
	"net/http"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Onboarding checklist (S10 item 8, docs/S10_DESIGN.md §8, D10): the first
// steps of a new shop, each ticked from real data (never from a click),
// each linking to its page. Shown to users with settings:edit until every
// step is done or the owner hides it for good.

// Step is one checklist line.
type Step struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	Path  string `json:"path"`
	Done  bool   `json:"done"`
}

// Onboarding is GET /api/onboarding.
type Onboarding struct {
	Hidden   bool       `json:"hidden"`
	HiddenAt *time.Time `json:"hiddenAt"`
	Steps    []Step     `json:"steps"`
	Done     int        `json:"done"`
	Total    int        `json:"total"`
}

// One query: every tick from the shop's own data.
const qOnboarding = `SELECT s.onboarding_hidden_at,
	coalesce(s.address, '') <> '' AND coalesce(s.phone, '') <> '' AND coalesce(s.tax_registration_no, '') <> '',
	s.logo_file_id IS NOT NULL,
	EXISTS (SELECT 1 FROM stores st WHERE st.deleted_at IS NULL AND st.status = 'Active' AND coalesce(st.address, '') <> ''),
	EXISTS (SELECT 1 FROM products p WHERE p.deleted_at IS NULL),
	(SELECT count(*) FROM users u WHERE u.deleted_at IS NULL) > 1,
	EXISTS (SELECT 1 FROM tenant_email_settings e WHERE e.enabled AND e.status = 'ok')
		OR EXISTS (SELECT 1 FROM email_outbox o WHERE o.status = 'sent'),
	EXISTS (SELECT 1 FROM sales x WHERE x.status <> 'Void'),
	EXISTS (SELECT 1 FROM store_products sp JOIN stores st ON st.id = sp.store_id
		WHERE sp.is_published AND st.deleted_at IS NULL AND st.is_public_enabled)
FROM settings s WHERE s.id = 1`

func (a *API) onboarding(w http.ResponseWriter, r *http.Request) {
	var out Onboarding
	var profile, logo, store, product, staff, email, sale, shop bool
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), qOnboarding).Scan(&out.HiddenAt, &profile, &logo, &store, &product, &staff, &email, &sale, &shop)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out.Hidden = out.HiddenAt != nil
	out.Steps = []Step{
		{"profile", "Business profile and tax number", "Address, phone and tax number appear on every receipt.", "/settings", profile},
		{"logo", "Logo", "Shown on receipts and your online shop.", "/settings", logo},
		{"store", "Store address", "Each store's address goes on its receipts.", "/stores", store},
		{"product", "First product", "Add one, or import a CSV file of products.", "/products", product},
		{"staff", "First staff user", "Give each cashier their own sign-in.", "/users", staff},
		{"email", "Email sending", "Receipts by email: set up your own email server, or send one receipt.", "/settings", email},
		{"sale", "First sale", "Sell something at the checkout.", "/pos", sale},
	}
	if ok, err := a.d.Plans.HasFor(r.Context(), tenant(r), plans.FeaturePublicShop); err == nil && ok {
		out.Steps = append(out.Steps, Step{"shop", "Online shop", "Publish products on your storefront.", "/showcase-settings", shop})
	} else if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out.Total = len(out.Steps)
	for _, s := range out.Steps {
		if s.Done {
			out.Done++
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

// hideOnboarding is POST /api/onboarding/hide: hidden for good (all users).
func (a *API) hideOnboarding(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE settings SET onboarding_hidden_at = now() WHERE id = 1 AND onboarding_hidden_at IS NULL`)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: p.UserID, Action: "settings.onboarding_hidden", Entity: "settings", EntityID: "1",
			RequestID: httpx.RequestID(r.Context()), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
