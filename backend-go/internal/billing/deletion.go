package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Deletion warnings (S10 item 4): 14, 7 and 1 day(s) before the deletion
// date (purge_after), to the billing contact and the owner. Only the
// nearest warning is sent (a shop that enters with 10 days left gets the
// 14-day one at once, then 7 and 1).
var deletionWarnings = []int{1, 7, 14}

// NoticeDeletionWarning is the kind prefix; the kind ends in _14, _7 or _1.
const NoticeDeletionWarning = "deletion_warning"

const qDeletionWarn = `SELECT id, purge_after, owner_email::text, coalesce(billing_email::text, '') FROM platform.tenants
	WHERE status = 'pending_deletion' AND purge_after IS NOT NULL AND purge_after > $1
	  AND purge_after <= $1::timestamptz + interval '14 days'
	ORDER BY purge_after LIMIT 2000`

// RunDeletionWarnings sends the due warnings and returns how many tenants
// got one.
func (s *Service) RunDeletionWarnings(ctx context.Context) (int, error) {
	now := s.now()
	sent := 0
	err := s.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qDeletionWarn, now)
		if err != nil {
			return err
		}
		type due struct {
			id          string
			at          time.Time
			owner, bill string
		}
		var list []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.at, &d.owner, &d.bill); err != nil {
				rows.Close()
				return err
			}
			list = append(list, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range list {
			left := d.at.Sub(now)
			days := 0
			for _, w := range deletionWarnings {
				if left <= time.Duration(w)*24*time.Hour {
					days = w
					break
				}
			}
			if days == 0 {
				continue
			}
			n := Notice{Kind: fmt.Sprintf("%s_%d", NoticeDeletionWarning, days), PeriodKey: day(d.at)}
			data := map[string]any{"purge_after": stamp(d.at)}
			ok, err := Notify(ctx, tx, d.id, n, data)
			if err != nil {
				return err
			}
			// The owner too, when the billing contact is someone else.
			if d.bill != "" && !strings.EqualFold(d.bill, d.owner) {
				if _, err := notify(ctx, tx, d.id, Notice{Kind: n.Kind, PeriodKey: n.PeriodKey + ":owner"},
					map[string]any{"purge_after": stamp(d.at)}, d.owner); err != nil {
					return err
				}
			}
			if ok {
				sent++
			}
		}
		return nil
	})
	return sent, err
}
