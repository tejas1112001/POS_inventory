package purchases

// Partial purchase receiving (industry P1 item 8, A16, A17; DB13-DB15). A
// purchase is received in one or more goods receipts (GRN-000001); each
// receipt line puts stock in through the ledger. The purchase row lock is
// held while the outstanding quantities are read and changed, so stock goes
// in at most once per piece.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const statusPartial = "Partially received"

// want is one requested receipt line; nil wants everything outstanding.
type want struct {
	itemID  int64
	qty     int
	field   string      // "lines[i]"
	batches []batchWant // industry P3 item 2: tracked products only
}

// batchWant is one batch of a receipt line (A26).
type batchWant struct {
	arrival batches.Arrival
	qty     int
	field   string // "lines[i].batches[j]"
	id      int64  // resolved
	near    bool
}

// receiveOpts are the confirmations a receipt may carry (A26).
type receiveOpts struct {
	acceptNear, confirmFar bool
}

// received is what receive did.
type received struct {
	receiptID    int64
	reference    string
	units        int
	full         bool             // every line is now fully received
	nearAccepted []map[string]any // industry P3 item 2: batches accepted close to expiry
}

// maxYearsAhead: an expiry further ahead needs confirmFarExpiry.
const maxYearsAhead = 10

// receive records a goods receipt for the purchase and puts its quantities
// into stock. lines nil receives everything outstanding (A17 and creating a
// purchase as Received: the ledger keeps today's ref "purchase"); otherwise
// each line must be on this purchase and at most its outstanding quantity
// (A16: ledger ref "purchase_receipt"). The caller holds the purchase's row
// lock.
func receive(ctx context.Context, tx tenancy.TenantTx, purchaseID, userID int64, lines []want, note string, opts receiveOpts) (received, error) {
	type item struct {
		store, variant   int64
		qty, outstanding int
		cost             decimal.Decimal
		w                *want
	}
	rows, err := tx.Query(ctx, `SELECT id, store_id, variant_id, quantity - received_qty, unit_price FROM purchase_items
		WHERE purchase_id = $1 ORDER BY id`, purchaseID)
	if err != nil {
		return received{}, err
	}
	items := map[int64]*item{}
	var order []int64
	for rows.Next() {
		var id int64
		it := &item{}
		if err := rows.Scan(&id, &it.store, &it.variant, &it.outstanding, &it.cost); err != nil {
			rows.Close()
			return received{}, err
		}
		items[id] = it
		order = append(order, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return received{}, err
	}
	legacy := lines == nil
	if legacy {
		for _, id := range order {
			items[id].qty = items[id].outstanding
		}
	} else {
		f := map[string]string{}
		for i, l := range lines {
			k := fmt.Sprintf("lines[%d]", i)
			it := items[l.itemID]
			switch {
			case it == nil:
				f[k+".purchaseItemId"] = "This line is not on the purchase."
			case it.qty > 0:
				f[k+".purchaseItemId"] = "This line is listed twice."
			case l.qty > it.outstanding:
				f[k+".quantity"] = fmt.Sprintf("At most %d still to receive.", it.outstanding)
			default:
				it.qty = l.qty
				it.w = &lines[i]
			}
		}
		if err := apperr.Fields(f); err != nil {
			return received{}, err
		}
	}
	out := received{}
	deltas := map[inventory.Key]int{}
	for _, id := range order {
		if it := items[id]; it.qty > 0 {
			deltas[inventory.Key{Store: it.store, Variant: it.variant}] += it.qty
			out.units += it.qty
		}
	}
	if out.units == 0 {
		return received{}, apperr.Invalid("lines", "Nothing to receive.")
	}
	// Stock rows first, then batches (industry P3 §1.5: the same order as a
	// sale, which locks stock rows, then batch rows).
	keys, _, err := inventory.Lock(ctx, tx, deltas, nil)
	if err != nil {
		return received{}, err
	}
	// Industry P3 item 2: tracked products are received into named batches.
	var vids []int64
	for _, id := range order {
		if it := items[id]; it.qty > 0 {
			vids = append(vids, it.variant)
		}
	}
	tr, err := inventory.Tracked(ctx, tx, vids)
	if err != nil {
		return received{}, err
	}
	alloc := inventory.Allocation{}
	if legacy {
		for _, id := range order {
			if it := items[id]; it.qty > 0 && tr[it.variant] {
				return received{}, apperr.Conflict(apperr.CodeInvalidState,
					"Some products on this purchase track batches. Receive the goods line by line and enter batch and expiry for each.")
			}
		}
	} else {
		nearOut, err := checkBatches(ctx, tx, lines, func(l want) (bool, int64, decimal.Decimal) {
			it := items[l.itemID]
			return tr[it.variant], it.variant, it.cost
		}, opts, userID)
		if err != nil {
			return received{}, err
		}
		out.nearAccepted = nearOut
		for _, id := range order {
			it := items[id]
			if it.qty == 0 || !tr[it.variant] {
				continue
			}
			k := inventory.Key{Store: it.store, Variant: it.variant}
			for _, b := range it.w.batches {
				alloc[k] = append(alloc[k], inventory.BatchQty{BatchID: b.id, Qty: b.qty})
			}
		}
	}
	var n int64
	if err := tx.QueryRow(ctx, `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'goods_receipt' RETURNING next_value - 1`).Scan(&n); err != nil {
		return received{}, err
	}
	out.reference = fmt.Sprintf("GRN-%06d", n)
	if err := tx.QueryRow(ctx, `INSERT INTO purchase_receipts (purchase_id, reference, received_by, note)
		VALUES ($1, $2, $3, NULLIF($4, '')) RETURNING id`, purchaseID, out.reference, userID, note).Scan(&out.receiptID); err != nil {
		return received{}, err
	}
	for _, id := range order {
		it := items[id]
		if it.qty <= 0 {
			continue
		}
		var riID int64
		if err := tx.QueryRow(ctx, `INSERT INTO purchase_receipt_items (receipt_id, purchase_item_id, quantity) VALUES ($1, $2, $3) RETURNING id`,
			out.receiptID, id, it.qty).Scan(&riID); err != nil {
			return received{}, err
		}
		if it.w != nil && tr[it.variant] {
			per := map[int64]int{}
			near := map[int64]bool{}
			var ids []int64
			for _, b := range it.w.batches {
				if _, ok := per[b.id]; !ok {
					ids = append(ids, b.id)
				}
				per[b.id] += b.qty
				near[b.id] = near[b.id] || b.near
			}
			for _, bid := range ids {
				if _, err := tx.Exec(ctx, `INSERT INTO purchase_receipt_batches (receipt_item_id, batch_id, quantity, near_expiry_accepted)
					VALUES ($1, $2, $3, $4)`, riID, bid, per[bid], near[bid]); err != nil {
					return received{}, err
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_items SET received_qty = received_qty + $2 WHERE id = $1`, id, it.qty); err != nil {
			return received{}, err
		}
	}
	ref := inventory.Ref{Type: "purchase_receipt", ID: out.receiptID}
	if legacy {
		ref = inventory.Ref{Type: "purchase", ID: purchaseID}
	}
	if err := inventory.ApplyBatched(ctx, tx, keys, deltas, alloc, inventory.ReasonPurchase, ref, userID); err != nil {
		return received{}, err
	}
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM purchase_items WHERE purchase_id = $1 AND received_qty < quantity)`, purchaseID).Scan(&out.full)
	return out, err
}

// checkBatches validates and resolves the batches of a receipt's lines
// (industry P3 item 2, A26): tracked lines need batches adding up to the
// line; an expired batch is refused; one within the near-expiry window
// needs acceptNearExpiry (409 NEAR_EXPIRY lists them); an existing batch
// number is reused only with the same expiry. It returns the near-expiry
// batches accepted, for the audit.
func checkBatches(ctx context.Context, tx tenancy.TenantTx, lines []want, info func(want) (bool, int64, decimal.Decimal),
	opts receiveOpts, userID int64) ([]map[string]any, error) {
	st, err := batches.LoadSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	f := map[string]string{}
	var near []map[string]any
	for i := range lines {
		l := &lines[i]
		tracked, _, _ := info(*l)
		if !tracked {
			if len(l.batches) > 0 {
				on, err := inventory.ModuleOn(ctx, tx)
				if err != nil {
					return nil, err
				}
				if !on {
					return nil, modules.ErrOff(modules.BatchExpiry)
				}
				f[l.field+".batches"] = "This product does not track batches."
			}
			continue
		}
		if len(l.batches) == 0 {
			f[l.field+".batches"] = "Enter the batch number and expiry for this product."
			continue
		}
		sum := 0
		for j := range l.batches {
			b := &l.batches[j]
			sum += b.qty
			exp := b.arrival.Expiry
			switch {
			case st.Expired(exp):
				f[b.field+".expiry"] = "This batch has expired."
			case !opts.confirmFar && exp.After(st.Today.AddDate(maxYearsAhead, 0, 0)):
				f[b.field+".expiry"] = fmt.Sprintf("More than %d years ahead; check the date, then confirm it.", maxYearsAhead)
			case st.NearExpiry(exp):
				b.near = true
				near = append(near, map[string]any{"line": l.field, "batchNo": b.arrival.BatchNo,
					"expiry": exp.Format("2006-01-02"), "daysLeft": int(exp.Sub(st.Today).Hours() / 24), "quantity": b.qty})
			}
		}
		if sum != l.qty {
			f[l.field+".batches"] = fmt.Sprintf("The batch quantities add up to %d; the line receives %d.", sum, l.qty)
		}
	}
	if err := apperr.Fields(f); err != nil {
		return nil, err
	}
	if len(near) > 0 && !opts.acceptNear {
		return nil, apperr.Conflict(apperr.CodeNearExpiry,
			fmt.Sprintf("Some batches expire within %d days. Check them, then confirm to receive them.", st.NearExpiryDays)).
			WithDetails(map[string]any{"batches": near, "nearExpiryDays": st.NearExpiryDays})
	}
	for i := range lines {
		l := &lines[i]
		tracked, variant, cost := info(*l)
		if !tracked {
			continue
		}
		for j := range l.batches {
			b := &l.batches[j]
			if b.arrival.Cost == nil {
				c := cost
				b.arrival.Cost = &c
			}
			id, _, err := batches.Resolve(ctx, tx, variant, b.arrival, userID)
			switch {
			case errors.Is(err, batches.ErrExpiryMismatch):
				f[b.field+".expiry"] = "This batch number is already recorded for this product with another expiry date."
			case errors.Is(err, batches.ErrReservedNo):
				f[b.field+".batchNo"] = "This name is kept for stock with no batch."
			case err != nil:
				return nil, err
			default:
				b.id = id
			}
		}
	}
	if err := apperr.Fields(f); err != nil {
		return nil, err
	}
	return near, nil
}

// parseBatches reads a receipt line's batches (A26).
func parseBatches(in []batchIn, field string, f map[string]string) []batchWant {
	if len(in) > maxBatchesPerLine {
		f[field+".batches"] = fmt.Sprintf("At most %d batches per line.", maxBatchesPerLine)
		return nil
	}
	var out []batchWant
	for j, b := range in {
		k := fmt.Sprintf("%s.batches[%d]", field, j)
		w := batchWant{field: k}
		no, ok := batches.ValidBatchNo(b.BatchNo)
		if !ok {
			f[k+".batchNo"] = "Batch number must be 1 to 40 characters."
		}
		w.arrival.BatchNo = no
		exp, err := batches.ParseExpiry(b.Expiry)
		if err != nil {
			f[k+".expiry"] = "Expiry: use YYYY-MM-DD or MM/YYYY."
		}
		w.arrival.Expiry = exp
		if b.MfgDate != nil && *b.MfgDate != "" {
			m, err := time.Parse("2006-01-02", *b.MfgDate)
			switch {
			case err != nil:
				f[k+".mfgDate"] = "Use YYYY-MM-DD."
			case m.After(exp):
				f[k+".mfgDate"] = "The manufacturing date must be on or before the expiry date."
			default:
				w.arrival.Mfg = &m
			}
		}
		if b.MRP.Set && b.MRP.Valid {
			if b.MRP.D.IsNegative() {
				f[k+".mrp"] = "MRP cannot be negative."
			}
			v := b.MRP.D.Round(2)
			w.arrival.MRP = &v
		}
		if !b.Quantity.Valid || b.Quantity.V <= 0 || b.Quantity.V > maxQty {
			f[k+".quantity"] = "Quantity must be a whole number above 0."
		}
		w.qty = int(b.Quantity.V)
		out = append(out, w)
	}
	return out
}

// ---------------------------------------------------------------- A16

// maxBatchesPerLine bounds one receipt line's batches.
const maxBatchesPerLine = 50

// batchIn is one batch of a receipt line (industry P3 item 2, A26).
type batchIn struct {
	BatchNo  string    `json:"batchNo"`
	Expiry   string    `json:"expiry"`
	MfgDate  *string   `json:"mfgDate"`
	MRP      money.Dec `json:"mrp"`
	Quantity money.Int `json:"quantity"`
}

type receiptLineIn struct {
	PurchaseItemID money.Int `json:"purchaseItemId"`
	Quantity       money.Int `json:"quantity"`
	Batches        []batchIn `json:"batches"`
}

type receiptIn struct {
	Lines []receiptLineIn `json:"lines"`
	Note  *string         `json:"note"`
	// Industry P3 item 2: confirmations for batches close to expiry, and
	// for an expiry more than 10 years ahead.
	AcceptNearExpiry bool `json:"acceptNearExpiry"`
	ConfirmFarExpiry bool `json:"confirmFarExpiry"`
}

// ReceiptBatchJSON is one batch of a received line (A26).
type ReceiptBatchJSON struct {
	BatchID            int64            `json:"batchId"`
	BatchNo            string           `json:"batchNo"`
	Expiry             *string          `json:"expiry"`
	MRP                *decimal.Decimal `json:"mrp"`
	Quantity           int              `json:"quantity"`
	NearExpiryAccepted bool             `json:"nearExpiryAccepted"`
}

// ReceiptLineJSON is one received line.
type ReceiptLineJSON struct {
	PurchaseItemID int64              `json:"purchaseItemId"`
	VariantID      int64              `json:"variantId"`
	ProductName    string             `json:"productName"`
	SKU            *string            `json:"sku"`
	Quantity       int                `json:"quantity"`
	Batches        []ReceiptBatchJSON `json:"batches"` // industry P3 item 2; empty for untracked products
	rowID          int64
}

// ReceiptJSON is one goods receipt.
type ReceiptJSON struct {
	ID         int64             `json:"id"`
	PurchaseID int64             `json:"purchaseId"`
	Reference  string            `json:"reference"`
	ReceivedAt time.Time         `json:"receivedAt"`
	ReceivedBy *NameRef          `json:"receivedBy"`
	Note       *string           `json:"note"`
	Lines      []ReceiptLineJSON `json:"lines"`
}

const qReceipts = `SELECT r.id, r.purchase_id, r.reference, r.received_at, r.received_by, u.name, r.note,
		i.id, i.variant_id, i.product_name, i.sku, ri.quantity, ri.id
	FROM purchase_receipts r
	LEFT JOIN users u ON u.id = r.received_by
	JOIN purchase_receipt_items ri ON ri.receipt_id = r.id
	JOIN purchase_items i ON i.id = ri.purchase_item_id
	WHERE r.purchase_id = $1 AND ($2 = 0 OR r.id = $2)
	ORDER BY r.received_at, r.id, i.id`

func loadReceipts(ctx context.Context, tx tenancy.TenantTx, purchaseID, receiptID int64) ([]ReceiptJSON, error) {
	rows, err := tx.Query(ctx, qReceipts, purchaseID, receiptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReceiptJSON{}
	for rows.Next() {
		var (
			r      ReceiptJSON
			by     *int64
			byName *string
			l      ReceiptLineJSON
		)
		if err := rows.Scan(&r.ID, &r.PurchaseID, &r.Reference, &r.ReceivedAt, &by, &byName, &r.Note,
			&l.PurchaseItemID, &l.VariantID, &l.ProductName, &l.SKU, &l.Quantity, &l.rowID); err != nil {
			return nil, err
		}
		l.Batches = []ReceiptBatchJSON{}
		if n := len(out); n == 0 || out[n-1].ID != r.ID {
			if by != nil {
				r.ReceivedBy = &NameRef{ID: *by, Name: deref(byName)}
			}
			r.Lines = []ReceiptLineJSON{}
			out = append(out, r)
		}
		out[len(out)-1].Lines = append(out[len(out)-1].Lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, receiptBatches(ctx, tx, out)
}

// receiptBatches fills the batches of received lines.
func receiptBatches(ctx context.Context, tx tenancy.TenantTx, rcs []ReceiptJSON) error {
	at := map[int64]*ReceiptLineJSON{}
	var ids []int64
	for i := range rcs {
		for j := range rcs[i].Lines {
			l := &rcs[i].Lines[j]
			at[l.rowID] = l
			ids = append(ids, l.rowID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT rb.receipt_item_id, rb.batch_id, b.batch_no::text, to_char(b.expiry_date, 'YYYY-MM-DD'), b.mrp,
			rb.quantity, rb.near_expiry_accepted
		FROM purchase_receipt_batches rb JOIN batches b ON b.id = rb.batch_id
		WHERE rb.receipt_item_id = ANY($1) ORDER BY rb.receipt_item_id, b.expiry_date, rb.batch_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ri int64
		var b ReceiptBatchJSON
		if err := rows.Scan(&ri, &b.BatchID, &b.BatchNo, &b.Expiry, &b.MRP, &b.Quantity, &b.NearExpiryAccepted); err != nil {
			return err
		}
		l := at[ri]
		l.Batches = append(l.Batches, b)
	}
	return rows.Err()
}

// listReceipts is GET /api/purchases/{id}/receipts: oldest first.
func (a *API) listReceipts(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out []ReceiptJSON
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var ok bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM purchases WHERE id = $1)`, id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound()
		}
		out, err = loadReceipts(r.Context(), tx, id, 0)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// receiveResult is the A16 response.
type receiveResult struct {
	Receipt  ReceiptJSON  `json:"receipt"`
	Purchase PurchaseJSON `json:"purchase"`
}

// createReceipt is POST /api/purchases/{id}/receipts {lines, note}: stock
// in what arrived. The purchase becomes Partially received, or Received
// when nothing is outstanding. An Idempotency-Key replays the first answer.
func (a *API) createReceipt(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key, err := idempotency.Key(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hash := idempotency.Hash(r, "POST /api/purchases/"+strconv.FormatInt(id, 10)+"/receipts\n")
	var in receiptIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	f := map[string]string{}
	switch {
	case len(in.Lines) == 0:
		f["lines"] = "Add at least one line."
	case len(in.Lines) > maxLines:
		f["lines"] = fmt.Sprintf("At most %d lines.", maxLines)
	}
	var lines []want
	for i, l := range in.Lines {
		k := fmt.Sprintf("lines[%d]", i)
		if !l.PurchaseItemID.Valid || l.PurchaseItemID.V <= 0 {
			f[k+".purchaseItemId"] = "purchaseItemId is required."
		}
		if !l.Quantity.Valid || l.Quantity.V <= 0 || l.Quantity.V > maxQty {
			f[k+".quantity"] = "Quantity must be a whole number above 0."
		}
		lines = append(lines, want{itemID: l.PurchaseItemID.V, qty: int(l.Quantity.V), field: k, batches: parseBatches(l.Batches, k, f)})
	}
	note, _ := noteChoice(in.Note, f)
	if err := apperr.Fields(f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var (
		out   receiveResult
		again *idempotency.Replay
	)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" {
			var err error
			if again, err = idempotency.Claim(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		var status, ref string
		err := tx.QueryRow(ctx, `SELECT status, reference FROM purchases WHERE id = $1 FOR UPDATE`, id).Scan(&status, &ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		switch status {
		case statusReceived:
			return apperr.Conflict(apperr.CodeInvalidState, "This purchase is already fully received.")
		case statusCancelled:
			return apperr.Conflict(apperr.CodeInvalidState, "A cancelled purchase cannot be received.")
		}
		rc, err := receive(ctx, tx, id, p.UserID, lines, note, receiveOpts{acceptNear: in.AcceptNearExpiry, confirmFar: in.ConfirmFarExpiry})
		if err != nil {
			return err
		}
		newStatus := statusPartial
		if rc.full {
			newStatus = statusReceived
		}
		if _, err := tx.Exec(ctx, `UPDATE purchases SET status = $2,
				received_at = CASE WHEN $3 THEN now() ELSE received_at END,
				received_by = CASE WHEN $3 THEN $4::bigint ELSE received_by END,
				updated_by = $4, updated_at = now()
			WHERE id = $1`, id, newStatus, rc.full, p.UserID); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "purchase.receive", Entity: "purchase",
			EntityID: strconv.FormatInt(id, 10), RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent(),
			Before: map[string]any{"status": status},
			After: map[string]any{"reference": ref, "receipt": rc.reference, "units": rc.units, "status": newStatus,
				"nearExpiryAccepted": rc.nearAccepted}}); err != nil {
			return err
		}
		rcs, err := loadReceipts(ctx, tx, id, rc.receiptID)
		if err != nil {
			return err
		}
		ps, err := load(ctx, tx, []int64{id})
		if err != nil {
			return err
		}
		if len(rcs) != 1 || len(ps) != 1 {
			return apperr.Internal(nil)
		}
		out = receiveResult{Receipt: rcs[0], Purchase: ps[0]}
		if key != "" {
			return idempotency.Store(ctx, tx, p.UserID, key, http.StatusCreated, out)
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if again != nil {
		idempotency.Write(w, again)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}
