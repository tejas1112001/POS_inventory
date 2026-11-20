//go:build integration

package isolation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 8: the pharmacy import template (A35, design §8).

func TestBatchImport(t *testing.T) {
	ctx := context.Background()
	l, tok, s1 := pharmacyShop(t, "bi-imp")
	host := tenants[l].Host
	st := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Second branch", "status": "Active"}})
	require.Equal(t, 201, st.Code, st.Raw)
	s2, slug2 := st.Body["id"].(float64), st.Body["slug"].(string)

	// The template.
	tpl := do(t, rq{method: "GET", host: host, path: "/api/products/import/template?type=pharmacy&format=xlsx", token: tok})
	require.Equal(t, 200, tpl.Code, tpl.Raw)
	head := sheetRows(t, tpl.Raw)[0]
	for _, c := range []string{"batch_no", "expiry", "mfg_date", "mrp", "opening_qty", "store"} {
		require.Contains(t, head, c)
	}
	csvTpl := do(t, rq{method: "GET", host: host, path: "/api/products/import/template?type=pharmacy", token: tok})
	require.Equal(t, 200, csvTpl.Code)
	require.True(t, strings.HasPrefix(csvTpl.Raw, "name,sku,barcode"), csvTpl.Raw)

	hdr := []string{"name", "sku", "barcode", "price", "cost", "tax_rate", "batch_no", "expiry", "mfg_date", "mrp", "store", "opening_qty"}
	exp := func(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02") }
	soon := exp(30)
	later := time.Now().UTC().AddDate(2, 0, 0)
	monthYear := later.Format("01/2006")

	// Errors: bad dates, a reserved batch number, a batch repeated in one
	// store, an expiry that differs, product values that differ.
	bad := workbook(t, hdr,
		[]string{"Para 500", "BI-PARA", "", "30", "18", "12", "P1", "31/02/2027", "", "", "", "5"},
		[]string{"Para 500", "BI-PARA", "", "30", "18", "12", "UNBATCHED", soon, "", "", "", "5"},
		[]string{"Para 500", "BI-PARA", "", "30", "18", "12", "P2", soon, "", "", "", "5"},
		[]string{"Para 500", "BI-PARA", "", "30", "18", "12", "P2", soon, "", "", "", "1"},
		[]string{"Para 500", "BI-PARA", "", "30", "18", "12", "P3", soon, "", "", "", "1"},
		[]string{"Para 500", "BI-PARA", "", "30", "18", "12", "P3", exp(60), "", "", slug2, "1"},
		[]string{"Para 500 strip", "BI-PARA", "", "31", "18", "12", "P4", soon, exp(10), "-1", "", "1"},
		[]string{"Cough syrup", "BI-SYR", "", "90", "60", "12", "S1", "", "", "", "", "1"},
	)
	r := uploadFile(t, l, tok, "bad.xlsx", bad, true)
	require.Equal(t, 400, r.Code, r.Raw)
	fields := map[string]bool{}
	for _, e := range r.Body["errors"].([]any) {
		m := e.(map[string]any)
		fields[strings.TrimSuffix(strings.Split(m["message"].(string), " ")[0], ":")+"@"+m["field"].(string)] = true
	}
	for _, want := range []string{"expiry@expiry", "UNBATCHED@batch_no", "batch@batch_no", "batch@expiry", "name@name", "price@price",
		"mfg_date@mfg_date", "must@mrp"} {
		require.True(t, fields[want], "missing %s in %s", want, r.Raw)
	}
	require.Contains(t, r.Raw, `"row":9`, "a missing expiry")

	// A good file: a product with three batches over two stores, one
	// expired row (reported, not imported), one near-expiry row (imported,
	// warned), MM/YYYY dates.
	good := workbook(t, hdr,
		[]string{"Para 500", "BI-PARA", "8901234500011", "30", "18", "12", "P1", monthYear, "01/2025", "32", "", "10"},
		[]string{"Para 500", "BI-PARA", "8901234500011", "30", "18", "12", "P2", soon, "", "31", "", "4"},
		[]string{"Para 500", "BI-PARA", "8901234500011", "30", "18", "12", "P1", monthYear, "", "32", slug2, "6"},
		[]string{"Para 500", "BI-PARA", "8901234500011", "30", "18", "12", "OLD", exp(-3), "", "", "", "7"},
		[]string{"Cough syrup", "BI-SYR", "", "90", "60", "12", "S1", exp(400), "", "95", slug2, "3"},
	)
	r = uploadFile(t, l, tok, "good.xlsx", good, true)
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, true, r.Body["dryRun"])
	require.EqualValues(t, 2, r.Body["created"])
	require.EqualValues(t, 2, r.Body["variants"])
	warns := r.Body["warnings"].([]any)
	require.Len(t, warns, 2, r.Raw)
	require.Contains(t, warns[0].(map[string]any)["message"], "within 90 days")
	require.EqualValues(t, 3, warns[0].(map[string]any)["row"])
	require.Contains(t, warns[1].(map[string]any)["message"], "not imported")
	pv := r.Body["preview"].([]any)[0].(map[string]any)["variants"].([]any)[0].(map[string]any)
	require.Len(t, pv["batches"], 4)
	require.Len(t, pv["stock"], 2)
	// The dry run saved nothing.
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.batches`).Scan(&n))
	require.Zero(t, n)

	r = uploadFile(t, l, tok, "good.xlsx", good, false)
	require.Equal(t, 200, r.Code, r.Raw)
	var vid float64
	var tracks bool
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT v.id::float8, p.tracks_batches FROM `+schema(l)+`.product_variants v
		JOIN `+schema(l)+`.products p ON p.id = v.product_id WHERE v.sku = 'BI-PARA'`).Scan(&vid, &tracks))
	require.True(t, tracks)
	p1, p1exp := batchByNo(t, l, vid, "P1")
	require.Equal(t, time.Date(later.Year(), later.Month()+1, 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), p1exp, "MM/YYYY is the month's last day")
	p2, _ := batchByNo(t, l, vid, "P2")
	require.Equal(t, 10, batchQty(t, l, p1, s1))
	require.Equal(t, 6, batchQty(t, l, p1, s2))
	require.Equal(t, 4, batchQty(t, l, p2, s1))
	var old int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.batches WHERE batch_no = 'OLD'`).Scan(&old))
	require.Zero(t, old, "the expired row is not imported")
	// Totals per store equal the batch sum; no unknown batch.
	require.Equal(t, 14, stockOf(t, l, s1, vid))
	require.Equal(t, 6, stockOf(t, l, s2, vid))
	var unk int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT coalesce(sum(s.qty), 0)::int FROM `+schema(l)+`.batch_stock s
		JOIN `+schema(l)+`.batches b ON b.id = s.batch_id WHERE b.variant_id = $1 AND b.is_unknown`, int64(vid)).Scan(&unk))
	require.Zero(t, unk, "nothing in the unknown batch")
	var mrp, mfg string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT mrp::text, to_char(mfg_date, 'YYYY-MM-DD') FROM `+schema(l)+`.batches WHERE id = $1`, p1).Scan(&mrp, &mfg))
	require.Equal(t, "32.00", mrp)
	require.Equal(t, "2025-01-01", mfg)
	require.Empty(t, reconcile(t, l))

	// The same SKU again: refused (it exists now).
	r = uploadFile(t, l, tok, "again.xlsx", good, true)
	require.Equal(t, 400, r.Code)
	require.Contains(t, r.Raw, "already exists")

	// The retail template in a pharmacy shop: tracked, stock in the unknown batch.
	retail := workbook(t, []string{"name", "sku", "price", "store", "opening_qty"}, []string{"Bandage", "BI-BAND", "20", slug2, "8"})
	r = uploadFile(t, l, tok, "retail.xlsx", retail, false)
	require.Equal(t, 200, r.Code, r.Raw)
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT v.id::float8, p.tracks_batches FROM `+schema(l)+`.product_variants v
		JOIN `+schema(l)+`.products p ON p.id = v.product_id WHERE v.sku = 'BI-BAND'`).Scan(&vid, &tracks))
	require.True(t, tracks)
	require.Equal(t, 8, batchQty(t, l, unknownBatch(t, l, vid), s2))
	require.Empty(t, reconcile(t, l))

	// Module off: a pharmacy file is refused; a retail file imports as before (untracked).
	l2, tok2, store2 := pharmacyShop(t, "bi-imp2")
	require.Equal(t, 200, putBusiness(t, l2, tok2, map[string]any{"modules": []string{}}).Code)
	r = uploadFile(t, l2, tok2, "good.xlsx", good, true)
	require.Equal(t, 403, r.Code, r.Raw)
	st2 := do(t, rq{method: "POST", host: tenants[l2].Host, path: "/api/stores", token: tok2, body: map[string]any{"name": "Second branch", "status": "Active"}})
	require.Equal(t, 201, st2.Code, st2.Raw)
	store2 = st2.Body["id"].(float64)
	retail = workbook(t, []string{"name", "sku", "price", "store", "opening_qty"}, []string{"Bandage", "BI-BAND", "20", st2.Body["slug"].(string), "8"})
	r = uploadFile(t, l2, tok2, "retail.xlsx", retail, false)
	require.Equal(t, 200, r.Code, r.Raw)
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT v.id::float8, p.tracks_batches FROM `+schema(l2)+`.product_variants v
		JOIN `+schema(l2)+`.products p ON p.id = v.product_id WHERE v.sku = 'BI-BAND'`).Scan(&vid, &tracks))
	require.False(t, tracks)
	require.Equal(t, 8, stockOf(t, l2, store2, vid))
}
