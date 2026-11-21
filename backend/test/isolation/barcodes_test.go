//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Several barcodes per variant (tenant 00043): an old EAN and a case code
// of 12. Codes are unique across main and extra codes in both directions;
// a unit must be the product's; the POS lookup, the product search, the
// POS list and stock counts find them, and a case code counts a case.
func TestVariantBarcodes(t *testing.T) {
	l := billingTenant(t, "barcodes")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}
	store := storeIDs(t, l, tok)[0]
	unit := func(name string) float64 {
		u := call("POST", "/api/units", map[string]any{"name": name, "short_name": name[:2]})
		require.Equal(t, 201, u.Code, u.Raw)
		return num(u.Body["id"])
	}
	piece, box, other := unit("Piece"), unit("Box"), unit("Crate")
	body := addFormSingle("BC-SOAP", store, 100)
	body["unitId"] = piece
	body["units"] = []map[string]any{{"unitId": box, "factor": 12}}
	r := createProduct(t, l, tok, body)
	require.Equal(t, 201, r.Code, r.Raw)
	soap := stocked{pid: num(r.Body["id"]), vid: num(r.Body["ProductVariants"].([]any)[0].(map[string]any)["id"]), sku: "BC-SOAP"}
	pen := stockedProduct(t, l, tok, "BC-PEN", store, 5) // main barcode BCBC-PEN
	path := "/api/products/" + jsonID(soap.pid) + "/barcodes"
	code := func(c string, u float64) map[string]any {
		m := map[string]any{"variantId": soap.vid, "code": c}
		if u > 0 {
			m["unitId"] = u
		}
		return m
	}

	// Refused: another product's main code, the same code twice, a unit
	// the product does not have, a bad character.
	for _, c := range []struct {
		codes []map[string]any
		field string
	}{
		{[]map[string]any{code("BCBC-PEN", 0)}, "barcodes[0].code"},
		{[]map[string]any{code("OLD-1", 0), code("OLD-1", 0)}, "barcodes[1].code"},
		{[]map[string]any{code("CRATE-1", other)}, "barcodes[0].unitId"},
		{[]map[string]any{code("bad code", 0)}, "barcodes[0].code"},
	} {
		x := call("PUT", path, map[string]any{"barcodes": c.codes})
		require.Equal(t, 400, x.Code, x.Raw)
		require.Equal(t, c.field, errField(x))
	}

	r = call("PUT", path, map[string]any{"barcodes": []map[string]any{code("OLD-EAN-1", 0), code("CASE-12", box)}})
	require.Equal(t, 200, r.Code, r.Raw)
	bs := r.Body["ProductVariants"].([]any)[0].(map[string]any)["barcodes"].([]any)
	require.Len(t, bs, 2)
	cs := bs[1].(map[string]any)
	require.Equal(t, "CASE-12", cs["code"])
	require.Equal(t, box, num(cs["unitId"]))
	require.Equal(t, 12.0, num(cs["factor"]))

	// The other direction: a main barcode that is an extra code.
	clash := addFormSingle("BC-CLASH", store, 1)
	clash["variants"].([]map[string]any)[0]["itemBarcode"] = "CASE-12"
	require.Equal(t, 409, createProduct(t, l, tok, clash).Code)

	// The POS lookup names the case unit; the old EAN is a single unit.
	lk := call("GET", "/api/products/barcode/CASE-12", nil)
	require.Equal(t, 200, lk.Code, lk.Raw)
	require.Equal(t, soap.vid, num(lk.Body["variantId"]))
	require.Equal(t, box, num(lk.Body["unitId"]))
	lk = call("GET", "/api/products/barcode/OLD-EAN-1", nil)
	require.Equal(t, 200, lk.Code, lk.Raw)
	require.Nil(t, lk.Body["unitId"])
	require.Equal(t, 200, call("GET", "/api/products/barcode/BCBC-PEN", nil).Code, "main codes still work")

	// The product search and the POS list.
	sr := call("GET", "/api/products?search=OLD-EAN", nil)
	require.Equal(t, 200, sr.Code, sr.Raw)
	require.Contains(t, sr.Raw, "BC-SOAP")
	pos := call("GET", fmt.Sprintf("/api/products/pos?storeId=%v", store), nil)
	require.Equal(t, 200, pos.Code, pos.Raw)
	require.Contains(t, pos.Raw, `"code":"CASE-12"`)

	// A stock count: one scan of the case code counts 12.
	setModules(t, l, "clothing")
	cr := call("POST", "/api/stock-counts", map[string]any{"storeId": store})
	require.Equal(t, 201, cr.Code, cr.Raw)
	cpath := fmt.Sprintf("/api/stock-counts/%v", cr.Body["id"])
	sc := call("PUT", cpath, map[string]any{"lines": []any{map[string]any{"code": "CASE-12", "add": 1}}})
	require.Equal(t, 200, sc.Code, sc.Raw)
	sc = call("PUT", cpath, map[string]any{"lines": []any{map[string]any{"code": "OLD-EAN-1", "add": 1}}})
	require.Equal(t, 200, sc.Code, sc.Raw)
	found := false
	for _, x := range sc.Body["lines"].([]any) {
		if m := x.(map[string]any); num(m["variantId"]) == soap.vid {
			require.EqualValues(t, 13, m["countedQty"], "a case of 12 and a single")
			found = true
		}
	}
	require.True(t, found, "the soap's count line: %s", sc.Raw)

	// Removing the codes: the case code is free again.
	require.Equal(t, 200, call("PUT", path, map[string]any{"barcodes": []map[string]any{}}).Code)
	require.Equal(t, 404, call("GET", "/api/products/barcode/CASE-12", nil).Code)
	_ = pen
}
