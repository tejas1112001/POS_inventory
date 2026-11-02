package platform

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Final deletion step (S10 item 4, docs/S10_DESIGN.md §4). A tenant
// pending deletion is finalised by a Super Admin (tenants:delete) with
// step-up, a written reason and the slug typed, once its deletion date has
// come and its final export has passed a restore test. Effect: status
// deleted (the host stops resolving, so every login and session ends; the
// worker also revokes the shop's refresh tokens), the provider
// subscription is cancelled at once. The schema and files are not dropped.

// DeletionView is the deletion panel of a tenant.
type DeletionView struct {
	Status          string     `json:"status"`
	RequestedAt     *time.Time `json:"requestedAt"`
	DeletionDate    *time.Time `json:"deletionDate"`
	DeletedAt       *time.Time `json:"deletedAt"`
	FinalExportID   *string    `json:"finalExportId"`
	FinalExport     *string    `json:"finalExportStatus"`
	FinalExportAt   *time.Time `json:"finalExportAt"`
	RestoreTest     *string    `json:"restoreTestStatus"`
	RestoreTestAt   *time.Time `json:"restoreTestAt"`
	WarningsSent    []string   `json:"warningsSent"`
	CanFinalise     bool       `json:"canFinalise"`
	Blocker         string     `json:"blocker,omitempty"`
	ProviderToEnd   bool       `json:"providerToCancel"`
	DeletedByUserID *int64     `json:"deletedBy"`
}

const (
	qDeletionTenant = `SELECT t.status, t.slug::text, t.deletion_requested_at, t.purge_after, t.deleted_at, t.deleted_by
		FROM platform.tenants t WHERE t.id = $1`
	qDeletionTenantLock = qDeletionTenant + ` FOR UPDATE`
	// The newest final export since the deletion started, and its newest
	// restore test.
	qFinalExport = `SELECT e.id::text, e.status, e.finished_at, r.status, r.finished_at
		FROM platform.tenant_exports e
		LEFT JOIN LATERAL (SELECT status, finished_at FROM platform.tenant_restores
			WHERE export_id = e.id AND mode IN ('test','scratch') ORDER BY (status = 'succeeded') DESC, created_at DESC LIMIT 1) r ON true
		WHERE e.tenant_id = $1 AND e.kind = 'final' AND e.created_at >= coalesce($2::timestamptz, '-infinity'::timestamptz)
		ORDER BY (e.status = 'succeeded' AND r.status = 'succeeded') DESC, e.created_at DESC LIMIT 1`
	qProviderLive = `SELECT coalesce(provider_subscription_id, ''), coalesce(provider_status, '') FROM platform.subscriptions WHERE tenant_id = $1`
)

func (s *Service) deletionView(ctx context.Context, tx tenancy.PlatformTx, id string, lock bool) (DeletionView, string, error) {
	var v DeletionView
	var slug string
	var row pgx.Row
	if lock {
		row = tx.QueryRow(ctx, qDeletionTenantLock, id)
	} else {
		row = tx.QueryRow(ctx, qDeletionTenant, id)
	}
	err := row.Scan(&v.Status, &slug, &v.RequestedAt, &v.DeletionDate, &v.DeletedAt, &v.DeletedByUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, "", apperr.NotFound()
	}
	if err != nil {
		return v, "", err
	}
	err = tx.QueryRow(ctx, qFinalExport, id, v.RequestedAt).Scan(&v.FinalExportID, &v.FinalExport, &v.FinalExportAt, &v.RestoreTest, &v.RestoreTestAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, "", err
	}
	v.WarningsSent = []string{}
	rows, err := tx.Query(ctx, `SELECT kind FROM platform.notification_log WHERE tenant_id = $1 AND kind LIKE 'deletion\_warning\_%'
		AND period_key NOT LIKE '%:owner' ORDER BY created_at`, id)
	if err != nil {
		return v, "", err
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return v, "", err
		}
		v.WarningsSent = append(v.WarningsSent, strings.TrimPrefix(k, "deletion_warning_")+" days")
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return v, "", err
	}
	var sub, pstatus string
	if err := tx.QueryRow(ctx, qProviderLive, id).Scan(&sub, &pstatus); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, "", err
	}
	v.ProviderToEnd = sub != "" && pstatus != "cancelled" && pstatus != "completed" && pstatus != "expired"
	now := s.now()
	switch {
	case v.Status != "pending_deletion":
		v.Blocker = "Only a tenant pending deletion can be finalised."
	case v.DeletionDate == nil:
		v.Blocker = "The tenant has no deletion date."
	case now.Before(*v.DeletionDate):
		v.Blocker = "The deletion date is " + v.DeletionDate.UTC().Format("2 Jan 2006 15:04 UTC") + "."
	case v.FinalExport == nil || *v.FinalExport != "succeeded":
		v.Blocker = "The final export has not succeeded yet."
	case v.RestoreTest == nil || *v.RestoreTest != "succeeded":
		v.Blocker = "The final export's restore test has not passed yet (posctl tenant restore-tests)."
	}
	v.CanFinalise = v.Blocker == ""
	return v, slug, nil
}

// Deletion reads a tenant's deletion panel.
func (s *Service) Deletion(ctx context.Context, id string) (DeletionView, error) {
	if !tenancy.ValidTenantID(id) {
		return DeletionView{}, apperr.NotFound()
	}
	var v DeletionView
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		var err error
		v, _, err = s.deletionView(ctx, tx, id, false)
		return err
	})
	return v, err
}

// FinaliseDeletion ends a tenant for good (see above).
func (s *Service) FinaliseDeletion(ctx context.Context, a Actor, id, reason, confirm string, m Meta) (DeletionView, error) {
	if !tenancy.ValidTenantID(id) {
		return DeletionView{}, apperr.NotFound()
	}
	reason, err := reasonText(reason)
	if err != nil {
		return DeletionView{}, err
	}
	var out DeletionView
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		v, slug, err := s.deletionView(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(confirm), slug) {
			return apperr.Validation(map[string]string{"confirm": "type the tenant's subdomain exactly"})
		}
		if !v.CanFinalise {
			return apperr.Conflict(apperr.CodeInvalidState, v.Blocker)
		}
		var sub, pstatus string
		if err := tx.QueryRow(ctx, qProviderLive, id).Scan(&sub, &pstatus); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		cancelled := false
		if v.ProviderToEnd {
			if s.cfg.Billing == nil || s.cfg.Billing.Provider == nil {
				return apperr.Conflict(apperr.CodeInvalidState, "The payment provider is not configured, so its subscription cannot be cancelled here.")
			}
			if err := s.cfg.Billing.Provider.CancelSubscription(ctx, sub, false); err != nil {
				if s.cfg.Log != nil {
					s.cfg.Log.ErrorContext(ctx, "provider cancel at final deletion failed", "err", err)
				}
				return apperr.Unavailable()
			}
			cancelled = true
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET status = 'cancelled', cancelled_at = coalesce(cancelled_at, now()),
				cancel_at_period_end = false, pending_plan_id = NULL, pending_change_at = NULL,
				provider_status = CASE WHEN $2 THEN 'cancelled' ELSE provider_status END, updated_at = now()
			WHERE tenant_id = $1`, id, cancelled); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.tenants SET status = 'deleted', deleted_at = now(), deleted_by = $2, updated_at = now()
			WHERE id = $1`, id, a.UserID); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.finalise_deletion", TargetTenant: id,
			Before: map[string]any{"status": v.Status, "deletion_date": v.DeletionDate},
			After:  map[string]any{"status": "deleted", "reason": reason, "final_export": v.FinalExportID, "provider_cancelled": cancelled},
			IP:     m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID}); err != nil {
			return err
		}
		out, _, err = s.deletionView(ctx, tx, id, false)
		return err
	})
	if err == nil {
		s.flush()
	}
	return out, err
}

func (h *HTTP) mountDeletion(p chi.Router) {
	p.With(h.need(PermTenantsView)).Get("/tenants/{id}/deletion", h.deletion)
	p.With(h.need(PermTenantsDelete), h.stepUpRequired).Post("/tenants/{id}/finalise-deletion", h.finaliseDeletion)
}

func (h *HTTP) deletion(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Deletion(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *HTTP) finaliseDeletion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason  string `json:"reason"`
		Confirm string `json:"confirm"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	v, err := h.svc.FinaliseDeletion(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.Reason, in.Confirm, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}
