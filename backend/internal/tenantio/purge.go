package tenantio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/files"
	"github.com/tejas1112001/POS_inventory/backend/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Purge of deleted shops (ARCHITECTURE_REVIEW M4). A Super Admin finalises
// a deletion (platform.FinaliseDeletion: status deleted, only after the
// final export's restore test passed). After the retention window the
// purge job drops the shop's schema and role and its stored files
// (tenants/<id>/), and records the tombstone: purged_at on the tenant row
// and a platform audit entry. The final export (exports/<id>/) is kept:
// it is encrypted and is what the shop's owner was promised.
//
// Every step can run again: a purge that stops half way is finished by
// the next run.

// Purger drops deleted shops. The migrator DSN drops schemas and roles.
type Purger struct {
	Sessions    *tenancy.Sessions // pos_jobs: platform reads and the tombstone
	MigratorDSN secrets.Secret
	Storage     files.Storage
	// After is the retention window after the final deletion.
	After time.Duration
	Log   *slog.Logger
}

// Enabled reports whether purging is configured.
func (p *Purger) Enabled() bool {
	return p != nil && p.Sessions != nil && !p.MigratorDSN.IsZero() && p.Storage != nil && p.After > 0
}

const (
	// A shop is due when deleted for longer than the window, with a final
	// export since the deletion started that succeeded and passed a
	// restore test (the conditions FinaliseDeletion checked, checked again).
	qPurgeDue = `SELECT t.id::text FROM platform.tenants t
		WHERE t.status = 'deleted' AND t.purged_at IS NULL AND t.deleted_at < now() - make_interval(secs => $1)
		  AND EXISTS (SELECT 1 FROM platform.tenant_exports e
			JOIN platform.tenant_restores r ON r.export_id = e.id AND r.mode IN ('test','scratch') AND r.status = 'succeeded'
			WHERE e.tenant_id = t.id AND e.kind = 'final' AND e.status = 'succeeded'
			  AND e.created_at >= coalesce(t.deletion_requested_at, '-infinity'::timestamptz))
		ORDER BY t.deleted_at LIMIT $2`
	qPurgeLock  = `SELECT status, purged_at IS NOT NULL, slug::text, deleted_at FROM platform.tenants WHERE id = $1 FOR UPDATE`
	qPurgeMark  = `UPDATE platform.tenants SET purged_at = now(), updated_at = now() WHERE id = $1`
	qPurgeAudit = `INSERT INTO platform.platform_audit_log (action, target_tenant, after) VALUES ('tenant.purge', $1, $2)`
)

// purgeBatch bounds one run; the rest wait for the next.
const purgeBatch = 20

// ErrNotPurgeable means the shop is not deleted, or already purged.
var ErrNotPurgeable = errors.New("tenantio: tenant is not deleted or already purged")

// Due lists the shops ready to purge, oldest deletion first.
func (p *Purger) Due(ctx context.Context) ([]string, error) {
	var ids []string
	err := p.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qPurgeDue, p.After.Seconds(), purgeBatch)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	return ids, err
}

// Run purges every due shop and returns how many were purged. One shop's
// failure does not stop the others.
func (p *Purger) Run(ctx context.Context) (int, error) {
	if !p.Enabled() {
		return 0, nil
	}
	ids, err := p.Due(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, id := range ids {
		if err := p.Purge(ctx, id); err != nil {
			p.Log.ErrorContext(ctx, "tenant purge failed", slog.String("tenant_id", id), slog.Any("err", err))
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// Purge drops one deleted shop's schema, role and files and records the
// tombstone. The caller has checked it is due (Due); Purge checks only
// that it is deleted and not yet purged. The tenant row stays locked
// throughout, so its status cannot change under the purge.
func (p *Purger) Purge(ctx context.Context, id string) error {
	t, err := migrate.TargetFor(id)
	if err != nil {
		return err
	}
	return p.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var status, slug string
		var purged bool
		var deletedAt *time.Time
		if err := tx.QueryRow(ctx, qPurgeLock, id).Scan(&status, &purged, &slug, &deletedAt); err != nil {
			return err
		}
		if status != "deleted" || purged {
			return ErrNotPurgeable
		}
		prefix := "tenants/" + id + "/"
		removed, err := p.Storage.DeletePrefix(ctx, prefix)
		if err != nil {
			return fmt.Errorf("delete files: %w", err)
		}
		if err := migrate.DropTenantSchema(ctx, p.MigratorDSN, t); err != nil {
			return fmt.Errorf("drop schema: %w", err)
		}
		after, _ := json.Marshal(map[string]any{"slug": slug, "schema": t.Schema.String(), "role": t.Role.String(),
			"files_removed": removed, "deleted_at": deletedAt, "retention_days": int(p.After.Hours() / 24)})
		if _, err := tx.Exec(ctx, qPurgeMark, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qPurgeAudit, id, string(after)); err != nil {
			return err
		}
		p.Log.InfoContext(ctx, "tenant purged", slog.String("tenant_id", id), slog.Int("files", removed))
		return nil
	})
}
