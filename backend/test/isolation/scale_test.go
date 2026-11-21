//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ean13 adds the EAN-13 check digit to 12 digits.
func ean13(first12 string) string {
	sum := 0
	for i, c := range first12 {
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return first12 + string(rune('0'+(10-sum%10)%10))
}

// Scale barcodes (tenant 00044): the shop's rules read a weighing scale's
// label (21 + 5-digit scale code + grams; 22 + 4-digit code + paise); the
// POS lookup returns the product and the quantity, by weight or by the
// label price at the store's price. A whole-unit product refuses a part
// quantity; scale codes are unique.
func TestScaleBarcodes(t *testing.T) {
	l := billingTenant(t, "scale")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}
	store := storeIDs(t, l, tok)[0]

	r := call("PUT", "/api/settings", map[string]any{"scaleBarcodes": []map[string]any{{"prefix": "19", "itemDigits": 5, "value": "weight", "decimals": 3}}})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "scaleBarcodes[0]", errField(r))
	r = call("PUT", "/api/settings", map[string]any{"scaleBarcodes": []map[string]any{
		{"prefix": "21", "itemDigits": 5, "value": "weight", "decimals": 3},
		{"prefix": "22", "itemDigits": 4, "value": "price", "decimals": 2},
	}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Len(t, r.Body["scaleBarcodes"], 2)

	loose := func(sku string) stocked {
		b := addFormSingle(sku, store, 0)
		b["allowFraction"] = true
		b["variants"].([]map[string]any)[0]["price"] = 60
		p := createProduct(t, l, tok, b)
		require.Equal(t, 201, p.Code, p.Raw)
		return stocked{pid: num(p.Body["id"]), vid: num(p.Body["ProductVariants"].([]any)[0].(map[string]any)["id"]), sku: sku}
	}
	setScale := func(p stocked, c string) resp {
		return call("PUT", "/api/products/"+jsonID(p.pid)+"/barcodes", map[string]any{"scaleCodes": []map[string]any{{"variantId": p.vid, "scaleCode": c}}})
	}
	rice, dal := loose("SC-RICE"), loose("SC-DAL")
	pen := stockedProduct(t, l, tok, "SC-PEN", store, 5)
	r = setScale(rice, "00042")
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "00042", r.Body["ProductVariants"].([]any)[0].(map[string]any)["scaleCode"])
	r = setScale(dal, "00042")
	require.Equal(t, 400, r.Code, "taken: %s", r.Raw)
	require.Equal(t, "scaleCodes[0].scaleCode", errField(r))
	require.Equal(t, 400, setScale(dal, "42").Code, "4 to 6 digits")
	require.Equal(t, 200, setScale(dal, "0077").Code)
	require.Equal(t, 200, setScale(pen, "00099").Code)

	lookup := func(code string) resp {
		return call("GET", "/api/products/barcode/"+code+"?storeId="+jsonID(store), nil)
	}
	// 1250 g of rice.
	x := lookup(ean13("210004201250"))
	require.Equal(t, 200, x.Code, x.Raw)
	require.Equal(t, rice.vid, num(x.Body["variantId"]))
	require.Equal(t, 1.25, num(x.Body["quantity"]))
	require.Equal(t, "weight", x.Body["scale"])
	// ₹75.00 of dal at ₹60 a kg: 1.25 kg.
	x = lookup(ean13("220077007500"))
	require.Equal(t, 200, x.Code, x.Raw)
	require.Equal(t, dal.vid, num(x.Body["variantId"]))
	require.Equal(t, 1.25, num(x.Body["quantity"]))
	require.Equal(t, "price", x.Body["scale"])
	// A pen sold whole: 0.5 is refused; 2000 g would be 2 and is fine.
	require.Equal(t, 409, lookup(ean13("210009900500")).Code)
	require.Equal(t, 2.0, num(lookup(ean13("210009902000")).Body["quantity"]))
	// Unknown scale code, a bad check digit, and no rule for 23.
	require.Equal(t, 404, lookup(ean13("210005501250")).Code)
	good := ean13("210004201250")
	require.Equal(t, 404, lookup(good[:12]+string(rune('0'+(good[12]-'0'+1)%10))).Code)
	require.Equal(t, 404, lookup(ean13("230004201250")).Code)

	// The other barcodes are kept when only scale codes are sent.
	require.Equal(t, 200, call("PUT", "/api/products/"+jsonID(rice.pid)+"/barcodes", map[string]any{"barcodes": []map[string]any{{"variantId": rice.vid, "code": "RICE-OLD"}}}).Code)
	require.Equal(t, 200, setScale(rice, "00042").Code)
	require.Equal(t, 200, lookup("RICE-OLD").Code)
}
