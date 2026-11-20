// Package batches is the batch list, detail and correction API (industry
// Phase 3 item 1, docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md §1, A23, A24).
// Stock per batch is kept by the inventory package (batch.go); this package
// only reads it and corrects batch details.
package batches

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Module owns every route here.
const Module = modules.BatchExpiry

const (
	maxList      = 500
	maxMovements = 200
	maxReasonLen = 500
	maxBatchNo   = 40
	// maxYearsAhead: an expiry further away is almost always a typing error.
	maxYearsAhead = 10
)

// Deps are the package's dependencies.
type Deps struct{ Sessions *tenancy.Sessions }

// API serves the batch routes.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API { return &API{d: d} }

// Mount registers the routes (all in the batch_expiry module).
func (a *API) Mount(rt *rbac.Router) {
	req := func(action string) rbac.Requirement { return rbac.Require("batches", action).InModule(Module) }
	rt.Handle(http.MethodGet, "/batches", req("view"), a.list)
	rt.Handle(http.MethodGet, "/batches/{id}", req("view"), a.get)
	rt.Handle(http.MethodPut, "/batches/{id}", req("manage"), a.update)
	rt.Handle(http.MethodPost, "/batches/{id}/quarantine", req("manage"), a.quarantine) // item 4 (A29)
	rt.Handle(http.MethodPost, "/batches/{id}/release", req("manage"), a.release)
	// Item 5 (A30, A31): write-offs, recalls, the trace report.
	need := func(action string) rbac.Requirement { return rbac.Require("batches", action).InModule(Module) }
	rt.Handle(http.MethodPost, "/batches/{id}/recall", need("recall"), a.recall)
	rt.Handle(http.MethodPost, "/batches/{id}/lift-recall", need("recall"), a.liftRecall)
	rt.Handle(http.MethodGet, "/batches/{id}/trace", req("view"), a.trace)
	rt.Handle(http.MethodPost, "/write-offs", need("write_off"), a.createWriteOff)
	rt.Handle(http.MethodGet, "/write-offs", req("view"), a.listWriteOffs)
	rt.Handle(http.MethodGet, "/write-offs/{id}", req("view"), a.getWriteOff)
	// Item 7 (A34): batch settings, kept while the module is off.
	rt.Handle(http.MethodGet, "/settings/batches", rbac.Require("settings", "view"), a.getSettings)
	rt.Handle(http.MethodPut, "/settings/batches", rbac.Require("settings", "edit"), a.putSettings)
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func (a *API) rw(r *http.Request, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(r.Context(), tenant(r), tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (a *API) ro(r *http.Request, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(r.Context(), tenant(r), tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

// ------------------------------------------------------------------ dates

var monthYear = regexp.MustCompile(`^(0?[1-9]|1[0-2])/([0-9]{4})$`)

// ParseExpiry reads an expiry date: YYYY-MM-DD, or MM/YYYY meaning the last
// day of that month (as printed on packs: "EXP 03/2027" = 31 March 2027).
func ParseExpiry(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if m := monthYear.FindStringSubmatch(s); m != nil {
		mo, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		return time.Date(y, time.Month(mo)+1, 0, 0, 0, 0, 0, time.UTC), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errors.New("use YYYY-MM-DD or MM/YYYY")
	}
	return t, nil
}

// CheckExpiry refuses an expiry too far ahead to be real.
func CheckExpiry(t time.Time, today time.Time) error {
	if t.After(today.AddDate(maxYearsAhead, 0, 0)) {
		return fmt.Errorf("more than %d years ahead; check the date, then confirm it", maxYearsAhead)
	}
	return nil
}

// sqlToday is today's date in the shop's time zone.
const sqlToday = `(now() AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date`

// ------------------------------------------------------------------- JSON

// StockJSON is a batch's stock in one store.
type StockJSON struct {
	StoreID     int64  `json:"storeId"`
	StoreName   string `json:"storeName"`
	Qty         qty.Q  `json:"qty"`
	Quarantined qty.Q  `json:"quarantined"`
}

// BatchJSON is one batch.
type BatchJSON struct {
	ID               int64            `json:"id"`
	VariantID        int64            `json:"variantId"`
	ProductID        int64            `json:"productId"`
	ProductName      string           `json:"productName"`
	SKU              *string          `json:"sku"`
	BatchNo          string           `json:"batchNo"`
	Expiry           *string          `json:"expiry"`
	MfgDate          *string          `json:"mfgDate"`
	MRP              *decimal.Decimal `json:"mrp"`
	Cost             *decimal.Decimal `json:"cost"`
	Unknown          bool             `json:"unknown"`
	Status           string           `json:"status"`
	Expired          bool             `json:"expired"`
	DaysToExpiry     *int             `json:"daysToExpiry"`
	RecalledAt       *time.Time       `json:"recalledAt"`
	RecallReason     *string          `json:"recallReason"`
	Stock            []StockJSON      `json:"stock"`
	TotalQty         qty.Q            `json:"totalQty"`
	TotalQuarantined qty.Q            `json:"totalQuarantined"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

// MovementJSON is one batch ledger row.
type MovementJSON struct {
	ID        int64     `json:"id"`
	StoreID   int64     `json:"storeId"`
	StoreName string    `json:"storeName"`
	Bucket    string    `json:"bucket"`
	Delta     qty.Q     `json:"delta"`
	QtyAfter  qty.Q     `json:"qtyAfter"`
	Reason    string    `json:"reason"`
	RefType   *string   `json:"refType"`
	RefID     *int64    `json:"refId"`
	UserName  *string   `json:"userName"`
	CreatedAt time.Time `json:"createdAt"`
}

// DetailJSON is GET /api/batches/{id}.
type DetailJSON struct {
	BatchJSON
	Movements []MovementJSON `json:"movements"`
}

const qBatchCols = `SELECT b.id, b.variant_id, p.id, p.name, v.sku::text, b.batch_no::text, to_char(b.expiry_date, 'YYYY-MM-DD'),
		to_char(b.mfg_date, 'YYYY-MM-DD'), b.mrp, b.cost, b.is_unknown, b.status,
		coalesce(b.expiry_date < ` + sqlToday + `, false), (b.expiry_date - ` + sqlToday + `)::int,
		b.recalled_at, b.recall_reason, b.created_at, b.updated_at
	FROM batches b
	JOIN product_variants v ON v.id = b.variant_id
	JOIN products p ON p.id = v.product_id`

// qList: filters $1 product, $2 variant, $3 store (0 = any), $4 status
// (” | available | recalled | expired | unknown), $5 expiring within days
// (-1 = any), $6 include batches with no stock, $7 limit.
const qList = qBatchCols + `
	WHERE ($1 = 0 OR p.id = $1) AND ($2 = 0 OR b.variant_id = $2)
	  AND ($3 = 0 OR EXISTS (SELECT 1 FROM batch_stock s WHERE s.batch_id = b.id AND s.store_id = $3 AND ($6 OR s.qty + s.quarantined_qty > 0)))
	  AND ($6 OR EXISTS (SELECT 1 FROM batch_stock s WHERE s.batch_id = b.id AND s.qty + s.quarantined_qty > 0))
	  AND (CASE $4
	        WHEN 'available' THEN b.status = 'available' AND NOT coalesce(b.expiry_date < ` + sqlToday + `, false)
	        WHEN 'recalled' THEN b.status = 'recalled'
	        WHEN 'expired' THEN coalesce(b.expiry_date < ` + sqlToday + `, false)
	        WHEN 'unknown' THEN b.is_unknown
	        ELSE true END)
	  AND ($5 < 0 OR (b.expiry_date IS NOT NULL AND b.expiry_date <= ` + sqlToday + ` + $5::int))
	ORDER BY b.expiry_date NULLS LAST, b.id
	LIMIT $7`

const qStock = `SELECT s.batch_id, s.store_id, st.name, s.qty, s.quarantined_qty
	FROM batch_stock s JOIN stores st ON st.id = s.store_id AND st.deleted_at IS NULL
	WHERE s.batch_id = ANY($1) AND ($2 = 0 OR s.store_id = $2) ORDER BY s.batch_id, s.store_id`

func scanBatches(rows pgx.Rows) ([]BatchJSON, error) {
	defer rows.Close()
	out := []BatchJSON{}
	for rows.Next() {
		var b BatchJSON
		if err := rows.Scan(&b.ID, &b.VariantID, &b.ProductID, &b.ProductName, &b.SKU, &b.BatchNo, &b.Expiry, &b.MfgDate,
			&b.MRP, &b.Cost, &b.Unknown, &b.Status, &b.Expired, &b.DaysToExpiry, &b.RecalledAt, &b.RecallReason,
			&b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.Stock = []StockJSON{}
		out = append(out, b)
	}
	return out, rows.Err()
}

// withStock fills each batch's stock per store (storeID 0: every store).
func withStock(ctx context.Context, tx tenancy.TenantTx, bs []BatchJSON, storeID int64) error {
	if len(bs) == 0 {
		return nil
	}
	ids := make([]int64, len(bs))
	at := make(map[int64]int, len(bs))
	for i, b := range bs {
		ids[i], at[b.ID] = b.ID, i
	}
	rows, err := tx.Query(ctx, qStock, ids, storeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s StockJSON
		if err := rows.Scan(&id, &s.StoreID, &s.StoreName, &s.Qty, &s.Quarantined); err != nil {
			return err
		}
		b := &bs[at[id]]
		b.Stock = append(b.Stock, s)
		b.TotalQty = b.TotalQty.Add(s.Qty)
		b.TotalQuarantined = b.TotalQuarantined.Add(s.Quarantined)
	}
	return rows.Err()
}

// ------------------------------------------------------------- handlers

func intParam(r *http.Request, name string, def int64) (int64, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, apperr.Invalid(name, "Must be a whole number, 0 or more.")
	}
	return n, nil
}

// list is GET /api/batches (A23).
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	product, err := intParam(r, "productId", 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	variant, err := intParam(r, "variantId", 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	store, err := intParam(r, "storeId", 0)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	within, err := intParam(r, "expiringWithinDays", -1)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	status := strings.TrimSpace(q.Get("status"))
	switch status {
	case "", "available", "recalled", "expired", "unknown":
	default:
		httpx.WriteError(w, r, apperr.Invalid("status", "Use available, recalled, expired or unknown."))
		return
	}
	includeEmpty := q.Get("includeEmpty") == "true"
	var out []BatchJSON
	err = a.ro(r, func(tx tenancy.TenantTx) error {
		rows, err := tx.Query(r.Context(), qList, product, variant, store, status, within, includeEmpty, maxList)
		if err != nil {
			return err
		}
		if out, err = scanBatches(rows); err != nil {
			return err
		}
		return withStock(r.Context(), tx, out, store)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out, "limit": maxList})
}

func loadOne(ctx context.Context, tx tenancy.TenantTx, id int64) (DetailJSON, error) {
	var d DetailJSON
	rows, err := tx.Query(ctx, qBatchCols+` WHERE b.id = $1`, id)
	if err != nil {
		return d, err
	}
	bs, err := scanBatches(rows)
	if err != nil {
		return d, err
	}
	if len(bs) == 0 {
		return d, apperr.NotFound()
	}
	if err := withStock(ctx, tx, bs, 0); err != nil {
		return d, err
	}
	d.BatchJSON = bs[0]
	mr, err := tx.Query(ctx, `SELECT m.id, m.store_id, st.name, m.bucket, m.delta, m.qty_after, m.reason, m.ref_type, m.ref_id, u.name, m.created_at
		FROM batch_movements m JOIN stores st ON st.id = m.store_id LEFT JOIN users u ON u.id = m.user_id
		WHERE m.batch_id = $1 ORDER BY m.id DESC LIMIT $2`, id, maxMovements)
	if err != nil {
		return d, err
	}
	defer mr.Close()
	d.Movements = []MovementJSON{}
	for mr.Next() {
		var m MovementJSON
		if err := mr.Scan(&m.ID, &m.StoreID, &m.StoreName, &m.Bucket, &m.Delta, &m.QtyAfter, &m.Reason, &m.RefType, &m.RefID,
			&m.UserName, &m.CreatedAt); err != nil {
			return d, err
		}
		d.Movements = append(d.Movements, m)
	}
	return d, mr.Err()
}

// get is GET /api/batches/{id} (A23).
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out DetailJSON
	if err := a.ro(r, func(tx tenancy.TenantTx) error {
		var err error
		out, err = loadOne(r.Context(), tx, id)
		return err
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// updateIn is PUT /api/batches/{id}: only the fields sent change.
type updateIn struct {
	BatchNo *string   `json:"batchNo"`
	Expiry  *string   `json:"expiry"`
	MfgDate *string   `json:"mfgDate"`
	MRP     money.Dec `json:"mrp"`
	Cost    money.Dec `json:"cost"`
	Reason  string    `json:"reason"`
	// ConfirmFarExpiry accepts an expiry more than 10 years ahead (design
	// §1 risks: a confirmation, not a refusal).
	ConfirmFarExpiry bool `json:"confirmFarExpiry"`
}

type batchRow struct {
	variantID   int64
	batchNo     string
	expiry, mfg *time.Time
	mrp, cost   *decimal.Decimal
	unknown     bool
	today       time.Time
}

func (b batchRow) audit() map[string]any {
	day := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.Format("2006-01-02")
	}
	return map[string]any{"batchNo": b.batchNo, "expiry": day(b.expiry), "mfgDate": day(b.mfg), "mrp": b.mrp, "cost": b.cost}
}

// update is PUT /api/batches/{id} (A24): correct a batch's details with a
// reason. Receipts of sales already made keep what they were sold with
// (item 3 copies the details onto the sale).
func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in updateIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := len([]rune(in.Reason)); n < 5 || n > maxReasonLen {
		f["reason"] = fmt.Sprintf("Give a reason of 5 to %d characters.", maxReasonLen)
	}
	if in.MRP.Set && in.MRP.Valid && in.MRP.D.IsNegative() {
		f["mrp"] = "MRP cannot be negative."
	}
	if in.Cost.Set && in.Cost.Valid && in.Cost.D.IsNegative() {
		f["cost"] = "Cost cannot be negative."
	}
	if len(f) > 0 {
		httpx.WriteError(w, r, apperr.Fields(f))
		return
	}
	p := principal(r)
	var out DetailJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var b batchRow
		err := tx.QueryRow(ctx, `SELECT b.variant_id, b.batch_no::text, b.expiry_date, b.mfg_date, b.mrp, b.cost, b.is_unknown, `+sqlToday+`
			FROM batches b WHERE b.id = $1 FOR UPDATE`, id).
			Scan(&b.variantID, &b.batchNo, &b.expiry, &b.mfg, &b.mrp, &b.cost, &b.unknown, &b.today)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if b.unknown {
			return apperr.Conflict(apperr.CodeInvalidState, "The unknown batch has no details to correct. Count the stock by batch instead.")
		}
		before := b.audit()
		f := map[string]string{}
		if in.BatchNo != nil {
			v := strings.TrimSpace(*in.BatchNo)
			switch {
			case v == "" || len([]rune(v)) > maxBatchNo:
				f["batchNo"] = fmt.Sprintf("Batch number must be 1 to %d characters.", maxBatchNo)
			case strings.EqualFold(v, inventory.UnknownBatchNo):
				f["batchNo"] = "This name is kept for stock with no batch."
			default:
				b.batchNo = v
			}
		}
		if in.Expiry != nil {
			t, err := ParseExpiry(*in.Expiry)
			if err == nil && !in.ConfirmFarExpiry {
				err = CheckExpiry(t, b.today)
			}
			if err != nil {
				f["expiry"] = "Expiry: " + err.Error() + "."
			} else {
				b.expiry = &t
			}
		}
		if in.MfgDate != nil {
			if v := strings.TrimSpace(*in.MfgDate); v == "" {
				b.mfg = nil
			} else if t, err := time.Parse("2006-01-02", v); err != nil {
				f["mfgDate"] = "Use YYYY-MM-DD."
			} else {
				b.mfg = &t
			}
		}
		if b.mfg != nil && b.expiry != nil && b.mfg.After(*b.expiry) {
			f["mfgDate"] = "The manufacturing date must be on or before the expiry date."
		}
		if in.MRP.Set {
			b.mrp = nil
			if in.MRP.Valid {
				v := in.MRP.D.Round(2)
				b.mrp = &v
			}
		}
		if in.Cost.Set {
			b.cost = nil
			if in.Cost.Valid {
				v := in.Cost.D.Round(2)
				b.cost = &v
			}
		}
		if len(f) > 0 {
			return apperr.Fields(f)
		}
		if _, err := tx.Exec(ctx, `UPDATE batches SET batch_no = $2, expiry_date = $3, mfg_date = $4, mrp = $5, cost = $6, updated_at = now()
			WHERE id = $1`, id, b.batchNo, b.expiry, b.mfg, b.mrp, b.cost); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return apperr.Conflict(apperr.CodeConflict, "This product already has a batch with that number.")
			}
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "batch.edit", Entity: "batch", EntityID: strconv.FormatInt(id, 10),
			Before: before, After: map[string]any{"values": b.audit(), "reason": in.Reason},
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
