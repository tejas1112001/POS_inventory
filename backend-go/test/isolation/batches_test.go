//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 1: batches and expiry (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md §1).

// twoStoreProduct creates a single product with stock in two stores.
func twoStoreProduct(t *testing.T, l, tok, sku string, s1, s2 float64, q1, q2 int) stocked {
	t.Helper()
	body := addFormSingle(sku, s1, q1)
	inv := []map[string]any{{"storeId": fmt.Sprint(s1), "qty": q1, "quantityAlert": 2}}
	if s2 > 0 {
		inv = append(inv, map[string]any{"storeId": fmt.Sprint(s2), "qty": q2, "quantityAlert": 2})
	}
	body["variants"].([]map[string]any)[0]["inventories"] = inv
	p := createProduct(t, l, tok, body)
	require.Equal(t, 201, p.Code, p.Raw)
	v := p.Body["ProductVariants"].([]any)[0].(map[string]any)
	return stocked{pid: p.Body["id"].(float64), vid: v["id"].(float64), sku: sku}
}

// setTracking switches a product's batch tracking with the product form.
func setTracking(t *testing.T, l, tok string, p stocked, on any) resp {
	t.Helper()
	body := map[string]any{"name": "Product " + p.sku, "productType": "single",
		"variants": []map[string]any{{"id": p.vid, "sku": p.sku, "price": 120.5}}}
	if on != nil {
		body["tracksBatches"] = on
	}
	return do(t, rq{method: "PUT", host: tenants[l].Host, path: fmt.Sprintf("/api/products/%v", p.pid), token: tok, body: body})
}

func batchList(t *testing.T, l, tok, query string) []map[string]any {
	t.Helper()
	r := get(t, tenants[l].Host, "/api/batches"+query, tok)
	require.Equal(t, 200, r.Code, r.Raw)
	out := []map[string]any{}
	for _, x := range r.Body["items"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// batchQty is a batch's stock in a store (0 when it has none there).
func batchQty(t *testing.T, l string, batchID int64, store float64) int {
	t.Helper()
	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT coalesce((SELECT qty FROM `+schema(l)+`.batch_stock
		WHERE batch_id = $1 AND store_id = $2), 0)`, batchID, int64(store)).Scan(&n))
	return n
}

func unknownBatch(t *testing.T, l string, vid float64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id FROM `+schema(l)+`.batches WHERE variant_id = $1 AND is_unknown`,
		int64(vid)).Scan(&id))
	return id
}

// moveToBatch stands in for item 2/6 (receiving, counting by batch): it
// moves qty units of a variant in a store from its unknown batch into a new
// dated batch, keeping both ledgers right.
func moveToBatch(t *testing.T, l string, vid, store float64, batchNo string, expiry time.Time, qty int) int64 {
	t.Helper()
	ctx := context.Background()
	s := schema(l)
	tx, err := admin(t).Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	require.NoError(t, tx.QueryRow(ctx, `INSERT INTO `+s+`.batches (variant_id, batch_no, expiry_date) VALUES ($1, $2, $3) RETURNING id`,
		int64(vid), batchNo, expiry).Scan(&id))
	unk := unknownBatch(t, l, vid)
	for _, step := range []struct {
		b int64
		d int
	}{{unk, -qty}, {id, qty}} {
		var after int
		q := `UPDATE ` + s + `.batch_stock SET qty = qty + $3 WHERE store_id = $1 AND batch_id = $2 RETURNING qty`
		if step.d > 0 {
			q = `INSERT INTO ` + s + `.batch_stock (store_id, batch_id, qty) VALUES ($1, $2, $3) RETURNING qty`
		}
		require.NoError(t, tx.QueryRow(ctx, q, int64(store), step.b, step.d).Scan(&after))
		_, err = tx.Exec(ctx, `INSERT INTO `+s+`.batch_movements (store_id, batch_id, delta, qty_after, reason, ref_type)
			VALUES ($1, $2, $3, $4, 'stock_count', 'test')`, int64(store), step.b, step.d, after)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(ctx))
	return id
}

// batchReport prints, per tracked product and store, the stock against the
// batch sum and the split (the copy-run report, design §10).
func batchReport(t *testing.T, l, stage string) (stock, inBatches int) {
	t.Helper()
	rows, err := admin(t).Query(context.Background(), `SELECT p.name, st.name, i.qty,
			coalesce((SELECT sum(s.qty) FROM `+schema(l)+`.batch_stock s JOIN `+schema(l)+`.batches b ON b.id = s.batch_id
			          WHERE s.store_id = i.store_id AND b.variant_id = i.variant_id), 0)::int,
			coalesce((SELECT sum(s.qty) FROM `+schema(l)+`.batch_stock s JOIN `+schema(l)+`.batches b ON b.id = s.batch_id
			          WHERE s.store_id = i.store_id AND b.variant_id = i.variant_id AND b.is_unknown), 0)::int,
			(SELECT count(*) FROM `+schema(l)+`.batch_stock s JOIN `+schema(l)+`.batches b ON b.id = s.batch_id
			          WHERE s.store_id = i.store_id AND b.variant_id = i.variant_id AND s.qty > 0 AND NOT b.is_unknown)
		FROM `+schema(l)+`.inventories i
		JOIN `+schema(l)+`.product_variants v ON v.id = i.variant_id
		JOIN `+schema(l)+`.products p ON p.id = v.product_id AND p.tracks_batches
		JOIN `+schema(l)+`.stores st ON st.id = i.store_id
		ORDER BY p.name, st.name`)
	require.NoError(t, err)
	defer rows.Close()
	t.Logf("REPORT %s | product | store | stock | batch sum | unknown batch | dated batches with stock", stage)
	for rows.Next() {
		var p, s string
		var q, sum, unk, dated int
		require.NoError(t, rows.Scan(&p, &s, &q, &sum, &unk, &dated))
		t.Logf("REPORT %s | %s | %s | %d | %d | %d | %d", stage, p, s, q, sum, unk, dated)
		stock += q
		inBatches += sum
	}
	require.NoError(t, rows.Err())
	t.Logf("REPORT %s | TOTAL | | %d | %d | | | mismatches: %d", stage, stock, inBatches, len(reconcile(t, l)))
	return stock, inBatches
}

func TestBatchTrackingSwitchAndReconcile(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "bt-switch")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	s1 := storeIDs(t, l, tok)[0]
	st := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Second branch", "status": "Active"}})
	require.Equal(t, 201, st.Code, st.Raw)
	s2 := st.Body["id"].(float64)

	// Before the module: plain stock, sold as today.
	a := twoStoreProduct(t, l, tok, "BT-PARA", s1, s2, 10, 4)
	b := twoStoreProduct(t, l, tok, "BT-SOAP", s1, 0, 6, 0)
	require.Equal(t, 201, sell(t, l, tok, posSale(s1, line(a, 2)), nil).Code)
	require.Equal(t, 403, get(t, host, "/api/batches", tok).Code, "module off")
	require.Equal(t, 403, setTracking(t, l, tok, a, true).Code, "tracksBatches with the module off")
	require.Equal(t, 200, setTracking(t, l, tok, a, nil).Code, "the product form without the field still works")

	// Module on (pharmacy preset): existing products stay untracked.
	setModules(t, l, "pharmacy")
	require.Empty(t, batchList(t, l, tok, ""))
	pr := get(t, host, fmt.Sprintf("/api/products/%v", a.pid), tok)
	require.Equal(t, false, pr.Body["tracksBatches"])
	require.Nil(t, pr.Body["batchStock"])

	// Switch on: today's stock goes into the unknown batch, store by store.
	r := setTracking(t, l, tok, a, true)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, true, r.Body["tracksBatches"])
	require.Equal(t, 12.0, r.Body["batchStock"].(map[string]any)["unbatched"])
	unk := unknownBatch(t, l, a.vid)
	require.Equal(t, 8, batchQty(t, l, unk, s1))
	require.Equal(t, 4, batchQty(t, l, unk, s2))
	require.Empty(t, reconcile(t, l))
	stock, inB := batchReport(t, l, "after switch-on")
	require.Equal(t, stock, inB)

	// A new product in the shop tracks batches by default; its opening
	// stock is unbatched.
	c := stockedProduct(t, l, tok, "BT-NEW", s1, 7)
	pc := get(t, host, fmt.Sprintf("/api/products/%v", c.pid), tok)
	require.Equal(t, true, pc.Body["tracksBatches"])
	require.Equal(t, 7, batchQty(t, l, unknownBatch(t, l, c.vid), s1))

	// Dated batches (stand-in for receiving): sales take the earliest
	// expiry first, the unknown batch last; increases go to the unknown batch.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	far := moveToBatch(t, l, a.vid, s1, "B-FAR", today.AddDate(0, 0, 200), 3)
	near := moveToBatch(t, l, a.vid, s1, "B-NEAR", today.AddDate(0, 0, 30), 4)
	require.Equal(t, 1, batchQty(t, l, unk, s1))
	require.Equal(t, 201, sell(t, l, tok, posSale(s1, line(a, 5)), nil).Code)
	require.Equal(t, 0, batchQty(t, l, near, s1), "earliest expiry first")
	require.Equal(t, 2, batchQty(t, l, far, s1))
	require.Equal(t, 1, batchQty(t, l, unk, s1), "unknown batch last")
	require.Equal(t, 201, postAdjustment(t, l, tok, adjust(a.vid, s1, "Subtraction", 3), nil).Code)
	require.Equal(t, 0, batchQty(t, l, far, s1))
	require.Equal(t, 0, batchQty(t, l, unk, s1))
	require.Equal(t, 201, postAdjustment(t, l, tok, adjust(a.vid, s1, "Addition", 5), nil).Code)
	require.Equal(t, 5, batchQty(t, l, unk, s1))
	require.Equal(t, 201, sell(t, l, tok, posSale(s2, line(a, 1)), nil).Code)
	require.Equal(t, 3, batchQty(t, l, unk, s2))
	require.Empty(t, reconcile(t, l))

	// Every sold unit has one batch movement with the sale's reference.
	var sold int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT coalesce(-sum(delta), 0) FROM `+schema(l)+`.batch_movements
		WHERE reason = 'sale' AND ref_type = 'sale'`).Scan(&sold))
	require.Equal(t, 6, sold)

	// Product view and filters.
	pr = get(t, host, fmt.Sprintf("/api/products/%v", a.pid), tok)
	bs := pr.Body["batchStock"].(map[string]any)
	require.Equal(t, 8.0, bs["unbatched"], "every store: 5 + 3")
	require.Equal(t, 8.0, bs["sellable"], "the unknown batch sells (with a warning)")
	require.Equal(t, 0.0, bs["expired"])
	exp := moveToBatch(t, l, a.vid, s1, "B-OLD", today.AddDate(0, 0, -1), 1)
	expired := batchList(t, l, tok, "?status=expired")
	require.Len(t, expired, 1)
	require.Equal(t, float64(exp), expired[0]["id"])
	require.Equal(t, true, expired[0]["expired"])
	require.Len(t, batchList(t, l, tok, fmt.Sprintf("?productId=%v&status=unknown", a.pid)), 1)
	require.Len(t, batchList(t, l, tok, "?expiringWithinDays=60"), 1, "only batches with stock unless includeEmpty")
	require.Len(t, batchList(t, l, tok, "?expiringWithinDays=60&includeEmpty=true"), 2)
	require.Equal(t, 400, get(t, host, "/api/batches?status=old", tok).Code)
	d := get(t, host, fmt.Sprintf("/api/batches/%d", unk), tok)
	require.Equal(t, 200, d.Code, d.Raw)
	require.Equal(t, "UNBATCHED", d.Body["batchNo"])
	require.Equal(t, 7.0, d.Body["totalQty"], "4 in the first store (1 moved to B-OLD), 3 in the second")
	require.NotEmpty(t, d.Body["movements"])

	// A24: correct a batch's details, with a reason.
	put := func(id int64, body map[string]any) resp {
		return do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/batches/%d", id), token: tok, body: body})
	}
	e := put(far, map[string]any{"expiry": "03/2031", "mrp": 45.5, "reason": "typo on receiving"})
	require.Equal(t, 200, e.Code, e.Raw)
	require.Equal(t, "2031-03-31", e.Body["expiry"])
	require.Equal(t, "45.5", fmt.Sprint(e.Body["mrp"]))
	require.Equal(t, 400, put(far, map[string]any{"expiry": "2031-03-31", "reason": "no"}).Code)
	require.Equal(t, 400, put(far, map[string]any{"batchNo": "unbatched", "reason": "rename it"}).Code)
	require.Equal(t, 400, put(far, map[string]any{"expiry": today.AddDate(12, 0, 0).Format("2006-01-02"), "reason": "far ahead"}).Code)
	fe := put(far, map[string]any{"expiry": today.AddDate(12, 0, 0).Format("2006-01-02"), "reason": "far ahead", "confirmFarExpiry": true})
	require.Equal(t, 200, fe.Code, fe.Raw)
	require.Equal(t, 200, put(far, map[string]any{"expiry": "03/2031", "reason": "back again"}).Code)
	require.Equal(t, 400, put(far, map[string]any{"mfgDate": "2032-01-01", "reason": "after expiry"}).Code)
	require.Equal(t, 409, put(far, map[string]any{"batchNo": "B-NEAR", "reason": "duplicate"}).Code)
	require.Equal(t, 409, put(unk, map[string]any{"expiry": "2031-03-31", "reason": "unknown has none"}).Code)
	require.Equal(t, 404, put(999999, map[string]any{"expiry": "2031-03-31", "reason": "missing one"}).Code)
	var edits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'batch.edit'`).Scan(&edits))
	require.Equal(t, 3, edits)

	// Permissions: billers see batches (P7) but cannot correct them or
	// change tracking (P8).
	createUser(t, l, tok, "biller@bt-switch.test", "biller", "biller-pass-12345")
	btok, _ := login(t, l, "biller@bt-switch.test", "biller-pass-12345")
	require.Equal(t, 200, get(t, host, "/api/batches", btok).Code)
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/batches/%d", far), token: btok,
		body: map[string]any{"expiry": "2031-03-31", "reason": "biller edit"}}).Code)
	editor := userWithPerms(t, l, tok, "editor@bt-switch.test", "products:view", "products:edit")
	require.Equal(t, 403, setTracking(t, l, editor, a, false).Code, "tracking needs batches:manage")
	require.Equal(t, 200, setTracking(t, l, editor, a, true).Code, "sending the current value is no change")
	require.Equal(t, 200, setTracking(t, l, editor, a, nil).Code)

	// Switch off: batch stock closes, history stays; on again: unbatched.
	r = setTracking(t, l, tok, a, false)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Nil(t, r.Body["batchStock"])
	require.Equal(t, 0, batchQty(t, l, unk, s1))
	require.Empty(t, batchList(t, l, tok, fmt.Sprintf("?productId=%v", a.pid)))
	require.Len(t, batchList(t, l, tok, fmt.Sprintf("?productId=%v&includeEmpty=true", a.pid)), 4)
	require.Equal(t, 201, sell(t, l, tok, posSale(s1, line(a, 1)), nil).Code, "untracked: sold as today")
	require.Equal(t, 200, setTracking(t, l, tok, a, true).Code)
	require.Equal(t, 4, batchQty(t, l, unk, s1), "5 - 1 sold while untracked, all unbatched again")
	require.Empty(t, reconcile(t, l))

	// Units in quarantine stop a switch-off (they would be lost).
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batch_stock SET quarantined_qty = 1 WHERE batch_id = $1 AND store_id = $2`, unk, int64(s1))
	require.NoError(t, err)
	q := setTracking(t, l, tok, a, false)
	require.Equal(t, 409, q.Code, q.Raw)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batch_stock SET quarantined_qty = 0 WHERE batch_id = $1 AND store_id = $2`, unk, int64(s1))
	require.NoError(t, err)

	// Module off: every tracked product closes, sales go on as today, the
	// batch routes and the field are refused; on again: unbatched.
	r = putBusiness(t, l, tok, map[string]any{"modules": []string{"barcode_labels", "stock_counts"}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, 0, batchQty(t, l, unk, s1))
	require.Equal(t, 403, get(t, host, "/api/batches", tok).Code)
	require.Equal(t, 403, setTracking(t, l, tok, b, true).Code)
	require.Equal(t, 201, sell(t, l, tok, posSale(s1, line(c, 2)), nil).Code)
	require.Empty(t, reconcile(t, l))
	r = putBusiness(t, l, tok, map[string]any{"modules": []string{"barcode_labels", "stock_counts", "batch_expiry"}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, 4, batchQty(t, l, unk, s1))
	require.Equal(t, 5, batchQty(t, l, unknownBatch(t, l, c.vid), s1), "7 - 2 sold while off")
	require.Empty(t, reconcile(t, l))
	stock, inB = batchReport(t, l, "after module off and on")
	require.Equal(t, stock, inB)

	// The batch ledger is append-only for the tenant role.
	var canUpdate, canDelete bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT has_table_privilege($1, $2, 'UPDATE'), has_table_privilege($1, $2, 'DELETE')`,
		schema(l)+"_rw", schema(l)+".batch_movements").Scan(&canUpdate, &canDelete))
	require.False(t, canUpdate)
	require.False(t, canDelete)

	// Every switch is audited.
	var switches int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'product.batch_tracking'`).Scan(&switches))
	require.Equal(t, 4, switches, "a on, c on at create, a off, a on")
}

// Many tills selling one tracked product at once: no deadlock, stock never
// below zero, batches always equal the stock.
func TestBatchStockUnderConcurrentSales(t *testing.T) {
	l := billingTenant(t, "bt-race")
	tok, _ := ownerLogin(t, l)
	s1 := storeIDs(t, l, tok)[0]
	setModules(t, l, "pharmacy")
	a := stockedProduct(t, l, tok, "BT-RACE-A", s1, 12)
	b := stockedProduct(t, l, tok, "BT-RACE-B", s1, 12)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	moveToBatch(t, l, a.vid, s1, "RA-1", today.AddDate(0, 0, 40), 5)
	moveToBatch(t, l, a.vid, s1, "RA-2", today.AddDate(0, 0, 90), 5)
	moveToBatch(t, l, b.vid, s1, "RB-1", today.AddDate(0, 0, 40), 6)

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := range 10 {
		wg.Go(func() {
			// Opposite line order on alternate tills.
			lines := []map[string]any{line(a, 2), line(b, 2)}
			if i%2 == 1 {
				lines[0], lines[1] = lines[1], lines[0]
			}
			r := sell(t, l, tok, posSale(s1, lines...), nil)
			mu.Lock()
			codes[r.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	require.Equal(t, 6, codes[201], codes)
	require.Equal(t, 4, codes[409], codes)
	require.Equal(t, 0, stockOf(t, l, s1, a.vid))
	require.Empty(t, reconcile(t, l))
}

func TestBatchesIsolationAcrossTenants(t *testing.T) {
	l := billingTenant(t, "bt-iso")
	tok, _ := ownerLogin(t, l)
	s1 := storeIDs(t, l, tok)[0]
	setModules(t, l, "pharmacy")
	p := stockedProduct(t, l, tok, "BT-ISO", s1, 3)
	id := unknownBatch(t, l, p.vid)

	o := billingTenant(t, "bt-iso-other")
	otok, _ := ownerLogin(t, o)
	setModules(t, o, "pharmacy")
	path := fmt.Sprintf("/api/batches/%d", id)
	require.Equal(t, 404, get(t, tenants[o].Host, path, otok).Code)
	require.Equal(t, 404, do(t, rq{method: "PUT", host: tenants[o].Host, path: path, token: otok,
		body: map[string]any{"expiry": "2030-01-31", "reason": "cross shop"}}).Code)
	require.Empty(t, batchList(t, o, otok, ""))
	require.Equal(t, 401, get(t, tenants[o].Host, path, tok).Code, "a token of another shop")

	// An existing retail shop sees nothing new.
	atok, _ := ownerLogin(t, "a")
	require.Equal(t, 403, get(t, tenants["a"].Host, "/api/batches", atok).Code)
	var names []string
	for _, x := range batchList(t, l, tok, "") {
		names = append(names, x["batchNo"].(string))
	}
	sort.Strings(names)
	require.Equal(t, "UNBATCHED", strings.Join(names, ","))
}
