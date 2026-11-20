package labels

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
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/products"
	"github.com/tejas1112001/POS_inventory/backend/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Module is the industry module that owns these routes (modules.BarcodeLabels).
const Module = modules.BarcodeLabels

// Request limits.
const (
	MaxVariants  = 500 // per A9 or A10 request
	MaxCopies    = 500 // per label line
	maxCodeTries = 1000
)

// API serves A9-A11.
type API struct {
	sessions *tenancy.Sessions
	modules  *modules.Service
}

// New creates the API and lets the variant matrix (A8) give new variants
// a barcode when this module is on.
func New(s *tenancy.Sessions, m *modules.Service) *API {
	a := &API{sessions: s, modules: m}
	products.GenerateBarcodes = a.generateFromMatrix
	return a
}

// Mount registers the routes: A9 and A10 in the barcode_labels module.
func (a *API) Mount(rt *rbac.Router) {
	rt.Handle(http.MethodPost, "/barcodes/generate", rbac.Require("products", "edit").InModule(Module), a.generate)
	rt.Handle(http.MethodPost, "/labels/pdf", rbac.Require("labels", "print").InModule(Module), a.pdf)
	// Layouts are settings (A11), kept when the module is turned off.
	rt.Handle(http.MethodGet, "/settings/labels", rbac.Require("settings", "view"), a.getLayouts)
	rt.Handle(http.MethodPut, "/settings/labels", rbac.Require("settings", "edit"), a.putLayouts)
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func (a *API) rw(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

// ---- A9: generate barcodes ----

type generateIn struct {
	VariantIDs []int64 `json:"variantIds"`
}

// Generated is one variant's new code.
type Generated struct {
	VariantID int64  `json:"variantId"`
	Barcode   string `json:"barcode"`
}

// GenerateJSON is the A9 response. Variants that already have a barcode
// are listed in skipped and keep it.
type GenerateJSON struct {
	Generated []Generated `json:"generated"`
	Skipped   []int64     `json:"skipped"`
}

const (
	qLockVariants = `SELECT id, item_barcode, product_id FROM product_variants
		WHERE id = ANY($1::bigint[]) AND deleted_at IS NULL ORDER BY id FOR UPDATE`
	qEnsureCounter = `INSERT INTO doc_counters (name) VALUES ('barcode') ON CONFLICT (name) DO NOTHING`
	qNextBarcode   = `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'barcode' RETURNING next_value - 1`
	// A code any variant ever held (deleted ones too) is never reused.
	qCodeTaken = `SELECT EXISTS (SELECT 1 FROM product_variants WHERE item_barcode = $1)`
	qSetCode   = `UPDATE product_variants SET item_barcode = $1, updated_at = now() WHERE id = $2 AND item_barcode IS NULL`
	qTouch     = `UPDATE products SET updated_at = now() WHERE id = ANY($1::bigint[])`
)

func uniqueIDs(ids []int64) ([]int64, error) {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if id <= 0 {
			return nil, apperr.Fields(map[string]string{"variantIds": "variant ids are positive numbers"})
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, apperr.Fields(map[string]string{"variantIds": "choose at least one variant"})
	}
	if len(out) > MaxVariants {
		return nil, apperr.Fields(map[string]string{"variantIds": fmt.Sprintf("at most %d variants at a time", MaxVariants)})
	}
	return out, nil
}

func (a *API) generate(w http.ResponseWriter, r *http.Request) {
	var in generateIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ids, err := uniqueIDs(in.VariantIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out GenerateJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = Generate(r.Context(), tx, ids, principal(r).UserID, true)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// Generate gives each variant without a barcode the next free in-store
// EAN-13. Existing barcodes are never changed. strict: an unknown or
// deleted variant id is a 404.
func Generate(ctx context.Context, tx tenancy.TenantTx, ids []int64, userID int64, strict bool) (GenerateJSON, error) {
	out := GenerateJSON{Generated: []Generated{}, Skipped: []int64{}}
	rows, err := tx.Query(ctx, qLockVariants, ids)
	if err != nil {
		return out, err
	}
	type v struct {
		id, product int64
		code        *string
	}
	var found []v
	for rows.Next() {
		var x v
		if err := rows.Scan(&x.id, &x.code, &x.product); err != nil {
			rows.Close()
			return out, err
		}
		found = append(found, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	if strict && len(found) != len(ids) {
		return out, apperr.NotFound()
	}
	var touched []int64
	for _, x := range found {
		if x.code != nil && *x.code != "" {
			out.Skipped = append(out.Skipped, x.id)
			continue
		}
		code, err := nextCode(ctx, tx)
		if err != nil {
			return out, err
		}
		if _, err := tx.Exec(ctx, qSetCode, code, x.id); err != nil {
			return out, err
		}
		out.Generated = append(out.Generated, Generated{VariantID: x.id, Barcode: code})
		touched = append(touched, x.product)
	}
	if len(out.Generated) == 0 {
		return out, nil
	}
	if _, err := tx.Exec(ctx, qTouch, touched); err != nil {
		return out, err
	}
	ids = ids[:0]
	for _, g := range out.Generated {
		ids = append(ids, g.VariantID)
	}
	return out, audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: "barcode.generate", Entity: "product_variant",
		EntityID: strconv.FormatInt(out.Generated[0].VariantID, 10),
		After:    map[string]any{"variantIds": ids, "first": out.Generated[0].Barcode, "count": len(out.Generated)}})
}

// nextCode takes counter values until one gives a code no variant holds.
func nextCode(ctx context.Context, tx tenancy.TenantTx) (string, error) {
	// A restore from an export made before the counter existed has no row.
	if _, err := tx.Exec(ctx, qEnsureCounter); err != nil {
		return "", err
	}
	for range maxCodeTries {
		var n int64
		if err := tx.QueryRow(ctx, qNextBarcode).Scan(&n); err != nil {
			return "", err
		}
		code, err := InternalCode(n)
		if err != nil {
			return "", apperr.Conflict(apperr.CodeInvalidState, "The in-store barcode range is used up.")
		}
		var taken bool
		if err := tx.QueryRow(ctx, qCodeTaken, code).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
	}
	return "", errors.New("labels: no free barcode found")
}

// generateFromMatrix is the variant matrix's "give new variants a
// barcode" option (A8): refused unless this module is on.
func (a *API) generateFromMatrix(ctx context.Context, tx tenancy.TenantTx, ids []int64, userID int64) error {
	t, _ := tenancy.TenantFrom(ctx)
	if err := a.modules.Require(ctx, t, Module); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := Generate(ctx, tx, ids, userID, false)
	return err
}

// ---- A11: layouts ----

// LayoutsJSON is the A11 body: the built-in presets (read only) and the
// shop's own layouts.
type LayoutsJSON struct {
	Presets []Layout `json:"presets"`
	Layouts []Layout `json:"layouts"`
}

const (
	qLayouts    = `SELECT label_layouts FROM settings WHERE id = 1`
	qSetLayouts = `UPDATE settings SET label_layouts = $1::jsonb, updated_at = now() WHERE id = 1`
)

func loadLayouts(ctx context.Context, tx tenancy.TenantTx) ([]Layout, error) {
	var raw []byte
	err := tx.QueryRow(ctx, qLayouts).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Layout{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Layout{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// findLayout returns a preset or a saved layout by id.
func findLayout(ctx context.Context, tx tenancy.TenantTx, id string) (Layout, bool, error) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true, nil
		}
	}
	saved, err := loadLayouts(ctx, tx)
	if err != nil {
		return Layout{}, false, err
	}
	for _, l := range saved {
		if l.ID == id {
			return l, true, nil
		}
	}
	return Layout{}, false, nil
}

func (a *API) getLayouts(w http.ResponseWriter, r *http.Request) {
	var out LayoutsJSON
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out.Layouts, err = loadLayouts(r.Context(), tx)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out.Presets = Presets
	httpx.WriteJSON(w, http.StatusOK, out)
}

type layoutsIn struct {
	Layouts []Layout `json:"layouts"`
}

func (a *API) putLayouts(w http.ResponseWriter, r *http.Request) {
	var in layoutsIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	if len(in.Layouts) > MaxCustomLayouts {
		f["layouts"] = fmt.Sprintf("at most %d layouts", MaxCustomLayouts)
	}
	ids := map[string]bool{}
	for i := range in.Layouts {
		prefix := fmt.Sprintf("layouts[%d]", i)
		for k, v := range in.Layouts[i].Validate(prefix) {
			f[k] = v
		}
		if ids[in.Layouts[i].ID] {
			f[prefix+".id"] = "two layouts have the id " + in.Layouts[i].ID
		}
		ids[in.Layouts[i].ID] = true
	}
	if len(f) > 0 {
		httpx.WriteError(w, r, apperr.Fields(f))
		return
	}
	if in.Layouts == nil {
		in.Layouts = []Layout{}
	}
	raw, err := json.Marshal(in.Layouts)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		before, err := loadLayouts(r.Context(), tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), qSetLayouts, string(raw)); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: principal(r).UserID, Action: "settings.labels",
			Entity: "settings", EntityID: "labels", Before: map[string]any{"layouts": before}, After: map[string]any{"layouts": in.Layouts}})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, LayoutsJSON{Presets: Presets, Layouts: in.Layouts})
}

// ---- A10: label PDF ----

type pdfItem struct {
	VariantID int64 `json:"variantId"`
	Copies    int   `json:"copies"`
	// BatchID prints the batch number and expiry on the label (industry P3
	// item 2; batch_expiry module).
	BatchID int64 `json:"batchId"`
}

type pdfIn struct {
	Items    []pdfItem `json:"items"`
	LayoutID string    `json:"layoutId"`
	// Test prints one page of sample labels to check the printer's
	// alignment and scaling (no variants needed).
	Test bool `json:"test"`
}

const qLabelVariants = `SELECT v.id, p.name, v.sku, v.item_barcode, v.price, v.attributes, p.attributes, p.variant_dims, v.mrp
	FROM product_variants v JOIN products p ON p.id = v.product_id
	WHERE v.id = ANY($1::bigint[]) AND v.deleted_at IS NULL AND p.deleted_at IS NULL`

func (a *API) pdf(w http.ResponseWriter, r *http.Request) {
	var in pdfIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	total := 0
	var ids []int64
	if !in.Test {
		if len(in.Items) == 0 {
			f["items"] = "choose at least one variant"
		}
		if len(in.Items) > MaxVariants {
			f["items"] = fmt.Sprintf("at most %d variants at a time", MaxVariants)
		}
		for i, it := range in.Items {
			if it.VariantID <= 0 {
				f[fmt.Sprintf("items[%d].variantId", i)] = "variantId is required"
			}
			if it.Copies < 0 || it.Copies > MaxCopies {
				f[fmt.Sprintf("items[%d].copies", i)] = fmt.Sprintf("copies must be between 0 and %d", MaxCopies)
			}
			total += max(it.Copies, 0)
			ids = append(ids, it.VariantID)
		}
		if len(f) == 0 && total == 0 {
			f["items"] = "nothing to print: every line has 0 copies"
		}
		if total > MaxLabels {
			f["items"] = fmt.Sprintf("%d labels asked; at most %d per PDF", total, MaxLabels)
		}
	}
	if len(f) > 0 {
		httpx.WriteError(w, r, apperr.Fields(f))
		return
	}
	var pdf []byte
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		lay, ok, err := findLayout(r.Context(), tx, in.LayoutID)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Fields(map[string]string{"layoutId": "unknown layout"})
		}
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		fm := receipts.NewFormatter(b.Locale, b.Currency, b.Location())
		var items []Label
		if in.Test {
			for i := range lay.PerPage() {
				code, _ := InternalCode(int64(i + 1))
				items = append(items, Label{Name: "Test label " + strconv.Itoa(i+1), Variant: "M / Blue",
					Price: fm.Money(decimal.NewFromInt(499)), MRP: "MRP " + fm.Money(decimal.NewFromInt(550)),
					SKU: "TEST-" + strconv.Itoa(i+1), Code: code})
			}
		} else {
			byID, err := labelData(r.Context(), tx, ids, fm)
			if err != nil {
				return err
			}
			batchText, err := labelBatches(r.Context(), tx, in.Items, fm)
			if err != nil {
				return err
			}
			for _, it := range in.Items {
				l, ok := byID[it.VariantID]
				if !ok {
					return apperr.NotFound()
				}
				if it.BatchID > 0 {
					bt := batchText[it.BatchID]
					l.Batch = bt.text
					if bt.mrp != "" { // the batch's MRP wins over the variant's
						l.MRP = bt.mrp
					}
				}
				for range it.Copies {
					items = append(items, l)
				}
			}
		}
		pdf, err = Render(lay, items)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="labels.pdf"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

// batchLabel is a batch's label text and MRP ("MRP ₹45.00"; empty: none).
type batchLabel struct{ text, mrp string }

// labelBatches returns "B <no> EXP <MM/YYYY>" and the MRP for the items'
// batches; a batch of another variant is not found, and batches need the
// module.
func labelBatches(ctx context.Context, tx tenancy.TenantTx, items []pdfItem, fm receipts.Formatter) (map[int64]batchLabel, error) {
	var ids []int64
	for _, it := range items {
		if it.BatchID > 0 {
			ids = append(ids, it.BatchID)
		}
	}
	out := map[int64]batchLabel{}
	if len(ids) == 0 {
		return out, nil
	}
	var on bool
	if err := tx.QueryRow(ctx, `SELECT 'batch_expiry' = ANY(modules) FROM settings WHERE id = 1`).Scan(&on); err != nil {
		return nil, err
	}
	if !on {
		return nil, modules.ErrOff(modules.BatchExpiry)
	}
	rows, err := tx.Query(ctx, `SELECT id, variant_id, batch_no::text, coalesce(to_char(expiry_date, 'MM/YYYY'), ''), mrp
		FROM batches WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	variant := map[int64]int64{}
	for rows.Next() {
		var id, vid int64
		var no, exp string
		var mrp *decimal.Decimal
		if err := rows.Scan(&id, &vid, &no, &exp, &mrp); err != nil {
			return nil, err
		}
		variant[id] = vid
		b := batchLabel{text: "B " + no}
		if exp != "" {
			b.text += " EXP " + exp
		}
		if mrp != nil {
			b.mrp = "MRP " + fm.Money(*mrp)
		}
		out[id] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.BatchID > 0 && variant[it.BatchID] != it.VariantID {
			return nil, apperr.NotFound()
		}
	}
	return out, nil
}

// labelData loads the label text for live variants.
func labelData(ctx context.Context, tx tenancy.TenantTx, ids []int64, fm receipts.Formatter) (map[int64]Label, error) {
	rows, err := tx.Query(ctx, qLabelVariants, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Label{}
	for rows.Next() {
		var (
			id          int64
			name, sku   string
			code        *string
			price       decimal.Decimal
			vAttrs      map[string]any
			pAttrs      map[string]string
			variantDims []string
			mrp         *decimal.Decimal
		)
		if err := rows.Scan(&id, &name, &sku, &code, &price, &vAttrs, &pAttrs, &variantDims, &mrp); err != nil {
			return nil, err
		}
		l := Label{Name: name, SKU: sku, Price: fm.Money(price), Code: sku}
		if mrp != nil {
			l.MRP = "MRP " + fm.Money(*mrp)
		}
		if code != nil && *code != "" {
			l.Code = *code
		}
		l.Variant = attributes.Label(variantDims, pAttrs, vAttrs)
		if l.Variant == "" {
			l.Variant = plainVariant(vAttrs)
		}
		out[id] = l
	}
	return out, rows.Err()
}

// plainVariant is the option text for products without variant
// dimensions, as the product page shows it ("colour: Red, size: L").
func plainVariant(attrs map[string]any) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if attrs[k] == nil {
			continue
		}
		if v := fmt.Sprint(attrs[k]); v != "" {
			parts = append(parts, k+": "+v)
		}
	}
	return strings.Join(parts, ", ")
}
