package tenantio

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Nightly exports and retention (S10 item 4, docs/S10_DESIGN.md §4, B5):
// at 01:00 India time every live tenant gets an encrypted nightly export
// (data only; the final export also holds the files). Retention keeps the
// last 7 nightly exports and the last 4 Sunday ones; older nightly
// exports have their file deleted from storage and are marked expired.
// Manual and final exports are never pruned, and each tenant's latest
// verified export (its restore test passed) is always kept.

// SystemNightly is requested_by for nightly exports.
const SystemNightly = "system:nightly"

const (
	// Live tenants without a nightly export in the last 20 hours (a rerun
	// of the job on the same night queues nothing).
	qNeedNightly = `SELECT t.id FROM platform.tenants t
		WHERE t.status NOT IN ('provisioning','failed','pending_deletion','deleted')
		  AND NOT EXISTS (SELECT 1 FROM platform.tenant_exports e WHERE e.tenant_id = t.id AND e.kind = 'nightly'
			AND e.created_at > now() - interval '20 hours')
		ORDER BY t.id LIMIT 20000`

	// Nightly exports past retention. Sundays are counted in India time
	// (the job runs at 01:00 IST, so Sunday's export is Sunday's data).
	qPrunable = `WITH n AS (
			SELECT e.id, e.object_key, e.created_at,
				row_number() OVER (PARTITION BY e.tenant_id ORDER BY e.created_at DESC) AS daily_rank,
				row_number() OVER (PARTITION BY e.tenant_id, extract(dow FROM e.created_at AT TIME ZONE 'Asia/Kolkata') = 0
					ORDER BY e.created_at DESC) AS sunday_rank,
				extract(dow FROM e.created_at AT TIME ZONE 'Asia/Kolkata') = 0 AS sunday
			FROM platform.tenant_exports e WHERE e.kind = 'nightly' AND e.status = 'succeeded'
		), verified AS (
			SELECT DISTINCT ON (e.tenant_id) e.id FROM platform.tenant_exports e
			WHERE e.status = 'succeeded' AND EXISTS (SELECT 1 FROM platform.tenant_restores r
				WHERE r.export_id = e.id AND r.mode IN ('test','scratch') AND r.status = 'succeeded')
			ORDER BY e.tenant_id, e.created_at DESC
		)
		SELECT n.id, n.object_key FROM n
		WHERE n.daily_rank > 7 AND NOT (n.sunday AND n.sunday_rank <= 4)
		  AND n.id NOT IN (SELECT id FROM verified)
		ORDER BY n.created_at LIMIT 2000`
	qExpire = `UPDATE platform.tenant_exports SET status = 'expired', expired_at = now()
		WHERE id = $1 AND status = 'succeeded' AND kind = 'nightly'`

	// Final exports that succeeded and have no restore test yet.
	qFinalsToTest = `SELECT e.id, e.tenant_id FROM platform.tenant_exports e JOIN platform.tenants t ON t.id = e.tenant_id
		WHERE e.kind = 'final' AND e.status = 'succeeded' AND t.status = 'pending_deletion'
		  AND NOT EXISTS (SELECT 1 FROM platform.tenant_restores r WHERE r.export_id = e.id
			AND (r.status IN ('queued','running','succeeded') OR r.created_at > now() - make_interval(hours => $1::int)))
		ORDER BY e.created_at LIMIT 200`
)

// QueueNightly queues tonight's export for every live tenant that has
// none. Tenants with an export still running are skipped (busy).
func (e *Exporter) QueueNightly(ctx context.Context) (queued []uuid.UUID, busy int, err error) {
	var tenants []string
	err = e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qNeedNightly)
		if err != nil {
			return err
		}
		tenants, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	for _, id := range tenants {
		var got uuid.UUID
		err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			var err error
			got, err = Queue(ctx, tx, id, KindNightly, SystemNightly, false)
			return err
		})
		switch {
		case errors.Is(err, ErrBusy):
			busy++
			continue
		case err != nil:
			return queued, busy, err
		}
		queued = append(queued, got)
	}
	return queued, busy, nil
}

// PruneNightly deletes the files of nightly exports past retention and
// marks them expired. It returns how many were pruned.
func (e *Exporter) PruneNightly(ctx context.Context) (int, error) {
	type victim struct {
		id  uuid.UUID
		key *string
	}
	var list []victim
	err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qPrunable)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v victim
			if err := rows.Scan(&v.id, &v.key); err != nil {
				return err
			}
			list = append(list, v)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	pruned := 0
	for _, v := range list {
		// The file first: a row marked expired never points at a file that
		// still exists. A failed delete is tried again tomorrow.
		if v.key != nil && *v.key != "" && e.Storage != nil {
			if err := e.Storage.Delete(ctx, *v.key); err != nil {
				e.log().WarnContext(ctx, "nightly export file delete failed", "export_id", v.id.String(), "err", err)
				continue
			}
		}
		err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			_, err := tx.Exec(ctx, qExpire, v.id)
			return err
		})
		if err != nil {
			return pruned, err
		}
		pruned++
	}
	return pruned, nil
}

// QueueFinalTests queues a restore test of every final export that has
// none (deletion cannot be finalised before one passes). posctl tenant
// restore-tests runs them. It returns the queued restore IDs.
func (e *Exporter) QueueFinalTests(ctx context.Context) ([]uuid.UUID, error) {
	type final struct {
		export uuid.UUID
		tenant string
	}
	var list []final
	err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qFinalsToTest, finalRetryAfterHours)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f final
			if err := rows.Scan(&f.export, &f.tenant); err != nil {
				return err
			}
			list = append(list, f)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, f := range list {
		var id uuid.UUID
		err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			var err error
			id, err = RequestRestore(ctx, tx, f.tenant, f.export, ModeTest, SystemFinal)
			return err
		})
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
