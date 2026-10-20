// Package inventory owns stock changes. Every change goes through Move or
// Set, which update inventories conditionally (stock never goes below zero,
// P0-4) and append one stock_movements row (S1).
package inventory

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Reasons (stock_movements.reason).
const (
	ReasonOpening     = "opening"
	ReasonSale        = "sale"
	ReasonSaleEdit    = "sale_edit"
	ReasonSaleVoid    = "sale_void"
	ReasonReturn      = "return"
	ReasonPurchase    = "purchase_receive"
	ReasonAdjustment  = "adjustment"
	ReasonManualSet   = "manual_set"
	ReasonProductEdit = "product_edit"
	ReasonImport      = "import"
)

// ErrInsufficient means a decrement would take stock below zero.
var ErrInsufficient = errors.New("inventory: insufficient stock")

// Ref links a movement to its cause (sale 12, purchase 3, ...).
type Ref struct {
	Type string
	ID   int64
}

const (
	qMoveExisting = `UPDATE inventories SET qty = qty + $3, updated_at = now()
		WHERE store_id = $1 AND variant_id = $2 AND qty + $3 >= 0 RETURNING qty`
	qMoveInsert = `INSERT INTO inventories (store_id, variant_id, qty) VALUES ($1, $2, $3)
		ON CONFLICT (store_id, variant_id) DO UPDATE SET qty = inventories.qty + EXCLUDED.qty, updated_at = now()
		RETURNING qty`
	qLedger = `INSERT INTO stock_movements (store_id, variant_id, delta, qty_after, reason, ref_type, ref_id, user_id)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, 0), NULLIF($8, 0))`
	qLockQty = `SELECT qty FROM inventories WHERE store_id = $1 AND variant_id = $2 FOR UPDATE`
)

// Move adds delta (negative to remove) and records the movement. A
// decrement that would go below zero returns ErrInsufficient and changes
// nothing. Callers that move several items lock them in variant order first
// (see LockRows) so concurrent sales cannot deadlock.
func Move(ctx context.Context, tx tenancy.TenantTx, storeID, variantID int64, delta int, reason string, ref Ref, userID int64) (int, error) {
	if delta == 0 {
		var q int
		err := tx.QueryRow(ctx, `SELECT coalesce((SELECT qty FROM inventories WHERE store_id = $1 AND variant_id = $2), 0)`, storeID, variantID).Scan(&q)
		return q, err
	}
	var after int
	err := tx.QueryRow(ctx, qMoveExisting, storeID, variantID, delta).Scan(&after)
	if errors.Is(err, pgx.ErrNoRows) {
		if delta < 0 {
			return 0, ErrInsufficient
		}
		err = tx.QueryRow(ctx, qMoveInsert, storeID, variantID, delta).Scan(&after)
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, qLedger, storeID, variantID, delta, after, reason, ref.Type, ref.ID, userID)
	return after, err
}

// Set makes qty an absolute value (manual set, product edit) and records
// the difference. qty must be >= 0.
func Set(ctx context.Context, tx tenancy.TenantTx, storeID, variantID int64, qty, alert int, reason string, ref Ref, userID int64) (int64, error) {
	if qty < 0 || alert < 0 {
		return 0, errors.New("inventory: negative quantity")
	}
	var before int
	err := tx.QueryRow(ctx, qLockQty, storeID, variantID).Scan(&before)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO inventories (store_id, variant_id, qty, quantity_alert) VALUES ($1, $2, $3, $4)
		ON CONFLICT (store_id, variant_id) DO UPDATE SET qty = EXCLUDED.qty, quantity_alert = EXCLUDED.quantity_alert, updated_at = now()
		RETURNING id`, storeID, variantID, qty, alert).Scan(&id); err != nil {
		return 0, err
	}
	if delta := qty - before; delta != 0 {
		if _, err := tx.Exec(ctx, qLedger, storeID, variantID, delta, qty, reason, ref.Type, ref.ID, userID); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// Item is one (store, variant) pair.
type Item struct {
	StoreID   int64
	VariantID int64
}

// LockRows locks inventory rows of one store in variant_id order and returns
// current quantities (missing rows = 0). Deterministic order prevents
// deadlocks between concurrent checkouts (PHASE1_DESIGN §4.4).
func LockRows(ctx context.Context, tx tenancy.TenantTx, storeID int64, variantIDs []int64) (map[int64]int, error) {
	rows, err := tx.Query(ctx, `SELECT variant_id, qty FROM inventories
		WHERE store_id = $1 AND variant_id = ANY($2) ORDER BY variant_id FOR UPDATE`, storeID, variantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]int, len(variantIDs))
	for _, v := range variantIDs {
		out[v] = 0
	}
	for rows.Next() {
		var v int64
		var q int
		if err := rows.Scan(&v, &q); err != nil {
			return nil, err
		}
		out[v] = q
	}
	return out, rows.Err()
}

// Mismatch is a ledger/stock disagreement found by Reconcile.
type Mismatch struct {
	StoreID, VariantID int64
	Qty, LedgerSum     int
}

// Reconcile compares every inventory row with the sum of its movements
// (the nightly S1 check; the job itself arrives with the worker step).
func Reconcile(ctx context.Context, tx tenancy.TenantTx) ([]Mismatch, error) {
	rows, err := tx.Query(ctx, `SELECT coalesce(i.store_id, m.store_id), coalesce(i.variant_id, m.variant_id),
			coalesce(i.qty, 0), coalesce(m.total, 0)
		FROM inventories i
		FULL JOIN (SELECT store_id, variant_id, sum(delta)::int AS total FROM stock_movements GROUP BY store_id, variant_id) m
		  ON m.store_id = i.store_id AND m.variant_id = i.variant_id
		WHERE coalesce(i.qty, 0) <> coalesce(m.total, 0)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mismatch
	for rows.Next() {
		var m Mismatch
		if err := rows.Scan(&m.StoreID, &m.VariantID, &m.Qty, &m.LedgerSum); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
