//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func reportRows(t *testing.T, l, tok, path string) []map[string]any {
	t.Helper()
	r := get(t, tenants[l].Host, path, tok)
	require.Equal(t, 200, r.Code, r.Raw)
	return list(t, r)
}

func TestSalesReportRowsScopeAndFilters(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	p := stockedProduct(t, "a", tok, "REP-SALE", store, 20)
	createUser(t, "a", tok, "report-biller@iso-a.test", "biller", "report-biller-pass-1")
	btok, _ := login(t, "a", "report-biller@iso-a.test", "report-biller-pass-1")

	own := sell(t, "a", tok, posSale(store, line(p, 2)), nil)
	require.Equal(t, 201, own.Code)
	theirs := sell(t, "a", btok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 201, theirs.Code)
	voided := sell(t, "a", tok, posSale(store, line(p, 1)), nil)
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v", voided.Body["id"]), token: tok}).Code)
	ret := postReturn(t, "a", tok, retBody(own.Body["id"], store, retItem(p, 1)), nil)
	require.Equal(t, 201, ret.Code, ret.Raw)

	base := fmt.Sprintf("/api/sales/report?productId=%s", jsonID(p.pid))
	rows := reportRows(t, "a", tok, base)
	require.Len(t, rows, 2, "voided sales are left out (C-11)")
	byRef := map[any]map[string]any{}
	for _, r := range rows {
		byRef[r["reference"]] = r
	}
	o := byRef[own.Body["reference"]]
	require.NotNil(t, o)
	require.Regexp(t, `^\d+-\d+$`, o["id"])
	require.Regexp(t, `^SO-\d+-\d+$`, o["sku"])
	require.Equal(t, "REP-SALE", o["itemSku"])
	require.Equal(t, "Product REP-SALE", o["product"].(map[string]any)["name"])
	require.Equal(t, "No Brand", o["product"].(map[string]any)["brand"])
	require.Equal(t, "Uncategorized", o["category"])
	require.Equal(t, 2.0, num(o["soldQty"]))
	require.Equal(t, num(lineOf(t, own.Body, p)["subtotal"]), num(o["soldAmount"]))
	require.Equal(t, 1.0, num(o["returnedQty"]))
	require.InDelta(t, num(o["soldAmount"])-num(ret.Body["total"]), num(o["netAmount"]), 0.001)
	require.Equal(t, float64(stockOf(t, "a", store, p.vid)), num(o["stockQty"]), "current stock, not the old 0")
	require.Equal(t, "Walk-in Customer", o["customer"].(map[string]any)["name"])
	require.Equal(t, "Walk-in Customer", o["customerName"])
	require.NotEmpty(t, o["store"].(map[string]any)["name"])

	// Billers see only their own lines (C-10); voided ones on request.
	mine := reportRows(t, "a", btok, base)
	require.Len(t, mine, 1)
	require.Equal(t, theirs.Body["reference"], mine[0]["reference"])
	require.Len(t, reportRows(t, "a", tok, base+"&includeVoided=true"), 3)

	// Filters and paging.
	require.Empty(t, reportRows(t, "a", tok, base+"&startDate=2000-01-01&endDate=2000-12-31"))
	require.Len(t, reportRows(t, "a", tok, base+"&startDate=2000-01-01"), 2)
	require.Empty(t, reportRows(t, "a", tok, base+"&customerId=99999999"))
	require.Empty(t, reportRows(t, "a", tok, base+"&storeId=99999999"))
	paged := get(t, host, base+"&page=1&limit=1", tok)
	require.Equal(t, 2.0, num(paged.Body["pagination"].(map[string]any)["total"]))
	require.Len(t, paged.Body["data"].([]any), 1)
	require.Equal(t, 400, get(t, host, base+"&startDate=yesterday", tok).Code)
}

func TestPurchaseReportRowsAndPermission(t *testing.T) {
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	sup := createSupplier(t, "a", tok, "Report Traders A")
	p := stockedProduct(t, "a", tok, "REP-PUR", store, 0)
	ok := postPurchase(t, "a", tok, purchaseBody(sup, "REP-1", "Received", purchaseLine(p.pid, p.vid, store, 3, 12.5)), nil)
	require.Equal(t, 201, ok.Code, ok.Raw)
	c := postPurchase(t, "a", tok, purchaseBody(sup, "REP-2", "Ordered", purchaseLine(p.pid, p.vid, store, 2, 10)), nil)
	require.Equal(t, 201, c.Code)
	require.Equal(t, 200, putPurchase(t, "a", tok, c.Body["id"], map[string]any{"status": "Cancelled"}).Code)

	base := fmt.Sprintf("/api/purchase/report?supplierId=%s", jsonID(sup))
	rows := reportRows(t, "a", tok, base)
	require.Len(t, rows, 1, "cancelled purchases are left out")
	r := rows[0]
	require.Equal(t, "REP-1", r["reference"])
	require.Regexp(t, `^PO-\d+-\d+$`, r["sku"])
	require.NotEmpty(t, r["dueDate"], "old name for the purchase date")
	require.Equal(t, "Product REP-PUR", r["product"].(map[string]any)["name"])
	require.Equal(t, 3.0, num(r["purchaseQty"]))
	require.Equal(t, 37.5, num(r["purchaseAmount"]))
	require.Equal(t, 3.0, num(r["stockQty"]))
	require.Equal(t, "Received", r["status"])
	require.Equal(t, "Report Traders A", r["supplier"].(map[string]any)["name"])
	require.Len(t, reportRows(t, "a", tok, base+"&includeCancelled=true"), 2)
	require.Empty(t, reportRows(t, "a", tok, base+"&startDate=2000-01-01&endDate=2000-01-02"))

	// Billers have reports:view but not purchases:view: no supplier costs.
	createUser(t, "a", tok, "report-biller2@iso-a.test", "biller", "report-biller2-pass-1")
	btok, _ := login(t, "a", "report-biller2@iso-a.test", "report-biller2-pass-1")
	require.Equal(t, 200, get(t, host, "/api/sales/report", btok).Code)
	require.Equal(t, 403, get(t, host, "/api/purchase/report", btok).Code)
}

func TestReportsIsolationAcrossTenants(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	ch := tenants["c"].Host
	aStore := storeIDs(t, "a", aTok)[0]
	p := stockedProduct(t, "a", aTok, "REP-ISO-A", aStore, 5)
	require.Equal(t, 201, sell(t, "a", aTok, posSale(aStore, line(p, 1)), nil).Code)
	sup := createSupplier(t, "a", aTok, "Secret Report Supplier A")
	require.Equal(t, 201, postPurchase(t, "a", aTok, purchaseBody(sup, "REP-ISO-1", "Pending", purchaseLine(p.pid, p.vid, aStore, 1, 1)), nil).Code)

	s := get(t, ch, "/api/sales/report", cTok)
	require.Equal(t, 200, s.Code)
	require.NotContains(t, s.Raw, "REP-ISO-A")
	pr := get(t, ch, "/api/purchase/report", cTok)
	require.Equal(t, 200, pr.Code)
	require.NotContains(t, pr.Raw, "Secret Report Supplier A")
	require.NotContains(t, pr.Raw, "REP-ISO-1")
	require.Equal(t, 401, get(t, ch, "/api/sales/report", aTok).Code)
	require.Equal(t, 401, get(t, ch, "/api/purchase/report", aTok).Code)
}
