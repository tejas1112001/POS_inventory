package products

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/attributes"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Variant matrix (industry P1 item 4, A8): every combination of the chosen
// values of a product's variant dimensions becomes a variant, with a SKU
// from a pattern. Existing combinations are skipped; a dry run only plans.

const (
	qMatrixProduct     = `SELECT product_type, variant_dims, attributes, article_no FROM products WHERE id = $1 AND deleted_at IS NULL`
	qMatrixProductLock = qMatrixProduct + ` FOR UPDATE`

	maxMatrixValues   = 50  // values per dimension
	maxVariantsPerPrd = 200 // same cap as the product form
	maxSKUPattern     = 120
)

// GenerateBarcodes is filled in by item 5 (barcode_labels): it gives the
// new variants internal EAN-13 codes. nil: the option is refused.
var GenerateBarcodes func(ctx context.Context, tx tenancy.TenantTx, variantIDs []int64, userID int64) error

type matrixIn struct {
	Dims             map[string][]any `json:"dims"`
	SKUPattern       string           `json:"skuPattern"`
	Price            money.Dec        `json:"price"`
	Cost             money.Dec        `json:"cost"`
	MRP              money.Dec        `json:"mrp"`
	GenerateBarcodes bool             `json:"generateBarcodes"`
}

// PlannedVariant is one variant the matrix would create.
type PlannedVariant struct {
	SKU        string            `json:"sku"`
	Attributes map[string]string `json:"attributes"`
	Label      string            `json:"label"`
}

// MatrixJSON is the response: planned (dry run) or created variants, and
// the combinations skipped because the product already has them.
type MatrixJSON struct {
	DryRun  bool             `json:"dryRun"`
	Dims    []string         `json:"dims"`
	Planned []PlannedVariant `json:"planned"`
	Skipped []string         `json:"skipped"`
	Created int              `json:"created"`
	Product *ProductJSON     `json:"product,omitempty"`
}

func (a *API) matrix(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in matrixIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	dry := r.URL.Query().Get("dryRun") == "true"
	f := fieldErrs{}
	if !in.Price.Valid {
		f.add("price", "price is required")
	} else if in.Price.D.IsNegative() || in.Price.D.GreaterThan(maxPrice) {
		f.add("price", "price must be between 0 and 99999999999999")
	}
	if in.Cost.Valid && (in.Cost.D.IsNegative() || in.Cost.D.GreaterThan(maxPrice)) {
		f.add("cost", "cost must not be negative")
	}
	if in.MRP.Valid && (in.MRP.D.IsNegative() || in.MRP.D.GreaterThan(maxPrice)) {
		f.add("mrp", "MRP must be between 0 and 99999999999999")
	}
	in.SKUPattern = strings.TrimSpace(in.SKUPattern)
	if len(in.SKUPattern) > maxSKUPattern {
		f.add("skuPattern", fmt.Sprintf("skuPattern must be at most %d characters", maxSKUPattern))
	}
	if in.GenerateBarcodes && GenerateBarcodes == nil {
		f.add("generateBarcodes", "Barcode generation needs the Barcode labels module.")
	}
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	run := a.rw
	if dry {
		run = a.ro
	}
	var out MatrixJSON
	err = run(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = buildMatrix(r.Context(), tx, id, in, dry, uid)
		if err != nil || dry {
			return err
		}
		p, _, err := loadOne(r.Context(), tx, id, loadOpts{})
		out.Product = &p
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	code := http.StatusCreated
	if dry {
		code = http.StatusOK
	}
	httpx.WriteJSON(w, code, out)
}

func buildMatrix(ctx context.Context, tx tenancy.TenantTx, id int64, in matrixIn, dry bool, uid int64) (MatrixJSON, error) {
	var (
		ptype   string
		dims    []string
		pattrs  map[string]string
		article *string
	)
	// One query only: an unread pgx row keeps the connection busy.
	var row pgx.Row
	if dry {
		row = tx.QueryRow(ctx, qMatrixProduct, id)
	} else {
		row = tx.QueryRow(ctx, qMatrixProductLock, id)
	}
	if err := row.Scan(&ptype, &dims, &pattrs, &article); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MatrixJSON{}, apperr.NotFound()
		}
		return MatrixJSON{}, err
	}
	if ptype != "variable" {
		return MatrixJSON{}, apperr.Conflict(apperr.CodeInvalidState, "Only a product with variants can get a size and colour grid.")
	}
	if len(dims) == 0 {
		return MatrixJSON{}, apperr.Conflict(apperr.CodeInvalidState, "Choose the product's variant dimensions first (edit the product).")
	}
	defs, err := attributes.Load(ctx, tx)
	if err != nil {
		return MatrixJSON{}, err
	}

	// The values of each dimension, as the definitions spell them.
	f := fieldErrs{}
	values := make([][]string, len(dims))
	given := map[string][]any{}
	for k, v := range in.Dims {
		given[strings.ToLower(k)] = v
	}
	for k := range given {
		if !containsStr(dims, k) {
			f.add("dims."+k, fmt.Sprintf("%q is not a variant dimension of this product", k))
		}
	}
	for i, k := range dims {
		d, ok := defs[k]
		if !ok {
			f.add("dims."+k, k+" is no longer a defined attribute")
			continue
		}
		vals := given[k]
		if len(vals) == 0 {
			f.add("dims."+k, "Choose at least one "+d.Label)
			continue
		}
		if len(vals) > maxMatrixValues {
			f.add("dims."+k, fmt.Sprintf("at most %d values", maxMatrixValues))
			continue
		}
		seen := map[string]bool{}
		for _, raw := range vals {
			c, msg := d.Canonical(raw)
			if msg != "" {
				f.add("dims."+k, msg)
				continue
			}
			if !seen[strings.ToLower(c)] {
				seen[strings.ToLower(c)] = true
				values[i] = append(values[i], c)
			}
		}
	}
	if err := f.err(); err != nil {
		return MatrixJSON{}, err
	}

	// Existing live variants: their combinations and count.
	rows, err := tx.Query(ctx, `SELECT attributes FROM product_variants WHERE product_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return MatrixJSON{}, err
	}
	existing := map[string]bool{}
	live := 0
	for rows.Next() {
		var va map[string]any
		if err := rows.Scan(&va); err != nil {
			rows.Close()
			return MatrixJSON{}, err
		}
		live++
		existing[comboKey(dims, func(k string) string { return fmt.Sprint(va[k]) })] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return MatrixJSON{}, err
	}

	pattern := in.SKUPattern
	if pattern == "" {
		pattern = "{article}"
		if article == nil {
			pattern = "P{id}"
		}
		for _, k := range dims {
			pattern += "-{" + k + "}"
		}
	}
	if strings.Contains(pattern, "{article}") && article == nil {
		return MatrixJSON{}, apperr.Invalid("skuPattern", "The pattern uses {article} but the product has no article number.")
	}

	out := MatrixJSON{DryRun: dry, Dims: dims, Planned: []PlannedVariant{}, Skipped: []string{}}
	skus := map[string]bool{}
	var dupes []string
	for _, combo := range cartesian(values) {
		attrs := map[string]string{}
		anyAttrs := map[string]any{}
		for i, k := range dims {
			attrs[k], anyAttrs[k] = combo[i], combo[i]
		}
		label := attributes.Label(dims, pattrs, anyAttrs)
		if existing[comboKey(dims, func(k string) string { return attrs[k] })] {
			out.Skipped = append(out.Skipped, label)
			continue
		}
		sku, msg := renderSKU(pattern, id, article, attrs)
		if msg != "" {
			return MatrixJSON{}, apperr.Invalid("skuPattern", msg)
		}
		if skus[strings.ToLower(sku)] {
			dupes = append(dupes, sku)
		}
		skus[strings.ToLower(sku)] = true
		out.Planned = append(out.Planned, PlannedVariant{SKU: sku, Attributes: attrs, Label: label})
	}
	if len(dupes) > 0 {
		e := apperr.Conflict(apperr.CodeConflict, "The SKU pattern gives the same SKU to several variants: "+strings.Join(firstN(dupes, 10), ", ")+". Add a dimension token to the pattern.")
		e.Details = map[string]any{"collisions": dupes}
		return MatrixJSON{}, e
	}
	if live+len(out.Planned) > maxVariantsPerPrd {
		return MatrixJSON{}, apperr.Invalid("dims", fmt.Sprintf("This would give the product %d variants; at most %d.", live+len(out.Planned), maxVariantsPerPrd))
	}
	if len(out.Planned) > 0 {
		lower := make([]string, 0, len(out.Planned))
		for _, p := range out.Planned {
			lower = append(lower, strings.ToLower(p.SKU))
		}
		taken, err := texts(tx.Query(ctx, `SELECT sku::text FROM product_variants WHERE deleted_at IS NULL AND lower(sku::text) = ANY($1) ORDER BY sku`, lower))
		if err != nil {
			return MatrixJSON{}, err
		}
		if len(taken) > 0 {
			e := apperr.Conflict(apperr.CodeConflict, "These SKUs already exist: "+strings.Join(firstN(taken, 10), ", ")+". Change the SKU pattern.")
			e.Details = map[string]any{"collisions": taken}
			return MatrixJSON{}, e
		}
	}
	if dry || len(out.Planned) == 0 {
		return out, nil
	}

	// Create: stock 0 in the stores the product is stocked in (else the
	// first store), so the new variants show at those tills.
	stores, err := ids(tx.Query(ctx, `SELECT DISTINCT i.store_id FROM inventories i JOIN product_variants v ON v.id = i.variant_id
		JOIN stores s ON s.id = i.store_id AND s.deleted_at IS NULL WHERE v.product_id = $1 ORDER BY i.store_id`, id))
	if err != nil {
		return MatrixJSON{}, err
	}
	if len(stores) == 0 {
		stores, err = ids(tx.Query(ctx, `SELECT id FROM stores WHERE deleted_at IS NULL ORDER BY id LIMIT 1`))
		if err != nil {
			return MatrixJSON{}, err
		}
	}
	cost := decimal.Zero
	if in.Cost.Valid {
		cost = in.Cost.D
	}
	var mrp *decimal.Decimal
	if in.MRP.Valid {
		m := in.MRP.D.Round(2)
		mrp = &m
	}
	var created []int64
	for _, p := range out.Planned {
		b, _ := json.Marshal(p.Attributes)
		cv := cleanVariant{sku: p.SKU, price: in.Price.D, cost: cost, mrp: mrp, attributes: b}
		for _, s := range stores {
			cv.inventories = append(cv.inventories, cleanInventory{storeID: s})
		}
		vid, err := insertVariant(ctx, tx, id, cv, uid, inventory.ReasonProductEdit)
		if err != nil {
			return MatrixJSON{}, err
		}
		created = append(created, vid)
	}
	if in.GenerateBarcodes {
		if err := GenerateBarcodes(ctx, tx, created, uid); err != nil {
			return MatrixJSON{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE products SET updated_at = now() WHERE id = $1`, id); err != nil {
		return MatrixJSON{}, err
	}
	out.Created = len(created)
	return out, audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "product.variants_matrix", Entity: "product",
		EntityID: strconv.FormatInt(id, 10), After: map[string]any{"created": len(created), "skipped": len(out.Skipped),
			"dims": dims, "skuPattern": pattern}})
}

// renderSKU fills a SKU pattern: {article}, {id} and each dimension key.
func renderSKU(pattern string, id int64, article *string, attrs map[string]string) (string, string) {
	s := strings.ReplaceAll(pattern, "{id}", strconv.FormatInt(id, 10))
	if article != nil {
		s = strings.ReplaceAll(s, "{article}", attributes.SKUToken(*article))
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		s = strings.ReplaceAll(s, "{"+k+"}", attributes.SKUToken(attrs[k]))
	}
	if i := strings.IndexByte(s, '{'); i >= 0 {
		j := strings.IndexByte(s[i:], '}')
		tok := s[i:]
		if j > 0 {
			tok = s[i : i+j+1]
		}
		return "", fmt.Sprintf("Unknown token %s in the SKU pattern. Use {article}, {id} or a dimension such as {size}.", tok)
	}
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return "", "The SKU pattern gives an empty SKU or one longer than 64 characters."
	}
	return s, ""
}

func comboKey(dims []string, val func(string) string) string {
	parts := make([]string, len(dims))
	for i, k := range dims {
		parts[i] = strings.ToLower(strings.TrimSpace(val(k)))
	}
	return strings.Join(parts, "\x1f")
}

// cartesian returns every combination, the first dimension varying slowest.
func cartesian(values [][]string) [][]string {
	out := [][]string{{}}
	for _, vals := range values {
		var next [][]string
		for _, prefix := range out {
			for _, v := range vals {
				c := append(append([]string(nil), prefix...), v)
				next = append(next, c)
			}
		}
		out = next
	}
	return out
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func texts(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func ids(rows pgx.Rows, err error) ([]int64, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
