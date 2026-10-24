// Package adjustments records manual stock additions and subtractions
// (PHASE1_DESIGN step 12; PRD §3.9; P0-4). Each adjustment locks its
// inventory row, refuses to take stock below zero, and writes the record,
// the stock change and the ledger row in one transaction.
package adjustments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const (
	maxQty        = 1_000_000
	maxReasonLen  = 200
	maxNoteLen    = 1000
	defaultReason = "Manual Adjustment"
	hashScope     = "POST /api/stock-adjustments\n"
)

// Deps are the adjustment dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	MaxRows  int
}

// API holds the handlers.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API { return &API{d: d} }

// Mount registers the routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	rt.Handle(http.MethodGet, "/stock-adjustments", req("stock_adjustments", "view"), a.list)
	rt.Handle(http.MethodPost, "/stock-adjustments", req("stock_adjustments", "create"), a.create)
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

// adjustmentIn is the POST body (the old API's fields; productId optional).
type adjustmentIn struct {
	VariantID money.Int `json:"variantId"`
	ProductID money.Int `json:"productId"`
	StoreID   money.Int `json:"storeId"`
	Type      *string   `json:"type"`
	Quantity  money.Int `json:"quantity"`
	Reason    *string   `json:"reason"`
	Note      *string   `json:"note"`
}

type clean struct {
	variantID, productID, storeID int64
	typ, reason, note             string
	qty                           int
}

func validate(in adjustmentIn) (clean, error) {
	f := map[string]string{}
	c := clean{variantID: in.VariantID.V, productID: in.ProductID.V, storeID: in.StoreID.V, reason: defaultReason}
	if !in.VariantID.Valid || in.VariantID.V <= 0 {
		f["variantId"] = "variantId is required."
	}
	if in.ProductID.Valid && in.ProductID.V <= 0 {
		f["productId"] = "productId is invalid."
	}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		f["storeId"] = "storeId is required."
	}
	if in.Type != nil {
		c.typ = strings.TrimSpace(*in.Type)
	}
	if c.typ != "Addition" && c.typ != "Subtraction" {
		f["type"] = "Type must be Addition or Subtraction."
	}
	if !in.Quantity.Valid || in.Quantity.V <= 0 || in.Quantity.V > maxQty {
		f["quantity"] = "Quantity must be a whole number above 0."
	}
	c.qty = int(in.Quantity.V)
	if in.Reason != nil {
		if r := strings.TrimSpace(*in.Reason); r != "" {
			c.reason = r
		}
		switch {
		case len([]rune(c.reason)) > maxReasonLen:
			f["reason"] = fmt.Sprintf("At most %d characters.", maxReasonLen)
		case strings.IndexFunc(c.reason, unicode.IsControl) >= 0:
			f["reason"] = "Reason cannot contain control characters."
		}
	}
	if in.Note != nil {
		c.note = strings.TrimSpace(*in.Note)
		if len([]rune(c.note)) > maxNoteLen {
			f["note"] = fmt.Sprintf("At most %d characters.", maxNoteLen)
		}
	}
	return c, apperr.Fields(f)
}

const (
	qVariant = `SELECT v.product_id, v.sku::text FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = $1 AND v.deleted_at IS NULL AND p.deleted_at IS NULL`
	qInsert = `INSERT INTO stock_adjustments (type, quantity, reason, note, product_id, variant_id, store_id, qty_after, created_by)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9) RETURNING id`
)

// create is POST /api/stock-adjustments.
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	key, err := idempotency.Key(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hash := idempotency.Hash(r, hashScope)
	var in adjustmentIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	var (
		out   AdjustmentJSON
		again *idempotency.Replay
	)
	err = a.d.Sessions.InTenantTx(r.Context(), tenant(r), tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" {
			var err error
			if again, err = idempotency.Claim(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		f := map[string]string{}
		var productID int64
		var sku string
		err := tx.QueryRow(ctx, qVariant, c.variantID).Scan(&productID, &sku)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			f["variantId"] = "Product variant not found."
		case err != nil:
			return err
		case c.productID > 0 && c.productID != productID:
			f["variantId"] = "The variant does not belong to this product."
		}
		var storeOK bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1 AND deleted_at IS NULL)`, c.storeID).Scan(&storeOK); err != nil {
			return err
		}
		if !storeOK {
			f["storeId"] = "Store not found."
		}
		if err := apperr.Fields(f); err != nil {
			return err
		}
		k := inventory.Key{Store: c.storeID, Variant: c.variantID}
		delta := c.qty
		if c.typ == "Subtraction" {
			delta = -c.qty
		}
		deltas := map[inventory.Key]int{k: delta}
		keys, have, err := inventory.Lock(ctx, tx, deltas, map[int64]string{c.variantID: sku})
		if err != nil {
			return err
		}
		after := have[k] + delta
		var id int64
		if err := tx.QueryRow(ctx, qInsert, c.typ, c.qty, c.reason, c.note, productID, c.variantID, c.storeID, after, p.UserID).Scan(&id); err != nil {
			return err
		}
		if err := inventory.Apply(ctx, tx, keys, deltas, inventory.ReasonAdjustment, inventory.Ref{Type: "stock_adjustment", ID: id}, p.UserID); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "stock.adjustment", Entity: "stock_adjustment",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"qty": have[k]},
			After:     map[string]any{"qty": after, "type": c.typ, "quantity": c.qty, "reason": c.reason, "store_id": c.storeID, "variant_id": c.variantID},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		l, err := load(ctx, tx, []int64{id})
		if err != nil {
			return err
		}
		if len(l) == 0 {
			return apperr.Internal(nil)
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

// ------------------------------------------------------------------- read

// NameRef is a nested {id, name}.
type NameRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// VariantRef is the nested Variant with its Product, as the old API had.
type VariantRef struct {
	ID         int64           `json:"id"`
	SKU        string          `json:"sku"`
	Attributes json.RawMessage `json:"attributes"`
	Product    NameRef         `json:"Product"`
}

// AdjustmentJSON keeps the old field names (Store, creator, Variant) and
// adds product_id and qtyAfter.
type AdjustmentJSON struct {
	ID        int64      `json:"id"`
	Type      string     `json:"type"`
	Quantity  int        `json:"quantity"`
	Reason    string     `json:"reason"`
	Note      *string    `json:"note"`
	ProductID int64      `json:"product_id"`
	VariantID int64      `json:"variant_id"`
	StoreID   int64      `json:"store_id"`
	QtyAfter  int        `json:"qtyAfter"`
	CreatedBy int64      `json:"created_by"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Store     NameRef    `json:"Store"`
	Creator   NameRef    `json:"creator"`
	Variant   VariantRef `json:"Variant"`
}

const qLoad = `SELECT a.id, a.type, a.quantity, a.reason, a.note, a.product_id, a.variant_id, a.store_id, a.qty_after,
		a.created_by, a.created_at, coalesce(s.name, ''), coalesce(u.name, ''), coalesce(v.sku::text, ''), v.attributes,
		coalesce(p.name, '')
	FROM stock_adjustments a
	LEFT JOIN stores s ON s.id = a.store_id
	LEFT JOIN users u ON u.id = a.created_by
	LEFT JOIN product_variants v ON v.id = a.variant_id
	LEFT JOIN products p ON p.id = a.product_id
	WHERE a.id = ANY($1)`

func load(ctx context.Context, tx tenancy.TenantTx, ids []int64) ([]AdjustmentJSON, error) {
	if len(ids) == 0 {
		return []AdjustmentJSON{}, nil
	}
	byID := make(map[int64]AdjustmentJSON, len(ids))
	rows, err := tx.Query(ctx, qLoad, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x AdjustmentJSON
		var attrs []byte
		if err := rows.Scan(&x.ID, &x.Type, &x.Quantity, &x.Reason, &x.Note, &x.ProductID, &x.VariantID, &x.StoreID, &x.QtyAfter,
			&x.CreatedBy, &x.CreatedAt, &x.Store.Name, &x.Creator.Name, &x.Variant.SKU, &attrs, &x.Variant.Product.Name); err != nil {
			return nil, err
		}
		if attrs == nil {
			attrs = []byte("{}")
		}
		x.UpdatedAt = x.CreatedAt
		x.Store.ID, x.Creator.ID = x.StoreID, x.CreatedBy
		x.Variant.ID, x.Variant.Attributes, x.Variant.Product.ID = x.VariantID, attrs, x.ProductID
		byID[x.ID] = x
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]AdjustmentJSON, 0, len(ids))
	for _, id := range ids {
		if x, ok := byID[id]; ok {
			out = append(out, x)
		}
	}
	return out, nil
}

func queryID(r *http.Request, name string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

const (
	qFilter = ` FROM stock_adjustments a WHERE ($1 = 0 OR a.store_id = $1) AND ($2 = 0 OR a.variant_id = $2)`
	qCount  = `SELECT count(*)` + qFilter
	qIDs    = `SELECT a.id` + qFilter + ` ORDER BY a.created_at DESC, a.id DESC LIMIT $3 OFFSET $4`
)

// list is GET /api/stock-adjustments: newest first, a bare array unless
// page/limit are given; ?storeId= and ?variantId= filter.
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	store, variant := queryID(r, "storeId"), queryID(r, "variantId")
	var (
		out   []AdjustmentJSON
		total int
	)
	err := a.d.Sessions.InTenantTx(r.Context(), tenant(r), tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qCount, store, variant).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qIDs, store, variant, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		out, err = load(r.Context(), tx, ids)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}
