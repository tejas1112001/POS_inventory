package tenantio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// ErrNotReadOnly means a replace restore was asked for a tenant that is not
// read-only (OPERATIONS §6.3: nobody may be writing while it is replaced).
var ErrNotReadOnly = errors.New("tenantio: the tenant must be read_only for a replace restore")

// ErrConfirm means the confirmation did not name the tenant's slug.
var ErrConfirm = errors.New("tenantio: confirm with the tenant's slug")

// OldSchemaKeep is how long a replaced schema is kept before Cleanup drops it.
const OldSchemaKeep = 30 * 24 * time.Hour

const (
	qReplaceTarget = `SELECT status, slug::text FROM platform.tenants WHERE id = $1`
	qLockTenant    = `SELECT status, coalesce(billing_email, owner_email)::text FROM platform.tenants WHERE id = $1 FOR UPDATE`
	qMigrateLock   = `SELECT pg_try_advisory_xact_lock(hashtextextended('migrate:' || $1, 0))`
	qTouchTenant   = `UPDATE platform.tenants SET schema_version = $2, updated_at = now() WHERE id = $1`
	qPlatformAudit = `INSERT INTO platform.platform_audit_log (action, target_tenant, target, after) VALUES ('tenant.restore_replace', $1, $2, $3)`
	qRestoreMail   = `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, template, template_data)
		VALUES ($1, 'platform_notice', $2, $3, $4) ON CONFLICT (idempotency_key) DO NOTHING`
)

// Replace restores an export over a tenant's live schema (posctl only): a
// full scratch restore and verification first, then one transaction swaps
// the schemas and ends every session. confirm must be the tenant's slug,
// and the tenant must be read_only. exportID nil: the latest export.
func (r *Restorer) Replace(ctx context.Context, tenantID string, exportID uuid.UUID, confirm, requestedBy string) (Report, error) {
	pc, err := r.platform()
	if err != nil {
		return Report{}, err
	}
	var status, slug string
	err = pc.QueryRowContext(ctx, qReplaceTarget, tenantID).Scan(&status, &slug)
	_ = pc.Close()
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, fmt.Errorf("tenantio: no tenant %s", tenantID)
	}
	if err != nil {
		return Report{}, err
	}
	if confirm != slug {
		return Report{}, ErrConfirm
	}
	if status != "read_only" {
		return Report{}, ErrNotReadOnly
	}
	id, err := r.Enqueue(ctx, tenantID, exportID, ModeReplace, requestedBy)
	if err != nil {
		return Report{}, err
	}
	return r.run(ctx, id, r.swap)
}

// swap is OPERATIONS §6.3 steps 7–8, after the scratch schema was verified
// and migrated to the latest version.
func (r *Restorer) swap(ctx context.Context, rep *Report, scratch tenancy.ScratchSchema) error {
	live, role, err := tenancy.SchemaFor(rep.TenantID)
	if err != nil {
		return err
	}
	old, err := tenancy.OldSchemaFor(rep.TenantID, r.now())
	if err != nil {
		return err
	}
	cfg, err := migrate.OwnerConnConfig(r.DSN)
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	m, err := r.manifestOf(ctx, rep.ExportID)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var status, email string
		if err := tx.QueryRow(ctx, qLockTenant, rep.TenantID).Scan(&status, &email); err != nil {
			return err
		}
		if status != "read_only" {
			return ErrNotReadOnly
		}
		var locked bool
		if err := tx.QueryRow(ctx, qMigrateLock, rep.TenantID).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return migrate.ErrLocked
		}
		steps := []func() error{
			func() error { return db.RenameSchema(ctx, tx, live.String(), old.String()) },
			func() error { return db.RenameSchema(ctx, tx, scratch.String(), live.String()) },
			// The tenant role loses the set-aside schema; the scratch role
			// loses the new live one.
			func() error { return db.RevokeSchemaUsage(ctx, tx, old.String(), role.String()) },
			func() error {
				return db.RevokeTenantPrivileges(ctx, tx, live.String(), scratch.Role(), migrate.OwnerRole)
			},
			func() error {
				return db.GrantTenantPrivileges(ctx, tx, live.String(), role.String(), migrate.OwnerRole)
			},
			func() error { return db.CreateOutboxTrigger(ctx, tx, live.String(), role.String(), rep.TenantID) },
			func() error { return db.RevokeTenantSessions(ctx, tx, live.String(), rep.TenantID) },
			func() error { return db.CancelRestoredOutbox(ctx, tx, live.String()) },
		}
		for _, step := range steps {
			if err := step(); err != nil {
				return err
			}
		}
		after, _ := json.Marshal(map[string]any{"export_id": rep.ExportID, "exported_at": m.ExportedAt, "old_schema": old.String(),
			"schema_version": rep.MigratedTo})
		if err := db.WriteTenantAudit(ctx, tx, live.String(), "tenant.restore", "tenant", rep.TenantID, after); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qTouchTenant, rep.TenantID, rep.MigratedTo); err != nil {
			return err // also NOTIFY tenant_changed (trigger): caches flush
		}
		if _, err := tx.Exec(ctx, qPlatformAudit, rep.TenantID, rep.ExportID.String(), after); err != nil {
			return err
		}
		data, _ := json.Marshal(map[string]string{"tenant_id": rep.TenantID, "backup_time": m.ExportedAt.UTC().Format(time.RFC3339)})
		_, err := tx.Exec(ctx, qRestoreMail, "tenant_restored:"+rep.ID.String(), email, mailtmpl.RestoreNoticeTemplate, data)
		return err
	})
	if err != nil {
		return err
	}
	rep.OldSchema = old.String()

	// After the commit: the scratch role has nothing left; drop it. The
	// post-step syncs the permission catalog (grants and trigger again,
	// idempotent). Failures here are logged, not fatal: the swap is done.
	fctx := context.WithoutCancel(ctx)
	if mig, err := pgx.Connect(fctx, r.DSN.Reveal()); err == nil {
		if err := db.DropRole(fctx, mig, scratch.Role()); err != nil {
			r.log().WarnContext(ctx, "replace restore: scratch role not dropped", slog.String("err", sanitize(err)))
		}
		_ = mig.Close(fctx)
	}
	if t, err := migrate.TargetFor(rep.TenantID); err == nil {
		if tc, err := migrate.TenantConn(r.DSN, t.Schema, "posctl-restore"); err == nil {
			if err := migrate.PostStep(fctx, tc, t); err != nil {
				r.log().WarnContext(ctx, "replace restore: post-step failed", slog.String("err", sanitize(err)))
			}
			_ = tc.Close()
		}
	}
	return nil
}

// manifestOf reads the export time the swap reports (from the verified
// export's record, not the archive, which was already checked).
func (r *Restorer) manifestOf(ctx context.Context, exportID uuid.UUID) (Manifest, error) {
	pc, err := r.platform()
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = pc.Close() }()
	var m Manifest
	err = pc.QueryRowContext(ctx, `SELECT coalesce(finished_at, created_at) FROM platform.tenant_exports WHERE id = $1`, exportID).Scan(&m.ExportedAt)
	return m, err
}

type cleanupDue struct {
	id           uuid.UUID
	scratch, old *string
}

// dueForCleanup reads the due rows (closing them before any drop: the
// platform connection has one session).
func dueForCleanup(ctx context.Context, pc *sql.DB, now time.Time) ([]cleanupDue, error) {
	rows, err := pc.QueryContext(ctx, qCleanupDue, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var list []cleanupDue
	for rows.Next() {
		var d cleanupDue
		if err := rows.Scan(&d.id, &d.scratch, &d.old); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

const (
	qCleanupDue = `SELECT id, scratch_schema, old_schema FROM platform.tenant_restores
		WHERE dropped_at IS NULL AND drop_after IS NOT NULL AND drop_after < $1 ORDER BY drop_after LIMIT 100`
	qCleanupDone = `UPDATE platform.tenant_restores SET dropped_at = now() WHERE id = $1`
)

// Cleanup drops kept scratch schemas and set-aside old schemas whose
// keep time has passed. It returns how many it dropped.
func (r *Restorer) Cleanup(ctx context.Context) (int, error) {
	pc, err := r.platform()
	if err != nil {
		return 0, err
	}
	defer func() { _ = pc.Close() }()
	list, err := dueForCleanup(ctx, pc, r.now())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range list {
		switch {
		case d.old != nil:
			o, err := tenancy.ParseOldSchema(*d.old)
			if err != nil {
				return n, err
			}
			if err := migrate.DropOldSchema(ctx, r.DSN, o); err != nil {
				return n, err
			}
		case d.scratch != nil:
			s, err := tenancy.ParseScratchSchema(*d.scratch)
			if err != nil {
				return n, err
			}
			if err := migrate.DropScratch(ctx, r.DSN, s); err != nil {
				return n, err
			}
		}
		if _, err := pc.ExecContext(ctx, qCleanupDone, d.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
