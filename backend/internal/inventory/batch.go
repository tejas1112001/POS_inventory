package inventory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Batches (industry Phase 3 item 1, docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md
// §1). A product that tracks batches, in a shop with the batch_expiry
// module on, keeps its stock per store and batch, and this rule always
// holds (§1.4):
//
//	inventories.qty = sum(batch_stock.qty) for each tracked (store, variant)
//
// Every stock change already goes through Move, Set or Apply, so they keep
// the batches in step here: an increase with no batch named goes to the
// variant's unknown batch, a decrease with no batch named comes out earliest
// expiry first (the unknown batch last). Later items name batches
// explicitly (ApplyBatched). Each batch change writes one batch_movements
// row.
//
// Locking (§1.5): the inventories row of a (store, variant) is always
// locked first (Lock, or Move's and Set's own UPDATE), then that variant's
// batch_stock rows in batch id order, so batch changes never deadlock.

// ModuleBatches is the module key (modules.BatchExpiry; repeated here so
// this package needs no import of modules).
const ModuleBatches = "batch_expiry"

// Batch-only ledger reasons (batch_movements.reason).
const (
	ReasonTrackingOn  = "tracking_on"
	ReasonTrackingOff = "tracking_off"
)

// UnknownBatchNo names the unknown batch of a variant.
const UnknownBatchNo = "UNBATCHED"

// BatchQty is a quantity of one batch.
type BatchQty struct {
	BatchID int64
	Qty     qty.Q // > 0; the sign comes from the key's delta
}

// Allocation names the batches of some keys' deltas. A key not listed, or
// the part of its delta not covered, follows the default rule.
type Allocation map[Key][]BatchQty

const (
	// qTracked: the variants among $1 that track batches now.
	qTracked = `SELECT v.id FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = ANY($1) AND p.tracks_batches
		  AND EXISTS (SELECT 1 FROM settings WHERE id = 1 AND 'batch_expiry' = ANY(modules))`
	qBatchAdd = `WITH s AS (INSERT INTO batch_stock (store_id, batch_id, qty) VALUES ($1, $2, $3)
			ON CONFLICT (store_id, batch_id) DO UPDATE SET qty = batch_stock.qty + EXCLUDED.qty, updated_at = now()
			RETURNING qty)
		INSERT INTO batch_movements (store_id, batch_id, bucket, delta, qty_after, reason, ref_type, ref_id, user_id)
		SELECT $1, $2, 'stock', $3, s.qty, $4, NULLIF($5, ''), NULLIF($6, 0), NULLIF($7, 0) FROM s
		RETURNING qty_after`
	qBatchTake = `WITH s AS (UPDATE batch_stock SET qty = qty - $3, updated_at = now()
			WHERE store_id = $1 AND batch_id = $2 AND qty >= $3 RETURNING qty)
		INSERT INTO batch_movements (store_id, batch_id, bucket, delta, qty_after, reason, ref_type, ref_id, user_id)
		SELECT $1, $2, 'stock', -$3::numeric, s.qty, $4, NULLIF($5, ''), NULLIF($6, 0), NULLIF($7, 0) FROM s
		RETURNING qty_after`
	// qBatchRows locks a variant's batch stock in a store (batch id order).
	qBatchRows = `SELECT bs.batch_id, bs.qty, bs.quarantined_qty, b.expiry_date, b.is_unknown
		FROM batch_stock bs JOIN batches b ON b.id = bs.batch_id
		WHERE bs.store_id = $1 AND b.variant_id = $2
		ORDER BY bs.batch_id FOR UPDATE OF bs`
	qQuarantineAdd = `WITH s AS (INSERT INTO batch_stock (store_id, batch_id, quarantined_qty) VALUES ($1, $2, $3)
			ON CONFLICT (store_id, batch_id) DO UPDATE SET quarantined_qty = batch_stock.quarantined_qty + EXCLUDED.quarantined_qty, updated_at = now()
			RETURNING quarantined_qty)
		INSERT INTO batch_movements (store_id, batch_id, bucket, delta, qty_after, reason, ref_type, ref_id, user_id)
		SELECT $1, $2, 'quarantine', $3, s.quarantined_qty, $4, NULLIF($5, ''), NULLIF($6, 0), NULLIF($7, 0) FROM s
		RETURNING qty_after`
	qQuarantineTake = `WITH s AS (UPDATE batch_stock SET quarantined_qty = quarantined_qty - $3, updated_at = now()
			WHERE store_id = $1 AND batch_id = $2 AND quarantined_qty >= $3 RETURNING quarantined_qty)
		INSERT INTO batch_movements (store_id, batch_id, bucket, delta, qty_after, reason, ref_type, ref_id, user_id)
		SELECT $1, $2, 'quarantine', -$3::numeric, s.quarantined_qty, $4, NULLIF($5, ''), NULLIF($6, 0), NULLIF($7, 0) FROM s
		RETURNING qty_after`
	qUnknownInsert = `INSERT INTO batches (variant_id, batch_no, is_unknown, created_by) VALUES ($1, 'UNBATCHED', true, NULLIF($2, 0))
		ON CONFLICT (variant_id) WHERE is_unknown DO NOTHING RETURNING id`
	qUnknownGet = `SELECT id FROM batches WHERE variant_id = $1 AND is_unknown`
)

// Tracked returns which of the variants track batches now (the product's
// flag and the module).
func Tracked(ctx context.Context, tx tenancy.TenantTx, variants []int64) (map[int64]bool, error) {
	return tracked(ctx, tx, variants)
}

// tracked returns which of the variants track batches now.
func tracked(ctx context.Context, tx tenancy.TenantTx, variants []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(variants) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, qTracked, variants)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// follow keeps the batches of tracked keys in step with their stock
// deltas. The inventories rows are already locked and moved.
func follow(ctx context.Context, tx tenancy.TenantTx, keys []Key, deltas map[Key]qty.Q, alloc Allocation, reason string, ref Ref, userID int64) error {
	vids := make([]int64, 0, len(keys))
	for _, k := range keys {
		if !deltas[k].IsZero() {
			vids = append(vids, k.Variant)
		}
	}
	tr, err := tracked(ctx, tx, vids)
	if err != nil || len(tr) == 0 {
		if err == nil && len(alloc) > 0 {
			return apperr.Invalid("batches", "This product does not track batches.")
		}
		return err
	}
	for k, a := range alloc {
		if len(a) > 0 && !tr[k.Variant] {
			return apperr.Invalid("batches", "This product does not track batches.")
		}
	}
	for _, k := range keys {
		d := deltas[k]
		if d.IsZero() || !tr[k.Variant] {
			continue
		}
		if err := moveBatches(ctx, tx, k, d, alloc[k], reason, ref, userID); err != nil {
			return err
		}
	}
	return nil
}

// moveBatches applies delta to one tracked (store, variant): first the named
// batches, then the default rule for the rest.
func moveBatches(ctx context.Context, tx tenancy.TenantTx, k Key, delta qty.Q, named []BatchQty, reason string, ref Ref, userID int64) error {
	rest := delta
	for _, b := range named {
		if !b.Qty.IsPositive() {
			return apperr.Invalid("batches", "Each batch quantity must be more than 0.")
		}
		step := b.Qty
		if delta.IsNegative() {
			step = b.Qty.Neg()
		}
		if left := rest.Sub(step); (delta.IsPositive() && left.IsNegative()) || (delta.IsNegative() && left.IsPositive()) {
			return apperr.Invalid("batches", "The batch quantities add up to more than the line.")
		}
		if err := batchStep(ctx, tx, k.Store, b.BatchID, step, reason, ref, userID); err != nil {
			return err
		}
		rest = rest.Sub(step)
	}
	switch {
	case rest.IsPositive():
		id, err := UnknownBatch(ctx, tx, k.Variant, userID)
		if err != nil {
			return err
		}
		return batchStep(ctx, tx, k.Store, id, rest, reason, ref, userID)
	case rest.IsNegative():
		return takeEarliest(ctx, tx, k, rest.Neg(), reason, ref, userID)
	}
	return nil
}

// batchStep moves one batch's stock and writes its ledger row.
func batchStep(ctx context.Context, tx tenancy.TenantTx, storeID, batchID int64, delta qty.Q, reason string, ref Ref, userID int64) error {
	var after qty.Q
	var err error
	if delta.IsPositive() {
		err = tx.QueryRow(ctx, qBatchAdd, storeID, batchID, delta, reason, ref.Type, ref.ID, userID).Scan(&after)
	} else {
		err = tx.QueryRow(ctx, qBatchTake, storeID, batchID, delta.Neg(), reason, ref.Type, ref.ID, userID).Scan(&after)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict(apperr.CodeInsufficientStock, "Not enough stock in this batch.").
				WithDetails(map[string]any{"batchId": batchID, "requested": delta.Neg()})
		}
	}
	return err
}

// batchRow is one locked batch_stock row.
type batchRow struct {
	batchID          int64
	qty, quarantined qty.Q
	expiry           *time.Time
	unknown          bool
}

func lockBatchRows(ctx context.Context, tx tenancy.TenantTx, storeID, variantID int64) ([]batchRow, error) {
	rows, err := tx.Query(ctx, qBatchRows, storeID, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []batchRow
	for rows.Next() {
		var r batchRow
		if err := rows.Scan(&r.batchID, &r.qty, &r.quarantined, &r.expiry, &r.unknown); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EarliestFirst orders batches for taking stock out: earliest expiry first,
// then the oldest batch; the unknown batch last (Q14).
func earliestFirst(rs []batchRow) {
	sort.SliceStable(rs, func(a, b int) bool {
		x, y := rs[a], rs[b]
		if x.unknown != y.unknown {
			return !x.unknown
		}
		if x.expiry != nil && y.expiry != nil && !x.expiry.Equal(*y.expiry) {
			return x.expiry.Before(*y.expiry)
		}
		return x.batchID < y.batchID
	})
}

// takeEarliest takes n units of a tracked variant out of a store, earliest
// expiry first. Less batch stock than n breaks the §1.4 rule, which the
// inventories check before it should make impossible.
func takeEarliest(ctx context.Context, tx tenancy.TenantTx, k Key, n qty.Q, reason string, ref Ref, userID int64) error {
	rs, err := lockBatchRows(ctx, tx, k.Store, k.Variant)
	if err != nil {
		return err
	}
	earliestFirst(rs)
	for _, r := range rs {
		if n.IsZero() {
			break
		}
		take := qty.Min(n, r.qty)
		if !take.IsPositive() {
			continue
		}
		if err := batchStep(ctx, tx, k.Store, r.batchID, take.Neg(), reason, ref, userID); err != nil {
			return err
		}
		n = n.Sub(take)
	}
	if n.IsPositive() {
		return fmt.Errorf("inventory: batch stock of variant %d in store %d is %s short of its stock", k.Variant, k.Store, n)
	}
	return nil
}

// Quarantine moves units into (delta > 0) or out of (delta < 0) a batch's
// quarantine in a store (industry P3 item 4), with its ledger row.
// Quarantined units are outside inventories.qty (§1.4): the caller moves
// the sellable side, having locked the stock row first (§1.5).
func Quarantine(ctx context.Context, tx tenancy.TenantTx, storeID, batchID int64, delta qty.Q, reason string, ref Ref, userID int64) error {
	var after qty.Q
	var err error
	switch {
	case delta.IsPositive():
		err = tx.QueryRow(ctx, qQuarantineAdd, storeID, batchID, delta, reason, ref.Type, ref.ID, userID).Scan(&after)
	case delta.IsNegative():
		err = tx.QueryRow(ctx, qQuarantineTake, storeID, batchID, delta.Neg(), reason, ref.Type, ref.ID, userID).Scan(&after)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict(apperr.CodeInsufficientStock, "Not enough units of this batch in quarantine.").
				WithDetails(map[string]any{"batchId": batchID, "requested": delta.Neg()})
		}
	}
	return err
}

// CountBatches sets a tracked variant's batches in a store to counted
// quantities (industry P3 item 6): batches not in counted go to 0. The
// stock row moves by the total difference, so stock = batch sum still
// holds (§1.4). The caller has locked the inventories row; the batch rows
// are locked here, in batch order. It returns each batch's quantity before.
func CountBatches(ctx context.Context, tx tenancy.TenantTx, storeID, variantID int64, counted map[int64]qty.Q, reason string, ref Ref, userID int64) (map[int64]qty.Q, error) {
	rs, err := lockBatchRows(ctx, tx, storeID, variantID)
	if err != nil {
		return nil, err
	}
	before := map[int64]qty.Q{}
	ids := make([]int64, 0, len(rs)+len(counted))
	for _, r := range rs {
		before[r.batchID] = r.qty
		ids = append(ids, r.batchID)
	}
	for id := range counted {
		if _, ok := before[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	var total qty.Q
	for _, id := range ids {
		d := counted[id].Sub(before[id])
		if d.IsZero() {
			continue
		}
		if err := batchStep(ctx, tx, storeID, id, d, reason, ref, userID); err != nil {
			return nil, err
		}
		total = total.Add(d)
	}
	if !total.IsZero() {
		if _, err := moveStock(ctx, tx, storeID, variantID, total, reason, ref, userID); err != nil {
			return nil, err
		}
	}
	return before, nil
}

// UnknownBatch returns the variant's unknown batch, creating it.
func UnknownBatch(ctx context.Context, tx tenancy.TenantTx, variantID, userID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, qUnknownInsert, variantID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, qUnknownGet, variantID).Scan(&id)
	}
	return id, err
}

// ApplyBatched is Apply with named batches for some keys.
func ApplyBatched(ctx context.Context, tx tenancy.TenantTx, keys []Key, deltas map[Key]qty.Q, alloc Allocation, reason string, ref Ref, userID int64) error {
	if err := applyStock(ctx, tx, keys, deltas, reason, ref, userID); err != nil {
		return err
	}
	return follow(ctx, tx, keys, deltas, alloc, reason, ref, userID)
}

// ------------------------------------------------------- tracking on / off

// stockRow is one locked inventories row.
type stockRow struct {
	store, variant int64
	qty            qty.Q
}

// lockProductStock locks the inventories rows of the products' variants in
// (store, variant) order, the same order as Lock.
func lockProductStock(ctx context.Context, tx tenancy.TenantTx, productIDs []int64) ([]stockRow, error) {
	rows, err := tx.Query(ctx, `SELECT i.store_id, i.variant_id, i.qty FROM inventories i
		JOIN product_variants v ON v.id = i.variant_id
		WHERE v.product_id = ANY($1) ORDER BY i.store_id, i.variant_id FOR UPDATE OF i`, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stockRow
	for rows.Next() {
		var r stockRow
		if err := rows.Scan(&r.store, &r.variant, &r.qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StartTracking brings the products' batches in line with their stock
// (§1.3): whatever stock no batch holds goes into the unknown batch. Run
// when a product starts tracking, or the module is turned on, in the same
// transaction.
func StartTracking(ctx context.Context, tx tenancy.TenantTx, productIDs []int64, userID int64) error {
	if len(productIDs) == 0 {
		return nil
	}
	stock, err := lockProductStock(ctx, tx, productIDs)
	if err != nil {
		return err
	}
	ref := Ref{Type: "tracking"}
	for _, s := range stock {
		rs, err := lockBatchRows(ctx, tx, s.store, s.variant)
		if err != nil {
			return err
		}
		var sum qty.Q
		for _, r := range rs {
			sum = sum.Add(r.qty)
		}
		k := Key{Store: s.store, Variant: s.variant}
		switch d := s.qty.Sub(sum); {
		case d.IsPositive():
			id, err := UnknownBatch(ctx, tx, s.variant, userID)
			if err != nil {
				return err
			}
			if err := batchStep(ctx, tx, s.store, id, d, ReasonTrackingOn, ref, userID); err != nil {
				return err
			}
		case d.IsNegative():
			if err := takeEarliest(ctx, tx, k, d.Neg(), ReasonTrackingOn, ref, userID); err != nil {
				return err
			}
		}
	}
	return nil
}

// StopTracking closes the products' batch stock (§1.3): batches and their
// history stay, the stock carries on as a plain quantity. Refused while
// units are in quarantine (they are outside the stock and would be lost).
func StopTracking(ctx context.Context, tx tenancy.TenantTx, productIDs []int64, userID int64) error {
	if len(productIDs) == 0 {
		return nil
	}
	stock, err := lockProductStock(ctx, tx, productIDs)
	if err != nil {
		return err
	}
	ref := Ref{Type: "tracking"}
	// Rows with batch stock but no inventories row cannot exist (§1.4), so
	// the locked inventories rows cover every batch row.
	for _, s := range stock {
		rs, err := lockBatchRows(ctx, tx, s.store, s.variant)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if r.quarantined.IsPositive() {
				return apperr.Conflict(apperr.CodeInvalidState, "Some units of this product are in quarantine. Write them off or release them first.")
			}
		}
		for _, r := range rs {
			if r.qty.IsPositive() {
				if err := batchStep(ctx, tx, s.store, r.batchID, r.qty.Neg(), ReasonTrackingOff, ref, userID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// TrackedProducts lists the products that have tracking switched on (the
// flag; the module decides whether it is active).
func TrackedProducts(ctx context.Context, tx tenancy.TenantTx) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM products WHERE tracks_batches ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// ModuleOn reports whether the batch_expiry module is switched on in the
// shop's settings (read in the caller's transaction).
func ModuleOn(ctx context.Context, tx tenancy.TenantTx) (bool, error) {
	var on bool
	err := tx.QueryRow(ctx, `SELECT 'batch_expiry' = ANY(modules) FROM settings WHERE id = 1`).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return on, err
}
