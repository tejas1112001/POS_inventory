package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Scheduled actor in the platform audit log.
const systemBilling = "system:billing"

const (
	qDueLifecycle = `SELECT id FROM platform.subscriptions
		WHERE (status = 'trialing' AND trial_end <= $1)
		   OR (status IN ('past_due','grace') AND grace_end <= $1)
		   OR (status = 'cancelled' AND (current_period_end IS NULL OR current_period_end <= $1))
		ORDER BY id LIMIT 1000`
	qLockSubByID  = `SELECT ` + subCols + ` FROM platform.subscriptions WHERE id = $1 FOR UPDATE SKIP LOCKED`
	qDueReminders = `SELECT id FROM platform.subscriptions
		WHERE (status = 'trialing' AND trial_end > $1 AND trial_end <= $1 + interval '8 days')
		   OR (status IN ('past_due','grace') AND grace_end > $1 AND grace_end <= $1 + interval '1 day')
		   OR ((status = 'cancelled' OR (status = 'active' AND cancel_at_period_end))
		       AND current_period_end > $1 AND current_period_end <= $1 + interval '8 days')
		ORDER BY id LIMIT 5000`
	qReadSubByID = `SELECT ` + subCols + ` FROM platform.subscriptions WHERE id = $1`
)

// due lists the subscriptions a job has to look at.
func (s *Service) due(ctx context.Context, reminders bool, now time.Time) ([]int64, error) {
	var ids []int64
	err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		var rows pgx.Rows
		var err error
		if reminders {
			rows, err = tx.Query(ctx, qDueReminders, now)
		} else {
			rows, err = tx.Query(ctx, qDueLifecycle, now)
		}
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	return ids, err
}

// RunLifecycle makes the time-driven moves (Tick) for every subscription
// whose clock has run out: unpaid trials become read-only, overdue
// payments are suspended after grace, cancelled periods end. Each
// subscription is handled in its own transaction and locked with SKIP
// LOCKED, so parallel runs never handle one twice. It returns how many
// subscriptions changed.
func (s *Service) RunLifecycle(ctx context.Context) (int, error) {
	now := s.now()
	ids, err := s.due(ctx, false, now)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, id := range ids {
		err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			r, err := scanSub(tx.QueryRow(ctx, qLockSubByID, id))
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // another worker has it
			}
			if err != nil {
				return err
			}
			next, notices, ok := Tick(r.Sub, now, s.Cfg)
			if !ok {
				return nil
			}
			if _, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET status = $2, updated_at = now() WHERE id = $1`,
				id, string(next.Status)); err != nil {
				return err
			}
			before, after, err := SyncTenant(ctx, tx, r.tenantID, next.Status, r.planID)
			if err != nil {
				return err
			}
			if err := Audit(ctx, tx, AuditEntry{Actor: systemBilling, Action: "billing.lifecycle", Tenant: r.tenantID,
				Before: map[string]any{"subscription": r.Status, "tenant": before},
				After:  map[string]any{"subscription": next.Status, "tenant": after}}); err != nil {
				return err
			}
			for _, n := range notices {
				if _, err := Notify(ctx, tx, r.tenantID, n, noticeData(next, Event{})); err != nil {
					return err
				}
			}
			changed++
			return nil
		})
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// RunReminders sends the dated reminders that are due (trial and
// subscription ending in 7/3/1 days, grace ending in a day). The notice
// log's unique key makes each one go out once, also when two runs
// overlap. It returns how many notices were queued.
func (s *Service) RunReminders(ctx context.Context) (int, error) {
	now := s.now()
	ids, err := s.due(ctx, true, now)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, id := range ids {
		err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			r, err := scanSub(tx.QueryRow(ctx, qReadSubByID, id))
			if err != nil {
				return err
			}
			for _, n := range Reminders(r.Sub, now) {
				ok, err := Notify(ctx, tx, r.tenantID, n, noticeData(r.Sub, Event{}))
				if err != nil {
					return err
				}
				if ok {
					sent++
				}
			}
			return nil
		})
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}
