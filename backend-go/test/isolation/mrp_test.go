//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// MRP on every variant (tenant migration 00032): kept when an edit leaves
// it out, a warning (never a refusal) when the price with tax is above it,
// a sale-time snapshot on sale lines, and the receipt's MRP and saving only
// when the shop turns them on.
func TestVariantMRP(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "mrp-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	variantOf := func(r resp) map[string]any { return r.Body["ProductVariants"].([]any)[0].(map[string]any) }

	// Price 100 + 18 % tax = 118: at MRP, not above it.
	body := addFormSingle("MRP-1", store, 10)
	v0 := body["variants"].([]map[string]any)[0]
	v0["price"], v0["mrp"] = 100, 118
	p := createProduct(t, l, tok, body)
	require.Equal(t, 201, p.Code, p.Raw)
	v := variantOf(p)
	require.EqualValues(t, 118, num(v["mrp"]))
	require.Equal(t, false, v["aboveMrp"])
	pid, vid := p.Body["id"], v["id"]

	// An edit that leaves MRP out keeps it; 101 + 18 % is above 118: saved, flagged.
	delete(v0, "mrp")
	v0["id"], v0["price"] = vid, 101
	u := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/products/%v", pid), token: tok, body: body})
	require.Equal(t, 200, u.Code, u.Raw)
	v = variantOf(u)
	require.EqualValues(t, 118, num(v["mrp"]), "left out: kept")
	require.Equal(t, true, v["aboveMrp"])

	// A negative MRP is refused.
	v0["mrp"] = -1
	bad := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/products/%v", pid), token: tok, body: body})
	require.Equal(t, 400, bad.Code, bad.Raw)
	require.Contains(t, bad.Raw, "variants[0].mrp")
	delete(v0, "mrp")

	item := func(qty int) map[string]any {
		return map[string]any{"productId": pid, "variantId": vid, "quantity": qty, "unitPrice": 101}
	}
	quote := func(b map[string]any) map[string]any {
		r := do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: tok, body: b})
		require.Equal(t, 200, r.Code, r.Raw)
		return r.Body["lines"].([]any)[0].(map[string]any)
	}
	// 2 × 101 + 18 % = 238.36 against MRP 2 × 118 = 236: warned.
	ql := quote(posSale(store, item(2)))
	require.Equal(t, true, ql["aboveMrp"])
	require.EqualValues(t, 118, num(ql["mrp"]))
	require.EqualValues(t, 236, num(ql["mrpAmount"]))
	require.Contains(t, fmt.Sprint(ql["warnings"]), "above the MRP")
	// A discount that brings the line under MRP clears the warning.
	disc := posSale(store, item(2))
	disc["discount"] = 10
	require.Equal(t, false, quote(disc)["aboveMrp"])

	// The sale goes through and keeps its MRP even after the product changes.
	s := sell(t, l, tok, posSale(store, item(2)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	v0["mrp"] = 200
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/products/%v", pid), token: tok, body: body}).Code)
	sale := get(t, host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), tok)
	it := sale.Body["items"].([]any)[0].(map[string]any)
	require.EqualValues(t, 118, num(it["mrp"]), "sale-time MRP")
	require.EqualValues(t, 236, num(it["mrpAmount"]))
	require.Equal(t, true, it["aboveMrp"])
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'product.mrp_change'`).Scan(&n))
	require.Equal(t, 1, n, "118 -> 200")

	// A product without MRP: no MRP on the line, never above it.
	plain := stockedProduct(t, l, tok, "MRP-NONE", store, 5)
	s2 := sell(t, l, tok, posSale(store, line(plain, 1)), nil)
	require.Equal(t, 201, s2.Code, s2.Raw)
	it2 := get(t, host, fmt.Sprintf("/api/sales/%v", s2.Body["id"]), tok).Body["items"].([]any)[0].(map[string]any)
	require.Nil(t, it2["mrp"])
	require.Equal(t, false, it2["aboveMrp"])

	// Receipts: MRP and the saving only when the shop shows MRP.
	view := func(saleID any) receipts.View {
		var rid int64
		require.NoError(t, admin(t).QueryRow(ctx, `SELECT max(id) FROM `+schema(l)+`.receipts WHERE sale_id = $1`,
			int64(saleID.(float64))).Scan(&rid))
		sch, role, err := tenancy.SchemaFor(tenants[l].ID)
		require.NoError(t, err)
		var out receipts.View
		require.NoError(t, tenancy.NewSessions(tdb.AppPool(t, 1), 0, nil, nil).InTenantTx(ctx,
			tenancy.Tenant{ID: tenants[l].ID, Schema: sch, Role: role}, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
				out, err = receipts.Load(ctx, tx, rid)
				return err
			}))
		return out
	}
	// 101 + 18 % = 119.18 against MRP 200: saved 80.82.
	s3 := sell(t, l, tok, posSale(store, item(1)), nil)
	require.Equal(t, 201, s3.Code, s3.Raw)
	require.Equal(t, false, get(t, host, "/api/settings", tok).Body["showMrpOnReceipt"], "off by default")
	off := view(s3.Body["id"])
	require.Empty(t, off.Lines[0].MRP)
	require.False(t, off.HasSaving)

	r := do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: map[string]any{"showMrpOnReceipt": true}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, true, r.Body["showMrpOnReceipt"])
	on := view(s3.Body["id"])
	require.Contains(t, on.Lines[0].MRP, "200")
	require.True(t, on.HasSaving)
	require.True(t, strings.Contains(on.Saving, "80.82"), on.Saving)
	// The sale above MRP saved nothing: no saving row.
	require.False(t, view(s.Body["id"]).HasSaving)
	pdf, err := receipts.RenderPDF(on, nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
}

// The import templates take an MRP column (the variant's MRP; in the
// pharmacy template it stays the batch's), with a warning for a price
// above it; labels can print it.
func TestImportAndLabelMRP(t *testing.T) {
	l := billingTenant(t, "mrp-imp")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host

	tpl := get(t, host, "/api/products/import/template", tok)
	require.Equal(t, 200, tpl.Code, tpl.Raw)
	require.Contains(t, strings.SplitN(tpl.Raw, "\n", 2)[0], ",mrp,")

	// 100 + 18 % = 118 is at its MRP; 120 + 18 % = 141.60 is above 130: warned, still imported.
	file := workbook(t,
		[]string{"name", "sku", "price", "mrp", "tax_type", "tax_rate"},
		[]string{"MRP tea", "MRPI-1", "100", "118", "Exclusive", "18"},
		[]string{"MRP coffee", "MRPI-2", "120", "130", "Exclusive", "18"},
		[]string{"No MRP", "MRPI-3", "50", "", "", ""},
	)
	bad := uploadFile(t, l, tok, "bad.xlsx", workbook(t, []string{"name", "sku", "price", "mrp"}, []string{"X", "MRPI-X", "1", "-5"}), true)
	require.Equal(t, 400, bad.Code, bad.Raw)
	require.Contains(t, bad.Raw, `"mrp"`)

	dry := uploadFile(t, l, tok, "mrp.xlsx", file, true)
	require.Equal(t, 200, dry.Code, dry.Raw)
	pv := dry.Body["preview"].([]any)
	require.EqualValues(t, 118, num(pv[0].(map[string]any)["variants"].([]any)[0].(map[string]any)["mrp"]))
	warnings := dry.Body["warnings"].([]any)
	require.Len(t, warnings, 1, dry.Raw)
	require.EqualValues(t, 3, num(warnings[0].(map[string]any)["row"]), "the coffee row")
	require.Equal(t, "mrp", warnings[0].(map[string]any)["field"])

	r := uploadFile(t, l, tok, "mrp.xlsx", file, false)
	require.Equal(t, 200, r.Code, r.Raw)
	require.EqualValues(t, 3, r.Body["created"])
	byName := map[string]map[string]any{}
	for _, p := range list(t, get(t, host, "/api/products", tok)) {
		byName[p["name"].(string)] = p["ProductVariants"].([]any)[0].(map[string]any)
	}
	require.EqualValues(t, 118, num(byName["MRP tea"]["mrp"]))
	require.Equal(t, false, byName["MRP tea"]["aboveMrp"])
	require.Equal(t, true, byName["MRP coffee"]["aboveMrp"])
	require.Nil(t, byName["No MRP"]["mrp"])

	// Labels: the MRP field is offered and a sheet with it prints.
	setModules(t, l, "clothing")
	lay := get(t, host, "/api/settings/labels", tok)
	require.Equal(t, 200, lay.Code, lay.Raw)
	require.Contains(t, fmt.Sprint(lay.Body["presets"].([]any)[0].(map[string]any)["fields"]), "mrp")
	pdf := do(t, rq{method: "POST", host: host, path: "/api/labels/pdf", token: tok, body: map[string]any{
		"layoutId": "a4-3x8", "items": []map[string]any{{"variantId": byName["MRP tea"]["id"], "copies": 2},
			{"variantId": byName["No MRP"]["id"], "copies": 1}}}})
	isPDF(t, pdf)
}
