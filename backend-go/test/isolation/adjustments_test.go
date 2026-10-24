//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func postAdjustment(t *testing.T, l, tok string, body map[string]any, hdr map[string]string) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/stock-adjustments", token: tok, body: body, header: hdr})
}

func adjust(variant, store float64, typ string, qty any) map[string]any {
	return map[string]any{"variantId": variant, "storeId": store, "type": typ, "quantity": qty, "reason": "Stock count", "note": "shelf 4"}
}

func TestStockAdjustmentsAtomicWithLedger(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "ADJ-1", store, 5)

	// A subtraction: record, stock and ledger together.
	s := postAdjustment(t, "a", tok, adjust(p.vid, store, "Subtraction", 2), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, 3.0, num(s.Body["qtyAfter"]))
	require.Equal(t, "Stock count", s.Body["reason"])
	require.Equal(t, p.pid, s.Body["product_id"])
	require.Equal(t, 3, stockOf(t, "a", store, p.vid))
	var reason string
	var delta int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT reason, delta FROM `+schema("a")+`.stock_movements
		WHERE ref_type = 'stock_adjustment' AND ref_id = $1`, int64(num(s.Body["id"]))).Scan(&reason, &delta))
	require.Equal(t, "adjustment", reason)
	require.Equal(t, -2, delta)
	var audited int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.audit_log
		WHERE action = 'stock.adjustment' AND entity_id = $1`, fmt.Sprint(s.Body["id"])).Scan(&audited))
	require.Equal(t, 1, audited)

	// Never below zero: nothing is recorded when it would be.
	countFor := func(vid float64) int {
		var n int
		require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.stock_adjustments WHERE variant_id = $1`, int64(vid)).Scan(&n))
		return n
	}
	short := postAdjustment(t, "a", tok, adjust(p.vid, store, "Subtraction", 4), nil)
	require.Equal(t, 409, short.Code, short.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", short.Body["code"])
	d := short.Body["details"].(map[string]any)["items"].([]any)[0].(map[string]any)
	require.Equal(t, []float64{3, 4}, []float64{num(d["available"]), num(d["requested"])})
	require.Equal(t, "ADJ-1", d["sku"])
	require.Equal(t, 3, stockOf(t, "a", store, p.vid))
	require.Equal(t, 1, countFor(p.vid))

	// An addition with string IDs (form data) and the default reason.
	add := postAdjustment(t, "a", tok, map[string]any{"variantId": jsonID(p.vid), "storeId": jsonID(store), "type": "Addition", "quantity": "10"}, nil)
	require.Equal(t, 201, add.Code, add.Raw)
	require.Equal(t, 13.0, num(add.Body["qtyAfter"]))
	require.Equal(t, "Manual Adjustment", add.Body["reason"])
	require.Equal(t, 13, stockOf(t, "a", store, p.vid))

	// A variant with no stock row in the store: addition creates it,
	// subtraction finds nothing to take.
	vp := createProduct(t, "a", tok, map[string]any{"name": "Socks", "productType": "variable", "variants": []map[string]any{
		{"sku": "SOCK-S", "price": "50"}, {"sku": "SOCK-M", "price": "50"}}})
	require.Equal(t, 201, vp.Code, vp.Raw)
	sock := vp.Body["ProductVariants"].([]any)[1].(map[string]any)["id"].(float64)
	require.Equal(t, 409, postAdjustment(t, "a", tok, adjust(sock, store, "Subtraction", 1), nil).Code)
	require.Equal(t, 201, postAdjustment(t, "a", tok, adjust(sock, store, "Addition", 2), nil).Code)
	require.Equal(t, 2, stockOf(t, "a", store, sock))

	bad := map[string]struct {
		body  map[string]any
		field string
	}{
		"unknown type":  {adjust(p.vid, store, "Remove", 1), "type"},
		"zero quantity": {adjust(p.vid, store, "Addition", 0), "quantity"},
		"negative":      {adjust(p.vid, store, "Addition", -3), "quantity"},
		"no variant":    {map[string]any{"storeId": store, "type": "Addition", "quantity": 1}, "variantId"},
		"unknown":       {adjust(99999999, store, "Addition", 1), "variantId"},
		"other product": {func() map[string]any {
			b := adjust(p.vid, store, "Addition", 1)
			b["productId"] = vp.Body["id"]
			return b
		}(), "variantId"},
		"unknown store": {adjust(p.vid, 99999999, "Addition", 1), "storeId"},
		"control chars": {func() map[string]any { b := adjust(p.vid, store, "Addition", 1); b["reason"] = "a\r\nb"; return b }(), "reason"},
		"long reason": {func() map[string]any {
			b := adjust(p.vid, store, "Addition", 1)
			b["reason"] = strings.Repeat("x", 201)
			return b
		}(), "reason"},
		"no store field": {map[string]any{"variantId": p.vid, "type": "Addition", "quantity": 1}, "storeId"},
	}
	for name, c := range bad {
		r := postAdjustment(t, "a", tok, c.body, nil)
		require.Equal(t, 400, r.Code, "%s: %s", name, r.Raw)
		require.Equal(t, c.field, errField(r), "%s: %s", name, r.Raw)
	}
	require.Equal(t, 400, postAdjustment(t, "a", tok, adjust(p.vid, store, "Addition", 1.5), nil).Code, "fractional quantity")
	require.Equal(t, 13, stockOf(t, "a", store, p.vid))

	// Idempotency-Key: a double click adjusts once.
	hdr := map[string]string{"Idempotency-Key": "adj-idem-0001"}
	first := postAdjustment(t, "a", tok, adjust(p.vid, store, "Subtraction", 1), hdr)
	require.Equal(t, 201, first.Code)
	again := postAdjustment(t, "a", tok, adjust(p.vid, store, "Subtraction", 1), hdr)
	require.Equal(t, "true", again.Header.Get("Idempotent-Replayed"))
	require.Equal(t, first.Body["id"], again.Body["id"])
	require.Equal(t, 12, stockOf(t, "a", store, p.vid))

	// The list keeps the old nested shape and is filterable and pageable.
	l := get(t, host, fmt.Sprintf("/api/stock-adjustments?variantId=%v", jsonID(p.vid)), tok)
	require.Equal(t, 200, l.Code, l.Raw)
	rows := list(t, l)
	require.Len(t, rows, 3)
	row := rows[0]
	require.Equal(t, first.Body["id"], row["id"], "newest first")
	require.NotEmpty(t, row["Store"].(map[string]any)["name"])
	require.NotEmpty(t, row["creator"].(map[string]any)["name"])
	require.Equal(t, "ADJ-1", row["Variant"].(map[string]any)["sku"])
	require.Equal(t, "Product ADJ-1", row["Variant"].(map[string]any)["Product"].(map[string]any)["name"])
	paged := get(t, host, fmt.Sprintf("/api/stock-adjustments?variantId=%v&page=1&limit=2", jsonID(p.vid)), tok)
	require.Equal(t, 3.0, num(paged.Body["pagination"].(map[string]any)["total"]))
	require.Len(t, paged.Body["data"].([]any), 2)

	// Append-only, permission-gated.
	var canUpdate, canDelete bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT has_table_privilege($1, $2, 'UPDATE'), has_table_privilege($1, $2, 'DELETE')`,
		schema("a")+"_rw", schema("a")+".stock_adjustments").Scan(&canUpdate, &canDelete))
	require.False(t, canUpdate)
	require.False(t, canDelete)
	createUser(t, "a", tok, "adjust-biller@iso-a.test", "biller", "adjust-biller-pass-1")
	btok, _ := login(t, "a", "adjust-biller@iso-a.test", "adjust-biller-pass-1")
	require.Equal(t, 403, get(t, host, "/api/stock-adjustments", btok).Code)
	require.Equal(t, 403, postAdjustment(t, "a", btok, adjust(p.vid, store, "Addition", 1), nil).Code)
	require.Empty(t, reconcile(t, "a"))
}

func TestConcurrentSubtractionsNeverGoNegative(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "ADJ-RACE", store, 5)

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	var after []int
	for range 12 {
		wg.Go(func() {
			r := postAdjustment(t, "a", tok, adjust(p.vid, store, "Subtraction", 1), nil)
			mu.Lock()
			defer mu.Unlock()
			codes[r.Code]++
			if r.Code == 201 {
				after = append(after, int(num(r.Body["qtyAfter"])))
			}
		})
	}
	wg.Wait()
	require.Equal(t, map[int]int{201: 5, 409: 7}, codes)
	require.Equal(t, 0, stockOf(t, "a", store, p.vid))
	sort.Ints(after)
	require.Equal(t, []int{0, 1, 2, 3, 4}, after, "each adjustment records the stock it left")
	require.Empty(t, reconcile(t, "a"))
}

func TestStockAdjustmentsIsolationAcrossTenants(t *testing.T) {
	ctx := context.Background()
	adm := admin(t)
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	ch := tenants["c"].Host
	// A's next variant ID does not exist in C.
	_, err := adm.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1, 'id'), GREATEST((SELECT coalesce(max(id), 0) FROM `+
		schema("a")+`.product_variants), 900000))`, schema("a")+".product_variants")
	require.NoError(t, err)
	aStore := storeIDs(t, "a", aTok)[0]
	p := stockedProduct(t, "a", aTok, "ADJ-ISO-A", aStore, 4)
	var inC int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+schema("c")+`.product_variants WHERE id = $1`, int64(p.vid)).Scan(&inC))
	require.Zero(t, inC)
	a := postAdjustment(t, "a", aTok, map[string]any{"variantId": p.vid, "storeId": aStore, "type": "Subtraction", "quantity": 1,
		"note": "iso-a-secret-note"}, nil)
	require.Equal(t, 201, a.Code, a.Raw)

	// A's variant means nothing on C, and A's stock is untouched.
	cStore := storeIDs(t, "c", cTok)[0]
	r := postAdjustment(t, "c", cTok, adjust(p.vid, cStore, "Subtraction", 1), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "variantId", errField(r))
	require.Equal(t, 400, postAdjustment(t, "c", cTok, adjust(p.vid, cStore, "Addition", 5), nil).Code)
	require.Equal(t, 3, stockOf(t, "a", aStore, p.vid))

	// C's list never shows A's adjustments; A's token is refused on C.
	l := get(t, ch, "/api/stock-adjustments", cTok)
	require.Equal(t, 200, l.Code)
	require.NotContains(t, l.Raw, "iso-a-secret-note")
	require.NotContains(t, l.Raw, "ADJ-ISO-A")
	require.Empty(t, list(t, get(t, ch, fmt.Sprintf("/api/stock-adjustments?variantId=%v", jsonID(p.vid)), cTok)))
	require.Equal(t, 401, postAdjustment(t, "c", aTok, adjust(p.vid, aStore, "Addition", 1), nil).Code)
	require.Equal(t, 401, get(t, ch, "/api/stock-adjustments", aTok).Code)
	require.Equal(t, 3, stockOf(t, "a", aStore, p.vid))
}
