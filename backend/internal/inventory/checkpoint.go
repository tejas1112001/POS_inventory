package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Checkpointed reconcile (ARCHITECTURE_REVIEW M2, tenant migration 00033).
// A full check (Reconcile) runs when a shop has no checkpoints or they are
// older than FullReconcileEvery, and then records each ledger row's
// balance up to a movement ID. Other nights compare the stock with that
// balance plus the movements since, so the work follows recent activity,
// not the shop's whole history. A key that looks wrong is re-checked
// against its whole ledger before it is reported: checkpoints can make the
// check slower, never wrong.

// FullReconcileEvery is the longest a shop goes without a full check.
const FullReconcileEvery = 7 * 24 * time.Hour

// checkpointLag keeps movements newer than this out of a checkpoint. A
// movement's ID is taken when it is written, but it is seen only when its
// transaction commits; a transaction still open when the checkpoint is
// taken could otherwise commit a lower ID later and be missed (it would
// then only cost a re-check).
const checkpointLag = time.Hour

// maxRechecks bounds the per-key re-checks of one run; beyond it the run
// falls back to a full check.
const maxRechecks = 500

// Ledger and mark names in ledger_checkpoints and ledger_checkpoint_marks.
const (
	ledgerStock      = "stock"
	ledgerBatchStock = "batch_stock"
	ledgerBatchQuar  = "batch_quarantine"
	markStock        = "stock"
	markBatch        = "batch"
)

const (
	qMarks = `SELECT ledger, last_movement_id, taken_at FROM ledger_checkpoint_marks`

	qStockSince = `SELECT coalesce(i.store_id, x.store_id), coalesce(i.variant_id, x.item_id), coalesce(i.qty, 0), coalesce(x.total, 0)
		FROM inventories i
		FULL JOIN (SELECT store_id, item_id, sum(total) AS total FROM (
				SELECT store_id, item_id, qty AS total FROM ledger_checkpoints WHERE ledger = 'stock'
				UNION ALL
				SELECT store_id, variant_id, delta FROM stock_movements WHERE id > $1
			) u GROUP BY store_id, item_id) x
		  ON x.store_id = i.store_id AND x.item_id = i.variant_id
		WHERE coalesce(i.qty, 0) <> coalesce(x.total, 0)`

	// The batch totals have no history to add up; the two batch ledgers
	// are checked from their checkpoints like the stock ledger.
	qBatchesSince = `SELECT i.store_id, i.variant_id, i.qty, coalesce(bs.total, 0), 'batch_total', 0::bigint
		FROM inventories i
		JOIN product_variants v ON v.id = i.variant_id
		JOIN products p ON p.id = v.product_id AND p.tracks_batches
		LEFT JOIN (SELECT s.store_id, b.variant_id, sum(s.qty) AS total FROM batch_stock s JOIN batches b ON b.id = s.batch_id
		           GROUP BY s.store_id, b.variant_id) bs ON bs.store_id = i.store_id AND bs.variant_id = i.variant_id
		WHERE EXISTS (SELECT 1 FROM settings WHERE id = 1 AND 'batch_expiry' = ANY(modules)) AND i.qty <> coalesce(bs.total, 0)
		UNION ALL
		SELECT s.store_id, b.variant_id, s.qty, coalesce(x.total, 0), 'batch_ledger', s.batch_id
		FROM batch_stock s JOIN batches b ON b.id = s.batch_id
		LEFT JOIN (SELECT store_id, item_id, sum(total) AS total FROM (
				SELECT store_id, item_id, qty AS total FROM ledger_checkpoints WHERE ledger = 'batch_stock'
				UNION ALL
				SELECT store_id, batch_id, delta FROM batch_movements WHERE bucket = 'stock' AND id > $1
			) u GROUP BY store_id, item_id) x ON x.store_id = s.store_id AND x.item_id = s.batch_id
		WHERE s.qty <> coalesce(x.total, 0)
		UNION ALL
		SELECT s.store_id, b.variant_id, s.quarantined_qty, coalesce(x.total, 0), 'batch_quarantine', s.batch_id
		FROM batch_stock s JOIN batches b ON b.id = s.batch_id
		LEFT JOIN (SELECT store_id, item_id, sum(total) AS total FROM (
				SELECT store_id, item_id, qty AS total FROM ledger_checkpoints WHERE ledger = 'batch_quarantine'
				UNION ALL
				SELECT store_id, batch_id, delta FROM batch_movements WHERE bucket = 'quarantine' AND id > $1
			) u GROUP BY store_id, item_id) x ON x.store_id = s.store_id AND x.item_id = s.batch_id
		WHERE s.quarantined_qty <> coalesce(x.total, 0)`

	// Whole-ledger re-checks of one key, stock and sum in one snapshot.
	qRecheckStock = `SELECT coalesce((SELECT qty FROM inventories WHERE store_id = $1 AND variant_id = $2), 0),
		coalesce((SELECT sum(delta) FROM stock_movements WHERE store_id = $1 AND variant_id = $2), 0)`
	qRecheckBatch = `SELECT coalesce((SELECT CASE WHEN $3 = 'quarantine' THEN quarantined_qty ELSE qty END
			FROM batch_stock WHERE store_id = $1 AND batch_id = $2), 0),
		coalesce((SELECT sum(delta) FROM batch_movements WHERE store_id = $1 AND batch_id = $2 AND bucket = $3), 0)`

	qStockHigh = `SELECT coalesce(max(id), 0) FROM stock_movements WHERE created_at < now() - make_interval(secs => $1)`
	qBatchHigh = `SELECT coalesce(max(id), 0) FROM batch_movements WHERE created_at < now() - make_interval(secs => $1)`

	qClearCheckpoints = `DELETE FROM ledger_checkpoints`
	qSaveStock        = `INSERT INTO ledger_checkpoints (ledger, store_id, item_id, qty)
		SELECT 'stock', store_id, variant_id, sum(delta) FROM stock_movements WHERE id <= $1 GROUP BY store_id, variant_id`
	qSaveBatches = `INSERT INTO ledger_checkpoints (ledger, store_id, item_id, qty)
		SELECT CASE WHEN bucket = 'quarantine' THEN 'batch_quarantine' ELSE 'batch_stock' END, store_id, batch_id, sum(delta)
		FROM batch_movements WHERE id <= $1 GROUP BY bucket, store_id, batch_id`
	qSaveMark = `INSERT INTO ledger_checkpoint_marks (ledger, last_movement_id, taken_at) VALUES ($1, $2, now())
		ON CONFLICT (ledger) DO UPDATE SET last_movement_id = EXCLUDED.last_movement_id, taken_at = EXCLUDED.taken_at`
)

// ReconcileResult is one shop's check.
type ReconcileResult struct {
	Mismatches []Mismatch
	Full       bool // the whole ledgers were added up (and checkpoints rebuilt)
	Rechecked  int  // keys re-checked against their whole ledger
}

// ReconcileCheckpointed checks the shop's stock against its ledgers, from
// checkpoints when they are recent enough. It writes checkpoints, so tx
// must be read-write. now decides whether a full check is due.
func ReconcileCheckpointed(ctx context.Context, tx tenancy.TenantTx, now time.Time) (ReconcileResult, error) {
	marks, err := loadMarks(ctx, tx)
	if err != nil {
		return ReconcileResult{}, err
	}
	stock, okS := marks[markStock]
	batch, okB := marks[markBatch]
	if !okS || !okB || now.Sub(stock.taken) >= FullReconcileEvery || now.Sub(batch.taken) >= FullReconcileEvery {
		return fullReconcile(ctx, tx)
	}
	rows, err := tx.Query(ctx, qStockSince, stock.id)
	if err != nil {
		return ReconcileResult{}, err
	}
	found, err := scanMismatches(rows, false)
	if err != nil {
		return ReconcileResult{}, err
	}
	if rows, err = tx.Query(ctx, qBatchesSince, batch.id); err != nil {
		return ReconcileResult{}, err
	}
	b, err := scanMismatches(rows, true)
	if err != nil {
		return ReconcileResult{}, err
	}
	found = append(found, b...)
	res := ReconcileResult{}
	for _, m := range found {
		if m.Kind == "batch_total" { // no ledger history involved: already exact
			res.Mismatches = append(res.Mismatches, m)
			continue
		}
		if res.Rechecked == maxRechecks {
			return fullReconcile(ctx, tx)
		}
		res.Rechecked++
		real, err := recheck(ctx, tx, &m)
		if err != nil {
			return ReconcileResult{}, err
		}
		if real {
			res.Mismatches = append(res.Mismatches, m)
		}
	}
	return res, nil
}

// fullReconcile adds up the whole ledgers and rebuilds the checkpoints.
func fullReconcile(ctx context.Context, tx tenancy.TenantTx) (ReconcileResult, error) {
	found, err := Reconcile(ctx, tx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := RebuildCheckpoints(ctx, tx); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Mismatches: found, Full: true}, nil
}

// RebuildCheckpoints records every ledger row's balance up to the newest
// movement older than checkpointLag.
func RebuildCheckpoints(ctx context.Context, tx tenancy.TenantTx) error {
	lag := checkpointLag.Seconds()
	var hiStock, hiBatch int64
	if err := tx.QueryRow(ctx, qStockHigh, lag).Scan(&hiStock); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, qBatchHigh, lag).Scan(&hiBatch); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qClearCheckpoints); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qSaveStock, hiStock); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qSaveBatches, hiBatch); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qSaveMark, markStock, hiStock); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, qSaveMark, markBatch, hiBatch)
	return err
}

type mark struct {
	id    int64
	taken time.Time
}

func loadMarks(ctx context.Context, tx tenancy.TenantTx) (map[string]mark, error) {
	rows, err := tx.Query(ctx, qMarks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]mark{}
	for rows.Next() {
		var name string
		var m mark
		if err := rows.Scan(&name, &m.id, &m.taken); err != nil {
			return nil, err
		}
		out[name] = m
	}
	return out, rows.Err()
}

func scanMismatches(rows pgx.Rows, batches bool) ([]Mismatch, error) {
	defer rows.Close()
	var out []Mismatch
	for rows.Next() {
		var m Mismatch
		dst := []any{&m.StoreID, &m.VariantID, &m.Qty, &m.LedgerSum}
		if batches {
			dst = append(dst, &m.Kind, &m.BatchID)
		}
		if err := rows.Scan(dst...); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// recheck adds up one key's whole ledger. It reports whether the key
// really disagrees, and updates m with the exact figures.
func recheck(ctx context.Context, tx tenancy.TenantTx, m *Mismatch) (bool, error) {
	var row pgx.Row
	switch m.Kind {
	case "":
		row = tx.QueryRow(ctx, qRecheckStock, m.StoreID, m.VariantID)
	case "batch_ledger":
		row = tx.QueryRow(ctx, qRecheckBatch, m.StoreID, m.BatchID, "stock")
	case "batch_quarantine":
		row = tx.QueryRow(ctx, qRecheckBatch, m.StoreID, m.BatchID, "quarantine")
	default:
		return false, errors.New("inventory: unknown reconcile kind " + m.Kind)
	}
	if err := row.Scan(&m.Qty, &m.LedgerSum); err != nil {
		return false, err
	}
	return m.Qty != m.LedgerSum, nil
}
