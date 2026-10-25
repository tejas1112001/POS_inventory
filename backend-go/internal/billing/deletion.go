package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// The deletion clock (BILLING_DESIGN §3, OPERATIONS §6.4). Day 0 is
// tenants.restricted_at, which SyncTenant sets when billing first makes a
// tenant read-only or suspends it, and clears when the tenant is live
// again. ReadOnlyDays later the tenant becomes pending_deletion: nobody can
// sign in, the export sweep takes a final export, and purge_after records
// the day it becomes eligible for deletion (RetentionDays after day 0).
// Nothing is dropped by a job (O-4).

const (
	qDueDeletion = `SELECT id FROM platform.tenants
		WHERE restricted_at <= $1::timestamptz - make_interval(days => $2::int)
		  AND (status = 'read_only' OR (status = 'suspended' AND suspension_source = 'billing'))
		ORDER BY restricted_at, id LIMIT 1000`
	qLockDueDeletion = `SELECT status, restricted_at FROM platform.tenants
		WHERE id = $1 AND restricted_at <= $2::timestamptz - make_interval(days => $3::int)
		  AND (status = 'read_only' OR (status = 'suspended' AND suspension_source = 'billing'))
		FOR UPDATE SKIP LOCKED`
	qPendingDeletion = `UPDATE platform.tenants SET status = 'pending_deletion', deletion_requested_at = $2,
		purge_after = restricted_at + make_interval(days => $3::int), updated_at = now()
		WHERE id = $1 RETURNING purge_after`
)

func (c Config) clockDays() (readOnly, retention int) {
	readOnly, retention = c.ReadOnlyDays, c.RetentionDays
	if readOnly <= 0 {
		readOnly = DefaultConfig.ReadOnlyDays
	}
	if retention <= readOnly {
		retention = max(DefaultConfig.RetentionDays, readOnly+1)
	}
	return readOnly, retention
}

// RunDeletionClock moves every tenant restricted by billing for
// ReadOnlyDays to pending_deletion, audits it and tells the billing
// contact once. Each tenant is handled in its own transaction and locked
// with SKIP LOCKED, so parallel runs never move one twice. It returns how
// many tenants moved.
func (s *Service) RunDeletionClock(ctx context.Context) (int, error) {
	now := s.now()
	readOnly, retention := s.Cfg.clockDays()
	var ids []string
	err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qDueDeletion, now, readOnly)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, id := range ids {
		err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
			var before string
			var since time.Time
			err := tx.QueryRow(ctx, qLockDueDeletion, id, now, readOnly).Scan(&before, &since)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // paid meanwhile, or another worker has it
			}
			if err != nil {
				return err
			}
			var purge time.Time
			if err := tx.QueryRow(ctx, qPendingDeletion, id, now, retention).Scan(&purge); err != nil {
				return err
			}
			if err := Audit(ctx, tx, AuditEntry{Actor: systemBilling, Action: "tenant.pending_deletion", Tenant: id,
				Before: map[string]any{"status": before, "restricted_at": stamp(since)},
				After:  map[string]any{"status": "pending_deletion", "purge_after": stamp(purge)}}); err != nil {
				return err
			}
			if _, err := Notify(ctx, tx, id, Notice{Kind: NoticePendingDeletion, PeriodKey: day(since)},
				map[string]any{"purge_after": stamp(purge)}); err != nil {
				return err
			}
			moved++
			return nil
		})
		if err != nil {
			return moved, err
		}
	}
	return moved, nil
}
