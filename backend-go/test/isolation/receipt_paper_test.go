//go:build integration

package isolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Industry P1 item 10: the shop's receipt paper (DB23) and the HSN code
// snapshot on sale lines (DB24); older lines without one are unchanged.
func TestReceiptPaperAndHSN(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "rcp-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	put := func(body any) resp {
		return do(t, rq{method: "PUT", host: host, path: "/api/settings", token: tok, body: body})
	}

	require.Equal(t, "a4", get(t, host, "/api/settings", tok).Body["receiptPaper"], "default: today's A4")
	r := put(map[string]any{"receiptPaper": "80mm"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "80mm", get(t, host, "/api/settings", tok).Body["receiptPaper"])
	bad := put(map[string]any{"receiptPaper": "letter"})
	require.Equal(t, 400, bad.Code)
	require.Contains(t, bad.Raw, "receiptPaper")
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.audit_log WHERE action = 'settings.update' AND after::text LIKE '%receiptPaper%'`).Scan(&n))
	require.Equal(t, 1, n)

	// A product with an HSN code: its sale lines keep the code even after
	// the product changes; an older line without one stays NULL.
	body := addFormSingle("HSN-1", store, 5)
	body["hsnCode"] = "0902"
	p := createProduct(t, l, tok, body)
	require.Equal(t, 201, p.Code, p.Raw)
	vid := p.Body["ProductVariants"].([]any)[0].(map[string]any)["id"]
	s := sell(t, l, tok, map[string]any{"storeId": store, "orderItems": []map[string]any{
		{"productId": p.Body["id"], "variantId": vid, "quantity": 1, "unitPrice": 120.5, "total": 120.5}}}, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.products SET hsn_code = '6109' WHERE id = $1`, int64(p.Body["id"].(float64)))
	require.NoError(t, err)
	sale := get(t, host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), tok)
	require.Equal(t, 200, sale.Code, sale.Raw)
	require.Equal(t, "0902", sale.Body["items"].([]any)[0].(map[string]any)["hsn_code"])

	plain := stockedProduct(t, l, tok, "NOHSN-1", store, 5)
	s2 := sell(t, l, tok, map[string]any{"storeId": store, "orderItems": []map[string]any{line(plain, 1)}}, nil)
	require.Equal(t, 201, s2.Code, s2.Raw)
	sale2 := get(t, host, fmt.Sprintf("/api/sales/%v", s2.Body["id"]), tok)
	require.Nil(t, sale2.Body["items"].([]any)[0].(map[string]any)["hsn_code"])
}
