package sales

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Industry P3 item 4 (design §4): returned units of a tracked product go
// back to the batches they were sold from. Resellable units of a batch that
// can still be sold go to its stock; the rest (damaged or defective, or the
// batch expired or recalled) go to its quarantine, which is outside
// inventories.qty (§1.4) and never sold.

const (
	bucketStock      = "stock"
	bucketQuarantine = "quarantine"
	condResellable   = "resellable"
)

// rowBatch is where some units of a return row go.
type rowBatch struct {
	batchID int64
	qty     qty.Q
	bucket  string
}

// retBatch is a batch a row can go back to.
type retBatch struct {
	id      int64
	left    qty.Q // sold from it and not yet returned
	expiry  *time.Time
	unknown bool
	status  string
}

func (b retBatch) sellable(st batches.Settings) bool {
	return b.status == "available" && (b.expiry == nil || !st.Expired(*b.expiry))
}

const (
	// Latest expiry first (the reverse of the order they were sold in), so
	// the unknown batch, sold last, comes back first.
	qReturnSoldBatches = `SELECT ob.batch_id, ob.quantity - coalesce((SELECT sum(rb.quantity) FROM return_item_batches rb
			JOIN return_items ri ON ri.id = rb.return_item_id WHERE ri.order_item_id = ob.order_item_id AND rb.batch_id = ob.batch_id), 0),
			b.expiry_date, b.is_unknown, b.status
		FROM order_item_batches ob JOIN batches b ON b.id = ob.batch_id
		WHERE ob.order_item_id = $1
		ORDER BY b.is_unknown DESC, b.expiry_date DESC NULLS FIRST, ob.batch_id DESC`
	qReturnOneBatch    = `SELECT variant_id, expiry_date, is_unknown, status FROM batches WHERE id = $1`
	qInsertReturnBatch = `INSERT INTO return_item_batches (return_item_id, batch_id, quantity, bucket) VALUES ($1, $2, $3, $4)`
)

// planReturnBatches works out each tracked row's batches (it runs before
// the stock rows are locked). For tracked products the condition is always
// asked, whatever the return policy. field names the body's lines
// (returnItems or returnLines). A batchId is taken on lines of a return
// without a sale only.
func planReturnBatches(ctx context.Context, tx tenancy.TenantTx, pol ReturnPolicy, pr *preparedReturn, c cleanReturn, field string, userID int64) error {
	vids := make([]int64, 0, len(pr.rows))
	for _, x := range pr.rows {
		vids = append(vids, x.variantID)
	}
	tr, err := inventory.Tracked(ctx, tx, vids)
	if err != nil {
		return err
	}
	f := fieldErrs{}
	named := false
	for i, l := range c.lines {
		if l.batchID == 0 {
			continue
		}
		named = true
		if c.saleID > 0 {
			f[fmt.Sprintf("%s[%d].batchId", field, i)] = "A return against a sale goes back to the batches it was sold from."
		}
	}
	if named && len(tr) == 0 {
		on, err := inventory.ModuleOn(ctx, tx)
		if err != nil {
			return err
		}
		if !on {
			return modules.ErrOff(modules.BatchExpiry)
		}
	}
	if err := f.err(); err != nil {
		return err
	}
	var st batches.Settings
	if len(tr) > 0 {
		if st, err = batches.LoadSettings(ctx, tx); err != nil {
			return err
		}
	}
	sold := map[int64][]*retBatch{} // by order item, shared by its rows
	for j := range pr.rows {
		x := &pr.rows[j]
		var line returnLine
		if x.line >= 0 && x.line < len(c.lines) {
			line = c.lines[x.line]
		}
		if !tr[x.variantID] {
			if line.batchID > 0 {
				f[fmt.Sprintf("%s[%d].batchId", field, x.line)] = "This product does not track batches."
			}
			continue
		}
		x.tracked = true
		if x.condition == "" {
			f[fmt.Sprintf("%s[%d].condition", field, x.line)] = "Choose the condition: resellable, damaged or defective (asked for every product that tracks batches)."
			continue
		}
		good := pol.restocks(x.condition) && (pol.Conditions || x.condition == condResellable)
		add := func(b retBatch, n qty.Q) {
			bucket := bucketQuarantine
			if good && b.sellable(st) {
				bucket = bucketStock
			}
			for i := range x.batches {
				if x.batches[i].batchID == b.id && x.batches[i].bucket == bucket {
					x.batches[i].qty = x.batches[i].qty.Add(n)
					return
				}
			}
			x.batches = append(x.batches, rowBatch{batchID: b.id, qty: n, bucket: bucket})
		}
		left := x.qty
		switch {
		case x.orderItemID != nil:
			list, ok := sold[*x.orderItemID]
			if !ok {
				if list, err = soldBatches(ctx, tx, *x.orderItemID); err != nil {
					return err
				}
				sold[*x.orderItemID] = list
			}
			for _, b := range list {
				take := qty.Min(left, b.left)
				if !take.IsPositive() {
					continue
				}
				b.left = b.left.Sub(take)
				left = left.Sub(take)
				add(*b, take)
			}
			if left.IsPositive() { // sold before tracking began: the unknown batch
				id, err := inventory.UnknownBatch(ctx, tx, x.variantID, userID)
				if err != nil {
					return err
				}
				add(retBatch{id: id, unknown: true, status: "available"}, left)
			}
		case line.batchID > 0:
			b := retBatch{id: line.batchID}
			var variant int64
			err := tx.QueryRow(ctx, qReturnOneBatch, line.batchID).Scan(&variant, &b.expiry, &b.unknown, &b.status)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && variant != x.variantID) {
				f[fmt.Sprintf("%s[%d].batchId", field, x.line)] = "Not a batch of this product."
				continue
			}
			if err != nil {
				return err
			}
			add(b, left)
		default: // no sale, no batch named: quarantine under the unknown batch
			id, err := inventory.UnknownBatch(ctx, tx, x.variantID, userID)
			if err != nil {
				return err
			}
			good = false
			add(retBatch{id: id, unknown: true, status: "available"}, left)
		}
		x.restocked = false
		for _, b := range x.batches {
			if b.bucket == bucketStock {
				x.restocked = true
			}
		}
	}
	return f.err()
}

func soldBatches(ctx context.Context, tx tenancy.TenantTx, orderItemID int64) ([]*retBatch, error) {
	rows, err := tx.Query(ctx, qReturnSoldBatches, orderItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*retBatch
	for rows.Next() {
		b := &retBatch{}
		if err := rows.Scan(&b.id, &b.left, &b.expiry, &b.unknown, &b.status); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// returnAlloc names the batches of the units going back to stock.
func returnAlloc(pr preparedReturn) inventory.Allocation {
	alloc := inventory.Allocation{}
	for _, x := range pr.rows {
		k := inventory.Key{Store: pr.storeID, Variant: x.variantID}
		for _, b := range x.batches {
			if b.bucket == bucketStock {
				alloc[k] = append(alloc[k], inventory.BatchQty{BatchID: b.batchID, Qty: b.qty})
			}
		}
	}
	return alloc
}

// quarantineKeys are the stock rows of units going to quarantine; they are
// locked with the rest (§1.5) though their stock does not change.
func quarantineKeys(pr preparedReturn) []inventory.Key {
	var out []inventory.Key
	for _, x := range pr.rows {
		for _, b := range x.batches {
			if b.bucket == bucketQuarantine {
				out = append(out, inventory.Key{Store: pr.storeID, Variant: x.variantID})
			}
		}
	}
	return out
}

// returnLocks is what a return locks: its stock deltas, and the rows of
// quarantined units (a positive placeholder locks without a check).
func returnLocks(pr preparedReturn, deltas map[inventory.Key]qty.Q) map[inventory.Key]qty.Q {
	out := make(map[inventory.Key]qty.Q, len(deltas))
	for k, d := range deltas {
		out[k] = d
	}
	for _, k := range quarantineKeys(pr) {
		if out[k].IsZero() {
			out[k] = qty.Int(1)
		}
	}
	return out
}

// writeReturnBatches records a row's batches (DB32).
func writeReturnBatches(ctx context.Context, tx tenancy.TenantTx, itemID int64, x returnRow) error {
	for _, b := range x.batches {
		if _, err := tx.Exec(ctx, qInsertReturnBatch, itemID, b.batchID, b.qty, b.bucket); err != nil {
			return err
		}
	}
	return nil
}

// quarantineReturned puts the quarantined units in their batches, in batch
// order. Their stock rows are already locked.
func quarantineReturned(ctx context.Context, tx tenancy.TenantTx, pr preparedReturn, ref inventory.Ref, userID int64) error {
	n := map[int64]qty.Q{}
	for _, x := range pr.rows {
		for _, b := range x.batches {
			if b.bucket == bucketQuarantine {
				n[b.batchID] = n[b.batchID].Add(b.qty)
			}
		}
	}
	ids := make([]int64, 0, len(n))
	for id := range n {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := inventory.Quarantine(ctx, tx, pr.storeID, id, n[id], inventory.ReasonReturn, ref, userID); err != nil {
			return err
		}
	}
	return nil
}
