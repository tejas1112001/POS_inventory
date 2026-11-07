package tenantio

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// The export sweep (posd worker): fail exports that stopped reporting,
// queue the final export of every tenant pending deletion (OPERATIONS
// §6.4), and list what waits to run.

// SystemFinal is requested_by for final exports.
const SystemFinal = "system:deletion"

// finalRetryAfterHours is how long a failed final export waits before
// the sweep queues another one.
const finalRetryAfterHours = 6

const (
	qFailStaleAll = `UPDATE platform.tenant_exports SET status = 'failed', error = 'interrupted', finished_at = now()
		WHERE status = 'running' AND started_at < now() - make_interval(secs => $1)`
	qQueuedExports = `SELECT id FROM platform.tenant_exports WHERE status = 'queued' ORDER BY created_at, id LIMIT $1`
	// Pending deletion and no final export since the deletion started that
	// is waiting, running, done, or failed only recently.
	qNeedFinal = `SELECT t.id FROM platform.tenants t
		WHERE t.status = 'pending_deletion'
		  AND NOT EXISTS (SELECT 1 FROM platform.tenant_exports e
			WHERE e.tenant_id = t.id AND e.kind = 'final'
			  AND e.created_at >= coalesce(t.deletion_requested_at, '-infinity'::timestamptz)
			  AND (e.status IN ('queued','running','succeeded')
			       OR e.created_at > now() - make_interval(hours => $1::int)))
		ORDER BY t.id LIMIT 200`
)

// FailStale marks exports that have run for too long without finishing
// (a crashed worker) as failed. It returns how many.
func (e *Exporter) FailStale(ctx context.Context) (int64, error) {
	var n int64
	err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		tag, err := tx.Exec(ctx, qFailStaleAll, staleAfter.Seconds())
		n = tag.RowsAffected()
		return err
	})
	return n, err
}

// Queued lists queued exports, oldest first.
func (e *Exporter) Queued(ctx context.Context, limit int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qQueuedExports, limit)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	return ids, err
}

// QueueFinals queues a final export (with files) for every tenant pending
// deletion that has none. A tenant whose other export is still running is
// tried again on the next sweep. It returns the queued export IDs.
func (e *Exporter) QueueFinals(ctx context.Context) ([]uuid.UUID, error) {
	var tenants []string
	err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qNeedFinal, finalRetryAfterHours)
		if err != nil {
			return err
		}
		tenants, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, id := range tenants {
		var got uuid.UUID
		err := e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			var err error
			got, err = Queue(ctx, tx, id, KindFinal, SystemFinal, true)
			return err
		})
		switch {
		case errors.Is(err, ErrBusy):
			continue
		case err != nil:
			return out, err
		}
		out = append(out, got)
	}
	return out, nil
}
