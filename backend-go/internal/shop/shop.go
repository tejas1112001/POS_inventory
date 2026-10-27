// Package shop serves the public product showcase of a store
// (PHASE1_DESIGN step 15; API_CONTRACT §2.10, C-18; THREAT_MODEL §3.10).
// No sign-in: the tenant comes from the host, the store from its slug in
// that tenant (never a numeric ID), and only products published in that
// store are shown, through an explicit public allow-list (no costs,
// suppliers or stock counts). Every route is rate limited per IP and per
// tenant and sends cache headers.
package shop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Defaults for Deps.
const (
	DefaultPerIP     = 120  // requests per minute per client IP
	DefaultPerTenant = 3000 // requests per minute per tenant
	MaxLimit         = 48   // products per page (C-18)
	DefaultLimit     = 24
	FeaturedLimit    = 8
	maxSearch        = 100
	cacheControl     = "public, max-age=60"
	homeCacheControl = "no-cache" // a settings change shows at once; the ETag still saves the body
)

// Deps are the shop dependencies.
type Deps struct {
	Sessions  *tenancy.Sessions
	Plans     *plans.Service
	Network   httpx.Network
	Metrics   *obs.Metrics // optional
	PerIP     int          // 0: DefaultPerIP
	PerTenant int          // 0: DefaultPerTenant
}

// API holds the handlers.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API {
	if d.PerIP <= 0 {
		d.PerIP = DefaultPerIP
	}
	if d.PerTenant <= 0 {
		d.PerTenant = DefaultPerTenant
	}
	return &API{d: d}
}

func (a *API) limiter(name string, n int, key func(*http.Request) string) func(http.Handler) http.Handler {
	return httprate.NewRateLimiter(n, time.Minute,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) { return name + ":" + key(r), nil }),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			if a.d.Metrics != nil {
				a.d.Metrics.RateLimited.WithLabelValues(name).Inc()
			}
			httpx.WriteError(w, r, apperr.RateLimited(apperr.CodeRateLimited, time.Minute))
		})).Handler
}

// Mount registers the public shop routes.
func (a *API) Mount(rt *rbac.Router) {
	byIP := a.limiter("shop_ip", a.d.PerIP, func(r *http.Request) string { return a.d.Network.ClientIP(r).String() })
	byTenant := a.limiter("shop_tenant", a.d.PerTenant, func(r *http.Request) string {
		t, _ := tenancy.TenantFrom(r.Context())
		return t.ID
	})
	wrap := func(h http.HandlerFunc) http.HandlerFunc { return byIP(byTenant(h)).ServeHTTP }
	pub := rbac.Public()
	rt.Handle(http.MethodGet, "/public/home", pub, wrap(a.home))
	rt.Handle(http.MethodGet, "/public/shops", pub, wrap(a.shops))
	rt.Handle(http.MethodGet, "/public/shop/{slug}", pub, wrap(a.info))
	rt.Handle(http.MethodGet, "/public/shop/{slug}/products", pub, wrap(a.products))
	rt.Handle(http.MethodGet, "/public/shop/{slug}/products/{product}", pub, wrap(a.product))
	rt.Handle(http.MethodGet, "/public/shop/{slug}/categories", pub, wrap(a.categories))
	rt.Handle(http.MethodGet, "/public/shop/{slug}/featured", pub, wrap(a.featured))
}

// hiddenStatuses are tenants whose shop is not shown (BILLING_DESIGN §3:
// no reads at all). read_only and the grace states keep their shop.
var hiddenStatuses = map[string]bool{"suspended": true, "pending_deletion": true}

var slugRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,58}[A-Za-z0-9])?$`)

// store is the public part of a store row.
type store struct {
	id                          int64
	name, slug                  string
	address, phone, desc, hours *string
	logo, banner                *uuid.UUID
	public                      bool
}

const qStore = `SELECT id, name, slug::text, address, phone, description, opening_hours, logo_file_id, banner_file_id,
		is_public_enabled
	FROM stores WHERE slug = $1 AND deleted_at IS NULL AND status = 'Active'`

// visible checks the tenant (status, plan feature) and finds the store.
// Anything not shown is the same generic 404 as an unknown shop.
func (a *API) visible(r *http.Request, tx tenancy.TenantTx) (store, error) {
	t := tx.Tenant()
	if hiddenStatuses[t.Status] {
		return store{}, apperr.NotFound()
	}
	ok, err := a.d.Plans.Has(r.Context(), t.PlanID, plans.FeaturePublicShop)
	if err != nil {
		return store{}, err
	}
	slug := chi.URLParam(r, "slug")
	if !ok || !slugRe.MatchString(slug) {
		return store{}, apperr.NotFound()
	}
	var s store
	err = tx.QueryRow(r.Context(), qStore, slug).Scan(&s.id, &s.name, &s.slug, &s.address, &s.phone, &s.desc, &s.hours,
		&s.logo, &s.banner, &s.public)
	if errors.Is(err, pgx.ErrNoRows) {
		return store{}, apperr.NotFound()
	}
	return s, err
}

// serve runs fn in a read-only transaction for a published shop and
// writes the result with cache headers. needPublic false lets the shop
// info answer for a disabled showcase.
func (a *API) serve(w http.ResponseWriter, r *http.Request, needPublic bool,
	fn func(tx tenancy.TenantTx, s store, b settings.Business) (any, error)) {
	t, _ := tenancy.TenantFrom(r.Context())
	var out any
	err := a.d.Sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		s, err := a.visible(r, tx)
		if err != nil {
			return err
		}
		if needPublic && !s.public {
			return apperr.NotFound()
		}
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		out, err = fn(tx, s, b)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeCached(w, r, out)
}

// writeCached sends JSON with Cache-Control and a strong ETag, and 304
// when the client already has it.
func writeCached(w http.ResponseWriter, r *http.Request, v any) {
	writeCachedAs(w, r, v, cacheControl)
}

func writeCachedAs(w http.ResponseWriter, r *http.Request, v any, cc string) {
	body, err := json.Marshal(v)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := w.Header()
	h.Set("Cache-Control", cc)
	h.Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" {
		for _, e := range strings.Split(match, ",") {
			if e = strings.TrimSpace(e); e == etag || e == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func publicURL(id *uuid.UUID, purpose string) string {
	if id == nil {
		return ""
	}
	return files.URL(*id, purpose)
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ------------------------------------------------------------------ info

// Info is the shop header.
type Info struct {
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	Address         string `json:"address,omitempty"`
	Logo            string `json:"logo,omitempty"`
	Banner          string `json:"banner,omitempty"`
	Description     string `json:"description,omitempty"`
	Phone           string `json:"phone,omitempty"`
	OpeningHours    string `json:"openingHours,omitempty"`
	IsPublicEnabled bool   `json:"isPublicEnabled"`
	Message         string `json:"message,omitempty"`
}

// info is GET /api/public/shop/{slug}. A disabled showcase answers with
// isPublicEnabled false and a message, as before.
func (a *API) info(w http.ResponseWriter, r *http.Request) {
	a.serve(w, r, false, func(_ tenancy.TenantTx, s store, _ settings.Business) (any, error) {
		if !s.public {
			return Info{Name: s.name, Slug: s.slug, Message: "This shop showcase is currently unavailable."}, nil
		}
		return Info{Name: s.name, Slug: s.slug, Address: str(s.address), Logo: publicURL(s.logo, files.PurposeStoreLogo),
			Banner: publicURL(s.banner, files.PurposeStoreBanner), Description: str(s.desc), Phone: str(s.phone),
			OpeningHours: str(s.hours), IsPublicEnabled: true}, nil
	})
}

// ------------------------------------------------------------------ home

// Home is GET /api/public/home: what the tenant's main address shows.
type Home struct {
	HomePage string `json:"homePage"`       // "shop" or "login"
	Slug     string `json:"slug,omitempty"` // the store whose shop is shown, with "shop"
}

// home answers "login" whenever no shop is shown: home page set to the
// staff sign-in, tenant suspended, plan without public_shop, or no active
// public store. The answer does not say which. Otherwise it names the
// default store (settings.DefaultStore), which falls back to the next
// active public store when the chosen one is turned off or inactive.
func (a *API) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, _ := tenancy.TenantFrom(ctx)
	out := Home{HomePage: settings.HomeLogin}
	if !hiddenStatuses[t.Status] {
		ok, err := a.d.Plans.Has(ctx, t.PlanID, plans.FeaturePublicShop)
		if err == nil && ok {
			err = a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
				var page string
				if err := tx.QueryRow(ctx, `SELECT home_page FROM settings WHERE id = 1`).Scan(&page); err != nil {
					return err
				}
				if page != settings.HomeShop {
					return nil
				}
				_, slug, found, err := settings.DefaultStore(ctx, tx)
				if found {
					out = Home{HomePage: settings.HomeShop, Slug: slug}
				}
				return err
			})
		}
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	writeCachedAs(w, r, out, homeCacheControl)
}

// ------------------------------------------------------------------ shops

// StoreRef is one entry of the store switcher.
type StoreRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// qShops lists the store switcher (at most 50; plans allow far fewer).
const qShops = `SELECT name, slug::text FROM stores
	WHERE deleted_at IS NULL AND status = 'Active' AND is_public_enabled
	ORDER BY name, id LIMIT 50`

// shops is GET /api/public/shops: the tenant's active public stores, for
// the storefront's store switcher. Hidden tenants and plans without
// public_shop get the generic 404, like a shop.
func (a *API) shops(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, _ := tenancy.TenantFrom(ctx)
	ok, err := a.d.Plans.Has(ctx, t.PlanID, plans.FeaturePublicShop)
	if err == nil && (!ok || hiddenStatuses[t.Status]) {
		err = apperr.NotFound()
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := []StoreRef{}
	err = a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		rows, err := tx.Query(ctx, qShops)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s StoreRef
			if err := rows.Scan(&s.Name, &s.Slug); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeCachedAs(w, r, map[string]any{"stores": out}, homeCacheControl)
}
