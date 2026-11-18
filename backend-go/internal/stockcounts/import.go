package stockcounts

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/xlsx"
)

// Import limits (as the product import).
const (
	maxImportFile = 5 << 20
	maxImportRows = 5000
	maxRowErrors  = 200
)

// RowError is one problem in an imported sheet (header = row 1).
type RowError struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ImportJSON is the import answer.
type ImportJSON struct {
	Imported  int `json:"imported"`
	Version   int `json:"version"`
	LineCount int `json:"lineCount"`
}

// Column names: the code (sku or barcode) and the counted quantity.
var (
	codeColumns = map[string]bool{"code": true, "sku": true, "barcode": true}
	qtyColumns  = map[string]bool{"counted_qty": true, "qty": true, "quantity": true}
)

// readSheet returns the rows of an .xlsx (first sheet) or a CSV.
func readSheet(raw []byte) ([][]string, *RowError) {
	if bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		rows, err := xlsx.ReadLimit(raw, maxImportRows+1)
		switch {
		case errors.Is(err, xlsx.ErrTooManyRows):
			return nil, &RowError{Row: maxImportRows + 2, Field: "file", Message: "at most 5000 data rows"}
		case err != nil:
			return nil, &RowError{Row: 1, Field: "file", Message: "The file is not a readable Excel workbook. Save it again as .xlsx (or CSV UTF-8)."}
		}
		return rows, nil
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, &RowError{Row: 1, Field: "file", Message: "The file contains NUL characters. Save it as \"CSV UTF-8\" or .xlsx."}
	}
	cr := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	var out [][]string
	for line := 1; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, &RowError{Row: line, Field: "file", Message: "malformed CSV"}
		}
		if line > maxImportRows+1 {
			return nil, &RowError{Row: line, Field: "file", Message: "at most 5000 data rows"}
		}
		out = append(out, rec)
	}
}

// parseCount maps the sheet to changes (absolute counted quantities). Each
// code appears once.
func parseCount(rows [][]string) ([]change, map[int]string, []RowError) {
	if len(rows) == 0 {
		return nil, nil, []RowError{{Row: 1, Field: "file", Message: "a header row is required"}}
	}
	codeCol, qtyCol := -1, -1
	for i, h := range rows[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		switch {
		case codeColumns[h] && codeCol < 0:
			codeCol = i
		case qtyColumns[h] && qtyCol < 0:
			qtyCol = i
		}
	}
	var errs []RowError
	if codeCol < 0 {
		errs = append(errs, RowError{Row: 1, Field: "code", Message: "a code column is required (sku, barcode or code)"})
	}
	if qtyCol < 0 {
		errs = append(errs, RowError{Row: 1, Field: "counted_qty", Message: "a counted_qty column is required"})
	}
	if len(errs) > 0 {
		return nil, nil, errs
	}
	var changes []change
	codes := map[int]string{}
	seen := map[string]int{}
	cell := func(r []string, i int) string {
		if i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}
	for i, r := range rows[1:] {
		line := i + 2
		code, qs := cell(r, codeCol), cell(r, qtyCol)
		if code == "" && qs == "" {
			continue
		}
		add := func(field, msg string) {
			if len(errs) < maxRowErrors {
				errs = append(errs, RowError{Row: line, Field: field, Message: msg})
			}
		}
		switch {
		case code == "":
			add("code", "the code is required")
		case len(code) > 64:
			add("code", "the code is at most 64 characters")
		case seen[strings.ToLower(code)] > 0:
			add("code", fmt.Sprintf("this code repeats row %d", seen[strings.ToLower(code)]))
		}
		seen[strings.ToLower(code)] = line
		q, err := strconv.Atoi(qs)
		if err != nil || q < 0 || q > MaxQty {
			add("counted_qty", fmt.Sprintf("a whole number from 0 to %d", MaxQty))
		}
		codes[len(changes)] = code
		changes = append(changes, change{field: "row " + strconv.Itoa(line), qty: q})
	}
	if len(changes) == 0 && len(errs) == 0 {
		errs = append(errs, RowError{Row: 2, Field: "file", Message: "no data rows"})
	}
	return changes, codes, errs
}

// importFile is POST /api/stock-counts/{id}/import: a sheet of code and
// counted_qty sets those lines (all rows or none).
func (a *API) importFile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportFile+1<<20)
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
	rowErrs := func(errs []RowError) {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"error": fmt.Sprintf("The file has %d error(s); nothing was imported.", len(errs)),
			"code":  apperr.CodeValidationFailed, "errors": errs,
		})
	}
	rows, rerr := readSheet(raw)
	if rerr != nil {
		rowErrs([]RowError{*rerr})
		return
	}
	changes, codes, errs := parseCount(rows)
	if len(errs) > 0 {
		rowErrs(errs)
		return
	}
	uid := principal(r).UserID
	var out ImportJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		d, err := lockDraft(ctx, tx, id, money.Int{})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qStoreLock, d.storeID); err != nil {
			return err
		}
		if _, err := apply(ctx, tx, d, changes, codes, uid); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, qBump, id, false, "").Scan(&out.Version, &out.LineCount); err != nil {
			return err
		}
		out.Imported = len(changes)
		return audit.Write(ctx, tx, audit.Entry{ActorID: uid, Action: "stock_count.import", Entity: "stock_count",
			EntityID: strconv.FormatInt(id, 10), After: map[string]any{"rows": len(changes)},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if ae, ok := apperr.As(err); ok && ae.Code == apperr.CodeValidationFailed {
		// Field errors are keyed "row N": turn them into row errors.
		if len(ae.Fields) > 0 && strings.HasPrefix(firstKey(ae.Fields), "row ") {
			var list []RowError
			for k, msg := range ae.Fields {
				n, _ := strconv.Atoi(strings.TrimPrefix(k, "row "))
				list = append(list, RowError{Row: n, Field: "code", Message: msg})
			}
			sortRowErrors(list)
			rowErrs(list)
			return
		}
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func firstKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

func sortRowErrors(l []RowError) {
	for i := 1; i < len(l); i++ {
		for j := i; j > 0 && l[j].Row < l[j-1].Row; j-- {
			l[j], l[j-1] = l[j-1], l[j]
		}
	}
}
