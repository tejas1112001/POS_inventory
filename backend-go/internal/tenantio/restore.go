package tenantio

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Restore modes (OPERATIONS §6.3).
const (
	ModeTest    = "test"    // into scratch, compare, drop
	ModeScratch = "scratch" // into scratch, compare, keep until drop_after
	ModeReplace = "replace" // into scratch, compare, then swap with the live schema
)

// ErrRestoreBusy means the tenant already has a queued or running restore.
var ErrRestoreBusy = errors.New("tenantio: a restore is already queued or running")

// ErrMismatch means the restored data does not equal the export.
var ErrMismatch = errors.New("tenantio: restored data does not match the export")

// Restorer restores exports. Only posctl builds one: it holds the age
// identity, which the API process never has.
type Restorer struct {
	DSN         secrets.Secret // DATABASE_MIGRATOR_URL
	Storage     files.Storage
	Identity    age.Identity
	ManifestKey []byte
	KeepFor     time.Duration // scratch mode (default 7 days)
	Log         *slog.Logger
	Now         func() time.Time

	afterLoad func(context.Context, tenancy.ScratchSchema) error // tests only
}

func (r *Restorer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Restorer) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// TableCheck compares one table of the restore with the manifest.
type TableCheck struct {
	Name       string `json:"name"`
	Rows       int64  `json:"rows"`
	RowsLoaded int64  `json:"rows_loaded"`
	RowsRead   int64  `json:"rows_read"`
	SHA256     string `json:"sha256"`
	SHA256Read string `json:"sha256_read"`
	OK         bool   `json:"ok"`
}

// Report is the outcome of one restore.
type Report struct {
	ID            uuid.UUID    `json:"-"`
	ExportID      uuid.UUID    `json:"export_id"`
	TenantID      string       `json:"tenant_id"`
	Mode          string       `json:"mode"`
	Scratch       string       `json:"scratch_schema"`
	SchemaVersion int64        `json:"schema_version"`
	MigratedTo    int64        `json:"migrated_to"`
	Tables        []TableCheck `json:"tables"`
	Files         int          `json:"files"`
	Rows          int64        `json:"rows"`
	OK            bool         `json:"ok"`
	Kept          bool         `json:"kept"`
	OldSchema     string       `json:"old_schema,omitempty"`
	DurationMS    int64        `json:"duration_ms"`
}

const (
	qLatestExport = `SELECT id FROM platform.tenant_exports WHERE tenant_id = $1 AND status = 'succeeded'
		ORDER BY finished_at DESC, id DESC LIMIT 1`
	qRequestRestore = `INSERT INTO platform.tenant_restores (id, export_id, tenant_id, mode, requested_by)
		SELECT $1, e.id, e.tenant_id, $4, $5 FROM platform.tenant_exports e
		WHERE e.id = $2 AND e.tenant_id = $3 AND e.status = 'succeeded'
		ON CONFLICT DO NOTHING RETURNING id`
	qRestoreActive     = `SELECT EXISTS (SELECT 1 FROM platform.tenant_restores WHERE tenant_id = $1 AND status IN ('queued','running'))`
	qFailStaleRestores = `UPDATE platform.tenant_restores SET status = 'failed', error = 'interrupted', finished_at = now()
		WHERE tenant_id = $1 AND status = 'running' AND started_at < now() - make_interval(secs => $2)`
)

// RequestRestore queues a restore of a tenant's export in tx (exportID nil:
// the latest succeeded export). The export must belong to the tenant.
func RequestRestore(ctx context.Context, tx tenancy.PlatformTx, tenantID string, exportID uuid.UUID, mode, requestedBy string) (uuid.UUID, error) {
	if !tenancy.ValidTenantID(tenantID) || (mode != ModeTest && mode != ModeScratch) {
		return uuid.Nil, apperr.NotFound()
	}
	if _, err := tx.Exec(ctx, qFailStaleRestores, tenantID, staleAfter.Seconds()); err != nil {
		return uuid.Nil, err
	}
	if exportID == uuid.Nil {
		err := tx.QueryRow(ctx, qLatestExport, tenantID).Scan(&exportID)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apperr.NotFound()
		}
		if err != nil {
			return uuid.Nil, err
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	var got uuid.UUID
	err = tx.QueryRow(ctx, qRequestRestore, id, exportID, tenantID, mode, requestedBy).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		var busy bool
		if err := tx.QueryRow(ctx, qRestoreActive, tenantID).Scan(&busy); err != nil {
			return uuid.Nil, err
		}
		if busy {
			return uuid.Nil, ErrRestoreBusy
		}
		return uuid.Nil, apperr.NotFound() // no such export of this tenant
	}
	return got, err
}

// exportRef is the stored export a restore reads.
type exportRef struct {
	id                       uuid.UUID
	tenantID, key, sha, mSHA string
	size, version            int64
}

const (
	qClaimRestore = `UPDATE platform.tenant_restores r SET status = 'running', started_at = now()
		FROM platform.tenant_exports e
		WHERE r.id = $1 AND r.status = 'queued' AND e.id = r.export_id AND e.tenant_id = r.tenant_id AND e.status = 'succeeded'
		RETURNING r.mode, e.id, e.tenant_id, e.object_key, e.sha256, e.manifest_sha256, e.size_bytes, e.schema_version`
	qRestoreDone = `UPDATE platform.tenant_restores SET status = $2, finished_at = now(), scratch_schema = $3, old_schema = $4,
		result = $5, error = $6, drop_after = $7, dropped_at = $8 WHERE id = $1`
	qQueuedRestores = `SELECT id FROM platform.tenant_restores WHERE status = 'queued' ORDER BY created_at, id LIMIT $1`
)

// platform opens the migrator's platform connection.
func (r *Restorer) platform() (*sql.DB, error) { return migrate.PlatformConn(r.DSN, "posctl-restore") }

// Queued lists queued restores (oldest first).
func (r *Restorer) Queued(ctx context.Context, limit int) ([]uuid.UUID, error) {
	pc, err := r.platform()
	if err != nil {
		return nil, err
	}
	defer func() { _ = pc.Close() }()
	rows, err := pc.QueryContext(ctx, qQueuedRestores, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Enqueue records a restore and returns its ID (posctl; the API uses
// RequestRestore in its own transaction).
func (r *Restorer) Enqueue(ctx context.Context, tenantID string, exportID uuid.UUID, mode, requestedBy string) (uuid.UUID, error) {
	pc, err := r.platform()
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = pc.Close() }()
	if _, err := pc.ExecContext(ctx, qFailStaleRestores, tenantID, staleAfter.Seconds()); err != nil {
		return uuid.Nil, err
	}
	if exportID == uuid.Nil {
		if err := pc.QueryRowContext(ctx, qLatestExport, tenantID).Scan(&exportID); errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("tenantio: tenant %s has no succeeded export", tenantID)
		} else if err != nil {
			return uuid.Nil, err
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	var got uuid.UUID
	err = pc.QueryRowContext(ctx, qRequestRestore, id, exportID, tenantID, mode, requestedBy).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		var busy bool
		if err := pc.QueryRowContext(ctx, qRestoreActive, tenantID).Scan(&busy); err != nil {
			return uuid.Nil, err
		}
		if busy {
			return uuid.Nil, ErrRestoreBusy
		}
		return uuid.Nil, fmt.Errorf("tenantio: export %s is not a succeeded export of tenant %s", exportID, tenantID)
	}
	return got, err
}

// Run executes one queued restore and records the result.
func (r *Restorer) Run(ctx context.Context, id uuid.UUID) (Report, error) {
	return r.run(ctx, id, nil)
}

// run claims the restore, restores into scratch and verifies, then, for
// replace, calls swap with the verified scratch schema.
func (r *Restorer) run(ctx context.Context, id uuid.UUID, swap func(context.Context, *Report, tenancy.ScratchSchema) error) (Report, error) {
	start := r.now()
	pc, err := r.platform()
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = pc.Close() }()
	var ref exportRef
	rep := Report{ID: id}
	err = pc.QueryRowContext(ctx, qClaimRestore, id).Scan(&rep.Mode, &ref.id, &ref.tenantID, &ref.key, &ref.sha, &ref.mSHA,
		&ref.size, &ref.version)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotQueued
	}
	if err != nil {
		return Report{}, err
	}
	rep.ExportID, rep.TenantID = ref.id, ref.tenantID
	if rep.Mode == ModeReplace && swap == nil {
		err = errors.New("tenantio: replace restores run through Replace")
	} else {
		err = r.restore(ctx, &rep, ref, swap)
	}
	rep.DurationMS = r.now().Sub(start).Milliseconds()

	status, errText := "succeeded", (*string)(nil)
	if err != nil {
		status = "failed"
		s := sanitize(err)
		errText = &s
	}
	var dropAfter, droppedAt *time.Time
	now := r.now()
	switch {
	case rep.OldSchema != "": // replace: the set-aside live schema is kept
		t := now.Add(OldSchemaKeep)
		dropAfter = &t
	case rep.Kept:
		keep := r.KeepFor
		if keep <= 0 {
			keep = 7 * 24 * time.Hour
		}
		t := now.Add(keep)
		dropAfter = &t
	case rep.Scratch != "":
		droppedAt = &now
	}
	result, _ := json.Marshal(rep)
	var scratch, old *string
	if rep.Scratch != "" {
		scratch = &rep.Scratch
	}
	if rep.OldSchema != "" {
		old = &rep.OldSchema
	}
	if uerr := func() error {
		_, e := pc.ExecContext(context.WithoutCancel(ctx), qRestoreDone, id, status, scratch, old, string(result), errText, dropAfter, droppedAt)
		return e
	}(); uerr != nil && err == nil {
		err = uerr
	}
	if err != nil {
		r.log().ErrorContext(ctx, "tenant restore failed", slog.String("tenant_id", rep.TenantID),
			slog.String("restore_id", id.String()), slog.String("err", sanitize(err)))
	}
	return rep, err
}

// fetch opens the stored export.
func (r *Restorer) fetch(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, _, err := r.Storage.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("tenantio: read export: %w", err)
	}
	return rc, nil
}

func (r *Restorer) verifyStored(ctx context.Context, ref exportRef) (Manifest, Summary, error) {
	rc, err := r.fetch(ctx, ref.key)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	defer func() { _ = rc.Close() }()
	m, sum, err := Verify(rc, r.Identity, r.ManifestKey)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	// The object must be the one the export recorded, for this tenant.
	if sum.SHA256 != ref.sha || sum.Size != ref.size || sum.ManifestSHA256 != ref.mSHA || m.TenantID != ref.tenantID ||
		m.SchemaVersion != ref.version {
		return Manifest{}, Summary{}, ErrChecksum
	}
	return m, sum, nil
}

const (
	qScratchTables = `SELECT c.relname FROM pg_class c
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relkind IN ('r','p') AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`
	qIdentityColumns = `SELECT a.attname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = $1 AND a.attidentity <> '' ORDER BY a.attnum`
)

// restore does OPERATIONS §6.3 steps 1–6 into a new scratch schema; the
// scratch schema is dropped on failure and in test mode.
func (r *Restorer) restore(ctx context.Context, rep *Report, ref exportRef, swap func(context.Context, *Report, tenancy.ScratchSchema) error) (err error) {
	// 1. Verify the stored object completely before creating anything.
	m, sum1, err := r.verifyStored(ctx, ref)
	if err != nil {
		return err
	}
	rep.SchemaVersion, rep.Files = m.SchemaVersion, len(m.Files)
	latest, err := migrate.LatestTenantVersion()
	if err != nil {
		return err
	}
	if m.SchemaVersion > latest {
		return fmt.Errorf("tenantio: export is at schema version %d, newer than this build (%d)", m.SchemaVersion, latest)
	}

	// 2–3. Scratch schema and role, migrated to the export's version.
	scratch, err := tenancy.ScratchFor(ref.tenantID, r.now())
	if err != nil {
		return err
	}
	if err := migrate.CreateScratch(ctx, r.DSN, scratch); err != nil {
		return err
	}
	rep.Scratch = scratch.String()
	defer func() {
		if err != nil || (rep.Mode == ModeTest) {
			if derr := migrate.DropScratch(context.WithoutCancel(ctx), r.DSN, scratch); derr != nil && err == nil {
				err = derr
			}
			return
		}
		rep.Kept = rep.Mode == ModeScratch
	}()
	v, err := migrate.UpScratch(ctx, r.DSN, scratch, m.SchemaVersion)
	if err != nil {
		return fmt.Errorf("tenantio: migrate scratch: %w", err)
	}
	if v != m.SchemaVersion {
		return fmt.Errorf("tenantio: scratch is at version %d, the export at %d", v, m.SchemaVersion)
	}
	if err := r.emptyScratch(ctx, scratch); err != nil {
		return err
	}

	// 4. Load every table as the scratch role, in one transaction.
	loaded, sum2, err := r.load(ctx, scratch, ref.key, m)
	if err != nil {
		return err
	}
	if sum2 != sum1 {
		return ErrChecksum // the object changed between the two passes
	}
	if err := r.resetIdentities(ctx, scratch, m); err != nil {
		return err
	}
	if r.afterLoad != nil {
		if err := r.afterLoad(ctx, scratch); err != nil {
			return err
		}
	}

	// 5. Verify: the export query again, at the export's version.
	checks, err := r.compare(ctx, scratch, m, loaded)
	rep.Tables = checks
	for _, c := range checks {
		rep.Rows += c.RowsRead
	}
	if err != nil {
		return err
	}
	rep.OK = true

	// 6. Migrate scratch to the latest version.
	if rep.MigratedTo, err = migrate.UpScratch(ctx, r.DSN, scratch, 0); err != nil {
		return fmt.Errorf("tenantio: migrate scratch to latest: %w", err)
	}
	if swap != nil {
		return swap(ctx, rep, scratch)
	}
	return nil
}

// emptyScratch removes rows the migrations seeded (doc_counters…), so
// the scratch schema holds exactly the export.
func (r *Restorer) emptyScratch(ctx context.Context, s tenancy.ScratchSchema) error {
	cfg, err := migrate.ScratchOwnerConfig(r.DSN, s, "posctl-restore")
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	tables, err := texts(conn.Query(ctx, qScratchTables))
	if err != nil {
		return err
	}
	return db.TruncateTables(ctx, conn, s.String(), tables)
}

// load streams the archive a second time into the scratch schema.
func (r *Restorer) load(ctx context.Context, s tenancy.ScratchSchema, key string, m Manifest) (map[string]int64, Summary, error) {
	cfg, err := migrate.ScratchSessionConfig(r.DSN, s, false, "posctl-restore")
	if err != nil {
		return nil, Summary{}, err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, Summary{}, err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	rc, err := r.fetch(ctx, key)
	if err != nil {
		return nil, Summary{}, err
	}
	defer func() { _ = rc.Close() }()
	loaded := map[string]int64{}
	var sum Summary
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var lerr error
		sum, lerr = Load(rc, r.Identity, r.ManifestKey, m, func(e TableEntry, csv io.Reader) error {
			q, err := db.CopyInSQL(e.Name, e.Columns)
			if err != nil {
				return err
			}
			tag, err := tx.Conn().PgConn().CopyFrom(ctx, csv, q)
			if err != nil {
				return fmt.Errorf("tenantio: load %s: %w", e.Name, err)
			}
			loaded[e.Name] = tag.RowsAffected()
			return nil
		}, nil)
		return lerr
	})
	return loaded, sum, err
}

func (r *Restorer) resetIdentities(ctx context.Context, s tenancy.ScratchSchema, m Manifest) error {
	cfg, err := migrate.ScratchOwnerConfig(r.DSN, s, "posctl-restore")
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	for _, t := range m.Tables {
		cols, err := texts(conn.Query(ctx, qIdentityColumns, t.Name))
		if err != nil {
			return err
		}
		for _, c := range cols {
			if err := db.ResetIdentity(ctx, conn, s.String(), t.Name, c); err != nil {
				return err
			}
		}
	}
	return nil
}

// compare re-runs each table's export query on the scratch schema (read
// only, the same output settings) and checks rows and SHA-256 against the
// manifest (OPERATIONS §6.3 step 5).
func (r *Restorer) compare(ctx context.Context, s tenancy.ScratchSchema, m Manifest, loaded map[string]int64) ([]TableCheck, error) {
	cfg, err := migrate.ScratchSessionConfig(r.DSN, s, true, "posctl-restore-verify")
	if err != nil {
		return nil, err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	var checks []TableCheck
	bad := false
	err = pgx.BeginTxFunc(ctx, conn, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		for _, t := range m.Tables {
			q, err := db.CopyOutSQL(t.Name, t.Columns, t.OrderBy)
			if err != nil {
				return err
			}
			h := sha256.New()
			tag, err := tx.Conn().PgConn().CopyTo(ctx, h, q)
			if err != nil {
				return fmt.Errorf("tenantio: verify %s: %w", t.Name, err)
			}
			c := TableCheck{Name: t.Name, Rows: t.Rows, RowsLoaded: loaded[t.Name], RowsRead: tag.RowsAffected(),
				SHA256: t.SHA256, SHA256Read: hex.EncodeToString(h.Sum(nil))}
			c.OK = c.RowsLoaded == c.Rows && c.RowsRead == c.Rows && c.SHA256Read == c.SHA256
			bad = bad || !c.OK
			checks = append(checks, c)
		}
		return nil
	})
	if err != nil {
		return checks, err
	}
	if bad {
		return checks, ErrMismatch
	}
	return checks, nil
}

// Weekly restore test (OPERATIONS §6.3): one random tenant and the tenant
// whose last successful test is oldest, among tenants with a recent
// succeeded export.
const qWeeklyCandidates = `WITH c AS (
	SELECT t.id,
	  (SELECT max(r.finished_at) FROM platform.tenant_restores r
	    WHERE r.tenant_id = t.id AND r.mode = 'test' AND r.status = 'succeeded') AS tested
	FROM platform.tenants t
	WHERE t.status NOT IN ('provisioning','failed','deleted')
	  AND EXISTS (SELECT 1 FROM platform.tenant_exports e WHERE e.tenant_id = t.id AND e.status = 'succeeded')
)
(SELECT id FROM c ORDER BY random() LIMIT 1)
UNION
(SELECT id FROM c ORDER BY tested NULLS FIRST, id LIMIT 1)`

// ScheduleWeekly queues the weekly restore tests and returns their IDs. A
// tenant with a restore already queued or running is skipped.
func (r *Restorer) ScheduleWeekly(ctx context.Context) ([]uuid.UUID, error) {
	tenants, err := r.weeklyCandidates(ctx)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, t := range tenants {
		id, err := r.Enqueue(ctx, t, uuid.Nil, ModeTest, "system:weekly")
		if errors.Is(err, ErrRestoreBusy) {
			continue
		}
		if err != nil {
			return out, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (r *Restorer) weeklyCandidates(ctx context.Context) ([]string, error) {
	pc, err := r.platform()
	if err != nil {
		return nil, err
	}
	defer func() { _ = pc.Close() }()
	rows, err := pc.QueryContext(ctx, qWeeklyCandidates)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tenants []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		tenants = append(tenants, id)
	}
	return tenants, rows.Err()
}
