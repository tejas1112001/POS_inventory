package plans

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Features that can be switched per tenant (docs/S9_SECURITY_NOTE.md).
// csv_import and new_checkout are not plan features: they are on unless
// an override turns them off.
const (
	FeatureCSVImport   = "csv_import"
	FeatureNewCheckout = "new_checkout"
)

// planGated features take their default from the plan; the others are on.
var planGated = map[string]bool{FeaturePublicShop: true, FeatureReportsExport: true}

// overrideSet is one tenant's active overrides.
type overrideSet struct {
	limits map[string]int64
	flags  map[string]bool
}

// overrideTTL keeps a change visible within half a minute on every
// instance; the console's own instance forgets at once (Forget).
const overrideTTL = 30 * time.Second

const qActiveOverrides = `SELECT key, value FROM platform.tenant_overrides
	WHERE tenant_id = $1 AND revoked_at IS NULL AND expires_at > now()`

func (s *Service) overrides(ctx context.Context, tenantID string) (overrideSet, error) {
	if o, ok := s.ovr.Get(tenantID); ok {
		return o, nil
	}
	o := overrideSet{limits: map[string]int64{}, flags: map[string]bool{}}
	err := s.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qActiveOverrides, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var raw []byte
			if err := rows.Scan(&key, &raw); err != nil {
				return err
			}
			var v any
			if json.Unmarshal(raw, &v) != nil {
				continue
			}
			switch x := v.(type) {
			case bool:
				o.flags[key] = x
			case float64:
				if x >= 1 {
					o.limits[key] = int64(x)
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		return o, err
	}
	s.ovr.Set(tenantID, o, overrideTTL)
	return o, nil
}

// Forget drops a tenant's cached overrides (after the console changes them).
func (s *Service) Forget(tenantID string) { s.ovr.Delete(tenantID) }

// LimitsFor is the tenant's plan limits with its active overrides applied.
// Every limit check uses it.
func (s *Service) LimitsFor(ctx context.Context, t tenancy.Tenant) (Limits, error) {
	l, err := s.For(ctx, t.PlanID)
	if err != nil {
		return l, err
	}
	o, err := s.overrides(ctx, t.ID)
	if err != nil {
		return l, err
	}
	for k, v := range o.limits {
		switch k {
		case "max_stores":
			l.MaxStores = v
		case "max_users":
			l.MaxUsers = v
		case "max_products":
			l.MaxProducts = v
		case "max_emails_per_day":
			l.MaxEmailsPerDay = v
		case "max_storage_mb":
			l.MaxStorageMB = v
		}
	}
	return l, nil
}

// HasFor reports whether the tenant may use a feature: an active flag
// override wins, then the plan (for plan features), else on.
func (s *Service) HasFor(ctx context.Context, t tenancy.Tenant, feature string) (bool, error) {
	o, err := s.overrides(ctx, t.ID)
	if err != nil {
		return false, err
	}
	if v, ok := o.flags[feature]; ok {
		return v, nil
	}
	if !planGated[feature] {
		return true, nil
	}
	return s.Has(ctx, t.PlanID, feature)
}

// RequireFeatureFor returns 403 FEATURE_NOT_IN_PLAN unless HasFor.
func (s *Service) RequireFeatureFor(ctx context.Context, t tenancy.Tenant, feature, what string) error {
	ok, err := s.HasFor(ctx, t, feature)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.Forbidden(apperr.CodeFeatureNotInPlan, "Your plan does not include "+what+". Contact support to use it.")
	}
	return nil
}
