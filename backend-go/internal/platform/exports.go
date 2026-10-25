package platform

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

// Per-tenant exports and restore tests in the console (API_CONTRACT §5.2,
// OPERATIONS §6.2–6.3). posd only queues: the worker runs exports, and
// restore tests run in posctl, the only process that holds the age
// identity (EXPORT_AGE_IDENTITY). Archives are never downloadable here.

// mountExports registers the routes in the signed-in console group.
func (h *HTTP) mountExports(p chi.Router) {
	p.With(h.need(PermTenantsExport), h.stepUpRequired).Post("/tenants/{id}/exports", h.requestExport)
	p.With(h.need(PermTenantsExport)).Get("/tenants/{id}/exports", h.listExports)
	p.With(h.need(PermTenantsExport)).Post("/tenants/{id}/restore-tests", h.requestRestoreTest)
	p.With(h.need(PermTenantsExport)).Get("/tenants/{id}/restore-tests", h.listRestores)
}

// ExportView is one export in the console. The object key is left out:
// archives are never served by the API.
type ExportView struct {
	ID            uuid.UUID  `json:"id"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	IncludeFiles  bool       `json:"includeFiles"`
	RequestedBy   string     `json:"requestedBy"`
	SizeBytes     *int64     `json:"sizeBytes"`
	SHA256        *string    `json:"sha256"`
	SchemaVersion *int64     `json:"schemaVersion"`
	TableCount    *int       `json:"tableCount"`
	RowCount      *int64     `json:"rowCount"`
	FileCount     *int       `json:"fileCount"`
	Error         *string    `json:"error"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
}

// RestoreView is one restore (test, scratch or replace) in the console.
type RestoreView struct {
	ID          uuid.UUID       `json:"id"`
	ExportID    uuid.UUID       `json:"exportId"`
	Mode        string          `json:"mode"`
	Status      string          `json:"status"`
	RequestedBy string          `json:"requestedBy"`
	Verified    *bool           `json:"verified"` // rows and checksums matched the manifest
	Rows        *int64          `json:"rows"`
	Tables      json.RawMessage `json:"tables"` // per-table counts and checksums
	Error       *string         `json:"error"`
	CreatedAt   time.Time       `json:"createdAt"`
	StartedAt   *time.Time      `json:"startedAt"`
	FinishedAt  *time.Time      `json:"finishedAt"`
}

const (
	exportCols = `id, kind, status, include_files, requested_by, size_bytes, sha256, schema_version,
		table_count, row_count, file_count, error, created_at, started_at, finished_at`
	qExportByID      = `SELECT ` + exportCols + ` FROM platform.tenant_exports WHERE id = $1`
	qExportsOfTenant = `SELECT ` + exportCols + ` FROM platform.tenant_exports WHERE tenant_id = $1
		ORDER BY created_at DESC, id DESC LIMIT 50`
	restoreCols = `id, export_id, mode, status, requested_by, (result->>'ok')::boolean, (result->>'rows')::bigint,
		result->'tables', error, created_at, started_at, finished_at`
	qRestoreByID      = `SELECT ` + restoreCols + ` FROM platform.tenant_restores WHERE id = $1`
	qRestoresOfTenant = `SELECT ` + restoreCols + ` FROM platform.tenant_restores WHERE tenant_id = $1
		ORDER BY created_at DESC, id DESC LIMIT 50`
	qTenantKnown = `SELECT EXISTS (SELECT 1 FROM platform.tenants WHERE id = $1)`
)

func scanExport(row pgx.Row) (ExportView, error) {
	var v ExportView
	err := row.Scan(&v.ID, &v.Kind, &v.Status, &v.IncludeFiles, &v.RequestedBy, &v.SizeBytes, &v.SHA256, &v.SchemaVersion,
		&v.TableCount, &v.RowCount, &v.FileCount, &v.Error, &v.CreatedAt, &v.StartedAt, &v.FinishedAt)
	return v, err
}

func scanRestore(row pgx.Row) (RestoreView, error) {
	var v RestoreView
	var tables []byte
	err := row.Scan(&v.ID, &v.ExportID, &v.Mode, &v.Status, &v.RequestedBy, &v.Verified, &v.Rows, &tables, &v.Error,
		&v.CreatedAt, &v.StartedAt, &v.FinishedAt)
	if len(tables) > 0 {
		v.Tables = tables
	}
	return v, err
}

// requester is the requested_by of a console request.
func requester(a Actor) string {
	if len(a.Email) > 120 {
		return a.Email[:120]
	}
	return a.Email
}

// RequestExport queues an export of a tenant and starts its job.
func (s *Service) RequestExport(ctx context.Context, a Actor, id string, includeFiles bool, m Meta) (ExportView, error) {
	if !tenancy.ValidTenantID(id) {
		return ExportView{}, apperr.NotFound()
	}
	if !s.cfg.ExportsEnabled {
		return ExportView{}, apperr.Conflict(apperr.CodeInvalidState, "Exports are not configured on this server")
	}
	var v ExportView
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		eid, err := tenantio.Queue(ctx, tx, id, tenantio.KindManual, requester(a), includeFiles)
		if errors.Is(err, tenantio.ErrBusy) {
			return apperr.Conflict(apperr.CodeConflict, "An export of this tenant is already queued or running")
		}
		if err != nil {
			return err
		}
		if v, err = scanExport(tx.QueryRow(ctx, qExportByID, eid)); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.export", TargetTenant: id,
			Target: eid.String(), After: map[string]any{"export_id": eid.String(), "include_files": includeFiles},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if err != nil {
		return ExportView{}, err
	}
	if s.cfg.ExportQueued != nil {
		if err := s.cfg.ExportQueued(ctx, v.ID); err != nil {
			// The export sweep starts it later.
			s.cfg.Log.WarnContext(ctx, "export job not queued", slog.String("export_id", v.ID.String()), slog.Any("err", err))
		}
	}
	return v, nil
}

// ListExports lists a tenant's exports, newest first (at most 50).
func (s *Service) ListExports(ctx context.Context, id string) ([]ExportView, error) {
	out := []ExportView{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := s.knownTenant(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, qExportsOfTenant, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanExport(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// RequestRestoreTest queues a restore test of a tenant's export (exportID
// nil: the latest succeeded one). posctl tenant restore-tests runs it.
func (s *Service) RequestRestoreTest(ctx context.Context, a Actor, id string, exportID uuid.UUID, m Meta) (RestoreView, error) {
	if !tenancy.ValidTenantID(id) {
		return RestoreView{}, apperr.NotFound()
	}
	var v RestoreView
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if err := s.knownTenant(ctx, tx, id); err != nil {
			return err
		}
		rid, err := tenantio.RequestRestore(ctx, tx, id, exportID, tenantio.ModeTest, requester(a))
		if errors.Is(err, tenantio.ErrRestoreBusy) {
			return apperr.Conflict(apperr.CodeConflict, "A restore of this tenant is already queued or running")
		}
		if err != nil {
			return err // NotFound: no succeeded export, or not this tenant's
		}
		if v, err = scanRestore(tx.QueryRow(ctx, qRestoreByID, rid)); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.restore_test", TargetTenant: id,
			Target: rid.String(), After: map[string]any{"restore_id": rid.String(), "export_id": v.ExportID.String()},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return v, err
}

// ListRestores lists a tenant's restores (every mode), newest first.
func (s *Service) ListRestores(ctx context.Context, id string) ([]RestoreView, error) {
	out := []RestoreView{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := s.knownTenant(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, qRestoresOfTenant, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanRestore(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) knownTenant(ctx context.Context, tx tenancy.PlatformTx, id string) error {
	if !tenancy.ValidTenantID(id) {
		return apperr.NotFound()
	}
	var ok bool
	if err := tx.QueryRow(ctx, qTenantKnown, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.NotFound()
	}
	return nil
}

// --------------------------------------------------------------- handlers

func (h *HTTP) requestExport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IncludeFiles bool `json:"includeFiles"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	v, err := h.svc.RequestExport(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.IncludeFiles, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, v)
}

func (h *HTTP) listExports(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListExports(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

func (h *HTTP) requestRestoreTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExportID string `json:"exportId"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	var exportID uuid.UUID
	if in.ExportID != "" {
		var err error
		if exportID, err = uuid.Parse(in.ExportID); err != nil {
			httpx.WriteError(w, r, apperr.Invalid("exportId", "must be an export ID"))
			return
		}
	}
	v, err := h.svc.RequestRestoreTest(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), exportID, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, v)
}

func (h *HTTP) listRestores(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListRestores(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}
