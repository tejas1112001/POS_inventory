// Package stockcounts counts a store's stock (or part of it) and posts the
// differences (docs/INDUSTRY_PHASE1_DESIGN.md §7, A15). A draft count holds
// counted quantities only; completing it moves each counted line's stock to
// the counted quantity through the ledger (reason stock_count). Variants
// that were not counted are left unchanged.
package stockcounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/attributes"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Module is the industry module the routes belong to.
const Module = modules.StockCounts

// Limits.
const (
	MaxQty         = 1_000_000
	MaxLinesPerPut = 500
	maxNoteLen     = 1000
	// MaxUncounted is how many uncounted variants a draft lists (the total
	// is always given).
	MaxUncounted = 200
	refType      = "stock_count"
)

// Statuses.
const (
	StatusDraft     = "Draft"
	StatusCompleted = "Completed"
	StatusCancelled = "Cancelled"
)

// Deps are the dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	MaxRows  int
}

// API holds the handlers.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API { return &API{d: d} }

// Mount registers the routes (all in the stock_counts module).
func (a *API) Mount(rt *rbac.Router) {
	req := func(action string) rbac.Requirement { return rbac.Require("stock_counts", action).InModule(Module) }
	rt.Handle(http.MethodGet, "/stock-counts", req("view"), a.list)
	rt.Handle(http.MethodPost, "/stock-counts", req("create"), a.create)
	rt.Handle(http.MethodGet, "/stock-counts/{id}", req("view"), a.get)
	rt.Handle(http.MethodPut, "/stock-counts/{id}", req("create"), a.update)
	rt.Handle(http.MethodPost, "/stock-counts/{id}/complete", req("complete"), a.complete)
	rt.Handle(http.MethodPost, "/stock-counts/{id}/cancel", req("create"), a.cancel)
	rt.Handle(http.MethodPost, "/stock-counts/{id}/import", req("create"), a.importFile)
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

// ------------------------------------------------------------------ JSON

// NameRef is a nested {id, name}.
type NameRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Scope is the part of the store being counted; empty = everything.
type Scope struct {
	CategoryID int64 `json:"categoryId,omitempty"`
	BrandID    int64 `json:"brandId,omitempty"`
}

// CountJSON is one count. Lines and Uncounted come with GET /{id} only.
type CountJSON struct {
	ID           int64      `json:"id"`
	Reference    string     `json:"reference"`
	StoreID      int64      `json:"storeId"`
	Store        NameRef    `json:"Store"`
	Scope        Scope      `json:"scope"`
	ScopeText    string     `json:"scopeText"`
	Status       string     `json:"status"`
	Note         *string    `json:"note"`
	Version      int        `json:"version"`
	LineCount    int        `json:"lineCount"`
	UnitsCounted int64      `json:"unitsCounted"`
	CreatedBy    *NameRef   `json:"createdBy"`
	CompletedBy  *NameRef   `json:"completedBy"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	CompletedAt  *time.Time `json:"completedAt"`

	Lines     []LineJSON      `json:"lines,omitempty"`
	Uncounted []UncountedJSON `json:"uncounted,omitempty"`
	// UncountedBatches (drafts): batches completion will set to 0 (industry
	// P3 item 6).
	UncountedBatches []UncountedBatchJSON `json:"uncountedBatches,omitempty"`
	UncountedTotal   *int                 `json:"uncountedTotal,omitempty"`
	Totals           *Totals              `json:"totals,omitempty"`
}

// LineJSON is one counted variant. For a draft, CurrentQty and Difference
// are live and MovedSince is the stock change since the line was counted
// (sales, purchases...): when it is not 0 the difference has shifted. For a
// completed count, Expected and Difference are what completion posted.
type LineJSON struct {
	ID          int64     `json:"id"`
	VariantID   int64     `json:"variantId"`
	ProductID   int64     `json:"productId"`
	ProductName string    `json:"productName"`
	SKU         string    `json:"sku"`
	Barcode     string    `json:"barcode,omitempty"`
	Label       string    `json:"label,omitempty"`
	CountedQty  int       `json:"countedQty"`
	CountedAt   time.Time `json:"countedAt"`
	CountedBy   *NameRef  `json:"countedBy"`
	CurrentQty  int       `json:"currentQty"`
	MovedSince  int       `json:"movedSince"`
	Expected    *int      `json:"expected"`
	Difference  int       `json:"difference"`
	// The batch counted (industry P3 item 6, A32); null for a line with no
	// batch. A product with batch lines is counted by batch, and its line
	// with no batch counts the unknown batch.
	BatchID *int64  `json:"batchId"`
	BatchNo *string `json:"batchNo"`
	Expiry  *string `json:"expiry"`
	Expired bool    `json:"expired"`
	ByBatch bool    `json:"byBatch"`
	// TracksBatches: the product can be counted by batch.
	TracksBatches bool `json:"tracksBatches"`
}

// UncountedJSON is a variant in scope that has no line (left unchanged on
// completion).
type UncountedJSON struct {
	VariantID   int64  `json:"variantId"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`
	Label       string `json:"label,omitempty"`
	CurrentQty  int    `json:"currentQty"`
}

// Totals sum the lines' differences.
type Totals struct {
	Lines     int `json:"lines"`
	Changed   int `json:"changed"`
	UnitsUp   int `json:"unitsUp"`
	UnitsDown int `json:"unitsDown"`
}

const qCount = `SELECT c.id, c.reference, c.store_id, coalesce(s.name, ''), c.scope, c.status, c.note, c.version,
		c.created_by, coalesce(cu.name, ''), c.completed_by, coalesce(du.name, ''), c.created_at, c.updated_at, c.completed_at,
		(SELECT count(*) FROM stock_count_lines l WHERE l.count_id = c.id),
		(SELECT coalesce(sum(l.counted_qty), 0) FROM stock_count_lines l WHERE l.count_id = c.id),
		coalesce((SELECT name::text FROM categories WHERE id = (c.scope->>'categoryId')::bigint), ''),
		coalesce((SELECT name::text FROM brands WHERE id = (c.scope->>'brandId')::bigint), '')
	FROM stock_counts c
	LEFT JOIN stores s ON s.id = c.store_id
	LEFT JOIN users cu ON cu.id = c.created_by
	LEFT JOIN users du ON du.id = c.completed_by
	WHERE c.id = ANY($1)`

func loadCounts(ctx context.Context, tx tenancy.TenantTx, ids []int64) ([]CountJSON, error) {
	byID := map[int64]CountJSON{}
	rows, err := tx.Query(ctx, qCount, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			c                      CountJSON
			scope                  []byte
			createdBy, completedBy *int64
			cName, dName           string
			catName, brandName     string
		)
		if err := rows.Scan(&c.ID, &c.Reference, &c.StoreID, &c.Store.Name, &scope, &c.Status, &c.Note, &c.Version,
			&createdBy, &cName, &completedBy, &dName, &c.CreatedAt, &c.UpdatedAt, &c.CompletedAt,
			&c.LineCount, &c.UnitsCounted, &catName, &brandName); err != nil {
			return nil, err
		}
		c.Store.ID = c.StoreID
		_ = json.Unmarshal(scope, &c.Scope)
		c.ScopeText = scopeText(catName, brandName)
		if createdBy != nil {
			c.CreatedBy = &NameRef{ID: *createdBy, Name: cName}
		}
		if completedBy != nil {
			c.CompletedBy = &NameRef{ID: *completedBy, Name: dName}
		}
		byID[c.ID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]CountJSON, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func scopeText(category, brand string) string {
	switch {
	case category != "" && brand != "":
		return "Category " + category + ", brand " + brand
	case category != "":
		return "Category " + category
	case brand != "":
		return "Brand " + brand
	}
	return "Whole store"
}

// qLines: a line of a product counted by batch (bm.on) compares with its
// batch's stock (the unknown batch for the line with no batch); other lines
// with the product's stock.
const qLines = `SELECT l.id, l.variant_id, v.product_id, coalesce(p.name, ''), coalesce(v.sku::text, ''), coalesce(v.item_barcode, ''),
		v.attributes, p.attributes, p.variant_dims, l.counted_qty, l.counted_at, l.counted_by, coalesce(u.name, ''),
		CASE WHEN bm.on THEN coalesce((SELECT s.qty FROM batch_stock s WHERE s.store_id = $2 AND s.batch_id = lb.id), 0)
			ELSE coalesce(i.qty, 0) END,
		CASE WHEN bm.on THEN coalesce((SELECT sum(m.delta) FROM batch_movements m WHERE m.store_id = $2 AND m.batch_id = lb.id
				AND m.bucket = 'stock' AND m.created_at > l.counted_at AND NOT (m.ref_type IS NOT DISTINCT FROM 'stock_count' AND m.ref_id = $1)), 0)
			ELSE coalesce((SELECT sum(m.delta) FROM stock_movements m WHERE m.store_id = $2 AND m.variant_id = l.variant_id
				AND m.created_at > l.counted_at AND NOT (m.ref_type IS NOT DISTINCT FROM 'stock_count' AND m.ref_id = $1)), 0) END,
		l.expected_at_completion, l.delta,
		l.batch_id, b.batch_no::text, to_char(b.expiry_date, 'YYYY-MM-DD'),
		coalesce(b.expiry_date < (now() AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date, false), bm.on,
		p.tracks_batches AND EXISTS (SELECT 1 FROM settings WHERE id = 1 AND 'batch_expiry' = ANY(modules))
	FROM stock_count_lines l
	JOIN product_variants v ON v.id = l.variant_id
	JOIN products p ON p.id = v.product_id
	LEFT JOIN batches b ON b.id = l.batch_id
	CROSS JOIN LATERAL (SELECT p.tracks_batches AND EXISTS (SELECT 1 FROM settings WHERE id = 1 AND 'batch_expiry' = ANY(modules))
		AND EXISTS (SELECT 1 FROM stock_count_lines o WHERE o.count_id = l.count_id AND o.variant_id = l.variant_id AND o.batch_id IS NOT NULL) AS on) bm
	CROSS JOIN LATERAL (SELECT coalesce(l.batch_id, (SELECT ub.id FROM batches ub WHERE ub.variant_id = l.variant_id AND ub.is_unknown)) AS id) lb
	LEFT JOIN inventories i ON i.store_id = $2 AND i.variant_id = l.variant_id
	LEFT JOIN users u ON u.id = l.counted_by
	WHERE l.count_id = $1 AND (cardinality($3::bigint[]) = 0 OR l.variant_id = ANY($3))
	ORDER BY p.name, v.sku, l.id`

func variantLabel(vAttrs, pAttrs []byte, dims []string) string {
	var va map[string]any
	var pa map[string]any
	_ = json.Unmarshal(vAttrs, &va)
	_ = json.Unmarshal(pAttrs, &pa)
	ps := map[string]string{}
	for k, v := range pa {
		if s, ok := v.(string); ok {
			ps[k] = s
		}
	}
	if len(dims) > 0 {
		return attributes.Label(dims, ps, va)
	}
	keys := make([]string, 0, len(va))
	for k := range va {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if va[k] == nil {
			continue
		}
		if s := fmt.Sprint(va[k]); s != "" {
			parts = append(parts, k+": "+s)
		}
	}
	return strings.Join(parts, ", ")
}

// loadLines returns the count's lines (only the given variants when ids is
// not empty).
func loadLines(ctx context.Context, tx tenancy.TenantTx, countID, storeID int64, status string, ids []int64) ([]LineJSON, error) {
	if ids == nil {
		ids = []int64{}
	}
	rows, err := tx.Query(ctx, qLines, countID, storeID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LineJSON{}
	for rows.Next() {
		var (
			l              LineJSON
			vAttrs, pAttrs []byte
			dims           []string
			by             *int64
			byName         string
			delta          *int
		)
		if err := rows.Scan(&l.ID, &l.VariantID, &l.ProductID, &l.ProductName, &l.SKU, &l.Barcode, &vAttrs, &pAttrs, &dims,
			&l.CountedQty, &l.CountedAt, &by, &byName, &l.CurrentQty, &l.MovedSince, &l.Expected, &delta,
			&l.BatchID, &l.BatchNo, &l.Expiry, &l.Expired, &l.ByBatch, &l.TracksBatches); err != nil {
			return nil, err
		}
		l.Label = variantLabel(vAttrs, pAttrs, dims)
		if by != nil {
			l.CountedBy = &NameRef{ID: *by, Name: byName}
		}
		switch {
		case status == StatusCompleted && delta != nil:
			l.Difference = *delta
		case status == StatusCompleted:
		default:
			l.Difference = l.CountedQty - l.CurrentQty
			l.Expected = nil
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

const qUncounted = `SELECT v.id, coalesce(p.name, ''), coalesce(v.sku::text, ''), v.attributes, p.attributes, p.variant_dims,
		coalesce(i.qty, 0), count(*) OVER ()
	FROM product_variants v
	JOIN products p ON p.id = v.product_id
	LEFT JOIN inventories i ON i.store_id = $2 AND i.variant_id = v.id
	WHERE v.deleted_at IS NULL AND p.deleted_at IS NULL
	  AND ($3::bigint = 0 OR p.category_id = $3) AND ($4::bigint = 0 OR p.brand_id = $4)
	  AND NOT EXISTS (SELECT 1 FROM stock_count_lines l WHERE l.count_id = $1 AND l.variant_id = v.id)
	ORDER BY coalesce(i.qty, 0) DESC, p.name, v.sku
	LIMIT $5`

func loadUncounted(ctx context.Context, tx tenancy.TenantTx, c CountJSON) ([]UncountedJSON, int, error) {
	rows, err := tx.Query(ctx, qUncounted, c.ID, c.StoreID, c.Scope.CategoryID, c.Scope.BrandID, MaxUncounted)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, total := []UncountedJSON{}, 0
	for rows.Next() {
		var (
			u              UncountedJSON
			vAttrs, pAttrs []byte
			dims           []string
		)
		if err := rows.Scan(&u.VariantID, &u.ProductName, &u.SKU, &vAttrs, &pAttrs, &dims, &u.CurrentQty, &total); err != nil {
			return nil, 0, err
		}
		u.Label = variantLabel(vAttrs, pAttrs, dims)
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// detail loads a count with its lines, totals and (for a draft) the
// uncounted variants in scope.
func detail(ctx context.Context, tx tenancy.TenantTx, id int64) (CountJSON, error) {
	cs, err := loadCounts(ctx, tx, []int64{id})
	if err != nil {
		return CountJSON{}, err
	}
	if len(cs) == 0 {
		return CountJSON{}, apperr.NotFound()
	}
	c := cs[0]
	if c.Lines, err = loadLines(ctx, tx, c.ID, c.StoreID, c.Status, nil); err != nil {
		return c, err
	}
	t := Totals{Lines: len(c.Lines)}
	for _, l := range c.Lines {
		switch {
		case l.Difference > 0:
			t.Changed++
			t.UnitsUp += l.Difference
		case l.Difference < 0:
			t.Changed++
			t.UnitsDown -= l.Difference
		}
	}
	if c.Status == StatusDraft {
		u, total, err := loadUncounted(ctx, tx, c)
		if err != nil {
			return c, err
		}
		c.Uncounted, c.UncountedTotal = u, &total
		if c.UncountedBatches, err = loadUncountedBatches(ctx, tx, c.ID, c.StoreID); err != nil {
			return c, err
		}
		for _, b := range c.UncountedBatches {
			t.Changed++
			t.UnitsDown += b.CurrentQty
		}
	}
	c.Totals = &t
	return c, nil
}

// ------------------------------------------------------------------ list

const (
	qListFilter = ` FROM stock_counts c WHERE ($1::bigint = 0 OR c.store_id = $1) AND ($2 = '' OR c.status = $2)`
	qListCount  = `SELECT count(*)` + qListFilter
	qListIDs    = `SELECT c.id` + qListFilter + ` ORDER BY c.created_at DESC, c.id DESC LIMIT $3 OFFSET $4`
)

// list is GET /api/stock-counts (?storeId=, ?status=; newest first).
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	store, _ := strconv.ParseInt(r.URL.Query().Get("storeId"), 10, 64)
	status := r.URL.Query().Get("status")
	if status != "" && status != StatusDraft && status != StatusCompleted && status != StatusCancelled {
		httpx.WriteError(w, r, apperr.Invalid("status", "status is Draft, Completed or Cancelled"))
		return
	}
	var (
		out   []CountJSON
		total int
	)
	err := a.ro(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, qListCount, store, status).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, qListIDs, store, status, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		out, err = loadCounts(ctx, tx, ids)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

// get is GET /api/stock-counts/{id}.
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out CountJSON
	err = a.ro(r, func(tx tenancy.TenantTx) error {
		var err error
		out, err = detail(r.Context(), tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- create

type createIn struct {
	StoreID money.Int `json:"storeId"`
	Scope   *struct {
		CategoryID money.Int `json:"categoryId"`
		BrandID    money.Int `json:"brandId"`
	} `json:"scope"`
	Note *string `json:"note"`
}

func cleanNote(s *string, f map[string]string) *string {
	if s == nil {
		return nil
	}
	n := strings.TrimSpace(*s)
	switch {
	case len([]rune(n)) > maxNoteLen:
		f["note"] = fmt.Sprintf("At most %d characters.", maxNoteLen)
	case strings.IndexFunc(n, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0:
		f["note"] = "The note cannot contain control characters."
	}
	return &n
}

// create is POST /api/stock-counts: a draft for a store, or part of it.
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var in createIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		f["storeId"] = "storeId is required."
	}
	var sc Scope
	if in.Scope != nil {
		if in.Scope.CategoryID.Valid {
			sc.CategoryID = in.Scope.CategoryID.V
		}
		if in.Scope.BrandID.Valid {
			sc.BrandID = in.Scope.BrandID.V
		}
	}
	note := cleanNote(in.Note, f)
	if err := apperr.Fields(f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	var out CountJSON
	err := a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var storeOK, catOK, brandOK bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1 AND deleted_at IS NULL),
				$2::bigint = 0 OR EXISTS (SELECT 1 FROM categories WHERE id = $2 AND deleted_at IS NULL),
				$3::bigint = 0 OR EXISTS (SELECT 1 FROM brands WHERE id = $3 AND deleted_at IS NULL)`,
			in.StoreID.V, sc.CategoryID, sc.BrandID).Scan(&storeOK, &catOK, &brandOK); err != nil {
			return err
		}
		if !storeOK {
			f["storeId"] = "Store not found."
		}
		if !catOK {
			f["scope.categoryId"] = "Category not found."
		}
		if !brandOK {
			f["scope.brandId"] = "Brand not found."
		}
		if err := apperr.Fields(f); err != nil {
			return err
		}
		var n int64
		if _, err := tx.Exec(ctx, `INSERT INTO doc_counters (name) VALUES ('stock_count') ON CONFLICT (name) DO NOTHING`); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'stock_count' RETURNING next_value - 1`).Scan(&n); err != nil {
			return err
		}
		ref := fmt.Sprintf("CNT-%06d", n)
		scope, _ := json.Marshal(sc)
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO stock_counts (reference, store_id, scope, note, created_by)
				VALUES ($1, $2, $3::jsonb, NULLIF($4, ''), $5) RETURNING id`, ref, in.StoreID.V, string(scope), deref(note), uid).Scan(&id); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "stock_count.create", Entity: "stock_count",
			EntityID: strconv.FormatInt(id, 10), After: map[string]any{"reference": ref, "store_id": in.StoreID.V, "scope": sc},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		var err error
		out, err = detail(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------------------------------------------------------- lines

// LineIn is one change: a variant (or a scanned code) and either an
// absolute quantity (typed) or an increment (a scan adds 1).
type LineIn struct {
	VariantID money.Int `json:"variantId"`
	Code      *string   `json:"code"`
	Qty       money.Int `json:"qty"`
	Add       money.Int `json:"add"`
	// Industry P3 item 6 (A32): the batch counted, or a batch number and
	// expiry found on the shelf (the batch is created).
	BatchID money.Int `json:"batchId"`
	BatchNo *string   `json:"batchNo"`
	Expiry  *string   `json:"expiry"`
}

type updateIn struct {
	Version money.Int `json:"version"`
	Note    *string   `json:"note"`
	Lines   []LineIn  `json:"lines"`
	Remove  []int64   `json:"remove"`
	// RemoveLines removes single lines by id (a batch line).
	RemoveLines []int64 `json:"removeLines"`
}

// UpdateJSON is the PUT answer: the changed lines only (a scan must stay
// fast on a large count).
type UpdateJSON struct {
	ID        int64      `json:"id"`
	Version   int        `json:"version"`
	LineCount int        `json:"lineCount"`
	Lines     []LineJSON `json:"lines"`
	Removed   []int64    `json:"removed"`
	// RemovedLines are the line ids removed by removeLines.
	RemovedLines []int64 `json:"removedLines"`
}

type draft struct {
	id, storeID int64
	scope       Scope
	version     int
}

// lockDraft locks a count for a change: 404 unknown, 409 when it is not a
// draft or the version moved on.
func lockDraft(ctx context.Context, tx tenancy.TenantTx, id int64, version money.Int) (draft, error) {
	var (
		d      draft
		status string
		scope  []byte
	)
	err := tx.QueryRow(ctx, `SELECT store_id, scope, status, version FROM stock_counts WHERE id = $1 FOR UPDATE`, id).
		Scan(&d.storeID, &scope, &status, &d.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, apperr.NotFound()
	}
	if err != nil {
		return d, err
	}
	d.id = id
	_ = json.Unmarshal(scope, &d.scope)
	if status != StatusDraft {
		return d, apperr.Conflict(apperr.CodeInvalidState, fmt.Sprintf("This count is %s and can no longer change.", strings.ToLower(status)))
	}
	if version.Valid && int(version.V) != d.version {
		return d, apperr.Conflict(apperr.CodeConflict, "This count was changed by someone else. Reload it and try again.")
	}
	return d, nil
}

const (
	// One count edit at a time per store (this shop's schema), so two
	// drafts cannot take the same variant at once.
	qStoreLock = `SELECT pg_advisory_xact_lock(hashtextextended('stock_count:' || current_schema() || ':' || $1::text, 0))`
	qByCode    = `SELECT v.id FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.deleted_at IS NULL AND p.deleted_at IS NULL AND (v.item_barcode = $1 OR lower(v.sku::text) = lower($1))
		ORDER BY (v.item_barcode = $1) DESC NULLS LAST, v.id LIMIT 1`
	qVariants = `SELECT v.id, coalesce(p.category_id, 0), coalesce(p.brand_id, 0) FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = ANY($1) AND v.deleted_at IS NULL AND p.deleted_at IS NULL`
	qOtherDraft = `SELECT DISTINCT ON (l.variant_id) l.variant_id, c.reference FROM stock_count_lines l JOIN stock_counts c ON c.id = l.count_id
		WHERE l.variant_id = ANY($1) AND c.store_id = $2 AND c.status = 'Draft' AND c.id <> $3 ORDER BY l.variant_id, c.id`
	qCurrent = `SELECT variant_id, coalesce(batch_id, 0), counted_qty FROM stock_count_lines WHERE count_id = $1 AND variant_id = ANY($2)`
	qUpsert  = `INSERT INTO stock_count_lines (count_id, variant_id, batch_id, counted_qty, counted_by)
		SELECT $1, x.v, NULLIF(x.b, 0), x.q, NULLIF($5, 0) FROM unnest($2::bigint[], $3::bigint[], $4::int[]) AS x(v, b, q)
		ON CONFLICT (count_id, variant_id, (coalesce(batch_id, 0))) DO UPDATE
		SET counted_qty = EXCLUDED.counted_qty, counted_at = now(), counted_by = EXCLUDED.counted_by`
	qRemove      = `DELETE FROM stock_count_lines WHERE count_id = $1 AND variant_id = ANY($2) RETURNING variant_id`
	qRemoveLines = `DELETE FROM stock_count_lines WHERE count_id = $1 AND id = ANY($2) RETURNING id`
	qBump        = `UPDATE stock_counts SET version = version + 1, updated_at = now(), note = CASE WHEN $2 THEN NULLIF($3, '') ELSE note END
		WHERE id = $1 RETURNING version, (SELECT count(*) FROM stock_count_lines WHERE count_id = $1)`
)

// change is a resolved line change.
type change struct {
	field   string // for errors: lines[i] or a file row
	variant int64
	qty     int
	add     bool
	// Industry P3 item 6: the batch (0: none), or a new one's number and expiry.
	batch   int64
	batchNo string
	expiry  time.Time
}

// apply resolves codes, checks scope and other drafts, and writes the
// lines. Field errors are keyed by each change's field.
func apply(ctx context.Context, tx tenancy.TenantTx, d draft, in []change, codes map[int]string, uid int64) ([]int64, error) {
	f := map[string]string{}
	for i, code := range codes {
		var id int64
		err := tx.QueryRow(ctx, qByCode, code).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			f[in[i].field] = fmt.Sprintf("No product has the code %q.", code)
		case err != nil:
			return nil, err
		default:
			in[i].variant = id
		}
	}
	if err := apperr.Fields(f); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(in))
	seen := map[int64]bool{}
	for _, c := range in {
		if !seen[c.variant] {
			seen[c.variant] = true
			ids = append(ids, c.variant)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	type vinfo struct{ cat, brand int64 }
	known := map[int64]vinfo{}
	rows, err := tx.Query(ctx, qVariants, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var v vinfo
		if err := rows.Scan(&id, &v.cat, &v.brand); err != nil {
			rows.Close()
			return nil, err
		}
		known[id] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	others := map[int64]string{}
	rows, err = tx.Query(ctx, qOtherDraft, ids, d.storeID, d.id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var ref string
		if err := rows.Scan(&id, &ref); err != nil {
			rows.Close()
			return nil, err
		}
		others[id] = ref
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range in {
		v, ok := known[c.variant]
		switch {
		case !ok:
			f[c.field] = "Product variant not found."
		case d.scope.CategoryID != 0 && v.cat != d.scope.CategoryID, d.scope.BrandID != 0 && v.brand != d.scope.BrandID:
			f[c.field] = "This item is not part of this count (another category or brand)."
		case others[c.variant] != "":
			f[c.field] = fmt.Sprintf("This item is already in draft count %s for this store. Complete or cancel that count first.", others[c.variant])
		}
	}
	if err := apperr.Fields(f); err != nil {
		return nil, err
	}
	if err := resolveCountBatches(ctx, tx, in, uid); err != nil {
		return nil, err
	}
	type lineKey struct{ v, b int64 }
	cur := map[lineKey]int{}
	rows, err = tx.Query(ctx, qCurrent, d.id, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k lineKey
		var q int
		if err := rows.Scan(&k.v, &k.b, &q); err != nil {
			rows.Close()
			return nil, err
		}
		cur[k] = q
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var keys []lineKey
	seenKey := map[lineKey]bool{}
	for _, c := range in {
		k := lineKey{c.variant, c.batch}
		q := c.qty
		if c.add {
			q += cur[k]
		}
		switch {
		case q < 0:
			f[c.field] = "The count cannot go below 0."
		case q > MaxQty:
			f[c.field] = fmt.Sprintf("At most %d.", MaxQty)
		}
		cur[k] = q
		if !seenKey[k] {
			seenKey[k] = true
			keys = append(keys, k)
		}
	}
	if err := apperr.Fields(f); err != nil {
		return nil, err
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].v != keys[j].v {
			return keys[i].v < keys[j].v
		}
		return keys[i].b < keys[j].b
	})
	vs, bs, qs := make([]int64, len(keys)), make([]int64, len(keys)), make([]int32, len(keys))
	for i, k := range keys {
		vs[i], bs[i], qs[i] = k.v, k.b, int32(cur[k]) //nolint:gosec // G115: at most MaxQty
	}
	if _, err := tx.Exec(ctx, qUpsert, d.id, vs, bs, qs, uid); err != nil {
		return nil, err
	}
	return ids, nil
}

// update is PUT /api/stock-counts/{id}: add or set counted quantities
// (by variant or scanned code), remove lines, change the note.
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
	if len(in.Lines)+len(in.Remove)+len(in.RemoveLines) > MaxLinesPerPut {
		f["lines"] = fmt.Sprintf("At most %d changes at a time.", MaxLinesPerPut)
	}
	note := cleanNote(in.Note, f)
	changes := make([]change, len(in.Lines))
	codes := map[int]string{}
	for i, l := range in.Lines {
		key := fmt.Sprintf("lines[%d]", i)
		c := change{field: key}
		hasCode := l.Code != nil && strings.TrimSpace(*l.Code) != ""
		switch {
		case l.VariantID.Valid == hasCode:
			f[key] = "Give either variantId or code."
		case hasCode:
			code := strings.TrimSpace(*l.Code)
			if len(code) > 64 {
				f[key] = "The code is at most 64 characters."
			}
			codes[i] = code
		case l.VariantID.V <= 0:
			f[key] = "variantId is invalid."
		default:
			c.variant = l.VariantID.V
		}
		switch {
		case l.Qty.Valid == l.Add.Valid:
			f[key+".qty"] = "Give either qty (counted quantity) or add (to add to it)."
		case l.Qty.Valid:
			if l.Qty.V < 0 || l.Qty.V > MaxQty {
				f[key+".qty"] = fmt.Sprintf("A whole number from 0 to %d.", MaxQty)
			}
			c.qty = int(l.Qty.V)
		default:
			if l.Add.V < -MaxQty || l.Add.V > MaxQty || l.Add.V == 0 {
				f[key+".add"] = "A whole number other than 0."
			}
			c.qty, c.add = int(l.Add.V), true
		}
		hasNo := l.BatchNo != nil && strings.TrimSpace(*l.BatchNo) != ""
		switch {
		case l.BatchID.Valid && hasNo:
			f[key+".batchId"] = "Give either batchId or batchNo with expiry."
		case l.BatchID.Valid && l.BatchID.V <= 0:
			f[key+".batchId"] = "batchId is invalid."
		case l.BatchID.Valid:
			c.batch = l.BatchID.V
		case hasNo:
			no, ok := batches.ValidBatchNo(*l.BatchNo)
			if !ok {
				f[key+".batchNo"] = "Batch number must be 1 to 40 characters."
			}
			c.batchNo = no
			exp := ""
			if l.Expiry != nil {
				exp = *l.Expiry
			}
			t, err := batches.ParseExpiry(exp)
			if err != nil {
				f[key+".expiry"] = "Expiry: " + err.Error() + "."
			}
			c.expiry = t
		case l.Expiry != nil && strings.TrimSpace(*l.Expiry) != "":
			f[key+".batchNo"] = "Give the batch number with the expiry."
		}
		changes[i] = c
	}
	for _, v := range in.Remove {
		if v <= 0 {
			f["remove"] = "remove holds variant ids."
		}
	}
	for _, v := range in.RemoveLines {
		if v <= 0 {
			f["removeLines"] = "removeLines holds line ids."
		}
	}
	if err := apperr.Fields(f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	var out UpdateJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		d, err := lockDraft(ctx, tx, id, in.Version)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qStoreLock, d.storeID); err != nil {
			return err
		}
		var touched []int64
		if len(changes) > 0 {
			if touched, err = apply(ctx, tx, d, changes, codes, uid); err != nil {
				return err
			}
		}
		out.Removed = []int64{}
		if len(in.Remove) > 0 {
			rows, err := tx.Query(ctx, qRemove, id, in.Remove)
			if err != nil {
				return err
			}
			if out.Removed, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
				return err
			}
		}
		out.RemovedLines = []int64{}
		if len(in.RemoveLines) > 0 {
			rows, err := tx.Query(ctx, qRemoveLines, id, in.RemoveLines)
			if err != nil {
				return err
			}
			if out.RemovedLines, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, qBump, id, note != nil, deref(note)).Scan(&out.Version, &out.LineCount); err != nil {
			return err
		}
		out.ID = id
		out.Lines = []LineJSON{}
		if len(touched) > 0 {
			out.Lines, err = loadLines(ctx, tx, id, d.storeID, StatusDraft, touched)
		}
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// -------------------------------------------------------- complete/cancel

type versionIn struct {
	Version money.Int `json:"version"`
}

const (
	qCountedLines = `SELECT id, variant_id, coalesce(batch_id, 0), counted_qty FROM stock_count_lines WHERE count_id = $1 ORDER BY variant_id, id`
	qSetPosted    = `UPDATE stock_count_lines l SET expected_at_completion = x.e, delta = x.d
		FROM unnest($2::bigint[], $3::int[], $4::int[]) AS x(id, e, d) WHERE l.count_id = $1 AND l.id = x.id`
	qFinish = `UPDATE stock_counts SET status = $2, completed_by = CASE WHEN $2 = 'Completed' THEN NULLIF($3, 0) END,
		completed_at = CASE WHEN $2 = 'Completed' THEN now() END, version = version + 1, updated_at = now() WHERE id = $1`
)

// complete is POST /api/stock-counts/{id}/complete: each counted line's
// stock becomes the counted quantity (one ledger movement per difference).
func (a *API) complete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in versionIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	var out CountJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		d, err := lockDraft(ctx, tx, id, in.Version)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, qCountedLines, id)
		if err != nil {
			return err
		}
		var ids []int64
		byVar := map[int64][]countLine{}
		for rows.Next() {
			var v int64
			var l countLine
			if err := rows.Scan(&l.id, &v, &l.batch, &l.qty); err != nil {
				rows.Close()
				return err
			}
			if _, ok := byVar[v]; !ok {
				ids = append(ids, v)
			}
			byVar[v] = append(byVar[v], l)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return apperr.Conflict(apperr.CodeInvalidState, "Count at least one item before completing.")
		}
		have, err := inventory.LockRows(ctx, tx, d.storeID, ids)
		if err != nil {
			return err
		}
		tr, err := inventory.Tracked(ctx, tx, ids)
		if err != nil {
			return err
		}
		var lineIDs []int64
		var exp, deltas []int32
		var changed, up, down, nlines int
		for _, v := range ids {
			lines := byVar[v]
			nlines += len(lines)
			byBatch := false
			for _, l := range lines {
				byBatch = byBatch || (tr[v] && l.batch != 0)
			}
			e, df, u, dn, err := postVariant(ctx, tx, d.storeID, v, lines, have[v], byBatch, inventory.Ref{Type: refType, ID: id}, uid)
			if err != nil {
				return err
			}
			if u+dn > 0 {
				changed++
			}
			up, down = up+u, down+dn
			for _, l := range lines {
				lineIDs = append(lineIDs, l.id)
				exp = append(exp, int32(e[l.id]))        //nolint:gosec // G115: quantities are bounded
				deltas = append(deltas, int32(df[l.id])) //nolint:gosec // G115: quantities are bounded
			}
		}
		if _, err := tx.Exec(ctx, qSetPosted, id, lineIDs, exp, deltas); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qFinish, id, StatusCompleted, uid); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "stock_count.complete", Entity: "stock_count",
			EntityID:  strconv.FormatInt(id, 10),
			After:     map[string]any{"store_id": d.storeID, "lines": nlines, "changed": changed, "units_up": up, "units_down": down},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = detail(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// cancel is POST /api/stock-counts/{id}/cancel: the draft is kept,
// read-only, and changes no stock.
func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in versionIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	var out CountJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		d, err := lockDraft(ctx, tx, id, in.Version)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qFinish, id, StatusCancelled, uid); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "stock_count.cancel", Entity: "stock_count",
			EntityID: strconv.FormatInt(id, 10), After: map[string]any{"store_id": d.storeID},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = detail(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
