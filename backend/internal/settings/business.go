package settings

import (
	"log/slog"
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

// putBusiness is PUT /api/settings/business (modules:manage, owner only by
// default). Choosing a new type without a module list switches on the
// type's recommended modules; a list replaces the set. A newly switched-on
// module must be entitled. The type's starting data is seeded only where
// none exists. Nothing is deleted when a module goes off.
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
		if !modules.ValidType(v) {
			httpx.WriteError(w, r, apperr.Invalid("businessType", "Use "+modules.TypeChoices()+"."))
			return
		}
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
		after = before
		if in.BusinessType != nil {
			after.BusinessType = *in.BusinessType
		}
		switch {
		case in.Modules != nil:
			after.Modules = wanted
		case after.BusinessType != before.BusinessType:
			after.Modules = modules.Recommended(after.BusinessType)
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
		if _, err := tx.Exec(ctx, `UPDATE settings SET business_type = $1, modules = $2::text[], updated_at = now() WHERE id = 1`,
			after.BusinessType, after.Modules); err != nil {
			return err
		}
		if after.BusinessType != before.BusinessType {
			if err := modules.SeedPreset(ctx, tx, after.BusinessType); err != nil {
				return err
			}
		}
		// Industry P3 item 1 (§1.3): batch tracking starts or stops with the
		// module for every product that has it switched on.
		if was, is := slices.Contains(before.Modules, modules.BatchExpiry), slices.Contains(after.Modules, modules.BatchExpiry); was != is {
			ids, err := inventory.TrackedProducts(ctx, tx)
			if err != nil {
				return err
			}
			if is {
				err = inventory.StartTracking(ctx, tx, ids, p.UserID)
			} else {
				err = inventory.StopTracking(ctx, tx, ids, p.UserID)
			}
			if err != nil {
				return err
			}
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
	if after.BusinessType != before.BusinessType {
		// The console's copy (DB3). The tenant's own row is the truth; a
		// failure here only delays console filtering until the next write.
		if err := a.d.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			_, err := tx.Exec(ctx, `UPDATE platform.tenants SET business_type = $2, updated_at = now() WHERE id = $1`, t.ID, after.BusinessType)
			return err
		}); err != nil {
			httpx.LoggerFrom(ctx).WarnContext(ctx, "business type not copied to the platform registry", slog.Any("err", err))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, modules.SummaryOf(after, entitled))
}
