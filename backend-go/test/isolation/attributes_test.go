//go:build integration

package isolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// variableProduct is a product body with variant dimensions (item 4, A7).
func variableProduct(name, article string, store float64, dims []string, attrs map[string]any, variants ...map[string]any) map[string]any {
	for _, v := range variants {
		if _, ok := v["price"]; !ok {
			v["price"] = 120.5
		}
		if _, ok := v["inventories"]; !ok {
			v["inventories"] = []map[string]any{{"storeId": store, "qty": 5, "quantityAlert": 1}}
		}
	}
	b := map[string]any{"name": name, "productType": "variable", "status": "Active", "taxType": "Exclusive", "tax": 18,
		"variants": variants}
	if dims != nil {
		b["variantDims"] = dims
	}
	if attrs != nil {
		b["attributes"] = attrs
	}
	if article != "" {
		b["articleNo"] = article
	}
	return b
}

func attrIDs(t *testing.T, r resp) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, d := range list(t, r) {
		out[d["key"].(string)] = d["id"]
	}
	return out
}

// Industry P1 item 4: attribute definitions (A6), typed product attributes
// (A7), the variant matrix (A8), the sale line label (DB8), MODULE_OFF and
// isolation.
func TestAttributesAndVariantMatrix(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "attr-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]

	// A retail shop: the module is off.
	off := get(t, host, "/api/attributes", tok)
	require.Equal(t, 403, off.Code)
	require.Equal(t, "MODULE_OFF", codeOf(off))

	// Clothing seeds its attributes and switches the module on.
	setModules(t, l, "clothing")
	defs := get(t, host, "/api/attributes", tok)
	require.Equal(t, 200, defs.Code, defs.Raw)
	ids := attrIDs(t, defs)
	for _, k := range []string{"department", "collection", "season", "size", "colour"} {
		require.Contains(t, ids, k)
	}
	one := get(t, host, fmt.Sprintf("/api/attributes/%v", ids["size"]), tok)
	require.Equal(t, 200, one.Code)
	require.Equal(t, "variant", one.Body["level"])

	// A new shop's biller may view, not change.
	createUser(t, l, tok, "biller@"+l+".test", "biller", "attr-biller-pass-1")
	btok, _ := login(t, l, "biller@"+l+".test", "attr-biller-pass-1")
	require.Equal(t, 200, get(t, host, "/api/attributes", btok).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/attributes", token: btok,
		body: map[string]any{"label": "Fit", "level": "variant", "kind": "text"}}).Code)

	// Create, duplicate, invalid.
	fit := do(t, rq{method: "POST", host: host, path: "/api/attributes", token: tok,
		body: map[string]any{"label": "Fit", "level": "variant", "kind": "choice", "choices": []string{"Slim", "Regular"}}})
	require.Equal(t, 201, fit.Code, fit.Raw)
	require.Equal(t, "fit", fit.Body["key"])
	require.Equal(t, 409, do(t, rq{method: "POST", host: host, path: "/api/attributes", token: tok,
		body: map[string]any{"key": "FIT", "label": "Fit 2", "level": "variant", "kind": "text"}}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/attributes", token: tok,
		body: map[string]any{"label": "Sleeve", "level": "variant", "kind": "choice"}}).Code, "a choice attribute needs choices")

	// A product with dimensions: validated, values spelled as defined.
	bad := createProduct(t, l, tok, variableProduct("Tee bad", "", store, []string{"size", "colour"}, nil,
		map[string]any{"sku": "TEE-BAD-1", "attributes": map[string]any{"size": "XXXXL", "colour": "Red"}}))
	require.Equal(t, 400, bad.Code)
	require.Contains(t, bad.Raw, "variants[0].attributes.size")
	dup := createProduct(t, l, tok, variableProduct("Tee dup", "", store, []string{"size", "colour"}, nil,
		map[string]any{"sku": "TEE-DUP-1", "attributes": map[string]any{"size": "M", "colour": "Red"}},
		map[string]any{"sku": "TEE-DUP-2", "attributes": map[string]any{"size": "m", "colour": "red"}}))
	require.Equal(t, 400, dup.Code, "the same size and colour twice")

	p := createProduct(t, l, tok, variableProduct("Crew tee", "TSH-01", store, []string{"size", "colour"},
		map[string]any{"season": "spring/summer", "department": "Men"},
		map[string]any{"sku": "TSH-01-S-RED", "attributes": map[string]any{"size": "s", "colour": "Red"}}))
	require.Equal(t, 201, p.Code, p.Raw)
	pid := p.Body["id"]
	require.Equal(t, "Spring/Summer", p.Body["attributes"].(map[string]any)["season"])
	require.Equal(t, []any{"size", "colour"}, p.Body["variantDims"])
	require.Equal(t, "TSH-01", p.Body["articleNo"])
	v0 := p.Body["ProductVariants"].([]any)[0].(map[string]any)
	require.Equal(t, "S / Red", v0["label"])
	require.Equal(t, "S", v0["attributes"].(map[string]any)["size"])
	require.Contains(t, names(list(t, get(t, host, "/api/products?search=tsh-01", tok))), "Crew tee", "search by article number")

	// A plain product in the same shop still saves its free attributes.
	plain := createProduct(t, l, tok, variableProduct("Plain bag", "", store, nil, nil,
		map[string]any{"sku": "BAG-1", "attributes": map[string]any{"Material": "Jute"}}))
	require.Equal(t, 201, plain.Code, plain.Raw)
	pv := plain.Body["ProductVariants"].([]any)[0].(map[string]any)
	require.Equal(t, "Jute", pv["attributes"].(map[string]any)["Material"])
	require.Nil(t, pv["label"])

	// Matrix: dry run plans 3 (S / Red exists), creates nothing.
	mpath := fmt.Sprintf("/api/products/%v/variants/matrix", pid)
	grid := map[string]any{"dims": map[string]any{"size": []string{"S", "M"}, "colour": []string{"Red", "Blue"}}, "price": 120.5, "cost": 60}
	dry := do(t, rq{method: "POST", host: host, path: mpath + "?dryRun=true", token: tok, body: grid})
	require.Equal(t, 200, dry.Code, dry.Raw)
	require.Len(t, dry.Body["planned"], 3)
	require.Equal(t, []any{"S / Red"}, dry.Body["skipped"])
	var skus []string
	for _, x := range dry.Body["planned"].([]any) {
		skus = append(skus, x.(map[string]any)["sku"].(string))
	}
	require.Equal(t, []string{"TSH-01-S-BLUE", "TSH-01-M-RED", "TSH-01-M-BLUE"}, skus)
	require.Len(t, get(t, host, fmt.Sprintf("/api/products/%v", pid), tok).Body["ProductVariants"], 1, "a dry run creates nothing")

	made := do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: grid})
	require.Equal(t, 201, made.Code, made.Raw)
	require.Equal(t, 3.0, num(made.Body["created"]))
	pv4 := made.Body["product"].(map[string]any)["ProductVariants"].([]any)
	require.Len(t, pv4, 4)
	for _, v := range pv4 {
		inv := v.(map[string]any)["Inventories"].([]any)
		require.NotEmpty(t, inv, "stock row in the product's store")
	}
	again := do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: grid})
	require.Equal(t, 201, again.Code)
	require.Equal(t, 0.0, num(again.Body["created"]))
	require.Len(t, again.Body["skipped"], 4)

	// Collisions and bad patterns.
	clash := do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: map[string]any{
		"dims": map[string]any{"size": []string{"L"}, "colour": []string{"Red"}}, "price": 1, "skuPattern": "TSH-01-S-RED"}})
	require.Equal(t, 409, clash.Code, clash.Raw)
	require.Contains(t, clash.Raw, "TSH-01-S-RED")
	require.Equal(t, 409, do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: map[string]any{
		"dims": map[string]any{"size": []string{"L", "XL"}, "colour": []string{"Red"}}, "price": 1, "skuPattern": "TSH-{colour}"}}).Code,
		"two variants would get the same SKU")
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: map[string]any{
		"dims": map[string]any{"size": []string{"L"}, "colour": []string{"Red"}}, "price": 1, "skuPattern": "{fit}-{size}"}}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: map[string]any{
		"dims": map[string]any{"size": []string{"XXXXL"}, "colour": []string{"Red"}}, "price": 1}}).Code)
	require.Equal(t, 409, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/products/%v/variants/matrix", plain.Body["id"]), token: tok,
		body: grid}).Code, "no dimensions yet")
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: mpath, token: btok, body: grid}).Code, "biller: no products:edit")

	// An older client's update (no attributes fields) keeps them.
	full := get(t, host, fmt.Sprintf("/api/products/%v", pid), tok)
	var vs []map[string]any
	for _, x := range full.Body["ProductVariants"].([]any) {
		v := x.(map[string]any)
		vs = append(vs, map[string]any{"id": v["id"], "sku": v["sku"], "price": v["price"], "attributes": v["attributes"]})
	}
	up := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/products/%v", pid), token: tok,
		body: map[string]any{"name": "Crew tee", "productType": "variable", "variants": vs}})
	require.Equal(t, 200, up.Code, up.Raw)
	require.Equal(t, []any{"size", "colour"}, up.Body["variantDims"])
	require.Equal(t, "TSH-01", up.Body["articleNo"])
	require.Equal(t, "Men", up.Body["attributes"].(map[string]any)["department"])

	// The sale line keeps "S / Red" even after the choice list changes.
	sale := sell(t, l, tok, posSale(store, line(stocked{pid: pid.(float64), vid: v0["id"].(float64), sku: "TSH-01-S-RED"}, 1)), nil)
	require.Equal(t, 201, sale.Code, sale.Raw)
	item := sale.Body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "S / Red", item["variantLabel"])

	// A choice in use cannot be removed or renamed; adding one is fine.
	spath := fmt.Sprintf("/api/attributes/%v", ids["size"])
	inUse := do(t, rq{method: "PUT", host: host, path: spath, token: tok,
		body: map[string]any{"label": "Size", "choices": []string{"Small", "M", "L", "XL", "XXL", "3XL"}}})
	require.Equal(t, 409, inUse.Code, inUse.Raw)
	require.Contains(t, inUse.Raw, `"S"`)
	grow := do(t, rq{method: "PUT", host: host, path: spath, token: tok,
		body: map[string]any{"label": "Size (clothing)", "choices": []string{"XXS", "XS", "S", "M", "L", "XL", "XXL", "3XL"}}})
	require.Equal(t, 200, grow.Code, grow.Raw)
	require.Equal(t, "Size (clothing)", grow.Body["label"])
	require.Equal(t, 400, do(t, rq{method: "PUT", host: host, path: spath, token: tok,
		body: map[string]any{"label": "Size", "kind": "text"}}).Code, "kind is fixed")
	require.Equal(t, 409, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/attributes/%v", ids["colour"]), token: tok}).Code)
	require.Equal(t, 200, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/attributes/%v", fit.Body["id"]), token: tok}).Code)
	var audits int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action IN
		('attribute.create','attribute.update','attribute.delete','product.variants_matrix')`).Scan(&audits))
	require.GreaterOrEqual(t, audits, 4)

	// Module off: routes refused, data kept and still shown on products.
	r := putBusiness(t, l, tok, map[string]any{"modules": []string{}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "MODULE_OFF", codeOf(get(t, host, "/api/attributes", tok)))
	require.Equal(t, "MODULE_OFF", codeOf(do(t, rq{method: "POST", host: host, path: mpath, token: tok, body: grid})))
	kept := get(t, host, fmt.Sprintf("/api/products/%v", pid), tok)
	require.Equal(t, "S / Red", kept.Body["ProductVariants"].([]any)[0].(map[string]any)["label"])

	// Another shop: its own presets, none of this shop's data; this shop's
	// IDs give 404 there and its token is refused.
	l2 := billingTenant(t, "attr-other")
	setModules(t, l2, "footwear")
	tok2, _ := ownerLogin(t, l2)
	h2 := tenants[l2].Host
	other := get(t, h2, "/api/attributes", tok2)
	require.Equal(t, 200, other.Code)
	require.NotContains(t, other.Raw, "Size (clothing)")
	require.Contains(t, attrIDs(t, other), "size_system")
	require.Equal(t, 404, get(t, h2, fmt.Sprintf("/api/attributes/%v", fit.Body["id"]), tok2).Code)
	require.Equal(t, 404, do(t, rq{method: "PUT", host: h2, path: fmt.Sprintf("/api/attributes/%v", fit.Body["id"]), token: tok2,
		body: map[string]any{"label": "x", "choices": []string{"a"}}}).Code)
	require.Equal(t, 404, do(t, rq{method: "DELETE", host: h2, path: fmt.Sprintf("/api/attributes/%v", fit.Body["id"]), token: tok2}).Code)
	require.Equal(t, 404, do(t, rq{method: "POST", host: h2, path: mpath, token: tok2, body: grid}).Code)
	require.Equal(t, 401, get(t, h2, "/api/attributes", tok).Code)
}
