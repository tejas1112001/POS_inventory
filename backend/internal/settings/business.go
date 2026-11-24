package settings

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Business type and modules (docs/INDUSTRY_PHASE1_DESIGN.md item 1, A2, A3).

// ModuleView is one row of GET /api/modules.
type ModuleView struct {
	modules.Module
	Entitled bool `json:"entitled"`
	Enabled  bool `json:"enabled"`
}

// ModulesJSON is GET /api/modules.
type ModulesJSON struct {
	BusinessType  string                 `json:"businessType"`
	BusinessTypes []modules.BusinessType `json:"businessTypes"`
	Modules       []ModuleView           `json:"modules"`
	Recommended   map[string][]string    `json:"recommended"` // per business type
}

func (a *API) listModules(w http.ResponseWriter, r *http.Request) {
	t := tenant(r)
	shop, err := a.d.Modules.Shop(r.Context(), t)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ent, err := a.d.Modules.Entitled(r.Context(), t)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := ModulesJSON{BusinessType: shop.BusinessType, BusinessTypes: modules.BusinessTypes, Recommended: map[string][]string{}}
	for _, m := range modules.Registry {
		out.Modules = append(out.Modules, ModuleView{Module: m, Entitled: slices.Contains(ent, m.Key), Enabled: slices.Contains(shop.Modules, m.Key)})
	}
	for _, b := range modules.BusinessTypes {
		out.Recommended[b.Key] = modules.Recommended(b.Key)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

type businessIn struct {
	BusinessType *string   `json:"businessType"`
	Modules      *[]string `json:"modules"`
}

// errTypeByPlatform: the business type is set in the platform console only
// (SetBusinessType); a shop, its owner included, cannot change it.
var errTypeByPlatform = apperr.Forbidden(apperr.CodeForbidden, "The business type is set by support. Contact them to change it.")

// putBusiness is PUT /api/settings/business (modules:manage, owner only by
// default): the shop's module set. A list replaces the set; a newly
// switched-on module must be entitled. Nothing is deleted when a module
// goes off. A businessType other than the current one is refused (403):
// only the platform console changes it.
func (a *API) putBusiness(w http.ResponseWriter, r *http.Request) {
	var in businessIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, p := tenant(r), principal(r)
	ctx := r.Context()
	if in.BusinessType != nil {
		v := strings.ToLower(strings.TrimSpace(*in.BusinessType))
		in.BusinessType = &v
	}
	var wanted []string
	if in.Modules != nil {
		var err error
		if wanted, err = modules.Normalize(modules.Registry, *in.Modules); err != nil {
			httpx.WriteError(w, r, apperr.Invalid("modules", "Not allowed: "+err.Error()+"."))
			return
		}
	}
	entitled, err := a.d.Modules.Entitled(ctx, t)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var before, after modules.Shop
	err = a.rw(ctx, t, func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(ctx, `SELECT business_type, modules FROM settings WHERE id = 1 FOR UPDATE`).
			Scan(&before.BusinessType, &before.Modules); err != nil {
			return err
		}
		if before.Modules == nil {
			before.Modules = []string{}
		}
		if in.BusinessType != nil && *in.BusinessType != before.BusinessType {
			return errTypeByPlatform
		}
		after = before
		if in.Modules != nil {
			after.Modules = wanted
		}
		for _, k := range after.Modules {
			if !slices.Contains(before.Modules, k) && !slices.Contains(entitled, k) {
				m, _ := modules.Get(k)
				return apperr.Forbidden(apperr.CodeFeatureNotInPlan, "Your plan does not include "+m.Label+". Contact support to use it.")
			}
		}
		if after.BusinessType == before.BusinessType && slices.Equal(after.Modules, before.Modules) {
			return nil
		}
		if err := writeShop(ctx, tx, before, after, p.UserID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "settings.business_update", Entity: "settings", EntityID: "1",
			Before:    map[string]any{"businessType": before.BusinessType, "modules": before.Modules},
			After:     map[string]any{"businessType": after.BusinessType, "modules": after.Modules},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a.d.Modules.Forget(t.ID)
	httpx.WriteJSON(w, http.StatusOK, modules.SummaryOf(after, entitled))
}

// writeShop saves a new type and module set with their side effects: the
// type's starting data is seeded where none exists, and (industry P3 item
// 1, §1.3) batch tracking starts or stops with the batch module for every
// product that has it switched on. userID 0: not a shop user (the console).
func writeShop(ctx context.Context, tx tenancy.TenantTx, before, after modules.Shop, userID int64) error {
	if _, err := tx.Exec(ctx, `UPDATE settings SET business_type = $1, modules = $2::text[], updated_at = now() WHERE id = 1`,
		after.BusinessType, after.Modules); err != nil {
		return err
	}
	if after.BusinessType != before.BusinessType {
		if err := modules.SeedPreset(ctx, tx, after.BusinessType); err != nil {
			return err
		}
	}
	if was, is := slices.Contains(before.Modules, modules.BatchExpiry), slices.Contains(after.Modules, modules.BatchExpiry); was != is {
		ids, err := inventory.TrackedProducts(ctx, tx)
		if err != nil {
			return err
		}
		if is {
			return inventory.StartTracking(ctx, tx, ids, userID)
		}
		return inventory.StopTracking(ctx, tx, ids, userID)
	}
	return nil
}

// SetBusinessType is the platform console's change of a shop's business
// type (the shop cannot change its own). The type's recommended modules
// that the plan allows replace the module set, its starting data is seeded
// where none exists, and batch tracking follows the batch module. The
// shop's audit log records a system change naming the console user (by);
// the caller writes the platform audit entry and updates the console's
// copy of the type. Returns the type before (no change when it is the same).
func SetBusinessType(ctx context.Context, sessions *tenancy.Sessions, mods *modules.Service, t tenancy.Tenant, businessType, by string) (string, error) {
	businessType = strings.ToLower(strings.TrimSpace(businessType))
	if !modules.ValidType(businessType) {
		return "", apperr.Invalid("businessType", "Use "+modules.TypeChoices()+".")
	}
	entitled, err := mods.Entitled(ctx, t)
	if err != nil {
		return "", err
	}
	var before modules.Shop
	err = sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(ctx, `SELECT business_type, modules FROM settings WHERE id = 1 FOR UPDATE`).
			Scan(&before.BusinessType, &before.Modules); err != nil {
			return err
		}
		if before.Modules == nil {
			before.Modules = []string{}
		}
		if before.BusinessType == businessType {
			return nil
		}
		after := modules.Shop{BusinessType: businessType, Modules: []string{}}
		for _, k := range modules.Recommended(businessType) {
			if slices.Contains(entitled, k) {
				after.Modules = append(after.Modules, k)
			}
		}
		if err := writeShop(ctx, tx, before, after, 0); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorType: audit.ActorSystem, Action: "settings.business_update", Entity: "settings", EntityID: "1",
			Before:    map[string]any{"businessType": before.BusinessType, "modules": before.Modules},
			After:     map[string]any{"businessType": after.BusinessType, "modules": after.Modules, "by": "platform console", "platformUser": by},
			RequestID: httpx.RequestID(ctx)})
	})
	if err != nil {
		return "", err
	}
	mods.Forget(t.ID)
	return before.BusinessType, nil
}
