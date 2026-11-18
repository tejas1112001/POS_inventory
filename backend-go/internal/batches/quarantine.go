package batches

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Quarantine and release (industry P3 item 4, A29, design §4): units found
// damaged on the shelf leave sellable stock for the batch's quarantine,
// which is counted but never sold; released units go back. Quarantined
// units are outside inventories.qty (§1.4), so both moves are on the stock
// ledger too.

const maxMoveQty = 1_000_000

type quarantineIn struct {
	StoreID  money.Int `json:"storeId"`
	Quantity money.Int `json:"quantity"`
	Reason   string    `json:"reason"`
}

// quarantine is POST /api/batches/{id}/quarantine.
func (a *API) quarantine(w http.ResponseWriter, r *http.Request) { a.moveQuarantine(w, r, true) }

// release is POST /api/batches/{id}/release. Refused for an expired or
// recalled batch: those are written off (item 5), never sold.
func (a *API) release(w http.ResponseWriter, r *http.Request) { a.moveQuarantine(w, r, false) }

func (a *API) moveQuarantine(w http.ResponseWriter, r *http.Request, in bool) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body quarantineIn
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	if !body.StoreID.Valid || body.StoreID.V <= 0 {
		f["storeId"] = "Choose the store."
	}
	if !body.Quantity.Valid || body.Quantity.V < 1 || body.Quantity.V > maxMoveQty {
		f["quantity"] = "Quantity must be a whole number of at least 1."
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if n := len([]rune(body.Reason)); n < 5 || n > maxReasonLen {
		f["reason"] = fmt.Sprintf("Give a reason of 5 to %d characters.", maxReasonLen)
	}
	if len(f) > 0 {
		httpx.WriteError(w, r, apperr.Fields(f))
		return
	}
	p := principal(r)
	store, n := body.StoreID.V, int(body.Quantity.V)
	var out DetailJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var (
			variant int64
			batchNo string
			expiry  *time.Time
			status  string
			today   time.Time
		)
		err := tx.QueryRow(ctx, `SELECT variant_id, batch_no::text, expiry_date, status, `+sqlToday+` FROM batches WHERE id = $1`, id).
			Scan(&variant, &batchNo, &expiry, &status, &today)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		var storeOK bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1)`, store).Scan(&storeOK); err != nil {
			return err
		}
		if !storeOK {
			return apperr.Fields(map[string]string{"storeId": "Store not found."})
		}
		tr, err := inventory.Tracked(ctx, tx, []int64{variant})
		if err != nil {
			return err
		}
		if !tr[variant] {
			return apperr.Conflict(apperr.CodeInvalidState, "This product does not track batches.")
		}
		if !in {
			why := ""
			switch {
			case status == "recalled":
				why = "recalled"
			case expiry != nil && expiry.Before(today):
				why = "expired"
			}
			if why != "" {
				return apperr.Conflict(apperr.CodeBatchNotSellable, fmt.Sprintf("Batch %s is %s; it cannot go back on sale. Write it off instead.", batchNo, why)).
					WithDetails(map[string]any{"batchId": id, "batchNo": batchNo, "reason": why})
			}
		}
		k := inventory.Key{Store: store, Variant: variant}
		d, reason, action := -n, inventory.ReasonQuarantine, "batch.quarantine"
		if !in {
			d, reason, action = n, inventory.ReasonQuarantineRelease, "batch.release"
		}
		// Stock row first, then the batch row (§1.5).
		keys, _, err := inventory.Lock(ctx, tx, map[inventory.Key]int{k: d}, nil)
		if err != nil {
			return err
		}
		ref := inventory.Ref{Type: "batch", ID: id}
		alloc := inventory.Allocation{k: {{BatchID: id, Qty: n}}}
		if in {
			if err := inventory.ApplyBatched(ctx, tx, keys, map[inventory.Key]int{k: d}, alloc, reason, ref, p.UserID); err != nil {
				return err
			}
			if err := inventory.Quarantine(ctx, tx, store, id, n, reason, ref, p.UserID); err != nil {
				return err
			}
		} else {
			if err := inventory.Quarantine(ctx, tx, store, id, -n, reason, ref, p.UserID); err != nil {
				return err
			}
			if err := inventory.ApplyBatched(ctx, tx, keys, map[inventory.Key]int{k: d}, alloc, reason, ref, p.UserID); err != nil {
				return err
			}
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: action, Entity: "batch", EntityID: strconv.FormatInt(id, 10),
			After:     map[string]any{"storeId": store, "quantity": n, "reason": body.Reason, "batchNo": batchNo},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = loadOne(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
