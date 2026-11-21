//go:build integration

package isolation

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend/internal/xlsx"
)

// GST returns (docs/GST_REPORTS_DESIGN.md): a Mumbai shop (27) with a
// Bengaluru store under its own GSTIN (29). The Mumbai GSTIN's GSTR-1 has
// a B2B invoice, a large inter-state B2C invoice (b2cl), small B2C sales
// summed by place and rate (b2cs, a credit note taken off), credit notes
// to the registered buyer (cdnr) and of the b2cl invoice (cdnur), a 0 %
// sale (nil), a voided invoice (docs: cancelled), the HSN summary, and the
// GSTR-3B totals. The Bengaluru sale is in its own GSTIN's report.
func TestGSTReturns(t *testing.T) {
	l := billingTenant(t, "gst-returns")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}
	require.Equal(t, 200, call("PUT", "/api/settings", map[string]any{"taxLabel": "GST", "taxRegistrationNo": gstinMH, "stateCode": "27", "currency": "INR"}).Code)
	mum := storeIDs(t, l, tok)[0]
	blr := call("POST", "/api/stores", map[string]any{"name": "Bengaluru", "gstin": gstinKA})
	require.Equal(t, 201, blr.Code, blr.Raw)

	product := func(sku, tax, hsn string, store float64, qty int) stocked {
		b := addFormSingle(sku, store, qty)
		b["tax"], b["hsnCode"] = tax, hsn
		p := createProduct(t, l, tok, b)
		require.Equal(t, 201, p.Code, p.Raw)
		return stocked{pid: num(p.Body["id"]), vid: num(p.Body["ProductVariants"].([]any)[0].(map[string]any)["id"]), sku: sku}
	}
	tea := product("GR-TEA", "18", "0902", mum, 2000) // 120.50 + 18 %
	rice := product("GR-RICE", "0", "1006", mum, 10)  // 0 %
	kaTea := product("GR-KATEA", "18", "0902", num(blr.Body["id"]), 10)
	kaveri := call("POST", "/api/customers", map[string]any{"name": "Kaveri", "gstin": gstinKA, "legalName": "Kaveri Traders Pvt Ltd"})
	require.Equal(t, 201, kaveri.Code, kaveri.Raw)
	big := call("POST", "/api/customers", map[string]any{"name": "Big buyer", "address": "1 MG Road, Bengaluru"})
	require.Equal(t, 201, big.Code, big.Raw)

	sale := func(store float64, ln map[string]any, place string, customer any) resp {
		t.Helper()
		b := posSale(store, ln)
		b["receivedAmount"], b["amountTendered"] = "200000", 200000
		if place != "" {
			b["placeOfSupply"] = place
		}
		if customer != nil {
			b["customerId"] = customer
		}
		s := sell(t, l, tok, b, nil)
		require.Equal(t, 201, s.Code, s.Raw)
		return s
	}
	ret := func(s resp, store float64, p stocked, q int) {
		t.Helper()
		r := postReturn(t, l, tok, retBody(s.Body["id"], store, map[string]any{"productId": p.pid, "variantId": p.vid, "quantity": q, "reason": "x"}), nil)
		require.Equal(t, 201, r.Code, r.Raw)
	}
	sale(mum, line(tea, 1), "", nil)                         // b2cs 27, 18 %
	toKA := sale(mum, line(tea, 1), "29", nil)               // b2cs 29, 18 % ...
	ret(toKA, mum, tea, 1)                                   // ... and its credit note: b2cs 29 nets to 0
	b2b := sale(mum, line(tea, 2), "", kaveri.Body["id"])    // b2b (supplied in Maharashtra)
	ret(b2b, mum, tea, 1)                                    // cdnr
	large := sale(mum, line(tea, 710), "29", big.Body["id"]) // b2cl: 710 x 142.19 > 1,00,000
	ret(large, mum, tea, 10)                                 // cdnur
	sale(mum, line(rice, 1), "", nil)                        // nil, intra-state B2C
	void := sale(mum, line(tea, 1), "", nil)
	require.Equal(t, 204, call("DELETE", fmt.Sprintf("/api/sales/%v?reason=test", void.Body["id"]), nil).Code)
	sale(num(blr.Body["id"]), line(kaTea, 1), "", nil) // the Karnataka GSTIN's

	now := time.Now()
	period := fmt.Sprintf("from=%s&to=%s", now.AddDate(0, -1, 0).Format("2006-01"), now.Format("2006-01"))
	g := call("GET", "/api/reports/gst/gstins", nil)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Contains(t, g.Raw, gstinMH)
	require.Contains(t, g.Raw, gstinKA)

	r := call("GET", "/api/reports/gst?gstin="+gstinMH+"&"+period, nil)
	require.Equal(t, 200, r.Code, r.Raw)
	rows := func(k string) []map[string]any {
		var out []map[string]any
		for _, x := range r.Body[k].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	b2bRows := rows("b2b")
	require.Len(t, b2bRows, 1)
	require.Equal(t, gstinKA, b2bRows[0]["buyerGstin"])
	require.Equal(t, "241.00", dec(b2bRows[0]["taxable"]).StringFixed(2))
	require.True(t, dec(b2bRows[0]["cgst"]).IsPositive(), "supplied in Maharashtra: CGST + SGST")
	b2cl := rows("b2cl")
	require.Len(t, b2cl, 1)
	require.Equal(t, "29", b2cl[0]["placeOfSupply"])
	require.True(t, dec(b2cl[0]["value"]).GreaterThan(dec(100000)))
	b2cs := map[string]map[string]any{}
	for _, x := range rows("b2cs") {
		b2cs[x["placeOfSupply"].(string)] = x
	}
	require.Equal(t, "120.50", dec(b2cs["27"]["taxable"]).StringFixed(2))
	require.Equal(t, "0.00", dec(b2cs["29"]["taxable"]).StringFixed(2), "the sale to Karnataka less its credit note")
	require.Len(t, rows("cdnr"), 1)
	require.Equal(t, gstinKA, rows("cdnr")[0]["buyerGstin"])
	require.NotEmpty(t, rows("cdnr")[0]["invoice"], "the original invoice")
	cdnur := rows("cdnur")
	require.Len(t, cdnur, 1)
	require.Equal(t, "1205.00", dec(cdnur[0]["taxable"]).StringFixed(2), "10 of the b2cl invoice")
	nilR := r.Body["nil"].(map[string]any)
	require.Equal(t, "120.50", dec(nilR["intraB2c"]).StringFixed(2))
	var invoiceDoc map[string]any
	for _, d := range rows("docs") {
		if d["kind"] == "invoice" {
			invoiceDoc = d
		}
	}
	require.NotNil(t, invoiceDoc)
	require.EqualValues(t, 6, invoiceDoc["issued"], "six Mumbai invoices, the void one included")
	require.EqualValues(t, 1, invoiceDoc["cancelled"])
	hsn := map[string]map[string]any{}
	for _, h := range rows("hsn") {
		hsn[h["hsn"].(string)] = h
	}
	require.Contains(t, hsn, "0902")
	require.Equal(t, "OTH", hsn["0902"]["uqc"], "no unit: OTH")
	// 1 + 1 - 1 + 2 - 1 + 710 - 10 (the void sale left out).
	require.Equal(t, 702.0, num(hsn["0902"]["quantity"]))
	g3 := r.Body["gstr3b"].(map[string]any)
	outward := g3["outward"].(map[string]any)
	// b2b 241 + b2cl 85,555 + b2cs 120.50 + 0 - cdnr 120.50 - cdnur 1,205.
	require.Equal(t, "84591.00", dec(outward["taxable"]).StringFixed(2))
	require.Equal(t, "120.50", dec(g3["nil"]).StringFixed(2))
	require.Contains(t, r.Raw, "GST unit code", "a warning for units without a UQC")

	// The Karnataka GSTIN has only its store's sale.
	k := call("GET", "/api/reports/gst?gstin="+gstinKA+"&"+period, nil)
	require.Equal(t, 200, k.Code, k.Raw)
	require.Len(t, k.Body["b2cs"], 1)

	// Refused: a GSTIN the shop does not have, a bad period, own-sales staff.
	require.Equal(t, 400, call("GET", "/api/reports/gst?gstin=07AAACI1681G1ZN&"+period, nil).Code)
	require.Equal(t, 400, call("GET", "/api/reports/gst?gstin="+gstinMH+"&from=2026-13", nil).Code)
	createUser(t, l, tok, "gst-biller@iso.test", "biller", "gst-biller-pass-12")
	btok, _ := login(t, l, "gst-biller@iso.test", "gst-biller-pass-12")
	require.Equal(t, 403, do(t, rq{method: "GET", host: host, path: "/api/reports/gst?gstin=" + gstinMH + "&" + period, token: btok}).Code)

	// The workbook: a summary first, then the GSTR-1 sheets.
	x := call("GET", "/api/reports/gst/export?gstin="+gstinMH+"&"+period, nil)
	require.Equal(t, 200, x.Code, x.Raw)
	require.Contains(t, x.Header.Get("Content-Disposition"), "GSTR1-"+gstinMH)
	sum, err := xlsx.Read([]byte(x.Raw))
	require.NoError(t, err)
	require.Equal(t, "GSTR-1 workbook", sum[0][0])
	zr, err := zip.NewReader(bytes.NewReader([]byte(x.Raw)), int64(len(x.Raw)))
	require.NoError(t, err)
	f, err := zr.Open("xl/workbook.xml")
	require.NoError(t, err)
	wb, _ := io.ReadAll(f)
	for _, sh := range []string{"summary", "b2b", "b2cl", "b2cs", "cdnr", "cdnur", "hsn", "nil", "docs"} {
		require.Contains(t, string(wb), `name="`+sh+`"`, "sheet %s", sh)
	}
}
