//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func variantsOf(t *testing.T, host, tok string, pid any) map[string]map[string]any {
	t.Helper()
	r := get(t, host, fmt.Sprintf("/api/products/%v", pid), tok)
	require.Equal(t, 200, r.Code, r.Raw)
	out := map[string]map[string]any{}
	for _, v := range r.Body["ProductVariants"].([]any) {
		m := v.(map[string]any)
		out[m["sku"].(string)] = m
	}
	return out
}

func isPDF(t *testing.T, r resp) {
	t.Helper()
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "application/pdf", r.Header.Get("Content-Type"))
	require.True(t, strings.HasPrefix(r.Raw, "%PDF-"), "a PDF")
}

// Industry P1 item 5: in-store barcodes (A9), the label PDF (A10), label
// layouts (A11), the matrix's barcode option, permissions (P3),
// MODULE_OFF and isolation.
func TestBarcodesAndLabels(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "lbl-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	post := func(tk, path string, body any) resp {
		return do(t, rq{method: "POST", host: host, path: path, token: tk, body: body})
	}

	// A retail shop: generation and printing are off; layouts are settings.
	p := createProduct(t, l, tok, variableProduct("Socks", "", store, nil, nil,
		map[string]any{"sku": "SOCK-A", "attributes": map[string]any{"colour": "Grey"}},
		map[string]any{"sku": "SOCK-B", "itemBarcode": "2000000000015"}, // typed by hand: counter value 1
		map[string]any{"sku": "SOCK-C"},
		map[string]any{"sku": "SOCK-M", "itemBarcode": "8901030865275"})) // a manufacturer's EAN
	require.Equal(t, 201, p.Code, p.Raw)
	vs := variantsOf(t, host, tok, p.Body["id"])
	a, b, c, m := vs["SOCK-A"]["id"], vs["SOCK-B"]["id"], vs["SOCK-C"]["id"], vs["SOCK-M"]["id"]
	off := post(tok, "/api/barcodes/generate", map[string]any{"variantIds": []any{a}})
	require.Equal(t, 403, off.Code)
	require.Equal(t, "MODULE_OFF", codeOf(off))
	require.Equal(t, "MODULE_OFF", codeOf(post(tok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8", "test": true})))
	lay := get(t, host, "/api/settings/labels", tok)
	require.Equal(t, 200, lay.Code, lay.Raw)
	require.Len(t, lay.Body["presets"], 3)
	require.Empty(t, lay.Body["layouts"])

	setModules(t, l, "clothing")

	// A9: codes for A and C; counter value 1 is taken (B), so A gets 2.
	// B and the manufacturer's code are never changed.
	g := post(tok, "/api/barcodes/generate", map[string]any{"variantIds": []any{a, b, c, m, a}})
	require.Equal(t, 200, g.Code, g.Raw)
	gen := g.Body["generated"].([]any)
	require.Len(t, gen, 2)
	require.Equal(t, "2000000000022", gen[0].(map[string]any)["barcode"])
	require.Equal(t, "2000000000039", gen[1].(map[string]any)["barcode"])
	require.ElementsMatch(t, []any{b, m}, g.Body["skipped"])
	vs = variantsOf(t, host, tok, p.Body["id"])
	require.Equal(t, "2000000000022", vs["SOCK-A"]["itemBarcode"])
	require.Equal(t, "2000000000015", vs["SOCK-B"]["itemBarcode"])
	require.Equal(t, "8901030865275", vs["SOCK-M"]["itemBarcode"])
	again := post(tok, "/api/barcodes/generate", map[string]any{"variantIds": []any{a}})
	require.Equal(t, 200, again.Code)
	require.Empty(t, again.Body["generated"], "a variant with a barcode keeps it")
	// The checkout lookup (A5) finds the new code.
	require.Equal(t, 200, get(t, host, "/api/products/barcode/2000000000022", tok).Code)
	require.Equal(t, 400, post(tok, "/api/barcodes/generate", map[string]any{"variantIds": []any{}}).Code)
	require.Equal(t, 404, post(tok, "/api/barcodes/generate", map[string]any{"variantIds": []any{999999}}).Code)

	// A10: a PDF; limits and refusals.
	isPDF(t, post(tok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8",
		"items": []any{map[string]any{"variantId": a, "copies": 3}, map[string]any{"variantId": m, "copies": 1}}}))
	isPDF(t, post(tok, "/api/labels/pdf", map[string]any{"layoutId": "roll-38x25", "test": true}))
	require.Equal(t, 400, post(tok, "/api/labels/pdf", map[string]any{"layoutId": "nope",
		"items": []any{map[string]any{"variantId": a, "copies": 1}}}).Code)
	require.Equal(t, 400, post(tok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8",
		"items": []any{map[string]any{"variantId": a, "copies": 0}}}).Code, "nothing to print")
	big := []any{}
	for _, id := range []any{a, b, c, m, a} {
		big = append(big, map[string]any{"variantId": id, "copies": 500})
	}
	tooMany := post(tok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8", "items": big})
	require.Equal(t, 400, tooMany.Code)
	require.Contains(t, tooMany.Raw, "2000")
	require.Equal(t, 404, post(tok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8",
		"items": []any{map[string]any{"variantId": 999999, "copies": 1}}}).Code)

	// A11: a custom layout is validated, saved and printable.
	custom := map[string]any{"id": "shelf-40x30", "name": "Shelf 40 × 30", "pageWidth": 40, "pageHeight": 30,
		"labelWidth": 40, "labelHeight": 30, "columns": 1, "rows": 1, "fields": []string{"name", "price", "barcode"}}
	saved := do(t, rq{method: "PUT", host: host, path: "/api/settings/labels", token: tok, body: map[string]any{"layouts": []any{custom}}})
	require.Equal(t, 200, saved.Code, saved.Raw)
	require.Len(t, get(t, host, "/api/settings/labels", tok).Body["layouts"], 1)
	isPDF(t, post(tok, "/api/labels/pdf", map[string]any{"layoutId": "shelf-40x30",
		"items": []any{map[string]any{"variantId": c, "copies": 2}}}))
	bad := do(t, rq{method: "PUT", host: host, path: "/api/settings/labels", token: tok, body: map[string]any{"layouts": []any{
		map[string]any{"id": "a4-3x8", "name": "Clash", "pageWidth": 40, "pageHeight": 30, "labelWidth": 60, "labelHeight": 30,
			"columns": 1, "rows": 1, "fields": []string{"name"}}}}})
	require.Equal(t, 400, bad.Code)
	require.Contains(t, bad.Raw, "layouts[0].id")
	require.Contains(t, bad.Raw, "layouts[0].columns", "60 mm label on a 40 mm page")

	// P3: a biller may not generate or print (and cannot change layouts).
	createUser(t, l, tok, "biller@"+l+".test", "biller", "lbl-biller-pass-1")
	btok, _ := login(t, l, "biller@"+l+".test", "lbl-biller-pass-1")
	require.Equal(t, 403, post(btok, "/api/barcodes/generate", map[string]any{"variantIds": []any{c}}).Code)
	require.Equal(t, 403, post(btok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8", "test": true}).Code)
	require.Equal(t, 403, do(t, rq{method: "PUT", host: host, path: "/api/settings/labels", token: btok,
		body: map[string]any{"layouts": []any{}}}).Code)

	// The variant matrix gives new variants a barcode on request.
	tee := createProduct(t, l, tok, variableProduct("Tee", "TEE-9", store, []string{"size", "colour"}, nil,
		map[string]any{"sku": "TEE-9-S-RED", "attributes": map[string]any{"size": "S", "colour": "Red"}}))
	require.Equal(t, 201, tee.Code, tee.Raw)
	mpath := fmt.Sprintf("/api/products/%v/variants/matrix", tee.Body["id"])
	grid := map[string]any{"dims": map[string]any{"size": []string{"S", "M"}, "colour": []string{"Red"}}, "price": 99,
		"generateBarcodes": true}
	made := post(tok, mpath, grid)
	require.Equal(t, 201, made.Code, made.Raw)
	tv := variantsOf(t, host, tok, tee.Body["id"])
	require.Equal(t, "2000000000046", tv["TEE-9-M-RED"]["itemBarcode"])
	require.Nil(t, tv["TEE-9-S-RED"]["itemBarcode"], "existing variants are untouched")

	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action IN ('barcode.generate','settings.labels')`).Scan(&audits))
	require.GreaterOrEqual(t, audits, 3)

	// Module off: generation and printing refused (the matrix option too);
	// codes and layouts are kept.
	setModules(t, l, "clothing", "product_attributes")
	require.Equal(t, "MODULE_OFF", codeOf(post(tok, "/api/barcodes/generate", map[string]any{"variantIds": []any{c}})))
	require.Equal(t, "MODULE_OFF", codeOf(post(tok, "/api/labels/pdf", map[string]any{"layoutId": "a4-3x8", "test": true})))
	grid["dims"] = map[string]any{"size": []string{"L"}, "colour": []string{"Red"}}
	offMatrix := post(tok, mpath, grid)
	require.Equal(t, 403, offMatrix.Code, offMatrix.Raw)
	require.Equal(t, "MODULE_OFF", codeOf(offMatrix))
	require.NotContains(t, variantsOf(t, host, tok, tee.Body["id"]), "TEE-9-L-RED", "the refused matrix created nothing")
	require.Len(t, get(t, host, "/api/settings/labels", tok).Body["layouts"], 1)
	require.Equal(t, "2000000000022", variantsOf(t, host, tok, p.Body["id"])["SOCK-A"]["itemBarcode"])
	setModules(t, l, "clothing")

	// Another shop: its own counter and layouts; this shop's variants are
	// 404 there and this shop's token is refused.
	l2 := billingTenant(t, "lbl-other")
	setModules(t, l2, "clothing")
	tok2, _ := ownerLogin(t, l2)
	h2 := tenants[l2].Host
	o := do(t, rq{method: "GET", host: h2, path: "/api/settings/labels", token: tok2})
	require.Equal(t, 200, o.Code)
	require.Empty(t, o.Body["layouts"])
	require.Equal(t, 404, do(t, rq{method: "POST", host: h2, path: "/api/barcodes/generate", token: tok2,
		body: map[string]any{"variantIds": []any{c}}}).Code)
	require.Equal(t, 404, do(t, rq{method: "POST", host: h2, path: "/api/labels/pdf", token: tok2,
		body: map[string]any{"layoutId": "a4-3x8", "items": []any{map[string]any{"variantId": a, "copies": 1}}}}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: h2, path: "/api/labels/pdf", token: tok2,
		body: map[string]any{"layoutId": "shelf-40x30", "test": true}}).Code, "the other shop's layout is unknown here")
	s2 := createProduct(t, l2, tok2, variableProduct("Cap", "", storeIDs(t, l2, tok2)[0], nil, nil, map[string]any{"sku": "CAP-1"}))
	require.Equal(t, 201, s2.Code, s2.Raw)
	g2 := do(t, rq{method: "POST", host: h2, path: "/api/barcodes/generate", token: tok2,
		body: map[string]any{"variantIds": []any{variantsOf(t, h2, tok2, s2.Body["id"])["CAP-1"]["id"]}}})
	require.Equal(t, 200, g2.Code, g2.Raw)
	require.Equal(t, "2000000000015", g2.Body["generated"].([]any)[0].(map[string]any)["barcode"], "each shop has its own counter")
	require.Equal(t, 401, do(t, rq{method: "POST", host: h2, path: "/api/barcodes/generate", token: tok,
		body: map[string]any{"variantIds": []any{c}}}).Code)
}
