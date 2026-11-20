package batches

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Write-offs (industry P3 item 5, A30, design §5): units of batches that
// leave the shop for good, from sellable stock or from quarantine. A
// write-off is posted when it is created and never changes.

const (
	maxWriteOffLines = 200
	writeOffScope    = "POST /api/write-offs\n"
	bucketStock      = "stock"
	bucketQuarantine = "quarantine"
)

var writeOffReasons = []string{"expired", "damaged", "recalled", "returned_to_supplier", "other"}

type writeOffLineIn struct {
	BatchID  money.Int `json:"batchId"`
	Quantity money.Int `json:"quantity"`
	From     string    `json:"from"` // stock (default) or quarantine
}

type writeOffIn struct {
	StoreID money.Int        `json:"storeId"`
	Reason  string           `json:"reason"`
	Note    string           `json:"note"`
	Lines   []writeOffLineIn `json:"lines"`
}

// WriteOffLineJSON is one line of a write-off.
type WriteOffLineJSON struct {
	BatchID     int64   `json:"batchId"`
	BatchNo     string  `json:"batchNo"`
	Expiry      *string `json:"expiry"`
	Unknown     bool    `json:"unknown"`
	VariantID   int64   `json:"variantId"`
	ProductID   int64   `json:"productId"`
	ProductName string  `json:"productName"`
	SKU         *string `json:"sku"`
	From        string  `json:"from"`
	Quantity    int     `json:"quantity"`
}

// WriteOffJSON is a write-off.
type WriteOffJSON struct {
	ID            int64              `json:"id"`
	Reference     string             `json:"reference"`
	StoreID       int64              `json:"storeId"`
	StoreName     string             `json:"storeName"`
	Reason        string             `json:"reason"`
	Note          *string            `json:"note"`
	CreatedBy     *int64             `json:"createdBy"`
	CreatedByName *string            `json:"createdByName"`
	CreatedAt     time.Time          `json:"createdAt"`
	Units         int                `json:"units"`
	Lines         []WriteOffLineJSON `json:"lines"`
}

type woLine struct {
	batch, variant int64
	qty            int
	from           string
}

func validateWriteOff(in writeOffIn) ([]woLine, error) {
	f := map[string]string{}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		f["storeId"] = "Choose the store."
	}
	if !slices.Contains(writeOffReasons, in.Reason) {
		f["reason"] = "Reason must be expired, damaged, recalled, returned_to_supplier or other."
	}
	note := strings.TrimSpace(in.Note)
	switch {
	case len([]rune(note)) > maxReasonLen:
		f["note"] = fmt.Sprintf("At most %d characters.", maxReasonLen)
	case in.Reason == "other" && note == "":
		f["note"] = "Say what happened."
	}
	if len(in.Lines) == 0 || len(in.Lines) > maxWriteOffLines {
		f["lines"] = fmt.Sprintf("Add 1 to %d lines.", maxWriteOffLines)
	}
	seen := map[string]bool{}
	out := make([]woLine, 0, len(in.Lines))
	for i, l := range in.Lines {
		k := fmt.Sprintf("lines[%d]", i)
		from := l.From
		if from == "" {
			from = bucketStock
		}
		if from != bucketStock && from != bucketQuarantine {
			f[k+".from"] = "From must be stock or quarantine."
		}
		if !l.BatchID.Valid || l.BatchID.V <= 0 {
			f[k+".batchId"] = "Choose the batch."
		}
		if !l.Quantity.Valid || l.Quantity.V < 1 || l.Quantity.V > maxMoveQty {
			f[k+".quantity"] = "Quantity must be a whole number of at least 1."
		}
		dup := fmt.Sprintf("%d/%s", l.BatchID.V, from)
		if seen[dup] {
			f[k+".batchId"] = "This batch is already on the write-off."
		}
		seen[dup] = true
		out = append(out, woLine{batch: l.BatchID.V, qty: int(l.Quantity.V), from: from})
	}
	if len(f) > 0 {
		return nil, apperr.Fields(f)
	}
	return out, nil
}

// createWriteOff is POST /api/write-offs (A30), idempotent with an
// Idempotency-Key.
func (a *API) createWriteOff(w http.ResponseWriter, r *http.Request) {
	key, err := idempotency.Key(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hash := idempotency.Hash(r, writeOffScope)
	var in writeOffIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	lines, err := validateWriteOff(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	store := in.StoreID.V
	var (
		out   WriteOffJSON
		again *idempotency.Replay
	)
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" {
			var err error
			if again, err = idempotency.Claim(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		var storeOK bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1)`, store).Scan(&storeOK); err != nil {
			return err
		}
		if !storeOK {
			return apperr.Fields(map[string]string{"storeId": "Store not found."})
		}
		f := map[string]string{}
		vids := make([]int64, 0, len(lines))
		for i := range lines {
			err := tx.QueryRow(ctx, `SELECT variant_id FROM batches WHERE id = $1`, lines[i].batch).Scan(&lines[i].variant)
			if errors.Is(err, pgx.ErrNoRows) {
				f[fmt.Sprintf("lines[%d].batchId", i)] = "Batch not found."
				continue
			}
			if err != nil {
				return err
			}
			vids = append(vids, lines[i].variant)
		}
		if len(f) > 0 {
			return apperr.Fields(f)
		}
		tr, err := inventory.Tracked(ctx, tx, vids)
		if err != nil {
			return err
		}
		deltas := map[inventory.Key]int{}
		locks := map[inventory.Key]int{}
		alloc := inventory.Allocation{}
		for i, l := range lines {
			if !tr[l.variant] {
				f[fmt.Sprintf("lines[%d].batchId", i)] = "This product does not track batches."
				continue
			}
			k := inventory.Key{Store: store, Variant: l.variant}
			if l.from == bucketStock {
				deltas[k] -= l.qty
				locks[k] -= l.qty
				alloc[k] = append(alloc[k], inventory.BatchQty{BatchID: l.batch, Qty: l.qty})
			} else if _, ok := locks[k]; !ok {
				locks[k] = 1 // quarantine only: the stock row is locked, not changed
			}
		}
		if len(f) > 0 {
			return apperr.Fields(f)
		}
		for k, d := range locks {
			if d == 0 {
				locks[k] = 1
			}
		}
		// Stock rows first, then batch rows (§1.5).
		keys, _, err := inventory.Lock(ctx, tx, locks, nil)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO doc_counters (name) VALUES ('write_off') ON CONFLICT (name) DO NOTHING`); err != nil {
			return err
		}
		var n int64
		if err := tx.QueryRow(ctx, `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'write_off' RETURNING next_value - 1`).Scan(&n); err != nil {
			return err
		}
		ref := fmt.Sprintf("WO-%06d", n)
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO write_offs (reference, store_id, reason, note, created_by) VALUES ($1, $2, $3, NULLIF($4, ''), $5)
			RETURNING id`, ref, store, in.Reason, strings.TrimSpace(in.Note), p.UserID).Scan(&id); err != nil {
			return err
		}
		units := 0
		for _, l := range lines {
			units += l.qty
			if _, err := tx.Exec(ctx, `INSERT INTO write_off_lines (write_off_id, batch_id, variant_id, bucket, quantity) VALUES ($1, $2, $3, $4, $5)`,
				id, l.batch, l.variant, l.from, l.qty); err != nil {
				return err
			}
		}
		iref := inventory.Ref{Type: "write_off", ID: id}
		if err := inventory.ApplyBatched(ctx, tx, keys, deltas, alloc, inventory.ReasonWriteOff, iref, p.UserID); err != nil {
			return err
		}
		q := slices.Clone(lines)
		slices.SortFunc(q, func(x, y woLine) int { return int(x.batch - y.batch) })
		for _, l := range q {
			if l.from != bucketQuarantine {
				continue
			}
			if err := inventory.Quarantine(ctx, tx, store, l.batch, -l.qty, inventory.ReasonWriteOff, iref, p.UserID); err != nil {
				return err
			}
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "write_off.create", Entity: "write_off", EntityID: strconv.FormatInt(id, 10),
			After:     map[string]any{"reference": ref, "storeId": store, "reason": in.Reason, "lines": len(lines), "units": units},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		l, err := loadWriteOffs(ctx, tx, []int64{id})
		if err != nil {
			return err
		}
		out = l[0]
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

const (
	qLoadWriteOffs = `SELECT w.id, w.reference, w.store_id, s.name, w.reason, w.note, w.created_by, u.name, w.created_at
		FROM write_offs w JOIN stores s ON s.id = w.store_id LEFT JOIN users u ON u.id = w.created_by
		WHERE w.id = ANY($1)`
	qLoadWriteOffLines = `SELECT l.write_off_id, l.batch_id, b.batch_no::text, to_char(b.expiry_date, 'YYYY-MM-DD'), b.is_unknown, l.variant_id,
			v.product_id, p.name, v.sku::text, l.bucket, l.quantity
		FROM write_off_lines l JOIN batches b ON b.id = l.batch_id
		JOIN product_variants v ON v.id = l.variant_id JOIN products p ON p.id = v.product_id
		WHERE l.write_off_id = ANY($1) ORDER BY l.write_off_id, l.id`
)

func loadWriteOffs(ctx context.Context, tx tenancy.TenantTx, ids []int64) ([]WriteOffJSON, error) {
	byID := map[int64]*WriteOffJSON{}
	rows, err := tx.Query(ctx, qLoadWriteOffs, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		x := &WriteOffJSON{Lines: []WriteOffLineJSON{}}
		if err := rows.Scan(&x.ID, &x.Reference, &x.StoreID, &x.StoreName, &x.Reason, &x.Note, &x.CreatedBy, &x.CreatedByName, &x.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byID[x.ID] = x
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, qLoadWriteOffLines, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var wid int64
		var l WriteOffLineJSON
		if err := rows.Scan(&wid, &l.BatchID, &l.BatchNo, &l.Expiry, &l.Unknown, &l.VariantID, &l.ProductID, &l.ProductName, &l.SKU, &l.From, &l.Quantity); err != nil {
			rows.Close()
			return nil, err
		}
		if x := byID[wid]; x != nil {
			x.Lines = append(x.Lines, l)
			x.Units += l.Quantity
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]WriteOffJSON, 0, len(ids))
	for _, id := range ids {
		if x := byID[id]; x != nil {
			out = append(out, *x)
		}
	}
	return out, nil
}

// listWriteOffs is GET /api/write-offs?storeId&batchId&limit&offset, newest
// first: {items, total}.
func (a *API) listWriteOffs(w http.ResponseWriter, r *http.Request) {
	store, err := intParam(r, "storeId", 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	batch, err := intParam(r, "batchId", 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", 50)
	if err != nil || limit < 1 || limit > maxList {
		httpx.WriteError(w, r, apperr.Invalid("limit", fmt.Sprintf("limit must be 1 to %d.", maxList)))
		return
	}
	offset, err := intParam(r, "offset", 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := struct {
		Items []WriteOffJSON `json:"items"`
		Total int            `json:"total"`
	}{Items: []WriteOffJSON{}}
	err = a.ro(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		const filter = ` FROM write_offs w WHERE ($1 = 0 OR w.store_id = $1)
			AND ($2 = 0 OR EXISTS (SELECT 1 FROM write_off_lines l WHERE l.write_off_id = w.id AND l.batch_id = $2))`
		if err := tx.QueryRow(ctx, `SELECT count(*)`+filter, store, batch).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT w.id`+filter+` ORDER BY w.created_at DESC, w.id DESC LIMIT $3 OFFSET $4`, store, batch, limit, offset)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			out.Items, err = loadWriteOffs(ctx, tx, ids)
		}
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// getWriteOff is GET /api/write-offs/{id}.
func (a *API) getWriteOff(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out WriteOffJSON
	err = a.ro(r, func(tx tenancy.TenantTx) error {
		l, err := loadWriteOffs(r.Context(), tx, []int64{id})
		if err != nil {
			return err
		}
		if len(l) == 0 {
			return apperr.NotFound()
		}
		out = l[0]
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
