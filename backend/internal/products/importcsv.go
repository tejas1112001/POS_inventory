package products

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/attributes"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend/internal/pricing"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend/internal/xlsx"
)

// Import limits (API_CONTRACT §5.1.1).
const (
	maxImportFile   = 5 << 20
	maxImportRows   = 5000
	maxImportErrors = 200
	importBodyLimit = maxImportFile + 1<<20 // multipart overhead
	// An error report (A14) is kept this long, one per user.
	importReportTTL = time.Hour
)

// importColumns are the retail template's columns (unchanged since C-19).
var importColumns = []string{"name", "sku", "price", "cost", "mrp", "barcode", "category", "sub_category", "brand", "unit",
	"tax_type", "tax_rate", "hsn_code", "store", "opening_qty", "quantity_alert", "status"}

// Template types (A12). Clothing and footwear columns follow the design
// note §6; attribute columns are kept only where the shop defines them.
const templateRetail = "retail"

var templateColumns = map[string][]string{
	templateRetail: importColumns,
	"clothing": {"article_no", "name", "department", "collection", "season", "size", "colour", "sku", "barcode", "price", "cost",
		"mrp", "category", "brand", "tax_type", "tax_rate", "hsn_code", "opening_qty", "quantity_alert"},
	"footwear": {"article_no", "name", "department", "size_system", "size", "colour", "sku", "barcode", "price", "cost",
		"mrp", "category", "brand", "tax_type", "tax_rate", "hsn_code", "opening_qty", "quantity_alert"},
	// Industry P3 item 8 (A35): one row per batch.
	templatePharmacy: {"name", "sku", "barcode", "category", "brand", "hsn_code", "tax_type", "tax_rate", "price", "cost",
		"batch_no", "expiry", "mfg_date", "mrp", "store", "opening_qty", "quantity_alert"},
}

var templateExample = map[string]map[string]string{
	templateRetail: {"name": "Assam Tea 250g", "sku": "TEA-250", "price": "180", "cost": "120", "mrp": "190", "barcode": "8901234567890",
		"tax_type": "Inclusive", "tax_rate": "5", "hsn_code": "0902", "store": "main", "opening_qty": "10", "quantity_alert": "2", "status": "Active"},
	"clothing": {"article_no": "TSH-01", "name": "Crew neck tee", "department": "Men", "collection": "Basics", "season": "Spring/Summer",
		"size": "M", "colour": "Blue", "sku": "TSH-01-M-BLUE", "price": "499", "cost": "250", "mrp": "599", "tax_type": "Inclusive", "tax_rate": "5",
		"hsn_code": "6109", "opening_qty": "10", "quantity_alert": "2"},
	"footwear": {"article_no": "RUN-01", "name": "Running shoe", "department": "Men", "size_system": "UK", "size": "8", "colour": "Black",
		"sku": "RUN-01-8-BLACK", "price": "2999", "cost": "1500", "mrp": "3499", "tax_type": "Inclusive", "tax_rate": "12", "hsn_code": "6404",
		"opening_qty": "6", "quantity_alert": "1"},
	templatePharmacy: {"name": "Paracetamol 500mg (10 tablets)", "sku": "PARA-500", "barcode": "8901234567891", "hsn_code": "3004",
		"tax_type": "Inclusive", "tax_rate": "12", "price": "30", "cost": "18", "batch_no": "PCM2401", "expiry": "03/2027",
		"mfg_date": "04/2024", "mrp": "32", "store": "main", "opening_qty": "50", "quantity_alert": "10"},
}

var columnHelp = map[string]string{
	"article_no":     "Your article or style number. Rows with the same article number become options (sizes, colours) of one product.",
	"name":           "Product name, up to 200 characters. Without an article number, rows with the same name become one product.",
	"sku":            "Unique code of this option, up to 64 characters. Must not exist yet.",
	"price":          "Selling price, a number such as 499 or 499.50.",
	"cost":           "Purchase cost, a number. Optional.",
	"barcode":        "The barcode printed on the item (letters, digits and . _ / + -). Optional; must not exist yet.",
	"category":       "An existing category name. Optional.",
	"sub_category":   "An existing sub-category of that category. Optional.",
	"brand":          "An existing brand name. Optional.",
	"unit":           "An existing unit name. Optional.",
	"tax_type":       "Inclusive or Exclusive. Optional.",
	"tax_rate":       "Tax rate in percent, 0 to 100. Optional.",
	"hsn_code":       "HSN code, 2 to 8 digits. Optional.",
	"store":          "The store's slug (Settings → Stores). Needed with opening_qty in the general template; clothing and footwear use the first store when empty.",
	"opening_qty":    "Stock you have now, a whole number. Optional.",
	"quantity_alert": "Low-stock level, a whole number. Optional.",
	"status":         "Active or Inactive. Optional (Active).",
	"batch_no":       "Batch (lot) number printed on the pack, up to 40 characters. Rows with the same SKU are batches of one product.",
	"expiry":         "Expiry date: YYYY-MM-DD, or MM/YYYY as printed on the pack (the last day of that month). Expired rows are not imported.",
	"mfg_date":       "Manufacturing date: YYYY-MM-DD or MM/YYYY. Optional.",
	"mrp":            "Maximum retail price printed on the pack, tax included. Optional. A price above it is imported with a warning. In the pharmacy template it is this batch's MRP.",
}

// codeColumns hold codes that Excel turns into numbers ("8.90123E+12").
var codeColumns = map[string]bool{"sku": true, "barcode": true, "article_no": true, "hsn_code": true,
	"batch_no": true, "expiry": true, "mfg_date": true}

var sciNotation = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?[eE][+-]?[0-9]+$`)

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
	mrp                                               *decimal.Decimal // the variant's MRP (not the pharmacy template: there it is the batch's)
	hasTax                                            bool
	qty, alert                                        qty.Q
	categoryID, subCategoryID, brandID, unitID, store int64
	b                                                 batchFields // pharmacy template
}

// PreviewStock is a variant's opening stock in one store.
type PreviewStock struct {
	Store string `json:"store"`
	Qty   qty.Q  `json:"qty"`
}

// PreviewVariant is one option the import would create.
type PreviewVariant struct {
	Row        int              `json:"row"`
	SKU        string           `json:"sku"`
	Barcode    string           `json:"barcode,omitempty"`
	Price      decimal.Decimal  `json:"price"`
	MRP        *decimal.Decimal `json:"mrp,omitempty"`
	Label      string           `json:"label,omitempty"`
	Attributes map[string]any   `json:"attributes"`
	Stock      []PreviewStock   `json:"stock"`
	Batches    []PreviewBatch   `json:"batches,omitempty"` // pharmacy template
}

// PreviewProduct is one product the import would create (A13 dry run).
type PreviewProduct struct {
	Row         int               `json:"row"`
	Action      string            `json:"action"`
	Name        string            `json:"name"`
	ArticleNo   string            `json:"articleNo,omitempty"`
	Attributes  map[string]string `json:"attributes"`
	VariantDims []string          `json:"variantDims"`
	Variants    []PreviewVariant  `json:"variants"`
}

type importResult struct {
	Created  int              `json:"created"`
	Variants int              `json:"variants"`
	Errors   []RowError       `json:"errors"`
	Warnings []RowError       `json:"warnings"` // pharmacy template: expired and near-expiry rows
	DryRun   bool             `json:"dryRun,omitempty"`
	Preview  []PreviewProduct `json:"preview,omitempty"`
}

var errDryRun = errors.New("dry run")

// ---- A12: templates ----

func (a *API) importTemplate(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	if typ == "" {
		typ = templateRetail
	}
	cols, ok := templateColumns[typ]
	if !ok {
		httpx.WriteError(w, r, apperr.Invalid("type", "type is retail, clothing, footwear or pharmacy"))
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "xlsx" {
		httpx.WriteError(w, r, apperr.Invalid("format", "format is csv or xlsx"))
		return
	}
	defs := attributes.Defs{}
	if typ != templateRetail && typ != templatePharmacy {
		err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			var err error
			defs, err = attributes.Load(r.Context(), tx)
			return err
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var keep []string
		for _, c := range cols {
			if _, isAttr := attributeColumn(c); !isAttr || defs[c].Key != "" {
				keep = append(keep, c)
			}
		}
		cols = keep
	}
	example := templateExample[typ]
	if format == "csv" {
		name := "products-template.csv"
		if typ != templateRetail {
			name = "products-" + typ + ".csv"
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.WriteHeader(http.StatusOK)
		cw := csv.NewWriter(w)
		_ = cw.Write(cols)
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = example[c]
		}
		_ = cw.Write(row)
		cw.Flush()
		return
	}
	var buf bytes.Buffer
	if err := writeTemplateXLSX(&buf, cols, example, defs); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="products-`+typ+`.xlsx"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// attributeColumn reports whether a template column is an attribute (not
// one of the fixed product columns).
func attributeColumn(c string) (string, bool) {
	if c == "article_no" || isPharmacyColumn(c) {
		return "", false
	}
	for _, f := range importColumns {
		if f == c {
			return "", false
		}
	}
	return c, true
}

// writeTemplateXLSX writes the Products sheet (header, one example row,
// code columns formatted as text) and the Help sheet.
func writeTemplateXLSX(w io.Writer, cols []string, example map[string]string, defs attributes.Defs) error {
	b, err := xlsx.NewBook(w, "#,##0.00")
	if err != nil {
		return err
	}
	sheetCols := make([]xlsx.Col, len(cols))
	head := make([]xlsx.Cell, len(cols))
	ex := make([]xlsx.Cell, len(cols))
	for i, c := range cols {
		st := xlsx.Plain
		if codeColumns[c] {
			st = xlsx.Code
		}
		sheetCols[i] = xlsx.Col{Width: max(14, float64(len(c))+4), Style: st}
		head[i] = xlsx.Text(c, xlsx.Header)
		ex[i] = xlsx.Text(example[c], st)
	}
	if err := b.Sheet("Products", sheetCols); err != nil {
		return err
	}
	if err := b.Row(head...); err != nil {
		return err
	}
	if err := b.Row(ex...); err != nil {
		return err
	}
	if err := b.Sheet("Help", []xlsx.Col{{Width: 16}, {Width: 10}, {Width: 70}, {Width: 60}}); err != nil {
		return err
	}
	if err := b.Row(xlsx.Text("Column", xlsx.Header), xlsx.Text("Required", xlsx.Header), xlsx.Text("What to enter", xlsx.Header),
		xlsx.Text("Allowed values", xlsx.Header)); err != nil {
		return err
	}
	for _, c := range cols {
		req := ""
		if c == "name" || c == "sku" || c == "price" {
			req = "Yes"
		}
		help, allowed := columnHelp[c], ""
		if d, ok := defs[c]; ok {
			help = d.Label + ": "
			if d.Level == attributes.LevelVariant {
				help += "one value per option (row)."
			} else {
				help += "the same for every option of the product."
			}
			if d.Kind == attributes.KindChoice {
				allowed = strings.Join(d.Choices, ", ")
			} else {
				help += " Any text up to 60 characters."
			}
		}
		if err := b.Row(xlsx.Text(c, xlsx.Bold), xlsx.Text(req, xlsx.Plain), xlsx.Text(help, xlsx.Plain), xlsx.Text(allowed, xlsx.Plain)); err != nil {
			return err
		}
	}
	for _, note := range []string{"",
		"Delete the example row before importing. The first sheet is read; keep the header row as it is.",
		"Code columns (sku, barcode, article_no, hsn_code) are formatted as text so Excel keeps them as typed.",
		"All rows are imported or none. At most 5,000 rows and 5 MB."} {
		if err := b.Row(xlsx.Text(note, xlsx.Plain)); err != nil {
			return err
		}
	}
	return b.Close()
}

// ---- A13: import ----

func (a *API) importCSV(w http.ResponseWriter, r *http.Request) {
	// A platform flag can turn product import off for this shop (UI S9).
	if err := a.d.Plans.RequireFeatureFor(r.Context(), tenant(r), plans.FeatureCSVImport, "CSV import"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
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
	f, _, err := r.FormFile("file")
	if err != nil {
		f, _, err = r.FormFile("csvFile")
	}
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("file", "file is required (.xlsx or .csv)"))
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
	isXLSX := bytes.HasPrefix(raw, []byte("PK\x03\x04"))
	// Postgres cannot store NUL; a CSV full of them is usually UTF-16.
	if !isXLSX && bytes.IndexByte(raw, 0) >= 0 {
		httpx.WriteError(w, r, apperr.Invalid("csvFile", "The file contains NUL characters. Save it as \"CSV UTF-8\" or .xlsx and try again."))
		return
	}
	dry := r.URL.Query().Get("dryRun") == "true"
	t, uid := tenant(r), principal(r).UserID
	res, rowErrs, records, err := a.runImport(r.Context(), t, uid, raw, isXLSX, dry)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if len(rowErrs) > 0 {
		body := map[string]any{
			"error": fmt.Sprintf("The file has %d error(s); nothing was imported.", len(rowErrs)),
			"code":  apperr.CodeValidationFailed, "errors": rowErrs,
		}
		if token, err := a.saveReport(t.ID, uid, records, rowErrs); err == nil {
			body["errorReport"] = token
		}
		httpx.WriteJSON(w, http.StatusBadRequest, body)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// readTable returns the file's rows (header first): the first sheet of an
// .xlsx, or the CSV records.
func readTable(raw []byte, isXLSX bool) ([][]string, []RowError) {
	if isXLSX {
		rows, err := xlsx.ReadLimit(raw, maxImportRows+1)
		switch {
		case errors.Is(err, xlsx.ErrTooManyRows):
			return nil, []RowError{{Row: maxImportRows + 2, Field: "file", Message: "at most 5000 data rows"}}
		case err != nil:
			return nil, []RowError{{Row: 1, Field: "file", Message: "The file is not a readable Excel workbook. Save it again as .xlsx (or CSV UTF-8) and try again."}}
		}
		return rows, nil
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	var out [][]string
	for line := 1; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, []RowError{{Row: line, Field: "file", Message: "malformed CSV"}}
		}
		if line > maxImportRows+1 {
			return out, []RowError{{Row: line, Field: "file", Message: "at most 5000 data rows"}}
		}
		out = append(out, rec)
	}
	return out, nil
}

// parseRows maps the rows to column values. known: the accepted columns.
func parseRows(records [][]string, known map[string]bool) ([]string, []map[string]string, []RowError) {
	if len(records) == 0 || len(records[0]) == 0 {
		return nil, nil, []RowError{{Row: 1, Field: "file", Message: "a header row is required"}}
	}
	cols := map[int]string{}
	have := map[string]bool{}
	var header []string
	var errs []RowError
	for i, h := range records[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		header = append(header, h)
		if h == "" {
			continue
		}
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
		return header, nil, errs
	}
	var rows []map[string]string
	for i, rec := range records[1:] {
		vals := map[string]string{"_line": strconv.Itoa(i + 2)}
		empty := true
		for j, v := range rec {
			if c, ok := cols[j]; ok {
				vals[c] = strings.TrimSpace(v)
				empty = empty && vals[c] == ""
			}
		}
		if !empty {
			rows = append(rows, vals)
		}
	}
	if len(rows) == 0 {
		return header, nil, []RowError{{Row: 2, Field: "file", Message: "no data rows"}}
	}
	return header, rows, nil
}

// group is one product to create: its rows and checked attributes.
type group struct {
	rows    []importRow
	article string
	checked attributes.Checked
}

// runImport validates and writes in one transaction: all or nothing. It
// returns the file's rows for the error report.
func (a *API) runImport(ctx context.Context, t tenancy.Tenant, uid int64, raw []byte, isXLSX, dry bool) (importResult, []RowError, [][]string, error) {
	records, terrs := readTable(raw, isXLSX)
	if len(terrs) > 0 {
		return importResult{}, terrs, records, nil
	}
	var (
		res      importResult
		rowErrs  []RowError
		warnings []RowError
	)
	addErr := func(row int, field, msg string) {
		if len(rowErrs) < maxImportErrors {
			rowErrs = append(rowErrs, RowError{Row: row, Field: field, Message: msg})
		}
	}
	warn := func(row int, field, msg string) {
		if len(warnings) < maxImportErrors {
			warnings = append(warnings, RowError{Row: row, Field: field, Message: msg})
		}
	}
	err := a.rw(ctx, t, func(tx tenancy.TenantTx) error {
		defs, err := attributes.Load(ctx, tx)
		if err != nil {
			return err
		}
		known := map[string]bool{"article_no": true}
		for _, c := range importColumns {
			known[c] = true
		}
		for _, c := range pharmacyColumns {
			known[c] = true
		}
		for k := range defs {
			known[k] = true
		}
		header, parsed, perrs := parseRows(records, known)
		if len(perrs) > 0 {
			rowErrs = perrs
			return nil
		}
		// A clothing or footwear file: article numbers or attribute columns.
		var attrCols []string
		industry := false
		for _, h := range header {
			if h == "article_no" {
				industry = true
			}
			if _, ok := defs[h]; ok {
				if _, isAttr := attributeColumn(h); isAttr {
					attrCols = append(attrCols, h)
					industry = true
				}
			}
		}
		sort.SliceStable(attrCols, func(i, j int) bool { return defs[attrCols[i]].Sort < defs[attrCols[j]].Sort })
		ph, perrs, err := pharmacyFile(ctx, tx, header, industry)
		if err != nil {
			return err
		}
		if len(perrs) > 0 {
			rowErrs = perrs
			return nil
		}

		lookups, err := loadLookups(ctx, tx)
		if err != nil {
			return err
		}
		rows := make([]importRow, 0, len(parsed))
		seenSKU, seenBarcode := map[string]int{}, map[string]int{}
		seenBarcodeSKU := map[string]string{} // pharmacy: a SKU's batches share its barcode
		for _, v := range parsed {
			line, _ := strconv.Atoi(v["_line"])
			ir := importRow{line: line, vals: v}
			sci := map[string]bool{}
			for c := range codeColumns {
				if sciNotation.MatchString(v[c]) {
					sci[c] = true
					addErr(line, c, fmt.Sprintf("Excel changed this code into a number (%s). Format the column as Text and type the code again.", v[c]))
				}
			}
			if v["name"] == "" || len([]rune(v["name"])) > 200 {
				addErr(line, "name", "name is required (max 200 characters)")
			}
			if s := v["article_no"]; len(s) > 64 {
				addErr(line, "article_no", "article_no is at most 64 characters")
			}
			sku := v["sku"]
			switch {
			case sci["sku"]:
			case sku == "" || len(sku) > 64:
				addErr(line, "sku", "sku is required (max 64 characters)")
			default:
				prev, dup := seenSKU[strings.ToLower(sku)]
				switch {
				case dup && ph.file: // another batch of the same product
				case dup:
					addErr(line, "sku", fmt.Sprintf("sku repeats row %d", prev))
				case lookups.skus[strings.ToLower(sku)]:
					addErr(line, "sku", "a product with this SKU already exists")
				}
				if !dup || !ph.file {
					seenSKU[strings.ToLower(sku)] = line
				}
			}
			if b := v["barcode"]; b != "" && !sci["barcode"] {
				switch {
				case !barcodePattern.MatchString(b):
					addErr(line, "barcode", "barcode may use letters, digits and . _ / + - (max 64)")
				case lookups.barcodes[b]:
					addErr(line, "barcode", "a product with this barcode already exists")
				case seenBarcode[b] > 0 && (!ph.file || seenBarcodeSKU[b] != strings.ToLower(sku)):
					addErr(line, "barcode", fmt.Sprintf("barcode repeats row %d", seenBarcode[b]))
				}
				seenBarcode[b], seenBarcodeSKU[b] = line, strings.ToLower(sku)
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
			if s := v["mrp"]; s != "" && !ph.file { // pharmacy: the batch's MRP (checkBatchRow)
				m, err := decimal.NewFromString(s)
				if err != nil || m.IsNegative() || m.GreaterThan(maxPrice) {
					addErr(line, "mrp", "must be a number >= 0")
				} else {
					m = m.Round(2)
					ir.mrp = &m
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
			if s := v["hsn_code"]; s != "" && !sci["hsn_code"] && !hsnPattern.MatchString(s) {
				addErr(line, "hsn_code", "must be 2-8 digits")
			}
			if s := v["status"]; s != "" && s != "Active" && s != "Inactive" {
				addErr(line, "status", "must be Active or Inactive")
			}
			for _, n := range []struct {
				col string
				out *qty.Q
			}{{"opening_qty", &ir.qty}, {"quantity_alert", &ir.alert}} {
				if s := v[n.col]; s != "" {
					// Imported products sell in whole units (H3).
					q, err := qty.Parse(s)
					if err != nil || q.IsNegative() || q.Greater(maxStock) || !q.IsWhole() {
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
			switch s := v["store"]; {
			case s != "":
				if ir.store, ok = lookups.stores[strings.ToLower(s)]; !ok {
					addErr(line, "store", "unknown store (use the store slug)")
				}
			case ir.qty.IsZero() && ir.alert.IsZero():
			case (industry || ph.file) && lookups.firstStore > 0:
				ir.store = lookups.firstStore // clothing, footwear and pharmacy: the first store
			default:
				addErr(line, "store", "store is required with opening_qty or quantity_alert")
			}
			if ph.file {
				checkBatchRow(&ir, ph, addErr, warn)
			}
			rows = append(rows, ir)
		}

		// Rows with the same article number (else the same name) become
		// one product.
		var order []string
		groups := map[string]*group{}
		for _, ir := range rows {
			k := "n:" + strings.ToLower(ir.vals["name"])
			if art := ir.vals["article_no"]; art != "" {
				k = "a:" + strings.ToLower(art)
			}
			if ph.file { // pharmacy: rows with the same SKU are batches of one product
				k = "s:" + strings.ToLower(ir.vals["sku"])
			}
			g, ok := groups[k]
			if !ok {
				g = &group{article: ir.vals["article_no"]}
				groups[k] = g
				order = append(order, k)
			}
			g.rows = append(g.rows, ir)
		}
		for _, k := range order {
			if ph.file {
				checkBatchGroup(groups[k], addErr)
			} else {
				checkGroup(groups[k], defs, attrCols, addErr)
			}
		}
		if len(rowErrs) > 0 {
			return nil
		}
		if err := a.checkImportLimit(ctx, tx, len(order)); err != nil {
			return err
		}
		storeNames := lookups.storeNames
		var includeTax bool // the shop default, for products without a tax type
		if err := tx.QueryRow(ctx, `SELECT prices_include_tax_default FROM settings WHERE id = 1`).Scan(&includeTax); err != nil {
			return err
		}
		var tracked []int64 // other templates with the module on (design §0)
		for _, k := range order {
			g := groups[k]
			first := g.rows[0]
			c := clean{name: first.vals["name"], status: "Active", productType: "single"}
			if !ph.file && (len(g.rows) > 1 || len(g.checked.Dims) > 0) {
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
			if g.article != "" {
				art := g.article
				c.articleIn = &art
			}
			if g.checked.Uses() {
				attrs := map[string]any{}
				for ak, av := range g.checked.Attributes {
					attrs[ak] = av
				}
				dims := append([]string{}, g.checked.Dims...)
				c.attrsIn, c.dimsIn = &attrs, &dims
			}
			pp := PreviewProduct{Row: first.line, Action: "create", Name: c.name, ArticleNo: g.article,
				Attributes: g.checked.Attributes, VariantDims: g.checked.Dims}
			variantRows := g.rows
			if ph.file {
				variantRows = g.rows[:1] // one variant; the rows are its batches
			}
			for i, ir := range variantRows {
				cv := cleanVariant{sku: ir.vals["sku"], price: ir.price, cost: ir.cost, mrp: ir.mrp, attributes: []byte("{}")}
				// The product's tax comes from its first row.
				inclusive := includeTax
				if c.taxType != nil {
					inclusive = *c.taxType == "Inclusive"
				}
				if pricing.AboveMRP(ir.price, first.taxRate, inclusive, ir.mrp) {
					warn(ir.line, "mrp", "The price with tax is above the MRP. The product is imported; check the price.")
				}
				if len(g.checked.Dims) > 0 {
					cv.attrMap = g.checked.Variants[i]
				}
				if b := ir.vals["barcode"]; b != "" {
					cv.barcode = &b
				}
				pv := PreviewVariant{Row: ir.line, SKU: cv.sku, Price: ir.price, MRP: ir.mrp, Attributes: map[string]any{}, Stock: []PreviewStock{}}
				if cv.barcode != nil {
					pv.Barcode = *cv.barcode
				}
				if cv.attrMap != nil {
					pv.Attributes = cv.attrMap
					pv.Label = attributes.Label(g.checked.Dims, g.checked.Attributes, cv.attrMap)
				}
				if ir.store > 0 {
					cv.inventories = []cleanInventory{{storeID: ir.store, qty: ir.qty, alert: ir.alert}}
					pv.Stock = append(pv.Stock, PreviewStock{Store: storeNames[ir.store], Qty: ir.qty})
				}
				if ph.file {
					cv.inventories, pv.Stock = batchStores(g, storeNames)
				}
				c.variants = append(c.variants, cv)
				pp.Variants = append(pp.Variants, pv)
			}
			pid, err := createProductWith(ctx, tx, c, nil, uid, inventory.ReasonImport, "product.import_create")
			if err != nil {
				return err
			}
			switch {
			case ph.file:
				if pp.Variants[0].Batches, err = importBatches(ctx, tx, pid, g, uid, storeNames); err != nil {
					return err
				}
			case ph.moduleOn:
				tracked = append(tracked, pid)
			}
			res.Created++
			res.Variants += len(variantRows)
			if dry {
				res.Preview = append(res.Preview, pp)
			}
		}
		if err := startImportTracking(ctx, tx, tracked, uid); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "products.import", Entity: "product",
			After: map[string]any{"created": res.Created, "variants": res.Variants, "dryRun": dry, "tracksBatches": ph.moduleOn}}); err != nil {
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
		return importResult{}, nil, records, skuConflict(err)
	}
	sort.SliceStable(rowErrs, func(i, j int) bool { return rowErrs[i].Row < rowErrs[j].Row })
	res.Errors = []RowError{}
	sort.SliceStable(warnings, func(i, j int) bool { return warnings[i].Row < warnings[j].Row })
	res.Warnings = append([]RowError{}, warnings...)
	if !dry {
		res.Preview = nil
	}
	return res, rowErrs, records, nil
}

// checkGroup checks one product's rows: the same name and product-level
// values on every row, and one valid value per option for each variant
// dimension (the variant-level columns filled in on any of its rows).
func checkGroup(g *group, defs attributes.Defs, attrCols []string, addErr func(int, string, string)) {
	first := g.rows[0]
	if g.article != "" {
		for _, ir := range g.rows[1:] {
			if !strings.EqualFold(ir.vals["name"], first.vals["name"]) {
				addErr(ir.line, "name", fmt.Sprintf("name differs from row %d (same article_no)", first.line))
			}
		}
	}
	attrs := map[string]any{}
	var dims []string
	for _, col := range attrCols {
		d := defs[col]
		if d.Level == attributes.LevelProduct {
			val, from := "", 0
			for _, ir := range g.rows {
				v := ir.vals[col]
				switch {
				case v == "":
				case val == "":
					val, from = v, ir.line
				case !strings.EqualFold(v, val):
					addErr(ir.line, col, fmt.Sprintf("%s differs from row %d (%s); one product has one %s", d.Label, from, val, strings.ToLower(d.Label)))
				}
			}
			if val != "" {
				attrs[col] = val
			}
			continue
		}
		for _, ir := range g.rows {
			if ir.vals[col] != "" {
				dims = append(dims, col)
				break
			}
		}
	}
	variants := make([]map[string]any, len(g.rows))
	for i, ir := range g.rows {
		va := map[string]any{}
		for _, k := range dims {
			if v := ir.vals[k]; v != "" {
				va[k] = v
			}
		}
		variants[i] = va
	}
	checked, f := defs.Check(attrs, dims, variants)
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		line, field, msg := first.line, key, f[key]
		switch {
		case key == "variantDims":
			field = "attributes"
		case strings.HasPrefix(key, "attributes."):
			field = strings.TrimPrefix(key, "attributes.")
		case strings.HasPrefix(key, "variants["):
			var i int
			if _, err := fmt.Sscanf(key, "variants[%d]", &i); err == nil && i < len(g.rows) {
				line = g.rows[i].line
			}
			rest := key[strings.Index(key, "]")+1:]
			if rest == ".attributes" { // two options alike
				field, msg = strings.Join(dims, "/"), sameAs(g, checked, i, dims, msg)
			} else {
				field = strings.TrimPrefix(rest, ".attributes.")
			}
		}
		addErr(line, field, msg)
	}
	g.checked = checked
}

// sameAs rewrites "the same size and colour as variant 2" with the row.
func sameAs(g *group, c attributes.Checked, i int, dims []string, msg string) string {
	if i >= len(c.Variants) || c.Variants[i] == nil {
		return msg
	}
	for j := 0; j < i; j++ {
		same := c.Variants[j] != nil
		for _, k := range dims {
			same = same && strings.EqualFold(fmt.Sprint(c.Variants[j][k]), fmt.Sprint(c.Variants[i][k]))
		}
		if same {
			return fmt.Sprintf("the same %s as row %d", strings.Join(dims, " and "), g.rows[j].line)
		}
	}
	return msg
}

func (a *API) checkImportLimit(ctx context.Context, tx tenancy.TenantTx, adding int) error {
	l, err := a.d.Plans.LimitsFor(ctx, tx.Tenant())
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
	storeNames                        map[int64]string
	firstStore                        int64
}

func loadLookups(ctx context.Context, tx tenancy.TenantTx) (lookupMaps, error) {
	m := lookupMaps{categories: map[string]int64{}, brands: map[string]int64{}, units: map[string]int64{},
		stores: map[string]int64{}, subCategories: map[string]int64{}, skus: map[string]bool{}, barcodes: map[string]bool{},
		storeNames: map[int64]string{}}
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
	if err := set(tx.Query(ctx, `SELECT item_barcode FROM product_variants WHERE deleted_at IS NULL AND item_barcode IS NOT NULL`))(m.barcodes); err != nil {
		return m, err
	}
	rows, err := tx.Query(ctx, `SELECT id, name FROM stores WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return m, err
		}
		if m.firstStore == 0 {
			m.firstStore = id
		}
		m.storeNames[id] = name
	}
	return m, rows.Err()
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

// ---- A14: error report ----

// importReport is a failed import's error report, kept in memory for an
// hour for the user who ran it (no file is stored).
type importReport struct {
	tenantID string
	userID   int64
	xlsx     []byte
	expires  time.Time
}

// saveReport builds the error report (each problem with its original row)
// and keeps it as the user's only report. It returns the download token.
func (a *API) saveReport(tenantID string, userID int64, records [][]string, errs []RowError) (string, error) {
	var header []string
	if len(records) > 0 {
		header = records[0]
	}
	var buf bytes.Buffer
	b, err := xlsx.NewBook(&buf, "#,##0.00")
	if err != nil {
		return "", err
	}
	cols := []xlsx.Col{{Width: 6}, {Width: 16}, {Width: 60}}
	for range header {
		cols = append(cols, xlsx.Col{Width: 14, Style: xlsx.Code})
	}
	if err := b.Sheet("Errors", cols); err != nil {
		return "", err
	}
	head := []xlsx.Cell{xlsx.Text("Row", xlsx.Header), xlsx.Text("Column", xlsx.Header), xlsx.Text("Problem", xlsx.Header)}
	for _, h := range header {
		head = append(head, xlsx.Text(h, xlsx.Header))
	}
	if err := b.Row(head...); err != nil {
		return "", err
	}
	for _, e := range errs {
		cells := []xlsx.Cell{xlsx.Num(float64(e.Row), xlsx.Plain), xlsx.Text(e.Field, xlsx.Plain), xlsx.Text(e.Message, xlsx.Plain)}
		if e.Row >= 1 && e.Row <= len(records) {
			for _, v := range records[e.Row-1] {
				cells = append(cells, xlsx.Text(v, xlsx.Code))
			}
		}
		if err := b.Row(cells...); err != nil {
			return "", err
		}
	}
	if err := b.Close(); err != nil {
		return "", err
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	now := time.Now()
	a.reports.DeleteFunc(func(_ string, r importReport) bool {
		return (r.tenantID == tenantID && r.userID == userID) || now.After(r.expires)
	})
	a.reports.Set(token, importReport{tenantID: tenantID, userID: userID, xlsx: buf.Bytes(), expires: now.Add(importReportTTL)}, importReportTTL)
	return token, nil
}

var reportToken = regexp.MustCompile(`^[0-9a-f]{32}$`)

// importErrors serves a report to the user who ran the import (A14).
func (a *API) importErrors(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rep, ok := importReport{}, false
	if reportToken.MatchString(token) {
		rep, ok = a.reports.Get(token)
	}
	if !ok || rep.tenantID != tenant(r).ID || rep.userID != principal(r).UserID {
		httpx.WriteError(w, r, apperr.NotFound())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="import-errors.xlsx"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(rep.xlsx)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rep.xlsx)
}
