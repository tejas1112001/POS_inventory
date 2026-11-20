package products

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/batches"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// The pharmacy import template (industry P3 item 8, A35, design §8). A
// file with batch columns is a pharmacy file: one row per batch, and rows
// with the same SKU are batches of one product (a single product: one
// variant). Expired rows are reported as warnings and their stock is not
// imported (Q11); near-expiry rows are imported and listed as warnings.
// Products are created tracking batches. In a shop with the module on,
// products from the other templates track batches too (design §0): their
// opening stock goes to the unknown batch.

const templatePharmacy = "pharmacy"

// pharmacyColumns are the batch columns. mrp is a general column (the
// variant's MRP) that the pharmacy template reads as the batch's.
var pharmacyColumns = []string{"batch_no", "expiry", "mfg_date"}

func isPharmacyColumn(c string) bool {
	for _, p := range pharmacyColumns {
		if p == c {
			return true
		}
	}
	return false
}

const importDateLayout = "2006-01-02"

// excelSerial is a date Excel stored as a day number (1900 date system).
var excelSerial = regexp.MustCompile(`^[0-9]{5}$`)

// importDate reads YYYY-MM-DD, MM/YYYY (an expiry is the month's last day,
// a manufacturing date its first) or an Excel day number.
func importDate(s string, expiry bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	if excelSerial.MatchString(s) {
		n, _ := strconv.Atoi(s)
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n), nil
	}
	t, err := batches.ParseExpiry(s)
	if err == nil && !expiry && strings.Contains(s, "/") {
		t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return t, err
}

// batchFields are a pharmacy row's batch values.
type batchFields struct {
	batchNo string
	expiry  time.Time
	mfg     *time.Time
	mrp     *decimal.Decimal
	expired bool
}

// PreviewBatch is one batch row of a pharmacy import (dry run).
type PreviewBatch struct {
	Row     int    `json:"row"`
	BatchNo string `json:"batchNo"`
	Expiry  string `json:"expiry"`
	Store   string `json:"store,omitempty"`
	Qty     qty.Q  `json:"qty"`
	Skipped bool   `json:"skipped,omitempty"` // expired: its stock is not imported
}

// pharmacyImport is what a file's header and the shop's settings decide.
type pharmacyImport struct {
	file     bool // the file has batch columns
	moduleOn bool
	settings batches.Settings
}

// pharmacyFile checks a pharmacy file can be imported: the module must be
// on, and the file has no article or attribute columns.
func pharmacyFile(ctx context.Context, tx tenancy.TenantTx, header []string, industry bool) (pharmacyImport, []RowError, error) {
	var ph pharmacyImport
	var err error
	if ph.moduleOn, err = inventory.ModuleOn(ctx, tx); err != nil {
		return ph, nil, err
	}
	for _, h := range header {
		if h == "batch_no" || h == "expiry" {
			ph.file = true
		}
	}
	if !ph.file {
		return ph, nil, nil
	}
	if !ph.moduleOn {
		return ph, nil, modules.ErrOff(modules.BatchExpiry)
	}
	if industry {
		return ph, []RowError{{Row: 1, Field: "file", Message: "The pharmacy template has no article_no or attribute columns."}}, nil
	}
	ph.settings, err = batches.LoadSettings(ctx, tx)
	return ph, nil, err
}

// checkBatchRow reads and checks a pharmacy row's batch columns.
func checkBatchRow(ir *importRow, ph pharmacyImport, addErr, warn func(int, string, string)) {
	v, line, bs := ir.vals, ir.line, ph.settings
	no, ok := batches.ValidBatchNo(v["batch_no"])
	switch {
	case !ok:
		addErr(line, "batch_no", "batch_no is required (max 40 characters)")
	case strings.EqualFold(no, inventory.UnknownBatchNo):
		addErr(line, "batch_no", no+" is kept for stock with no batch number")
	}
	ir.b.batchNo = no
	exp, err := importDate(v["expiry"], true)
	switch {
	case strings.TrimSpace(v["expiry"]) == "":
		addErr(line, "expiry", "expiry is required (YYYY-MM-DD or MM/YYYY)")
	case err != nil:
		addErr(line, "expiry", "expiry: "+err.Error())
	case batches.CheckExpiry(exp, bs.Today) != nil:
		addErr(line, "expiry", "expiry "+exp.Format(importDateLayout)+" is more than 10 years ahead; check the date")
	default:
		ir.b.expiry = exp
		switch {
		case bs.Expired(exp):
			ir.b.expired = true
			warn(line, "expiry", "expired on "+exp.Format(importDateLayout)+": its stock is not imported")
		case bs.NearExpiry(exp):
			warn(line, "expiry", fmt.Sprintf("expires on %s, within %d days", exp.Format(importDateLayout), bs.NearExpiryDays))
		}
	}
	if s := strings.TrimSpace(v["mfg_date"]); s != "" {
		m, err := importDate(s, false)
		switch {
		case err != nil:
			addErr(line, "mfg_date", "mfg_date: "+err.Error())
		case m.After(bs.Today):
			addErr(line, "mfg_date", "mfg_date is in the future")
		case !ir.b.expiry.IsZero() && !m.Before(ir.b.expiry):
			addErr(line, "mfg_date", "mfg_date must be before the expiry date")
		default:
			ir.b.mfg = &m
		}
	}
	if s := strings.TrimSpace(v["mrp"]); s != "" {
		d, err := decimal.NewFromString(s)
		if err != nil || d.IsNegative() || d.GreaterThan(maxPrice) {
			addErr(line, "mrp", "must be a number >= 0")
		} else {
			d = d.Round(2)
			ir.b.mrp = &d
		}
	}
}

// sameOnEveryBatch are the product columns that must match on every row of
// one SKU.
var sameOnEveryBatch = []string{"name", "barcode", "category", "sub_category", "brand", "unit", "tax_type", "hsn_code", "status"}

// checkBatchGroup checks one SKU's rows: the same product values, one
// expiry per batch number, and a batch at most once per store.
func checkBatchGroup(g *group, addErr func(int, string, string)) {
	first := g.rows[0]
	differs := func(ir importRow, col string) {
		addErr(ir.line, col, fmt.Sprintf("%s differs from row %d; rows with the same SKU are batches of one product", col, first.line))
	}
	for _, ir := range g.rows[1:] {
		for _, col := range sameOnEveryBatch {
			if !strings.EqualFold(strings.TrimSpace(ir.vals[col]), strings.TrimSpace(first.vals[col])) {
				differs(ir, col)
			}
		}
		if !ir.price.Equal(first.price) {
			differs(ir, "price")
		}
		if !ir.cost.Equal(first.cost) {
			differs(ir, "cost")
		}
		if ir.hasTax != first.hasTax || !ir.taxRate.Equal(first.taxRate) {
			differs(ir, "tax_rate")
		}
	}
	type seen struct {
		line   int
		expiry time.Time
	}
	byNo := map[string]seen{}
	perStore := map[string]int{}
	for _, ir := range g.rows {
		if ir.b.batchNo == "" || ir.b.expiry.IsZero() {
			continue
		}
		k := strings.ToLower(ir.b.batchNo)
		if s, ok := byNo[k]; !ok {
			byNo[k] = seen{ir.line, ir.b.expiry}
		} else if !s.expiry.Equal(ir.b.expiry) {
			addErr(ir.line, "expiry", fmt.Sprintf("batch %s has expiry %s on row %d", ir.b.batchNo, s.expiry.Format(importDateLayout), s.line))
		}
		sk := k + "|" + strconv.FormatInt(ir.store, 10)
		if prev, ok := perStore[sk]; ok {
			addErr(ir.line, "batch_no", fmt.Sprintf("batch %s repeats row %d for the same store", ir.b.batchNo, prev))
		} else {
			perStore[sk] = ir.line
		}
	}
}

// batchStores is a pharmacy product's stores: inventory rows created empty
// with the first low-stock level given (the batches bring the stock), and
// the preview's stock per store (expired rows left out).
func batchStores(g *group, storeNames map[int64]string) ([]cleanInventory, []PreviewStock) {
	var inv []cleanInventory
	have := map[int64]qty.Q{}
	for _, ir := range g.rows {
		if ir.store == 0 {
			continue
		}
		if _, ok := have[ir.store]; !ok {
			inv = append(inv, cleanInventory{storeID: ir.store, alert: ir.alert})
			have[ir.store] = qty.Zero
		}
		if !ir.b.expired {
			have[ir.store] = have[ir.store].Add(ir.qty)
		}
	}
	stock := []PreviewStock{}
	for _, c := range inv {
		stock = append(stock, PreviewStock{Store: storeNames[c.storeID], Qty: have[c.storeID]})
	}
	return inv, stock
}

// importBatches turns tracking on for a new pharmacy product and brings in
// its batches' stock (ledger reason import, on both ledgers).
func importBatches(ctx context.Context, tx tenancy.TenantTx, productID int64, g *group, uid int64, storeNames map[int64]string) ([]PreviewBatch, error) {
	var vid int64
	if err := tx.QueryRow(ctx, `SELECT id FROM product_variants WHERE product_id = $1 AND deleted_at IS NULL`, productID).Scan(&vid); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE products SET tracks_batches = true, updated_at = now() WHERE id = $1`, productID); err != nil {
		return nil, err
	}
	deltas := map[inventory.Key]qty.Q{}
	alloc := inventory.Allocation{}
	out := []PreviewBatch{}
	for _, ir := range g.rows {
		out = append(out, PreviewBatch{Row: ir.line, BatchNo: ir.b.batchNo, Expiry: ir.b.expiry.Format(importDateLayout),
			Store: storeNames[ir.store], Qty: ir.qty, Skipped: ir.b.expired})
		if ir.b.expired {
			continue
		}
		a := batches.Arrival{BatchNo: ir.b.batchNo, Expiry: ir.b.expiry, Mfg: ir.b.mfg, MRP: ir.b.mrp}
		if strings.TrimSpace(ir.vals["cost"]) != "" {
			c := ir.cost.Round(2)
			a.Cost = &c
		}
		id, _, err := batches.Resolve(ctx, tx, vid, a, uid)
		if err != nil {
			return nil, err
		}
		if ir.qty.IsPositive() && ir.store > 0 {
			k := inventory.Key{Store: ir.store, Variant: vid}
			deltas[k] = deltas[k].Add(ir.qty)
			alloc[k] = append(alloc[k], inventory.BatchQty{BatchID: id, Qty: ir.qty})
		}
	}
	if len(deltas) == 0 {
		return out, nil
	}
	keys, _, err := inventory.Lock(ctx, tx, deltas, map[int64]string{vid: g.rows[0].vals["sku"]})
	if err != nil {
		return nil, err
	}
	return out, inventory.ApplyBatched(ctx, tx, keys, deltas, alloc, inventory.ReasonImport, inventory.Ref{Type: "product", ID: productID}, uid)
}

// startImportTracking turns tracking on for products imported from another
// template while the module is on; their opening stock moves to the
// unknown batch.
func startImportTracking(ctx context.Context, tx tenancy.TenantTx, ids []int64, uid int64) error {
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if _, err := tx.Exec(ctx, `UPDATE products SET tracks_batches = true, updated_at = now() WHERE id = ANY($1)`, ids); err != nil {
		return err
	}
	return inventory.StartTracking(ctx, tx, ids, uid)
}
