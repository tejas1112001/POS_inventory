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

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const statusPartial = "Partially received"

// want is one requested receipt line; nil wants everything outstanding.
type want struct {
	itemID int64
	qty    int
}

// received is what receive did.
type received struct {
	receiptID int64
	reference string
	units     int
	full      bool // every line is now fully received
}

// receive records a goods receipt for the purchase and puts its quantities
// into stock. lines nil receives everything outstanding (A17 and creating a
// purchase as Received: the ledger keeps today's ref "purchase"); otherwise
// each line must be on this purchase and at most its outstanding quantity
// (A16: ledger ref "purchase_receipt"). The caller holds the purchase's row
// lock.
func receive(ctx context.Context, tx tenancy.TenantTx, purchaseID, userID int64, lines []want, note string) (received, error) {
	type item struct {
		store, variant   int64
		qty, outstanding int
	}
	rows, err := tx.Query(ctx, `SELECT id, store_id, variant_id, quantity - received_qty FROM purchase_items
		WHERE purchase_id = $1 ORDER BY id`, purchaseID)
	if err != nil {
		return received{}, err
	}
	items := map[int64]*item{}
	var order []int64
	for rows.Next() {
		var id int64
		it := &item{}
		if err := rows.Scan(&id, &it.store, &it.variant, &it.outstanding); err != nil {
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
			}
		}
		if err := apperr.Fields(f); err != nil {
			return received{}, err
		}
	}
	deltas := map[inventory.Key]int{}
	out := received{}
	for _, id := range order {
		if it := items[id]; it.qty > 0 {
			deltas[inventory.Key{Store: it.store, Variant: it.variant}] += it.qty
			out.units += it.qty
		}
	}
	if out.units == 0 {
		return received{}, apperr.Invalid("lines", "Nothing to receive.")
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
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_receipt_items (receipt_id, purchase_item_id, quantity) VALUES ($1, $2, $3)`,
			out.receiptID, id, it.qty); err != nil {
			return received{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_items SET received_qty = received_qty + $2 WHERE id = $1`, id, it.qty); err != nil {
			return received{}, err
		}
	}
	ref := inventory.Ref{Type: "purchase_receipt", ID: out.receiptID}
	if legacy {
		ref = inventory.Ref{Type: "purchase", ID: purchaseID}
	}
	if err := inventory.LockAndApply(ctx, tx, deltas, nil, inventory.ReasonPurchase, ref, userID); err != nil {
		return received{}, err
	}
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM purchase_items WHERE purchase_id = $1 AND received_qty < quantity)`, purchaseID).Scan(&out.full)
	return out, err
}

// ---------------------------------------------------------------- A16

type receiptLineIn struct {
	PurchaseItemID money.Int `json:"purchaseItemId"`
	Quantity       money.Int `json:"quantity"`
}

type receiptIn struct {
	Lines []receiptLineIn `json:"lines"`
	Note  *string         `json:"note"`
}

// ReceiptLineJSON is one received line.
type ReceiptLineJSON struct {
	PurchaseItemID int64   `json:"purchaseItemId"`
	VariantID      int64   `json:"variantId"`
	ProductName    string  `json:"productName"`
	SKU            *string `json:"sku"`
	Quantity       int     `json:"quantity"`
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
		i.id, i.variant_id, i.product_name, i.sku, ri.quantity
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
			&l.PurchaseItemID, &l.VariantID, &l.ProductName, &l.SKU, &l.Quantity); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != r.ID {
			if by != nil {
				r.ReceivedBy = &NameRef{ID: *by, Name: deref(byName)}
			}
			r.Lines = []ReceiptLineJSON{}
			out = append(out, r)
		}
		out[len(out)-1].Lines = append(out[len(out)-1].Lines, l)
	}
	return out, rows.Err()
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
		lines = append(lines, want{itemID: l.PurchaseItemID.V, qty: int(l.Quantity.V)})
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
		rc, err := receive(ctx, tx, id, p.UserID, lines, note)
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
			After:  map[string]any{"reference": ref, "receipt": rc.reference, "units": rc.units, "status": newStatus}}); err != nil {
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
