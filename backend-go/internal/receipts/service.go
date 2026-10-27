package receipts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// maxRenderAttempts before a receipt is marked failed (the email then
// goes out with a download link instead of the attachment).
const maxRenderAttempts = 3

// Service renders and stores receipts.
type Service struct {
	sessions *tenancy.Sessions
	store    files.Storage
	log      *slog.Logger
	// Rendered observes render time (pos_receipt_render_seconds); optional.
	Rendered func(time.Duration)
}

// NewService creates the service.
func NewService(s *tenancy.Sessions, store files.Storage, log *slog.Logger) *Service {
	return &Service{sessions: s, store: store, log: log}
}

const (
	qCreate = `INSERT INTO receipts (sale_id, sale_version) VALUES ($1, $2)
		ON CONFLICT (sale_id, sale_version) DO NOTHING RETURNING id`
	qFind      = `SELECT id FROM receipts WHERE sale_id = $1 AND sale_version = $2`
	qState     = `SELECT status, file_key, created_at FROM receipts WHERE id = $1`
	qStateLock = qState + ` FOR UPDATE`
	qRendered  = `UPDATE receipts SET status = 'rendered', file_key = $2, sha256 = $3, size_bytes = $4,
		rendered_at = now(), last_error = NULL WHERE id = $1`
	qFailed = `UPDATE receipts SET render_attempts = render_attempts + 1, last_error = $2,
		status = CASE WHEN render_attempts + 1 >= $3 THEN 'failed' ELSE status END WHERE id = $1 AND status <> 'rendered'`
)

// Create adds the receipt row for a sale version in the caller's
// transaction (idempotent) and returns its ID.
func Create(ctx context.Context, tx tenancy.TenantTx, saleID int64, version int) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, qCreate, saleID, version).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, qFind, saleID, version).Scan(&id)
	}
	return id, err
}

func objectKey(tenantID string, created time.Time, saleID int64, version int) string {
	created = created.UTC()
	return fmt.Sprintf("tenants/%s/receipts/%04d/%02d/%d-v%d.pdf", tenantID, created.Year(), int(created.Month()), saleID, version)
}

// Render draws a receipt, stores the PDF and marks the row rendered. The
// receipt row stays locked while it renders, so the pre-render job and an
// on-demand download never render the same receipt twice; the second one
// gets the stored file. Data is read in t's session only.
func (s *Service) Render(ctx context.Context, t tenancy.Tenant, receiptID int64) ([]byte, error) {
	start := time.Now()
	var (
		pdf      []byte
		existing string
	)
	err := s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		var (
			st      string
			key     *string
			created time.Time
		)
		if err := tx.QueryRow(ctx, qStateLock, receiptID).Scan(&st, &key, &created); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if st == "rendered" && key != nil {
			existing = *key
			return nil
		}
		v, err := Load(ctx, tx, receiptID)
		if err != nil {
			return err
		}
		var logo []byte
		if v.LogoKey != "" {
			if rc, _, err := s.store.Get(ctx, v.LogoKey); err == nil {
				logo, _ = io.ReadAll(io.LimitReader(rc, 4<<20))
				_ = rc.Close()
			}
		}
		if pdf, err = RenderPDF(v, logo); err != nil {
			return err
		}
		objKey := objectKey(t.ID, created, v.SaleID, v.Version)
		if err := s.store.Put(ctx, objKey, "application/pdf", pdf); err != nil {
			return err
		}
		sum := sha256.Sum256(pdf)
		_, err = tx.Exec(ctx, qRendered, receiptID, objKey, sum[:], len(pdf))
		return err
	})
	switch {
	case errors.Is(err, ErrSuperseded):
		s.markFailed(ctx, t, receiptID, "superseded by a newer sale version", true)
		return nil, err
	case errors.Is(err, ErrNotFound):
		return nil, err
	case err != nil:
		s.markFailed(ctx, t, receiptID, "render failed", false)
		return nil, err
	case existing != "":
		return s.read(ctx, existing)
	}
	if s.Rendered != nil {
		s.Rendered(time.Since(start))
	}
	return pdf, nil
}

func (s *Service) read(ctx context.Context, key string) ([]byte, error) {
	rc, _, err := s.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, 16<<20))
}

func (s *Service) markFailed(ctx context.Context, t tenancy.Tenant, id int64, msg string, final bool) {
	attempts := maxRenderAttempts
	if final {
		attempts = 1
	}
	err := s.sessions.InTenantTx(context.WithoutCancel(ctx), t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		_, err := tx.Exec(ctx, qFailed, id, msg, attempts)
		return err
	})
	if err != nil {
		s.log.WarnContext(ctx, "receipt failure not recorded", slog.String("tenant_id", t.ID), slog.Int64("receipt_id", id), slog.Any("err", err))
	}
}

// Open returns the stored PDF, rendering it first when needed (or when the
// stored object is missing).
func (s *Service) Open(ctx context.Context, t tenancy.Tenant, receiptID int64) ([]byte, error) {
	var (
		status string
		key    *string
		ign    time.Time
	)
	err := s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(ctx, qState, receiptID).Scan(&status, &key, &ign)
	})
	if err != nil {
		return nil, err
	}
	if status == "rendered" && key != nil {
		b, err := s.read(ctx, *key)
		if err == nil {
			return b, nil
		}
		// The stored object is gone: render again.
		s.log.WarnContext(ctx, "stored receipt missing, rendering again", slog.String("tenant_id", t.ID), slog.Int64("receipt_id", receiptID))
		if err := s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			_, err := tx.Exec(ctx, `UPDATE receipts SET status = 'pending' WHERE id = $1`, receiptID)
			return err
		}); err != nil {
			return nil, err
		}
	}
	return s.Render(ctx, t, receiptID)
}
