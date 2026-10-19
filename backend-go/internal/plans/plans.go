// Package plans enforces plan limits (API_CONTRACT C-20): stores, users,
// products and storage. Limits come from platform.plans.limits for the
// tenant's plan; counts come from the tenant schema.
package plans

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Resources with a limit.
const (
	Stores   = "stores"
	Users    = "users"
	Products = "products"
)

// Limits of one plan. Zero means "no limit set".
type Limits struct {
	MaxStores       int64 `json:"max_stores"`
	MaxUsers        int64 `json:"max_users"`
	MaxProducts     int64 `json:"max_products"`
	MaxStorageMB    int64 `json:"max_storage_mb"`
	MaxEmailsPerDay int64 `json:"max_emails_per_day"`
}

// Service reads plan limits with a small cache.
type Service struct {
	sessions *tenancy.Sessions
	cache    *tenancy.TTLCache[int64, Limits]
	ttl      time.Duration
}

// NewService creates the service.
func NewService(s *tenancy.Sessions) *Service {
	return &Service{sessions: s, cache: tenancy.NewTTLCache[int64, Limits](), ttl: time.Minute}
}

// For returns the limits of a plan. Plan 0 (no plan) has no limits.
func (s *Service) For(ctx context.Context, planID int64) (Limits, error) {
	if planID == 0 {
		return Limits{}, nil
	}
	if l, ok := s.cache.Get(planID); ok {
		return l, nil
	}
	var raw []byte
	err := s.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		return tx.QueryRow(ctx, `SELECT limits FROM platform.plans WHERE id = $1`, planID).Scan(&raw)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Limits{}, nil
	}
	if err != nil {
		return Limits{}, err
	}
	var l Limits
	if err := json.Unmarshal(raw, &l); err != nil {
		return Limits{}, err
	}
	s.cache.Set(planID, l, s.ttl)
	return l, nil
}

func errLimit(what string) error {
	return apperr.Forbidden(apperr.CodePlanLimit, "Your plan does not allow more "+what+". Upgrade the plan to add more.")
}

// Check verifies that one more resource fits the plan. Call it inside the
// creating transaction: the per-tenant advisory lock serializes concurrent
// creates, so two requests cannot both take the last slot.
func (s *Service) Check(ctx context.Context, tx tenancy.TenantTx, resource string) error {
	t := tx.Tenant()
	l, err := s.For(ctx, t.PlanID)
	if err != nil {
		return err
	}
	var max int64
	switch resource {
	case Stores:
		max = l.MaxStores
	case Users:
		max = l.MaxUsers
	case Products:
		max = l.MaxProducts
	default:
		return errors.New("plans: unknown resource " + resource)
	}
	if max <= 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "limit:"+t.ID+":"+resource); err != nil {
		return err
	}
	var n int64
	switch resource {
	case Stores:
		err = tx.QueryRow(ctx, `SELECT count(*) FROM stores WHERE deleted_at IS NULL`).Scan(&n)
	case Users:
		err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&n)
	case Products:
		err = tx.QueryRow(ctx, `SELECT count(*) FROM products WHERE deleted_at IS NULL`).Scan(&n)
	}
	if err != nil {
		return err
	}
	if n >= max {
		return errLimit(resource)
	}
	return nil
}

// CheckStorage verifies that add bytes fit the plan's storage limit.
func (s *Service) CheckStorage(ctx context.Context, t tenancy.Tenant, add int64) error {
	l, err := s.For(ctx, t.PlanID)
	if err != nil || l.MaxStorageMB <= 0 {
		return err
	}
	var used int64
	err = s.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		return tx.QueryRow(ctx, `SELECT storage_bytes FROM platform.tenants WHERE id = $1`, t.ID).Scan(&used)
	})
	if err != nil {
		return err
	}
	if used+add > l.MaxStorageMB<<20 {
		return errLimit("file storage")
	}
	return nil
}

// AddStorage records storage use (negative to release).
func (s *Service) AddStorage(ctx context.Context, tenantID string, delta int64) error {
	return s.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tx.Exec(ctx, `UPDATE platform.tenants SET storage_bytes = greatest(0, storage_bytes + $2) WHERE id = $1`, tenantID, delta)
		return err
	})
}
