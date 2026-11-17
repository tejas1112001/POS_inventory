package modules

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// cacheTTL matches the override cache: a toggle is seen everywhere within
// half a minute, and the instance that wrote it forgets at once.
const cacheTTL = 30 * time.Second

// Shop is a tenant's business type and switched-on modules.
type Shop struct {
	BusinessType string
	Modules      []string
}

// Service answers the entitled / enabled checks.
type Service struct {
	sessions *tenancy.Sessions
	plans    *plans.Service
	cache    *tenancy.TTLCache[string, Shop]
}

// NewService creates the service.
func NewService(s *tenancy.Sessions, p *plans.Service) *Service {
	return &Service{sessions: s, plans: p, cache: tenancy.NewTTLCache[string, Shop]()}
}

const qShop = `SELECT business_type, modules FROM settings WHERE id = 1`

// LoadShop reads the business type and modules in the caller's tenant
// transaction (no cache).
func LoadShop(ctx context.Context, tx tenancy.TenantTx) (Shop, error) {
	var s Shop
	err := tx.QueryRow(ctx, qShop).Scan(&s.BusinessType, &s.Modules)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shop{BusinessType: Retail, Modules: []string{}}, nil
	}
	if err != nil {
		return s, err
	}
	if s.Modules == nil {
		s.Modules = []string{}
	}
	return s, nil
}

// Shop returns the tenant's type and modules (cached).
func (s *Service) Shop(ctx context.Context, t tenancy.Tenant) (Shop, error) {
	if v, ok := s.cache.Get(t.ID); ok {
		return v, nil
	}
	var out Shop
	err := s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		var err error
		out, err = LoadShop(ctx, tx)
		return err
	})
	if err != nil {
		return out, err
	}
	s.cache.Set(t.ID, out, cacheTTL)
	return out, nil
}

// Forget drops a tenant's cached modules (after a settings write).
func (s *Service) Forget(tenantID string) { s.cache.Delete(tenantID) }

// Entitled returns the modules the tenant's plan and overrides allow, in
// registry order.
func (s *Service) Entitled(ctx context.Context, t tenancy.Tenant) ([]string, error) {
	out := []string{}
	for _, m := range Registry {
		ok, err := s.plans.HasModule(ctx, t, m.Feature)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, m.Key)
		}
	}
	return out, nil
}

// On reports whether a module is both entitled and enabled.
func (s *Service) On(ctx context.Context, t tenancy.Tenant, key string) (bool, error) {
	m, ok := Get(key)
	if !ok {
		return false, nil
	}
	shop, err := s.Shop(ctx, t)
	if err != nil {
		return false, err
	}
	if !contains(shop.Modules, key) {
		return false, nil
	}
	return s.plans.HasModule(ctx, t, m.Feature)
}

// ErrOff is the refusal for a module that is not entitled or not enabled.
func ErrOff(key string) error {
	label := key
	if m, ok := Get(key); ok {
		label = m.Label
	}
	return apperr.Forbidden(apperr.CodeModuleOff, label+" is turned off for this shop. The owner can turn it on in Settings.")
}

// Require returns 403 MODULE_OFF unless the module is on.
func (s *Service) Require(ctx context.Context, t tenancy.Tenant, key string) error {
	ok, err := s.On(ctx, t, key)
	if err != nil {
		return err
	}
	if !ok {
		return ErrOff(key)
	}
	return nil
}

// Guard is the route middleware for rbac.Requirement.Module. It runs after
// the RBAC guard, so the tenant is resolved and the caller signed in.
func (s *Service) Guard(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t, ok := tenancy.TenantFrom(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.NotFound())
				return
			}
			if err := s.Require(r.Context(), t, key); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Summary is what /api/auth/me and /api/settings add (A1).
type Summary struct {
	BusinessType    string   `json:"businessType"`
	Modules         []string `json:"modules"`         // switched on and entitled: what the shop can use
	EnabledModules  []string `json:"enabledModules"`  // switched on in settings (may include one not entitled)
	EntitledModules []string `json:"entitledModules"` // allowed by plan and overrides
}

// Summarize returns the tenant's summary.
func (s *Service) Summarize(ctx context.Context, t tenancy.Tenant) (Summary, error) {
	shop, err := s.Shop(ctx, t)
	if err != nil {
		return Summary{}, err
	}
	ent, err := s.Entitled(ctx, t)
	if err != nil {
		return Summary{}, err
	}
	return summary(shop, ent), nil
}

func summary(shop Shop, entitled []string) Summary {
	on := []string{}
	for _, k := range shop.Modules {
		if contains(entitled, k) {
			on = append(on, k)
		}
	}
	return Summary{BusinessType: shop.BusinessType, Modules: on, EnabledModules: shop.Modules, EntitledModules: entitled}
}

// SummaryOf builds a summary from values already read (settings write).
func SummaryOf(shop Shop, entitled []string) Summary { return summary(shop, entitled) }

func contains(list []string, k string) bool {
	for _, x := range list {
		if x == k {
			return true
		}
	}
	return false
}

// Execer is a tenant connection or transaction (provisioning uses a raw
// pgx.Tx, the settings write a tenancy.TenantTx).
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SeedPreset adds a business type's starting data where none exists yet
// (item 4: attribute definitions). It never overwrites or deletes.
func SeedPreset(ctx context.Context, tx Execer, businessType string) error {
	return seedAttributes(ctx, tx, businessType)
}

// seedAttributes is filled in by item 4 (typed attributes).
var seedAttributes = func(context.Context, Execer, string) error { return nil }
