package products

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// CSV import limits (API_CONTRACT §5.1.1).
const (
	maxImportFile   = 5 << 20
	maxImportRows   = 5000
	maxImportErrors = 200
	importBodyLimit = maxImportFile + 1<<20 // multipart overhead
)

var importColumns = []string{"name", "sku", "price", "cost", "barcode", "category", "sub_category", "brand", "unit",
	"tax_type", "tax_rate", "hsn_code", "store", "opening_qty", "quantity_alert", "status"}

// RowError is one problem in the file (1-based data row, header = row 1).
type RowError struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type importRow struct {
	line                                              int
	vals                                              map[string]string
	price, cost, taxRate                              decimal.Decimal
	hasTax                                            bool
	qty, alert                                        int
	categoryID, subCategoryID, brandID, unitID, store int64
}

type importResult struct {
	Created  int        `json:"created"`
	Variants int        `json:"variants"`
	Errors   []RowError `json:"errors"`
	DryRun   bool       `json:"dryRun,omitempty"`
}

var errDryRun = errors.New("dry run")

func (a *API) importTemplate(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="products-template.csv"`)
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write(importColumns)
	_ = cw.Write([]string{"Assam Tea 250g", "TEA-250", "180", "120", "8901234567890", "", "", "", "", "Inclusive", "5", "0902", "main", "10", "2", "Active"})
	cw.Flush()
}

func (a *API) importCSV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, importBodyLimit)
	if err := r.ParseMultipartForm(1 << 20); err != nil { //nolint:gosec // G120: body capped above
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, r, apperr.TooLarge())
			return
		}
		httpx.WriteError(w, r, apperr.UnsupportedMedia())
		return
	}
	f, _, err := r.FormFile("csvFile")
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("csvFile", "csvFile is required"))
		return
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxImportFile+1))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if len(raw) > maxImportFile {
		httpx.WriteError(w, r, apperr.TooLarge())
		return
	}
	dry := r.URL.Query().Get("dryRun") == "true"
	res, rowErrs, err := a.runImport(r.Context(), tenant(r), principal(r).UserID, raw, dry)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if len(rowErrs) > 0 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"error": fmt.Sprintf("The file has %d error(s); nothing was imported.", len(rowErrs)),
			"code":  apperr.CodeValidationFailed, "errors": rowErrs,
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func parseCSV(raw []byte) ([]map[string]string, []RowError) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return nil, []RowError{{Row: 1, Field: "file", Message: "a header row is required"}}
	}
	cols := map[int]string{}
	have := map[string]bool{}
	known := map[string]bool{}
	for _, c := range importColumns {
		known[c] = true
	}
	var errs []RowError
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		if !known[h] {
			errs = append(errs, RowError{Row: 1, Field: h, Message: "unknown column"})
			continue
		}
		if have[h] {
			errs = append(errs, RowError{Row: 1, Field: h, Message: "column appears twice"})
		}
		cols[i], have[h] = h, true
	}
	for _, req := range []string{"name", "sku", "price"} {
		if !have[req] {
			errs = append(errs, RowError{Row: 1, Field: req, Message: "required column missing"})
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	var rows []map[string]string
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, []RowError{{Row: line, Field: "file", Message: "malformed CSV"}}
		}
		if len(rows) >= maxImportRows {
			return nil, []RowError{{Row: line, Field: "file", Message: "at most 5000 data rows"}}
		}
		vals := map[string]string{"_line": strconv.Itoa(line)}
		empty := true
		for i, v := range rec {
			if c, ok := cols[i]; ok {
				vals[c] = strings.TrimSpace(v)
				empty = empty && vals[c] == ""
			}
		}
		if !empty {
			rows = append(rows, vals)
		}
	}
	if len(rows) == 0 {
		return nil, []RowError{{Row: 2, Field: "file", Message: "no data rows"}}
	}
	return rows, nil
}

// runImport validates and writes in one transaction: all or nothing.
func (a *API) runImport(ctx context.Context, t tenancy.Tenant, uid int64, raw []byte, dry bool) (importResult, []RowError, error) {
	parsed, perrs := parseCSV(raw)
	if len(perrs) > 0 {
		return importResult{}, perrs, nil
	}
	var (
		res     importResult
		rowErrs []RowError
	)
	addErr := func(row int, field, msg string) {
		if len(rowErrs) < maxImportErrors {
			rowErrs = append(rowErrs, RowError{Row: row, Field: field, Message: msg})
		}
	}
	err := a.rw(ctx, t, func(tx tenancy.TenantTx) error {
		lookups, err := loadLookups(ctx, tx)
		if err != nil {
			return err
		}
		rows := make([]importRow, 0, len(parsed))
		seenSKU, seenBarcode := map[string]int{}, map[string]int{}
		for _, v := range parsed {
			line, _ := strconv.Atoi(v["_line"])
			ir := importRow{line: line, vals: v}
			if v["name"] == "" || len([]rune(v["name"])) > 200 {
				addErr(line, "name", "name is required (max 200 characters)")
			}
			sku := v["sku"]
			if sku == "" || len(sku) > 64 {
				addErr(line, "sku", "sku is required (max 64 characters)")
			} else if prev, dup := seenSKU[strings.ToLower(sku)]; dup {
				addErr(line, "sku", fmt.Sprintf("sku repeats row %d", prev))
			} else if lookups.skus[strings.ToLower(sku)] {
				addErr(line, "sku", "a product with this SKU already exists")
			}
			seenSKU[strings.ToLower(sku)] = line
			if b := v["barcode"]; b != "" {
				switch {
				case !barcodePattern.MatchString(b):
					addErr(line, "barcode", "barcode may use letters, digits and . _ / + - (max 64)")
				case lookups.barcodes[b]:
					addErr(line, "barcode", "a product with this barcode already exists")
				case seenBarcode[b] > 0:
					addErr(line, "barcode", fmt.Sprintf("barcode repeats row %d", seenBarcode[b]))
				}
				seenBarcode[b] = line
			}
			ir.price, err = decimal.NewFromString(v["price"])
			if err != nil || ir.price.IsNegative() || ir.price.GreaterThan(maxPrice) {
				addErr(line, "price", "must be a number >= 0")
			}
			if s := v["cost"]; s != "" {
				if ir.cost, err = decimal.NewFromString(s); err != nil || ir.cost.IsNegative() {
					addErr(line, "cost", "must be a number >= 0")
				}
			}
			if s := v["tax_rate"]; s != "" {
				ir.taxRate, err = decimal.NewFromString(s)
				ir.hasTax = true
				if err != nil || ir.taxRate.IsNegative() || ir.taxRate.GreaterThan(hundred) {
					addErr(line, "tax_rate", "must be between 0 and 100")
				}
			}
			if s := v["tax_type"]; s != "" && s != "Exclusive" && s != "Inclusive" {
				addErr(line, "tax_type", "must be Exclusive or Inclusive")
			}
			if s := v["hsn_code"]; s != "" && !hsnPattern.MatchString(s) {
				addErr(line, "hsn_code", "must be 2-8 digits")
			}
			if s := v["status"]; s != "" && s != "Active" && s != "Inactive" {
				addErr(line, "status", "must be Active or Inactive")
			}
			for _, n := range []struct {
				col string
				out *int
			}{{"opening_qty", &ir.qty}, {"quantity_alert", &ir.alert}} {
				if s := v[n.col]; s != "" {
					q, err := strconv.Atoi(s)
					if err != nil || q < 0 || q > 1_000_000_000 {
						addErr(line, n.col, "must be a whole number >= 0")
					}
					*n.out = q
				}
			}
			var ok bool
			if s := v["category"]; s != "" {
				if ir.categoryID, ok = lookups.categories[strings.ToLower(s)]; !ok {
					addErr(line, "category", "unknown category")
				}
			}
			if s := v["sub_category"]; s != "" {
				if ir.subCategoryID, ok = lookups.subCategories[subKey(ir.categoryID, s)]; !ok {
					addErr(line, "sub_category", "unknown sub-category for this category")
				}
			}
			if s := v["brand"]; s != "" {
				if ir.brandID, ok = lookups.brands[strings.ToLower(s)]; !ok {
					addErr(line, "brand", "unknown brand")
				}
			}
			if s := v["unit"]; s != "" {
				if ir.unitID, ok = lookups.units[strings.ToLower(s)]; !ok {
					addErr(line, "unit", "unknown unit")
				}
			}
			if s := v["store"]; s != "" {
				if ir.store, ok = lookups.stores[strings.ToLower(s)]; !ok {
					addErr(line, "store", "unknown store (use the store slug)")
				}
			} else if ir.qty > 0 || ir.alert > 0 {
				addErr(line, "store", "store is required with opening_qty or quantity_alert")
			}
			rows = append(rows, ir)
		}
		if len(rowErrs) > 0 {
			return nil
		}

		// Rows with the same name become one variable product.
		var order []string
		groups := map[string][]importRow{}
		for _, ir := range rows {
			k := strings.ToLower(ir.vals["name"])
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], ir)
		}
		if err := a.checkImportLimit(ctx, tx, len(order)); err != nil {
			return err
		}
		for _, k := range order {
			g := groups[k]
			first := g[0]
			c := clean{name: first.vals["name"], status: "Active", productType: "single"}
			if len(g) > 1 {
				c.productType = "variable"
			}
			if first.vals["status"] == "Inactive" {
				c.status = "Inactive"
			}
			if s := first.vals["tax_type"]; s != "" {
				c.taxType = &s
			}
			if first.hasTax {
				tr := first.taxRate
				c.tax = &tr
			}
			if s := first.vals["hsn_code"]; s != "" {
				c.hsn = &s
			}
			c.categoryID, c.subCategoryID = nonZero(first.categoryID), nonZero(first.subCategoryID)
			c.brandID, c.unitID = nonZero(first.brandID), nonZero(first.unitID)
			for _, ir := range g {
				cv := cleanVariant{sku: ir.vals["sku"], price: ir.price, cost: ir.cost, attributes: []byte("{}")}
				if b := ir.vals["barcode"]; b != "" {
					cv.barcode = &b
				}
				if ir.store > 0 {
					cv.inventories = []cleanInventory{{storeID: ir.store, qty: ir.qty, alert: ir.alert}}
				}
				c.variants = append(c.variants, cv)
			}
			if _, err := createProductWith(ctx, tx, c, nil, uid, inventory.ReasonImport, "product.import_create"); err != nil {
				return err
			}
			res.Created++
			res.Variants += len(g)
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "products.import", Entity: "product",
			After: map[string]any{"created": res.Created, "variants": res.Variants, "dryRun": dry}}); err != nil {
			return err
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		res.DryRun = true
		err = nil
	}
	if err != nil {
		return importResult{}, nil, skuConflict(err)
	}
	res.Errors = []RowError{}
	return res, rowErrs, nil
}

func (a *API) checkImportLimit(ctx context.Context, tx tenancy.TenantTx, adding int) error {
	l, err := a.d.Plans.For(ctx, tx.Tenant().PlanID)
	if err != nil || l.MaxProducts <= 0 {
		return err
	}
	if err := a.d.Plans.Check(ctx, tx, plans.Products); err != nil {
		return err
	}
	var n int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM products WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n+int64(adding) > l.MaxProducts {
		return apperr.Forbidden(apperr.CodePlanLimit, fmt.Sprintf("Your plan allows %d products; this import would make %d.", l.MaxProducts, n+int64(adding)))
	}
	return nil
}

func nonZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func subKey(categoryID int64, name string) string {
	return strconv.FormatInt(categoryID, 10) + ":" + strings.ToLower(name)
}

type lookupMaps struct {
	categories, brands, units, stores map[string]int64
	subCategories                     map[string]int64
	skus, barcodes                    map[string]bool
}

func loadLookups(ctx context.Context, tx tenancy.TenantTx) (lookupMaps, error) {
	m := lookupMaps{categories: map[string]int64{}, brands: map[string]int64{}, units: map[string]int64{},
		stores: map[string]int64{}, subCategories: map[string]int64{}, skus: map[string]bool{}, barcodes: map[string]bool{}}
	if err := pairs(tx.Query(ctx, `SELECT name::text, id FROM categories WHERE deleted_at IS NULL`))(m.categories); err != nil {
		return m, err
	}
	if err := pairs(tx.Query(ctx, `SELECT name::text, id FROM brands WHERE deleted_at IS NULL`))(m.brands); err != nil {
		return m, err
	}
	if err := pairs(tx.Query(ctx, `SELECT name::text, id FROM units WHERE deleted_at IS NULL`))(m.units); err != nil {
		return m, err
	}
	if err := pairs(tx.Query(ctx, `SELECT slug::text, id FROM stores WHERE deleted_at IS NULL`))(m.stores); err != nil {
		return m, err
	}
	if err := pairs(tx.Query(ctx, `SELECT category_id::text || ':' || lower(name::text), id FROM sub_categories WHERE deleted_at IS NULL`))(m.subCategories); err != nil {
		return m, err
	}
	if err := set(tx.Query(ctx, `SELECT lower(sku::text) FROM product_variants WHERE deleted_at IS NULL`))(m.skus); err != nil {
		return m, err
	}
	err := set(tx.Query(ctx, `SELECT item_barcode FROM product_variants WHERE deleted_at IS NULL AND item_barcode IS NOT NULL`))(m.barcodes)
	return m, err
}

// pairs collects (name, id) rows into a lower-cased map.
func pairs(rows pgx.Rows, qerr error) func(map[string]int64) error {
	return func(dst map[string]int64) error {
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var id int64
			if err := rows.Scan(&k, &id); err != nil {
				return err
			}
			dst[strings.ToLower(k)] = id
		}
		return rows.Err()
	}
}

// set collects single-column rows into a set.
func set(rows pgx.Rows, qerr error) func(map[string]bool) error {
	return func(dst map[string]bool) error {
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			dst[k] = true
		}
		return rows.Err()
	}
}
