package tenantio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Export kinds.
const (
	KindManual  = "manual"
	KindNightly = "nightly"
	KindFinal   = "final"
)

// ErrBusy means the tenant already has a queued or running export.
var ErrBusy = errors.New("tenantio: an export is already queued or running")

// ErrNotQueued means the export is not waiting to run (taken or done).
var ErrNotQueued = errors.New("tenantio: export is not queued")

// staleAfter fails a running export that stopped reporting (a crash).
const staleAfter = 6 * time.Hour

// Exporter runs tenant exports (OPERATIONS §6.2).
type Exporter struct {
	Sessions    *tenancy.Sessions // platform records
	DSN         secrets.Secret    // snapshot login: pos_jobs, a member of the tenant roles
	Storage     files.Storage
	Recipient   age.Recipient
	ManifestKey []byte
	Log         *slog.Logger
	Now         func() time.Time
}

func (e *Exporter) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Exporter) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// Enabled reports whether exports are configured.
func (e *Exporter) Enabled() bool {
	return e != nil && e.Recipient != nil && len(e.ManifestKey) > 0 && e.Storage != nil
}

// ObjectKey is where an export is stored.
func ObjectKey(tenantID string, id uuid.UUID) string {
	return "exports/" + tenantID + "/" + id.String() + ".tar.zst.age"
}

const (
	qFailStale = `UPDATE platform.tenant_exports SET status = 'failed', error = 'interrupted', finished_at = now()
		WHERE tenant_id = $1 AND status = 'running' AND started_at < now() - make_interval(secs => $2)`
	qQueue = `INSERT INTO platform.tenant_exports (id, tenant_id, kind, include_files, requested_by)
		SELECT $1, t.id, $3, $4, $5 FROM platform.tenants t
		WHERE t.id = $2 AND t.status NOT IN ('provisioning','failed','deleted')
		ON CONFLICT DO NOTHING RETURNING id`
	qActive = `SELECT EXISTS (SELECT 1 FROM platform.tenant_exports WHERE tenant_id = $1 AND status IN ('queued','running'))`
)

// Queue records a queued export in tx. It fails with ErrBusy if one is
// queued or running, and NotFound for an unknown or unexportable tenant.
func Queue(ctx context.Context, tx tenancy.PlatformTx, tenantID, kind, requestedBy string, includeFiles bool) (uuid.UUID, error) {
	if !tenancy.ValidTenantID(tenantID) {
		return uuid.Nil, apperr.NotFound()
	}
	if _, err := tx.Exec(ctx, qFailStale, tenantID, staleAfter.Seconds()); err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	var got uuid.UUID
	err = tx.QueryRow(ctx, qQueue, id, tenantID, kind, includeFiles, requestedBy).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		var busy bool
		if err := tx.QueryRow(ctx, qActive, tenantID).Scan(&busy); err != nil {
			return uuid.Nil, err
		}
		if busy {
			return uuid.Nil, ErrBusy
		}
		return uuid.Nil, apperr.NotFound()
	}
	return got, err
}

// Record is a finished export.
type Record struct {
	ID            uuid.UUID
	TenantID      string
	ObjectKey     string
	Summary       Summary
	SchemaVersion int64
	Tables, Files int
	Rows          int64
	Missing       []string
}

const (
	qClaim = `UPDATE platform.tenant_exports e SET status = 'running', started_at = now()
		FROM platform.tenants t
		WHERE e.id = $1 AND e.status = 'queued' AND t.id = e.tenant_id
		RETURNING e.tenant_id, e.include_files, t.slug::text`
	qSucceeded = `UPDATE platform.tenant_exports SET status = 'succeeded', finished_at = now(), object_key = $2,
		size_bytes = $3, sha256 = $4, manifest_sha256 = $5, schema_version = $6, table_count = $7, row_count = $8,
		file_count = $9, error = $10 WHERE id = $1`
	qFailed = `UPDATE platform.tenant_exports SET status = 'failed', finished_at = now(), error = $2 WHERE id = $1`
)

// Run executes one queued export: running, then succeeded or failed. A
// failed export leaves no object behind.
func (e *Exporter) Run(ctx context.Context, id uuid.UUID) (Record, error) {
	if !e.Enabled() {
		return Record{}, errors.New("tenantio: exports are not configured")
	}
	var rec Record
	var slug string
	var withFiles bool
	err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		return tx.QueryRow(ctx, qClaim, id).Scan(&rec.TenantID, &withFiles, &slug)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotQueued
	}
	if err != nil {
		return Record{}, err
	}
	rec.ID, rec.ObjectKey = id, ObjectKey(rec.TenantID, id)
	err = e.export(ctx, &rec, slug, withFiles)
	fctx := context.WithoutCancel(ctx)
	if err != nil {
		_ = e.Storage.Delete(fctx, rec.ObjectKey)
		msg := sanitize(err)
		_ = e.Sessions.InPlatformTx(fctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			_, err := tx.Exec(fctx, qFailed, id, msg)
			return err
		})
		e.log().ErrorContext(ctx, "tenant export failed", slog.String("tenant_id", rec.TenantID),
			slog.String("export_id", id.String()), slog.String("err", msg))
		return rec, err
	}
	var note *string
	if len(rec.Missing) > 0 {
		s := fmt.Sprintf("%d files were missing from storage", len(rec.Missing))
		note = &s
	}
	err = e.Sessions.InPlatformTx(fctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tx.Exec(fctx, qSucceeded, id, rec.ObjectKey, rec.Summary.Size, rec.Summary.SHA256, rec.Summary.ManifestSHA256,
			rec.SchemaVersion, rec.Tables, rec.Rows, rec.Files, note)
		return err
	})
	return rec, err
}

// sanitize keeps an error short and free of connection strings.
func sanitize(err error) string {
	s := err.Error()
	if i := strings.Index(s, "postgres://"); i >= 0 {
		s = s[:i] + "[dsn]"
	}
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

const (
	qSchemaVersion = `SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied`
	qFileKeys      = `SELECT storage_key FROM files ORDER BY storage_key`
	qReceiptKeys   = `SELECT file_key FROM receipts WHERE file_key IS NOT NULL ORDER BY file_key`
)

// export writes the archive and streams it to storage.
func (e *Exporter) export(ctx context.Context, rec *Record, slug string, withFiles bool) error {
	schema, role, err := tenancy.SchemaFor(rec.TenantID)
	if err != nil {
		return err
	}
	cfg, err := migrate.SnapshotConfig(e.DSN, schema, role, "posd-export")
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("tenantio: connect: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	// One consistent snapshot of the whole schema.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := tx.QueryRow(ctx, qSchemaVersion).Scan(&rec.SchemaVersion); err != nil {
		return err
	}
	tables, err := plan(ctx, tx)
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	upload := make(chan error, 1)
	go func() {
		_, err := e.Storage.PutStream(ctx, rec.ObjectKey, "application/octet-stream", pr)
		_ = pr.CloseWithError(err) // unblock the writer if the upload stops
		upload <- err
	}()
	fail := func(err error) error {
		_ = pw.CloseWithError(err)
		<-upload
		return err
	}
	w, err := NewWriter(pw, e.Recipient, e.ManifestKey, e.now())
	if err != nil {
		return fail(err)
	}
	for _, t := range tables {
		q, err := db.CopyOutSQL(t.Name, t.Columns, t.OrderBy)
		if err != nil {
			return fail(err)
		}
		tw, err := w.Table(t.Name, t.Columns, t.OrderBy)
		if err != nil {
			return fail(err)
		}
		tag, err := copyTo(ctx, tx, tw, q)
		if err != nil {
			return fail(fmt.Errorf("tenantio: export %s: %w", t.Name, err))
		}
		if err := tw.Finish(tag.RowsAffected()); err != nil {
			return fail(err)
		}
		rec.Rows += tag.RowsAffected()
	}
	if withFiles {
		if err := e.files(ctx, tx, w, rec); err != nil {
			return fail(err)
		}
	}
	excluded := make([]string, 0, len(Excluded))
	for name := range Excluded {
		excluded = append(excluded, name)
	}
	slices.Sort(excluded)
	settings := map[string]string{}
	for k, v := range migrate.ExportSettings {
		settings[k] = v
	}
	m, sum, err := w.Close(Manifest{TenantID: rec.TenantID, Slug: slug, SchemaVersion: rec.SchemaVersion, ExportedAt: e.now(),
		AppVersion: buildinfo.Version, Settings: settings, Excluded: excluded, Missing: rec.Missing})
	if err != nil {
		return fail(err)
	}
	if err := pw.Close(); err != nil {
		return err
	}
	if err := <-upload; err != nil {
		return fmt.Errorf("tenantio: upload: %w", err)
	}
	rec.Summary, rec.Tables, rec.Files = sum, len(m.Tables), len(m.Files)
	return nil
}

func copyTo(ctx context.Context, tx pgx.Tx, w io.Writer, q string) (pgconn.CommandTag, error) {
	return tx.Conn().PgConn().CopyTo(ctx, w, q)
}

// files adds the tenant's objects. Only keys under tenants/<id>/ are
// read: a row pointing elsewhere is never followed (it is recorded as
// missing), so one tenant's export cannot carry another tenant's file.
func (e *Exporter) files(ctx context.Context, tx pgx.Tx, w *Writer, rec *Record) error {
	var keys []string
	fk, err := texts(tx.Query(ctx, qFileKeys))
	if err != nil {
		return err
	}
	rk, err := texts(tx.Query(ctx, qReceiptKeys))
	if err != nil {
		return err
	}
	keys = append(append(keys, fk...), rk...)
	slices.Sort(keys)
	prefix := "tenants/" + rec.TenantID + "/"
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		if !strings.HasPrefix(k, prefix) || !validFileKey(k) {
			e.log().WarnContext(ctx, "tenant export: file key outside the tenant's prefix skipped",
				slog.String("tenant_id", rec.TenantID))
			rec.Missing = append(rec.Missing, outsidePrefix)
			continue
		}
		rc, size, err := e.Storage.Get(ctx, k)
		if errors.Is(err, files.ErrNotFound) {
			rec.Missing = append(rec.Missing, k)
			continue
		}
		if err != nil {
			return err
		}
		err = w.File(k, size, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
