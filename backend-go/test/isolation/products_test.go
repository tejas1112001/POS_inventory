//go:build integration

package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

func storeIDs(t *testing.T, l, tok string) []float64 {
	t.Helper()
	var ids []float64
	for _, s := range list(t, get(t, tenants[l].Host, "/api/stores", tok)) {
		ids = append(ids, s["id"].(float64))
	}
	require.NotEmpty(t, ids)
	return ids
}

// addFormSingle is exactly what AddProduct.jsx sends for a single product.
func addFormSingle(sku string, store float64, qty int) map[string]any {
	return map[string]any{
		"name": "Product " + sku, "description": "d", "categoryId": "", "subCategoryId": "", "brandId": "", "unitId": "",
		"productType": "single", "taxType": "Exclusive", "tax": "18", "createdBy": 1, "discountType": "", "discountValue": "",
		"warranties": "", "barcodeSymbology": "", "sellingType": "POS", "image": nil, "supplierId": "", "slug": "",
		"variants": []map[string]any{{"sku": sku, "itemBarcode": "BC" + sku, "price": 120.5, "cost": 0, "weight": 0,
			"attributes": map[string]any{}, "expiryDate": nil, "manufacturedDate": nil,
			"inventories": []map[string]any{{"storeId": fmt.Sprint(store), "qty": qty, "quantityAlert": 2}}}},
		"sku": sku, "price": "120.5", "quantity": fmt.Sprint(qty), "storeId": fmt.Sprint(store), "itemBarcode": "BC" + sku, "quantityAlert": "2",
	}
}

func createProduct(t *testing.T, l, tok string, body map[string]any) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/products", token: tok, body: body})
}

func reconcile(t *testing.T, l string) []inventory.Mismatch {
	t.Helper()
	s, r, err := tenancy.SchemaFor(tenants[l].ID)
	require.NoError(t, err)
	sess := tenancy.NewSessions(tdb.AppPool(t, 1), 0, nil, nil)
	var out []inventory.Mismatch
	require.NoError(t, sess.InTenantTx(context.Background(), tenancy.Tenant{ID: tenants[l].ID, Schema: s, Role: r},
		tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
			var err error
			out, err = inventory.Reconcile(context.Background(), tx)
			return err
		}))
	return out
}

func TestProductsCreateListAndShape(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]

	p := createProduct(t, "a", tok, addFormSingle("TEA-1", store, 7))
	require.Equal(t, 201, p.Code, p.Raw)
	require.Equal(t, 120.5, p.Body["price"], "money is a JSON number")
	require.EqualValues(t, 7, p.Body["qty"])
	require.EqualValues(t, 18, p.Body["tax"])
	v := p.Body["ProductVariants"].([]any)[0].(map[string]any)
	require.Equal(t, "TEA-1", v["sku"])
	inv := v["Inventories"].([]any)[0].(map[string]any)
	require.EqualValues(t, 7, inv["qty"])
	require.EqualValues(t, store, inv["store_id"])
	for _, k := range []string{"category", "categoryId", "brand", "unit", "createdBy", "isLocked", "lockedAt", "isPublished"} {
		require.Contains(t, p.Body, k)
	}

	vp := createProduct(t, "a", tok, map[string]any{"name": "Shirt", "productType": "variable", "variants": []map[string]any{
		{"sku": "SHIRT-S", "price": "499", "attributes": map[string]any{"size": "S"}, "inventories": []map[string]any{{"storeId": store, "qty": 3}}},
		{"sku": "SHIRT-M", "price": "499", "attributes": map[string]any{"size": "M"}, "inventories": []map[string]any{{"storeId": store, "qty": 4}}},
	}})
	require.Equal(t, 201, vp.Code, vp.Raw)
	require.EqualValues(t, 7, vp.Body["qty"])
	require.Len(t, vp.Body["ProductVariants"], 2)

	// Validation and uniqueness.
	bad := map[string]map[string]any{
		"no name":          {"productType": "single", "variants": []map[string]any{{"sku": "X1", "price": 1}}},
		"bad type":         {"name": "x", "productType": "bundle", "variants": []map[string]any{{"sku": "X2", "price": 1}}},
		"negative price":   {"name": "x", "productType": "single", "variants": []map[string]any{{"sku": "X3", "price": -1}}},
		"single, 2 vars":   {"name": "x", "productType": "single", "variants": []map[string]any{{"sku": "X4", "price": 1}, {"sku": "X5", "price": 1}}},
		"unknown category": {"name": "x", "productType": "single", "categoryId": 99999, "variants": []map[string]any{{"sku": "X6", "price": 1}}},
		"unknown store":    {"name": "x", "productType": "single", "variants": []map[string]any{{"sku": "X7", "price": 1, "inventories": []map[string]any{{"storeId": 99999, "qty": 1}}}}},
		"tax over 100":     {"name": "x", "productType": "single", "tax": 120, "variants": []map[string]any{{"sku": "X8", "price": 1}}},
		"bad barcode":      {"name": "x", "productType": "single", "variants": []map[string]any{{"sku": "X9", "price": 1, "itemBarcode": "<script>"}}},
	}
	for name, body := range bad {
		require.Equal(t, 400, createProduct(t, "a", tok, body).Code, name)
	}
	dupSKU := createProduct(t, "a", tok, map[string]any{"name": "dup", "productType": "single", "variants": []map[string]any{{"sku": "tea-1", "price": 1}}})
	require.Equal(t, 409, dupSKU.Code, "SKUs are unique, case-insensitive")
	require.Contains(t, dupSKU.Body["error"], "SKU")
	dupBC := createProduct(t, "a", tok, map[string]any{"name": "dup", "productType": "single", "variants": []map[string]any{{"sku": "NEW-1", "price": 1, "itemBarcode": "BCTEA-1"}}})
	require.Equal(t, 409, dupBC.Code)

	// List: search by name or SKU, filters, pagination.
	require.Contains(t, names(list(t, get(t, host, "/api/products?search=shirt-m", tok))), "Shirt")
	pg := get(t, host, "/api/products?page=1&limit=1", tok)
	require.Len(t, pg.Body["data"], 1)

	// Ledger: opening stock recorded, and it reconciles.
	mv := get(t, host, "/api/stock-movements?page=1&limit=50", tok)
	require.Equal(t, 200, mv.Code)
	require.Contains(t, mv.Raw, `"reason":"opening"`)
	require.Empty(t, reconcile(t, "a"))
}

func TestPOSListBarcodeAndLocks(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	stores := storeIDs(t, "a", tok)
	s1 := stores[0]
	s2r := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Kiosk"}})
	require.Equal(t, 201, s2r.Code, s2r.Raw)
	s2 := s2r.Body["id"].(float64)

	p := createProduct(t, "a", tok, addFormSingle("POS-1", s1, 5))
	require.Equal(t, 201, p.Code, p.Raw)
	pid := p.Body["id"].(float64)

	require.Equal(t, 400, get(t, host, "/api/products/pos", tok).Code)
	pos := list(t, get(t, host, fmt.Sprintf("/api/products/pos?storeId=%v", s1), tok))
	found := false
	for _, x := range pos {
		if x["id"] == pid {
			found = true
		}
	}
	require.True(t, found)
	for _, x := range list(t, get(t, host, fmt.Sprintf("/api/products/pos?storeId=%v", s2), tok)) {
		require.NotEqual(t, pid, x["id"], "no stock row in the kiosk")
	}

	bc := get(t, host, fmt.Sprintf("/api/products/barcode/BCPOS-1?storeId=%v", s1), tok)
	require.Equal(t, 200, bc.Code)
	require.Equal(t, pid, bc.Body["id"])
	require.Equal(t, 200, get(t, host, "/api/products/barcode/pos-1", tok).Code, "SKU works too")
	require.Equal(t, 404, get(t, host, "/api/products/barcode/NOPE-999", tok).Code)

	// Locks per (store, product), atomic, owner-only release.
	createUser(t, "a", tok, "cashier2@iso-a.test", "biller", "cashier2-pass-word-1")
	btok, _ := login(t, "a", "cashier2@iso-a.test", "cashier2-pass-word-1")
	lockPath := fmt.Sprintf("/api/products/%v/lock", pid)
	l1 := do(t, rq{method: "PUT", host: host, path: lockPath, token: tok, body: map[string]any{"lock": true, "storeId": s1}})
	require.Equal(t, 200, l1.Code, l1.Raw)
	require.Equal(t, true, l1.Body["isLocked"])
	inv := l1.Body["ProductVariants"].([]any)[0].(map[string]any)["Inventories"].([]any)
	require.Len(t, inv, 1, "only the requested store's stock")
	l2 := do(t, rq{method: "PUT", host: host, path: lockPath, token: btok, body: map[string]any{"lock": true, "storeId": s1}})
	require.Equal(t, 409, l2.Code)
	require.Equal(t, "PRODUCT_LOCKED", l2.Body["code"])
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: lockPath, token: btok, body: map[string]any{"lock": false, "storeId": s1}}).Code)
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: lockPath, token: btok, body: map[string]any{"lock": true, "storeId": s2}}).Code, "other store, separate lock")
	require.Equal(t, 400, do(t, rq{method: "PUT", host: host, path: lockPath, token: btok, body: map[string]any{"lock": true}}).Code, "storeId required")

	// Expired locks can be taken over.
	_, err := admin(t).Exec(context.Background(), `UPDATE `+schema("a")+`.pos_locks SET expires_at = now() - interval '1 second' WHERE product_id = $1 AND store_id = $2`, pid, s1)
	require.NoError(t, err)
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: lockPath, token: btok, body: map[string]any{"lock": true, "storeId": s1}}).Code)
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: lockPath, token: btok, body: map[string]any{"lock": false, "storeId": s1}}).Code)

	// Race: many users grab the same lock at once; exactly one wins.
	var tokens []string
	for i := range 6 {
		email := fmt.Sprintf("racer%d@iso-a.test", i)
		createUser(t, "a", tok, email, "biller", "racer-pass-word-12")
		rt, _ := login(t, "a", email, "racer-pass-word-12")
		tokens = append(tokens, rt)
	}
	var wg sync.WaitGroup
	codes := make(chan int, len(tokens))
	for _, rt := range tokens {
		wg.Go(func() {
			codes <- do(t, rq{method: "PUT", host: host, path: lockPath, token: rt, body: map[string]any{"lock": true, "storeId": s1}}).Code
		})
	}
	wg.Wait()
	close(codes)
	wins := 0
	for c := range codes {
		if c == 200 {
			wins++
		} else {
			require.Equal(t, 409, c)
		}
	}
	require.Equal(t, 1, wins)
}

func TestProductEditRulesC14(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	a := createProduct(t, "a", tok, map[string]any{"name": "Combo", "productType": "variable", "variants": []map[string]any{
		{"sku": "CMB-1", "price": 10, "inventories": []map[string]any{{"storeId": store, "qty": 5}}},
		{"sku": "CMB-2", "price": 10, "inventories": []map[string]any{{"storeId": store, "qty": 0}}},
	}})
	require.Equal(t, 201, a.Code, a.Raw)
	other := createProduct(t, "a", tok, addFormSingle("OTH-1", store, 1))
	require.Equal(t, 201, other.Code)
	aID := a.Body["id"].(float64)
	va := a.Body["ProductVariants"].([]any)
	v1 := va[0].(map[string]any)["id"].(float64)
	v2 := va[1].(map[string]any)["id"].(float64)
	otherVariant := other.Body["ProductVariants"].([]any)[0].(map[string]any)["id"].(float64)
	path := fmt.Sprintf("/api/products/%v", aID)

	// IDOR: a variant of another product cannot be pulled in.
	r := do(t, rq{method: "PUT", host: host, path: path, token: tok, body: map[string]any{"name": "Combo", "productType": "variable",
		"variants": []map[string]any{{"id": v1, "sku": "CMB-1", "price": 10}, {"id": otherVariant, "sku": "OTH-1", "price": 0}}}})
	require.Equal(t, 400, r.Code, r.Raw)

	// Leaving out a variant that holds stock is refused.
	r = do(t, rq{method: "PUT", host: host, path: path, token: tok, body: map[string]any{"name": "Combo", "productType": "variable",
		"variants": []map[string]any{{"_id": v2, "sku": "CMB-2", "price": 10}}}})
	require.Equal(t, 409, r.Code, r.Raw)

	// Leaving out the empty one soft-deletes it; stock set and price change are recorded.
	r = do(t, rq{method: "PUT", host: host, path: path, token: tok, body: map[string]any{"name": "Combo 2", "productType": "variable",
		"variants": []map[string]any{{"_id": v1, "sku": "CMB-1", "price": 12, "inventories": []map[string]any{{"storeId": store, "qty": 9}}}}}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Len(t, r.Body["ProductVariants"], 1)
	require.EqualValues(t, 9, r.Body["qty"])
	require.Equal(t, 409, createProduct(t, "a", tok, map[string]any{"name": "reuse", "productType": "single", "variants": []map[string]any{{"sku": "CMB-1", "price": 1}}}).Code)
	require.Equal(t, 201, createProduct(t, "a", tok, map[string]any{"name": "reuse", "productType": "single", "variants": []map[string]any{{"sku": "CMB-2", "price": 1}}}).Code, "a deleted variant's SKU is free again")
	var edits, priceAudit int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.stock_movements WHERE reason = 'product_edit' AND variant_id = $1 AND delta = 4`, v1).Scan(&edits))
	require.Equal(t, 1, edits)
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.audit_log WHERE action = 'product.price_change' AND entity_id = $1`, fmt.Sprint(v1)).Scan(&priceAudit))
	require.Equal(t, 1, priceAudit)
	require.Equal(t, 400, do(t, rq{method: "PUT", host: host, path: path, token: tok, body: map[string]any{"name": "x", "productType": "single", "variants": []map[string]any{{"_id": v1, "sku": "CMB-1", "price": 1}}}}).Code, "type cannot change")

	// Delete: refused with stock, allowed at zero; gone from lists.
	require.Equal(t, 409, do(t, rq{method: "DELETE", host: host, path: path, token: tok}).Code)
	set := do(t, rq{method: "POST", host: host, path: "/api/inventory", token: tok, body: map[string]any{"variantId": v1, "storeId": store, "qty": 0, "quantityAlert": 1}})
	require.Equal(t, 201, set.Code, set.Raw)
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: path, token: tok}).Code)
	require.NotContains(t, names(list(t, get(t, host, "/api/products", tok))), "Combo 2")
	require.Empty(t, reconcile(t, "a"))
}

func TestInventoryEndpoints(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := createProduct(t, "a", tok, addFormSingle("INV-1", store, 3))
	require.Equal(t, 201, p.Code)
	vid := p.Body["ProductVariants"].([]any)[0].(map[string]any)["id"].(float64)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/inventory", token: tok, body: map[string]any{"variantId": vid, "storeId": store, "qty": -5}}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/inventory", token: tok, body: map[string]any{"variantId": 999999, "storeId": store, "qty": 5}}).Code)
	r := do(t, rq{method: "POST", host: host, path: "/api/inventory", token: tok, body: map[string]any{"variantId": vid, "storeId": store, "qty": 1, "quantityAlert": 2}})
	require.Equal(t, 201, r.Code, r.Raw)
	require.EqualValues(t, 1, r.Body["qty"])
	rows := list(t, get(t, host, "/api/inventory", tok))
	found := false
	for _, row := range rows {
		if row["variantId"] == vid {
			found = true
			require.Equal(t, "Low Stock", row["status"])
			require.Equal(t, "Product INV-1", row["productName"])
		}
	}
	require.True(t, found)
	var audits int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("a")+`.audit_log WHERE action = 'stock.manual_set'`).Scan(&audits))
	require.GreaterOrEqual(t, audits, 1)

	// The ledger is append-only for the tenant role.
	var canUpdate bool
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT has_table_privilege($1, $2, 'UPDATE')`, schema("a")+"_rw", schema("a")+".stock_movements").Scan(&canUpdate))
	require.False(t, canUpdate)
}

func TestShowcasePublicationAndProductImages(t *testing.T) {
	tok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	host := tenants["c"].Host
	stores := storeIDs(t, "c", tok)
	s1 := stores[0]
	body := addFormSingle("PUB-1", s1, 2)
	body["image"] = pngDataURI(t, color.RGBA{R: 50, A: 255})
	p := createProduct(t, "c", tok, body)
	require.Equal(t, 201, p.Code, p.Raw)
	pid := p.Body["id"].(float64)
	img := p.Body["image"].(string)
	require.True(t, strings.HasPrefix(img, "/api/files/"), "unpublished: image behind login")
	require.Equal(t, 200, get(t, host, img, tok).Code)
	public := strings.Replace(img, "/api/files/", "/files/t/", 1)
	require.Equal(t, 404, do(t, rq{method: "GET", host: host, path: public}).Code)

	tg := do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/stores/%v/showcase/toggle-product", s1), token: tok,
		body: map[string]any{"productId": pid, "field": "isPublished", "value": true}})
	require.Equal(t, 200, tg.Code, tg.Raw)
	require.Equal(t, true, tg.Body["storeProduct"].(map[string]any)["isPublished"])
	require.Equal(t, 200, do(t, rq{method: "GET", host: host, path: public}).Code, "published: public")
	require.Equal(t, 404, do(t, rq{method: "GET", host: tenants["a"].Host, path: public}).Code, "never on another host")

	sh := get(t, host, fmt.Sprintf("/api/stores/%v/showcase", s1), tok)
	require.Equal(t, 200, sh.Code)
	var shown bool
	for _, x := range sh.Body["products"].([]any) {
		m := x.(map[string]any)
		if m["id"] == pid {
			shown = m["isPublished"].(bool)
			require.Equal(t, public, m["image"])
		}
	}
	require.True(t, shown)

	// Edit form sends the public URL back: the image is kept.
	ed := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/products/%v", pid), token: tok, body: map[string]any{
		"name": "Product PUB-1", "productType": "single", "image": public,
		"variants": []map[string]any{{"_id": p.Body["ProductVariants"].([]any)[0].(map[string]any)["id"], "sku": "PUB-1", "price": 120.5}}}})
	require.Equal(t, 200, ed.Code, ed.Raw)
	require.Equal(t, public, ed.Body["image"])

	// Unpublish (batch) hides it again; publication is per store only.
	require.Equal(t, 200, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/stores/%v/showcase/batch-publish", s1), token: tok,
		body: map[string]any{"productIds": []float64{pid}, "action": "unpublish"}}).Code)
	require.Equal(t, 404, do(t, rq{method: "GET", host: host, path: public}).Code)
	require.Equal(t, 200, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/stores/%v/showcase/batch-publish", s1), token: tok,
		body: map[string]any{"publishAll": true}}).Code)
	require.Equal(t, 200, do(t, rq{method: "GET", host: host, path: public}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/stores/%v/showcase/batch-publish", s1), token: tok, body: map[string]any{}}).Code)
	require.Equal(t, 404, do(t, rq{method: "POST", host: host, path: "/api/stores/99999/showcase/toggle-product", token: tok,
		body: map[string]any{"productId": pid, "field": "isPublished", "value": true}}).Code)
}

func uploadCSV(t *testing.T, l, tok, csvBody string, dry bool) resp {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("csvFile", "products.csv")
	_, _ = fw.Write([]byte(csvBody))
	_ = mw.WriteField("product", "ignored by the new backend")
	_ = mw.Close()
	path := baseURL + "/api/products/import"
	if dry {
		path += "?dryRun=true"
	}
	req, _ := http.NewRequest("POST", path, &body)
	req.Host = tenants[l].Host
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Forwarded-For", "10.201.0.1")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	out := resp{Code: res.StatusCode, Raw: string(b), Header: res.Header}
	_ = json.Unmarshal(b, &out.Body)
	return out
}

func TestCSVImport(t *testing.T) {
	tok, _ := ownerLogin(t, "b")
	host := tenants["b"].Host
	tpl := get(t, host, "/api/products/import/template", tok)
	require.Equal(t, 200, tpl.Code)
	require.True(t, strings.HasPrefix(tpl.Raw, "name,sku,price"))
	require.Contains(t, tpl.Header.Get("Content-Disposition"), "attachment")

	cat := do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "Grocery"}})
	require.Equal(t, 201, cat.Code)
	var storeSlug string
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT slug::text FROM `+schema("b")+`.stores WHERE deleted_at IS NULL ORDER BY id LIMIT 1`).Scan(&storeSlug))

	good := "\xef\xbb\xbfName,SKU,Price,category,store,opening_qty,barcode,tax_rate,tax_type\n" +
		"Rice 5kg,RICE-5,450,grocery," + storeSlug + ",10,8900000000011,5,Inclusive\n" +
		"T-Shirt,TS-S,299,," + storeSlug + ",4,,,\n" +
		"T-Shirt,TS-M,299,," + storeSlug + ",6,,,\n"
	dry := uploadCSV(t, "b", tok, good, true)
	require.Equal(t, 200, dry.Code, dry.Raw)
	require.Equal(t, true, dry.Body["dryRun"])
	require.EqualValues(t, 2, dry.Body["created"])
	require.NotContains(t, names(list(t, get(t, host, "/api/products", tok))), "Rice 5kg", "dry run writes nothing")

	real := uploadCSV(t, "b", tok, good, false)
	require.Equal(t, 200, real.Code, real.Raw)
	require.EqualValues(t, 2, real.Body["created"])
	require.EqualValues(t, 3, real.Body["variants"])
	prods := list(t, get(t, host, "/api/products?search=t-shirt", tok))
	require.Len(t, prods, 1)
	require.Equal(t, "variable", prods[0]["productType"], "same name, two SKUs = variable product")
	require.EqualValues(t, 10, prods[0]["qty"])
	var importMoves int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("b")+`.stock_movements WHERE reason = 'import'`).Scan(&importMoves))
	require.Equal(t, 3, importMoves)

	bad := "name,sku,price,category,store,opening_qty\n" +
		"Good row,OK-1,10,,,\n" +
		"Existing,RICE-5,10,,,\n" +
		"No price,NP-1,abc,,,\n" +
		"Ghost cat,GC-1,10,Nonexistent,,\n" +
		"Stock no store,SN-1,10,,,5\n"
	r := uploadCSV(t, "b", tok, bad, false)
	require.Equal(t, 400, r.Code, r.Raw)
	errs := r.Body["errors"].([]any)
	require.Len(t, errs, 4)
	require.EqualValues(t, 3, errs[0].(map[string]any)["row"])
	require.NotContains(t, names(list(t, get(t, host, "/api/products", tok))), "Good row", "all or nothing")

	require.Equal(t, 400, uploadCSV(t, "b", tok, "title,price\nx,1\n", false).Code, "unknown/missing columns")
}

func TestProductsIsolationAndBillerAccess(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	bTok, _ := ownerLogin(t, "b")
	store := storeIDs(t, "a", aTok)[0]
	p := createProduct(t, "a", aTok, addFormSingle("ISO-SECRET", store, 2))
	require.Equal(t, 201, p.Code)
	pid := p.Body["id"].(float64)
	vid := p.Body["ProductVariants"].([]any)[0].(map[string]any)["id"].(float64)
	bh := tenants["b"].Host
	require.NotContains(t, get(t, bh, "/api/products", bTok).Raw, "ISO-SECRET")
	require.Equal(t, 404, get(t, bh, "/api/products/barcode/ISO-SECRET", bTok).Code)
	var bHas bool
	for _, x := range list(t, get(t, bh, "/api/products", bTok)) {
		bHas = bHas || x["id"] == pid
	}
	if !bHas {
		require.Equal(t, 404, do(t, rq{method: "DELETE", host: bh, path: fmt.Sprintf("/api/products/%v", pid), token: bTok}).Code)
		require.Equal(t, 404, do(t, rq{method: "PUT", host: bh, path: fmt.Sprintf("/api/products/%v/lock", pid), token: bTok, body: map[string]any{"lock": true, "storeId": 1}}).Code)
	}
	require.Equal(t, 400, do(t, rq{method: "POST", host: bh, path: "/api/inventory", token: bTok, body: map[string]any{"variantId": vid + 100000, "storeId": 1, "qty": 1}}).Code)
	require.NotContains(t, get(t, bh, "/api/stock-movements?page=1&limit=200", bTok).Raw, "ISO-SECRET")

	createUser(t, "a", aTok, "posonly@iso-a.test", "biller", "posonly-pass-word-1")
	btok, _ := login(t, "a", "posonly@iso-a.test", "posonly-pass-word-1")
	host := tenants["a"].Host
	require.Equal(t, 200, get(t, host, fmt.Sprintf("/api/products/pos?storeId=%v", store), btok).Code)
	require.Equal(t, 200, get(t, host, "/api/products", btok).Code)
	require.Equal(t, 200, get(t, host, "/api/inventory", btok).Code)
	require.Equal(t, 403, createProduct(t, "a", btok, addFormSingle("BILLER-1", store, 1)).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/inventory", token: btok, body: map[string]any{"variantId": vid, "storeId": store, "qty": 99}}).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/product-variants", token: btok, body: map[string]any{"productId": pid, "sku": "X", "price": 0}}).Code, "C-08: no longer open to everyone")
}
