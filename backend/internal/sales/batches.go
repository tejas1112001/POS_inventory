package sales

import (
	"context"
	"fmt"
	"sort"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Industry P3 item 3 (design §3): selling tracked products by batch.

// saleBatches is how a sale's tracked lines are filled.
type saleBatches struct {
	sold  map[int][]batches.Sold // by line index
	warns []batches.Warning
	alloc inventory.Allocation
}

// allocateBatches fills the tracked lines of a priced sale from their
// batches in the store. lock locks the batch rows (a checkout; the stock
// rows are already locked); without it the batches are only read (quote).
// A batch named on an untracked line is refused.
func allocateBatches(ctx context.Context, tx tenancy.TenantTx, store int64, pr priced, lock bool) (saleBatches, error) {
	out := saleBatches{sold: map[int][]batches.Sold{}, alloc: inventory.Allocation{}}
	ids := make([]int64, 0, len(pr.lines))
	for _, l := range pr.lines {
		ids = append(ids, l.v.id)
	}
	tr, err := inventory.Tracked(ctx, tx, ids)
	if err != nil {
		return out, err
	}
	var wants []batches.Want
	for i, l := range pr.lines {
		if !tr[l.v.id] {
			if len(l.picks) > 0 {
				on, err := inventory.ModuleOn(ctx, tx)
				if err != nil {
					return out, err
				}
				if !on {
					return out, modules.ErrOff(modules.BatchExpiry)
				}
				return out, apperr.Invalid(fmt.Sprintf("orderItems[%d].batches", i), "This product does not track batches.")
			}
			continue
		}
		wants = append(wants, batches.Want{Index: i, Variant: l.v.id, SKU: l.v.sku, Qty: l.qty, Picks: l.picks})
	}
	if len(wants) == 0 {
		return out, nil
	}
	st, err := batches.LoadSettings(ctx, tx)
	if err != nil {
		return out, err
	}
	sold, warns, err := batches.Allocate(ctx, tx, st, store, wants, lock)
	if err != nil {
		return out, err
	}
	out.sold, out.warns = sold, warns
	idx := make([]int, 0, len(sold))
	for i := range sold {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		k := inventory.Key{Store: store, Variant: pr.lines[i].v.id}
		for _, s := range sold[i] {
			out.alloc[k] = append(out.alloc[k], inventory.BatchQty{BatchID: s.BatchID, Qty: s.Qty})
		}
	}
	return out, nil
}

// insertSoldBatches records each line's batches with the details at sale
// time (DB31); one row per batch and line.
func insertSoldBatches(ctx context.Context, tx tenancy.TenantTx, itemIDs []int64, sb saleBatches) error {
	for i, sold := range sb.sold {
		type row struct {
			s      batches.Sold
			chosen bool
		}
		var order []int64
		merged := map[int64]*row{}
		for _, s := range sold {
			if r, ok := merged[s.BatchID]; ok {
				r.s.Qty = r.s.Qty.Add(s.Qty)
				r.chosen = r.chosen || s.Chosen
				continue
			}
			merged[s.BatchID] = &row{s: s, chosen: s.Chosen}
			order = append(order, s.BatchID)
		}
		for _, id := range order {
			r := merged[id]
			if _, err := tx.Exec(ctx, `INSERT INTO order_item_batches (order_item_id, batch_id, quantity, chosen, is_unknown, batch_no, expiry_date, mrp, mrp_pack)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, itemIDs[i], id, r.s.Qty, r.chosen, r.s.Unknown, r.s.BatchNo, r.s.Expiry, r.s.MRP, packOf(r.s.MRP, r.s.MRPPack)); err != nil {
				return err
			}
		}
	}
	return nil
}

// soldJSON is a quote line's batches.
func soldJSON(sold []batches.Sold) []ItemBatchJSON {
	out := []ItemBatchJSON{}
	for _, s := range sold {
		b := ItemBatchJSON{BatchID: s.BatchID, BatchNo: s.BatchNo, MRP: s.MRP, Quantity: s.Qty, Chosen: s.Chosen, Unknown: s.Unknown}
		if s.Expiry != nil {
			d := s.Expiry.Format("2006-01-02")
			b.Expiry = &d
		}
		out = append(out, b)
	}
	return out
}

// lineWarnings is a line's warnings (stock with no batch).
func lineWarnings(sold []batches.Sold) []string {
	for _, s := range sold {
		if s.Unknown {
			return []string{batches.UnbatchedWarning}
		}
	}
	return []string{}
}

// refuseIfBatched refuses to edit a sale with tracked lines, before or
// after the edit (Q13): an edit restocks and sells again, which would
// re-allocate batches the customer already has.
func refuseIfBatched(ctx context.Context, tx tenancy.TenantTx, saleID int64, old []oldItem, pr priced) error {
	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM order_item_batches ob JOIN order_items i ON i.id = ob.order_item_id
		WHERE i.sale_id = $1)`, saleID).Scan(&has); err != nil {
		return err
	}
	if !has {
		var ids []int64
		for _, o := range old {
			if o.variantID != nil {
				ids = append(ids, *o.variantID)
			}
		}
		for _, l := range pr.lines {
			ids = append(ids, l.v.id)
		}
		tr, err := inventory.Tracked(ctx, tx, ids)
		if err != nil {
			return err
		}
		has = len(tr) > 0
	}
	if has {
		return apperr.Conflict(apperr.CodeInvalidState, "This sale has products that track batches. Return or void the sale instead.")
	}
	return nil
}

// voidBatches returns a voided sale's units to the batches they were sold
// from (products that still track batches).
func voidBatches(ctx context.Context, tx tenancy.TenantTx, saleID, storeID int64) (inventory.Allocation, error) {
	rows, err := tx.Query(ctx, `SELECT i.variant_id, ob.batch_id, ob.quantity FROM order_item_batches ob
		JOIN order_items i ON i.id = ob.order_item_id WHERE i.sale_id = $1 AND i.variant_id IS NOT NULL ORDER BY ob.id`, saleID)
	if err != nil {
		return nil, err
	}
	type rec struct {
		variant, batch int64
		qty            qty.Q
	}
	var recs []rec
	var ids []int64
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.variant, &r.batch, &r.qty); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
		ids = append(ids, r.variant)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	alloc := inventory.Allocation{}
	if len(recs) == 0 {
		return alloc, nil
	}
	tr, err := inventory.Tracked(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if tr[r.variant] {
			k := inventory.Key{Store: storeID, Variant: r.variant}
			alloc[k] = append(alloc[k], inventory.BatchQty{BatchID: r.batch, Qty: r.qty})
		}
	}
	return alloc, nil
}
