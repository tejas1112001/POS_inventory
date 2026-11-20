package stockcounts

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Industry P3 item 6 (design §6, A32): counting tracked products by batch.
//
// A line of a tracked product may name a batch (an existing one, or a
// batch number and expiry found on the shelf, which creates it). A product
// counted with at least one batch line is counted *by batch*: on
// completion each of its batches in the store becomes its counted
// quantity, a line with no batch counts the unknown batch, and batches not
// on the count go to 0 (their units were counted under real batches). A
// product counted with no batch line is counted as a whole, as before.

// UncountedBatchJSON is a batch with stock that completion will set to 0,
// because its product is counted by batch and the batch has no line.
type UncountedBatchJSON struct {
	BatchID     int64   `json:"batchId"`
	BatchNo     string  `json:"batchNo"`
	Expiry      *string `json:"expiry"`
	Unknown     bool    `json:"unknown"`
	VariantID   int64   `json:"variantId"`
	ProductName string  `json:"productName"`
	SKU         string  `json:"sku"`
	CurrentQty  int     `json:"currentQty"`
}

const qUncountedBatches = `SELECT s.batch_id, b.batch_no::text, to_char(b.expiry_date, 'YYYY-MM-DD'), b.is_unknown, b.variant_id,
		coalesce(p.name, ''), coalesce(v.sku::text, ''), s.qty
	FROM batch_stock s JOIN batches b ON b.id = s.batch_id
	JOIN product_variants v ON v.id = b.variant_id JOIN products p ON p.id = v.product_id
	WHERE s.store_id = $2 AND s.qty > 0
	  AND EXISTS (SELECT 1 FROM stock_count_lines o WHERE o.count_id = $1 AND o.variant_id = b.variant_id AND o.batch_id IS NOT NULL)
	  AND NOT EXISTS (SELECT 1 FROM stock_count_lines o WHERE o.count_id = $1 AND o.variant_id = b.variant_id
	                  AND (o.batch_id = b.id OR (o.batch_id IS NULL AND b.is_unknown)))
	ORDER BY p.name, v.sku, b.expiry_date NULLS LAST, b.id`

func loadUncountedBatches(ctx context.Context, tx tenancy.TenantTx, countID, storeID int64) ([]UncountedBatchJSON, error) {
	rows, err := tx.Query(ctx, qUncountedBatches, countID, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UncountedBatchJSON{}
	for rows.Next() {
		var u UncountedBatchJSON
		if err := rows.Scan(&u.BatchID, &u.BatchNo, &u.Expiry, &u.Unknown, &u.VariantID, &u.ProductName, &u.SKU, &u.CurrentQty); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// resolveCountBatches checks the batches named on changes and creates the
// ones found on the shelf. Batches only go on lines of products that track
// batches (module off: 403).
func resolveCountBatches(ctx context.Context, tx tenancy.TenantTx, in []change, uid int64) error {
	var vids []int64
	for _, c := range in {
		if c.batch > 0 || c.batchNo != "" {
			vids = append(vids, c.variant)
		}
	}
	if len(vids) == 0 {
		return nil
	}
	tr, err := inventory.Tracked(ctx, tx, vids)
	if err != nil {
		return err
	}
	if len(tr) == 0 {
		on, err := inventory.ModuleOn(ctx, tx)
		if err != nil {
			return err
		}
		if !on {
			return modules.ErrOff(modules.BatchExpiry)
		}
	}
	f := map[string]string{}
	for i := range in {
		c := &in[i]
		if c.batch == 0 && c.batchNo == "" {
			continue
		}
		if !tr[c.variant] {
			f[c.field] = "This product does not track batches."
			continue
		}
		if c.batchNo != "" {
			id, _, err := batches.Resolve(ctx, tx, c.variant, batches.Arrival{BatchNo: c.batchNo, Expiry: c.expiry}, uid)
			switch {
			case errors.Is(err, batches.ErrExpiryMismatch):
				f[c.field+".expiry"] = "This batch number is on record with another expiry date. Check the pack."
			case errors.Is(err, batches.ErrReservedNo):
				f[c.field+".batchNo"] = "This name is kept for stock with no batch."
			case err != nil:
				return err
			default:
				c.batch = id
			}
			continue
		}
		var v int64
		err := tx.QueryRow(ctx, `SELECT variant_id FROM batches WHERE id = $1`, c.batch).Scan(&v)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && v != c.variant) {
			f[c.field+".batchId"] = "Not a batch of this product."
			continue
		}
		if err != nil {
			return err
		}
	}
	return apperr.Fields(f)
}

// countLine is one counted line at completion.
type countLine struct {
	id, batch int64
	qty       int
}

// postVariant posts one variant's count. It returns each line's expected
// quantity and difference (by line id), and the units moved up and down.
func postVariant(ctx context.Context, tx tenancy.TenantTx, storeID, variant int64, lines []countLine, have int, byBatch bool,
	ref inventory.Ref, uid int64) (exp, diff map[int64]int, up, down int, err error) {
	exp, diff = map[int64]int{}, map[int64]int{}
	if !byBatch {
		total := 0
		for _, l := range lines {
			total += l.qty
		}
		d := total - have
		for i, l := range lines {
			exp[l.id] = have
			if i == 0 {
				diff[l.id] = d
			}
		}
		if d != 0 {
			if _, err = inventory.Move(ctx, tx, storeID, variant, d, inventory.ReasonStockCount, ref, uid); err != nil {
				return
			}
		}
		if d > 0 {
			up = d
		} else {
			down = -d
		}
		return
	}
	counted := map[int64]int{}
	key := make([]int64, len(lines))
	for i, l := range lines {
		b := l.batch
		if b == 0 {
			if b, err = inventory.UnknownBatch(ctx, tx, variant, uid); err != nil {
				return
			}
		}
		key[i] = b
		counted[b] += l.qty
	}
	var before map[int64]int
	if before, err = inventory.CountBatches(ctx, tx, storeID, variant, counted, inventory.ReasonStockCount, ref, uid); err != nil {
		return
	}
	done := map[int64]bool{}
	for i, l := range lines {
		b := key[i]
		exp[l.id] = before[b]
		if !done[b] {
			diff[l.id] = counted[b] - before[b]
			done[b] = true
		}
	}
	for b := range before {
		if _, ok := counted[b]; !ok {
			counted[b] = 0
		}
	}
	for b, q := range counted {
		switch d := q - before[b]; {
		case d > 0:
			up += d
		case d < 0:
			down -= d
		}
	}
	return
}
