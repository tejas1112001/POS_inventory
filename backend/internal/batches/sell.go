package batches

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Selling (industry P3 item 3, design §3): each line of a tracked product
// is filled from its batches in the sale's store, earliest expiry first
// (FEFO), the unknown batch last with a warning (Q14). The cashier may name
// batches (Q12); expired, recalled and too-close-to-expiry batches are
// never sold.

// Pick is a batch the cashier chose for a line.
type Pick struct {
	BatchID int64
	Qty     qty.Q
}

// Want is one tracked sale line to fill.
type Want struct {
	Index   int // the line's index in the sale
	Variant int64
	SKU     string
	Qty     qty.Q
	Picks   []Pick
}

// Sold is one batch of a filled line, with the details copied onto the
// sale (A27 snapshot).
type Sold struct {
	BatchID int64
	BatchNo string
	Expiry  *time.Time
	MRP     *decimal.Decimal
	MRPPack decimal.Decimal // base units MRP is printed for (00046)
	Qty     qty.Q
	Chosen  bool
	Unknown bool
}

// Warning is shown at the till before payment and kept on the sale line.
type Warning struct {
	Index    int    `json:"index"`
	Variant  int64  `json:"variantId"`
	SKU      string `json:"sku"`
	Quantity qty.Q  `json:"quantity"`
	Message  string `json:"message"`
}

// UnbatchedWarning is the text of the unknown batch warning (Q10).
const UnbatchedWarning = "No batch or expiry recorded for this stock. Check the pack."

// sellRow is one batch's stock in the store.
type sellRow struct {
	id      int64
	no      string
	expiry  *time.Time
	mrp     *decimal.Decimal
	mrpPack decimal.Decimal
	unknown bool
	status  string
	qty     qty.Q // left to sell in this request
}

const (
	qSellRows = `SELECT b.id, b.batch_no::text, b.expiry_date, b.mrp, b.mrp_pack, b.is_unknown, b.status, s.qty
		FROM batch_stock s JOIN batches b ON b.id = s.batch_id
		WHERE s.store_id = $1 AND b.variant_id = $2 AND s.qty > 0 ORDER BY s.batch_id`
	qSellRowsLock = `SELECT b.id, b.batch_no::text, b.expiry_date, b.mrp, b.mrp_pack, b.is_unknown, b.status, s.qty
		FROM batch_stock s JOIN batches b ON b.id = s.batch_id
		WHERE s.store_id = $1 AND b.variant_id = $2 AND s.qty > 0 ORDER BY s.batch_id FOR UPDATE OF s FOR SHARE OF b`
	qBatchOf = `SELECT variant_id, batch_no::text FROM batches WHERE id = $1`
)

func sellRows(ctx context.Context, tx tenancy.TenantTx, store, variant int64, lock bool) ([]*sellRow, error) {
	var rows pgx.Rows
	var err error
	if lock {
		rows, err = tx.Query(ctx, qSellRowsLock, store, variant)
	} else {
		rows, err = tx.Query(ctx, qSellRows, store, variant)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*sellRow
	for rows.Next() {
		r := &sellRow{}
		if err := rows.Scan(&r.id, &r.no, &r.expiry, &r.mrp, &r.mrpPack, &r.unknown, &r.status, &r.qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// whyNot says why a batch cannot be sold today, or "" when it can.
func (s Settings) whyNot(r *sellRow) string {
	switch {
	case r.status == "recalled":
		return "recalled"
	case r.unknown || r.expiry == nil:
		return ""
	case s.Expired(*r.expiry):
		return "expired"
	case s.MinDaysToSell > 0 && r.expiry.Before(s.Today.AddDate(0, 0, s.MinDaysToSell)):
		return fmt.Sprintf("expires within %d days", s.MinDaysToSell)
	}
	return ""
}

// Unsellable is one entry of the shortage details.
type Unsellable struct {
	BatchID  int64   `json:"batchId"`
	BatchNo  string  `json:"batchNo"`
	Expiry   *string `json:"expiry"`
	Quantity qty.Q   `json:"quantity"`
	Reason   string  `json:"reason"`
}

func day(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// Allocate fills the tracked lines of a sale in one store. lock takes the
// batch stock rows FOR UPDATE (a checkout; the caller already holds the
// inventories rows), otherwise it only reads (the quote). It returns each
// line's batches by index and the warnings.
func Allocate(ctx context.Context, tx tenancy.TenantTx, st Settings, store int64, wants []Want, lock bool) (map[int][]Sold, []Warning, error) {
	byVariant := map[int64][]*sellRow{}
	var variants []int64
	for _, w := range wants {
		if _, ok := byVariant[w.Variant]; !ok {
			byVariant[w.Variant] = nil
			variants = append(variants, w.Variant)
		}
	}
	sort.Slice(variants, func(a, b int) bool { return variants[a] < variants[b] })
	for _, v := range variants {
		rs, err := sellRows(ctx, tx, store, v, lock)
		if err != nil {
			return nil, nil, err
		}
		byVariant[v] = rs
	}
	out := map[int][]Sold{}
	var warns []Warning
	for _, w := range wants {
		rs := byVariant[w.Variant]
		rest := w.Qty
		var got []Sold
		for _, p := range w.Picks {
			var r *sellRow
			for _, x := range rs {
				if x.id == p.BatchID {
					r = x
				}
			}
			if r == nil {
				var vid int64
				var no string
				if err := tx.QueryRow(ctx, qBatchOf, p.BatchID).Scan(&vid, &no); err != nil || vid != w.Variant {
					return nil, nil, apperr.Invalid(fmt.Sprintf("orderItems[%d].batches", w.Index), "This batch is not one of this product's.")
				}
				return nil, nil, apperr.Conflict(apperr.CodeInsufficientStock, fmt.Sprintf("Batch %s of %s has no stock in this store.", no, w.SKU)).
					WithDetails(map[string]any{"index": w.Index, "variantId": w.Variant, "batchId": p.BatchID, "available": 0, "requested": p.Qty})
			}
			if why := st.whyNot(r); why != "" {
				return nil, nil, apperr.Conflict(apperr.CodeBatchNotSellable, fmt.Sprintf("Batch %s of %s cannot be sold: %s.", r.no, w.SKU, why)).
					WithDetails(map[string]any{"index": w.Index, "variantId": w.Variant, "batchId": r.id, "batchNo": r.no,
						"expiry": day(r.expiry), "reason": why})
			}
			if p.Qty.Greater(r.qty) {
				return nil, nil, apperr.Conflict(apperr.CodeInsufficientStock, fmt.Sprintf("Batch %s of %s has %s left; %s asked.", r.no, w.SKU, r.qty, p.Qty)).
					WithDetails(map[string]any{"index": w.Index, "variantId": w.Variant, "batchId": r.id, "available": r.qty, "requested": p.Qty})
			}
			if p.Qty.Greater(rest) {
				return nil, nil, apperr.Invalid(fmt.Sprintf("orderItems[%d].batches", w.Index), "The batch quantities add up to more than the line.")
			}
			r.qty = r.qty.Sub(p.Qty)
			rest = rest.Sub(p.Qty)
			got = append(got, Sold{BatchID: r.id, BatchNo: r.no, Expiry: r.expiry, MRP: r.mrp, MRPPack: r.mrpPack, Qty: p.Qty, Chosen: true, Unknown: r.unknown})
		}
		if rest.IsPositive() {
			order := make([]*sellRow, 0, len(rs))
			var bad []Unsellable
			for _, r := range rs {
				if why := st.whyNot(r); why != "" {
					if r.qty.IsPositive() {
						bad = append(bad, Unsellable{BatchID: r.id, BatchNo: r.no, Expiry: day(r.expiry), Quantity: r.qty, Reason: why})
					}
					continue
				}
				order = append(order, r)
			}
			sort.SliceStable(order, func(a, b int) bool {
				x, y := order[a], order[b]
				if x.unknown != y.unknown {
					return !x.unknown
				}
				if x.expiry != nil && y.expiry != nil && !x.expiry.Equal(*y.expiry) {
					return x.expiry.Before(*y.expiry)
				}
				return x.id < y.id
			})
			var sellable qty.Q
			for _, r := range order {
				sellable = sellable.Add(r.qty)
			}
			if sellable.Less(rest) {
				avail := sellable.Add(w.Qty).Sub(rest)
				msg := fmt.Sprintf("Not enough sellable stock of %s: %s can be sold, %s asked.", w.SKU, avail, w.Qty)
				return nil, nil, apperr.Conflict(apperr.CodeInsufficientStock, msg).WithDetails(map[string]any{"items": []map[string]any{{
					"index": w.Index, "variantId": w.Variant, "sku": w.SKU, "available": avail, "requested": w.Qty,
					"unsellable": bad}}})
			}
			for _, r := range order {
				if rest.IsZero() {
					break
				}
				take := qty.Min(rest, r.qty)
				if !take.IsPositive() {
					continue
				}
				r.qty = r.qty.Sub(take)
				rest = rest.Sub(take)
				got = append(got, Sold{BatchID: r.id, BatchNo: r.no, Expiry: r.expiry, MRP: r.mrp, MRPPack: r.mrpPack, Qty: take, Unknown: r.unknown})
			}
		}
		for _, s := range got {
			if s.Unknown {
				warns = append(warns, Warning{Index: w.Index, Variant: w.Variant, SKU: w.SKU, Quantity: s.Qty, Message: UnbatchedWarning})
			}
		}
		out[w.Index] = got
	}
	return out, warns, nil
}
