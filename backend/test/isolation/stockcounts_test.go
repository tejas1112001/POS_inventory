//go:build integration

package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func uploadCountFile(t *testing.T, l, tok string, id any, name string, data []byte) resp {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/stock-counts/%v/import", baseURL, id), &body)
	req.Host = tenants[l].Host
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Forwarded-For", "10.201.0.3")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	out := resp{Code: res.StatusCode, Raw: string(b), Header: res.Header}
	_ = json.Unmarshal(b, &out.Body)
	return out
}

func lineBy(t *testing.T, c resp, sku string) map[string]any {
	t.Helper()
	for _, l := range c.Body["lines"].([]any) {
		if m := l.(map[string]any); m["sku"] == sku {
			return m
		}
	}
	t.Fatalf("no line %s in %s", sku, c.Raw)
	return nil
}

// Industry P1 item 7: stock counts (A15): drafts by scan, typing and
// import; the review (live difference, sold since counted, uncounted);
// completion through the ledger; parallel drafts and completions;
// permissions (P4); MODULE_OFF; isolation.
func TestStockCounts(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "cnt-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	stores := storeIDs(t, l, tok)
	store := stores[0]
	post := func(tk, path string, body any) resp {
		return do(t, rq{method: "POST", host: host, path: path, token: tk, body: body})
	}
	put := func(tk, path string, body any) resp {
		return do(t, rq{method: "PUT", host: host, path: path, token: tk, body: body})
	}

	// A retail shop: the module is off. Counts can be read (none yet) but
	// not started.
	require.Equal(t, 200, get(t, host, "/api/stock-counts", tok).Code)
	off := post(tok, "/api/stock-counts", map[string]any{"storeId": store})
	require.Equal(t, 403, off.Code)
	require.Equal(t, "MODULE_OFF", codeOf(off))
	setModules(t, l, "clothing")

	brand := post(tok, "/api/brands", map[string]any{"name": "Cnt brand"})
	require.Equal(t, 201, brand.Code, brand.Raw)
	p := createProduct(t, l, tok, variableProduct("Count tee", "", store, nil, nil,
		map[string]any{"sku": "CT-A", "itemBarcode": "CTBAR-A"},
		map[string]any{"sku": "CT-B"},
		map[string]any{"sku": "CT-C"}))
	require.Equal(t, 201, p.Code, p.Raw)
	vs := variantsOf(t, host, tok, p.Body["id"])
	a, b, c := vs["CT-A"]["id"].(float64), vs["CT-B"]["id"].(float64), vs["CT-C"]["id"].(float64)
	q := createProduct(t, l, tok, variableProduct("Branded cap", "", store, nil, nil, map[string]any{"sku": "CAP-1"}))
	require.Equal(t, 201, q.Code, q.Raw)
	capV := variantsOf(t, host, tok, q.Body["id"])["CAP-1"]["id"].(float64)
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.products SET brand_id = $1 WHERE id = $2`, int64(brand.Body["id"].(float64)), int64(q.Body["id"].(float64)))
	require.NoError(t, err)

	// Create: a whole-store count; validation.
	require.Equal(t, 400, post(tok, "/api/stock-counts", map[string]any{}).Code)
	require.Contains(t, post(tok, "/api/stock-counts", map[string]any{"storeId": store, "scope": map[string]any{"brandId": 999999}}).Raw, "scope.brandId")
	cr := post(tok, "/api/stock-counts", map[string]any{"storeId": store, "note": "Monday count"})
	require.Equal(t, 201, cr.Code, cr.Raw)
	id := cr.Body["id"]
	require.Regexp(t, `^CNT-\d{6}$`, cr.Body["reference"])
	require.Equal(t, "Draft", cr.Body["status"])
	require.Equal(t, "Whole store", cr.Body["scopeText"])
	require.EqualValues(t, 4, cr.Body["uncountedTotal"])
	path := fmt.Sprintf("/api/stock-counts/%v", id)

	// Scan A twice (+1 each), type B = 2, C by import = 9; nothing moves yet.
	for range 2 {
		s := put(tok, path, map[string]any{"lines": []any{map[string]any{"code": "CTBAR-A", "add": 1}}})
		require.Equal(t, 200, s.Code, s.Raw)
	}
	s := put(tok, path, map[string]any{"lines": []any{map[string]any{"variantId": b, "qty": 2}}})
	require.Equal(t, 200, s.Code, s.Raw)
	require.EqualValues(t, 2, s.Body["lineCount"])
	ln := s.Body["lines"].([]any)[0].(map[string]any)
	require.EqualValues(t, 2, ln["countedQty"])
	require.EqualValues(t, 5, ln["currentQty"])
	require.EqualValues(t, -3, ln["difference"])
	require.Contains(t, put(tok, path, map[string]any{"lines": []any{map[string]any{"code": "NOPE", "add": 1}}}).Raw, "No product has the code")
	require.Contains(t, put(tok, path, map[string]any{"lines": []any{map[string]any{"variantId": b, "add": -5}}}).Raw, "cannot go below 0")
	require.Equal(t, 400, put(tok, path, map[string]any{"lines": []any{map[string]any{"variantId": b, "qty": 1, "add": 1}}}).Code)
	imp := uploadCountFile(t, l, tok, id, "count.csv", []byte("SKU,Counted_Qty\nct-c,9\n"))
	require.Equal(t, 200, imp.Code, imp.Raw)
	require.EqualValues(t, 1, imp.Body["imported"])
	bad := uploadCountFile(t, l, tok, id, "count.csv", []byte("sku,counted_qty\nCT-C,x\nGHOST,1\n"))
	require.Equal(t, 400, bad.Code, bad.Raw)
	require.Contains(t, bad.Raw, `"row":2`)
	bad = uploadCountFile(t, l, tok, id, "count.csv", []byte("sku,counted_qty\nCT-C,1\nGHOST,1\n"))
	require.Equal(t, 400, bad.Code, bad.Raw)
	require.Contains(t, bad.Raw, `"row":3`)
	require.Contains(t, bad.Raw, "No product has the code")
	require.Equal(t, 5, stockOf(t, l, store, a), "a draft changes no stock")

	// A sale after counting shows as "moved since"; the review lists the
	// uncounted cap.
	sale := sell(t, l, tok, map[string]any{"storeId": store, "orderItems": []map[string]any{
		{"productId": p.Body["id"], "variantId": c, "quantity": 1, "unitPrice": 120.5, "total": 120.5}}}, nil)
	require.Equal(t, 201, sale.Code, sale.Raw)
	rv := get(t, host, path, tok)
	require.Equal(t, 200, rv.Code, rv.Raw)
	lc := lineBy(t, rv, "CT-C")
	require.EqualValues(t, -1, lc["movedSince"])
	require.EqualValues(t, 4, lc["currentQty"])
	require.EqualValues(t, 5, lineBy(t, rv, "CT-C")["difference"])
	require.EqualValues(t, 1, rv.Body["uncountedTotal"])
	require.Equal(t, "CAP-1", rv.Body["uncounted"].([]any)[0].(map[string]any)["sku"])
	require.Equal(t, map[string]any{"lines": 3.0, "changed": 3.0, "unitsUp": 5.0, "unitsDown": 6.0}, rv.Body["totals"])

	// Parallel drafts: another draft in the same store cannot take B; one
	// scoped to the brand can count only the cap; another store is free.
	o := post(tok, "/api/stock-counts", map[string]any{"storeId": store, "scope": map[string]any{"brandId": brand.Body["id"]}})
	require.Equal(t, 201, o.Code, o.Raw)
	require.Equal(t, "Brand Cnt brand", o.Body["scopeText"])
	opath := fmt.Sprintf("/api/stock-counts/%v", o.Body["id"])
	clash := put(tok, opath, map[string]any{"lines": []any{map[string]any{"variantId": b, "qty": 1}}})
	require.Equal(t, 400, clash.Code)
	require.Contains(t, clash.Raw, "not part of this count")
	require.Equal(t, 200, put(tok, opath, map[string]any{"lines": []any{map[string]any{"variantId": capV, "qty": 7}}}).Code)
	w := post(tok, "/api/stock-counts", map[string]any{"storeId": store})
	wpath := fmt.Sprintf("/api/stock-counts/%v", w.Body["id"])
	clash = put(tok, wpath, map[string]any{"lines": []any{map[string]any{"variantId": b, "qty": 1}}})
	require.Contains(t, clash.Raw, "already in draft count "+cr.Body["reference"].(string))
	if len(stores) > 1 {
		s2 := post(tok, "/api/stock-counts", map[string]any{"storeId": stores[1]})
		require.Equal(t, 200, put(tok, fmt.Sprintf("/api/stock-counts/%v", s2.Body["id"]), map[string]any{"lines": []any{map[string]any{"variantId": b, "qty": 1}}}).Code)
	}
	require.Equal(t, 200, post(tok, wpath+"/cancel", map[string]any{}).Code)
	require.Equal(t, 409, put(tok, wpath, map[string]any{"note": "x"}).Code, "a cancelled count is read-only")

	// Version check; empty counts cannot complete.
	require.Equal(t, 409, put(tok, path, map[string]any{"version": 1, "note": "stale"}).Code)
	e := post(tok, "/api/stock-counts", map[string]any{"storeId": store})
	require.Equal(t, 409, post(tok, fmt.Sprintf("/api/stock-counts/%v/complete", e.Body["id"]), map[string]any{}).Code)

	// Permissions: a biller has none; a counter can count but not complete.
	createUser(t, l, tok, "biller@"+l+".test", "biller", "cnt-biller-pass-1")
	btok, _ := login(t, l, "biller@"+l+".test", "cnt-biller-pass-1")
	require.Equal(t, 403, get(t, host, "/api/stock-counts", btok).Code)
	ctok := userWithPerms(t, l, tok, "counter@"+l+".test", "stock_counts:view", "stock_counts:create")
	require.Equal(t, 200, put(ctok, path, map[string]any{"lines": []any{map[string]any{"code": "CT-A", "add": 1}}}).Code)
	require.Equal(t, 403, post(ctok, path+"/complete", map[string]any{}).Code)

	// Complete, twice at once: one wins, the other gets 409; the stock
	// becomes the counted quantity through the ledger.
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = post(tok, path+"/complete", map[string]any{}).Code
		}()
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, codes)
	done := get(t, host, path, tok)
	require.Equal(t, "Completed", done.Body["status"])
	require.NotNil(t, done.Body["completedAt"])
	require.Nil(t, done.Body["uncounted"])
	la := lineBy(t, done, "CT-A")
	require.EqualValues(t, 5, la["expected"])
	require.EqualValues(t, -2, la["difference"])
	require.EqualValues(t, 4, lineBy(t, done, "CT-C")["expected"])
	require.Equal(t, 3, stockOf(t, l, store, a))
	require.Equal(t, 2, stockOf(t, l, store, b))
	require.Equal(t, 9, stockOf(t, l, store, c))
	require.Equal(t, 5, stockOf(t, l, store, capV), "uncounted variants are left unchanged")
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.stock_movements WHERE reason = 'stock_count' AND ref_type = 'stock_count' AND ref_id = $1`,
		int64(id.(float64))).Scan(&n))
	require.Equal(t, 3, n)
	require.Empty(t, reconcile(t, l))
	require.Equal(t, 409, put(tok, path, map[string]any{"note": "late"}).Code, "a completed count is read-only")
	require.Equal(t, 409, post(tok, path+"/cancel", map[string]any{}).Code)
	// B is free for a new draft now.
	require.Equal(t, 200, put(tok, fmt.Sprintf("/api/stock-counts/%v", e.Body["id"]),
		map[string]any{"lines": []any{map[string]any{"variantId": b, "qty": 2}}}).Code)

	// A sale racing a completion (cap counted 7, stock 5): both succeed in
	// some order (7, or 7 - 1) and the ledger reconciles.
	var race [2]resp
	wg.Add(2)
	go func() {
		defer wg.Done()
		race[0] = post(tok, opath+"/complete", map[string]any{})
	}()
	go func() {
		defer wg.Done()
		race[1] = sell(t, l, tok, map[string]any{"storeId": store, "orderItems": []map[string]any{
			{"productId": q.Body["id"], "variantId": capV, "quantity": 1, "unitPrice": 120.5, "total": 120.5}}}, nil)
	}()
	wg.Wait()
	require.Equal(t, 200, race[0].Code, race[0].Raw)
	require.Equal(t, 201, race[1].Code, race[1].Raw)
	require.Contains(t, []int{6, 7}, stockOf(t, l, store, capV))
	require.Empty(t, reconcile(t, l))

	// List and audit.
	ls := get(t, host, "/api/stock-counts?status=Completed&page=1&limit=10", tok)
	require.Equal(t, 200, ls.Code, ls.Raw)
	require.EqualValues(t, 2, ls.Body["pagination"].(map[string]any)["total"])
	require.Equal(t, 400, get(t, host, "/api/stock-counts?status=Open", tok).Code)
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action LIKE 'stock_count.%'`).Scan(&n))
	require.GreaterOrEqual(t, n, 7)

	// Module off: counts stay readable (list and detail), every change
	// refuses; counts and their movements stay.
	setModules(t, l, "retail")
	rd := get(t, host, path, tok)
	require.Equal(t, 200, rd.Code, rd.Raw)
	require.NotEmpty(t, rd.Body["reference"])
	require.Equal(t, 200, get(t, host, "/api/stock-counts", tok).Code)
	require.Equal(t, "MODULE_OFF", codeOf(post(tok, "/api/stock-counts", map[string]any{"storeId": store})))
	require.Equal(t, "MODULE_OFF", codeOf(put(tok, path, map[string]any{"lines": []any{}})))
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.stock_counts`).Scan(&n))
	require.GreaterOrEqual(t, n, 4)
	setModules(t, l, "clothing")

	// Isolation: another shop sees none of these counts.
	l2 := billingTenant(t, "cnt-other")
	setModules(t, l2, "clothing")
	tok2, _ := ownerLogin(t, l2)
	h2 := tenants[l2].Host
	require.Equal(t, 404, get(t, h2, path, tok2).Code)
	require.Equal(t, 404, do(t, rq{method: "PUT", host: h2, path: path, token: tok2, body: map[string]any{"note": "x"}}).Code)
	require.Equal(t, 404, do(t, rq{method: "POST", host: h2, path: path + "/complete", token: tok2, body: map[string]any{}}).Code)
	require.Empty(t, list(t, get(t, h2, "/api/stock-counts", tok2)))
	require.Equal(t, 401, get(t, h2, path, tok).Code)
}
