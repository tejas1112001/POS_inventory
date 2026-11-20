//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func postReceipt(t *testing.T, l, tok string, id any, body map[string]any, hdr map[string]string) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: fmt.Sprintf("/api/purchases/%v/receipts", id), token: tok, body: body, header: hdr})
}

func rline(item any, qty int) map[string]any {
	return map[string]any{"purchaseItemId": item, "quantity": qty}
}

// Industry P1 item 8 (A16, A17): partial then full receiving, over-receiving
// refused, the old PUT path receives the rest, cancel of a partly received
// purchase refused, idempotency and concurrency, the ledger and the report.
func TestPurchasePartialReceiving(t *testing.T) {
	ctx := context.Background()
	tok, _ := ownerLogin(t, "a")
	host := tenants["a"].Host
	store := storeIDs(t, "a", tok)[0]
	sup := createSupplier(t, "a", tok, "Partial Traders A")
	p1 := stockedProduct(t, "a", tok, "PUR-PART-1", store, 0)
	p2 := stockedProduct(t, "a", tok, "PUR-PART-2", store, 0)
	c := postPurchase(t, "a", tok, purchaseBody(sup, "PART-1", "Ordered",
		purchaseLine(p1.pid, p1.vid, store, 100, 10), purchaseLine(p2.pid, p2.vid, store, 5, 20)), nil)
	require.Equal(t, 201, c.Code, c.Raw)
	id := c.Body["id"]
	items := c.Body["items"].([]any)
	i1, i2 := items[0].(map[string]any)["id"], items[1].(map[string]any)["id"]
	require.Equal(t, 0.0, items[0].(map[string]any)["received_qty"])

	// 60 of 100 today.
	r := postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(i1, 60)}, "note": "first truck"}, map[string]string{"Idempotency-Key": "grn-part-0001"})
	require.Equal(t, 201, r.Code, r.Raw)
	rc := r.Body["receipt"].(map[string]any)
	require.Regexp(t, `^GRN-\d{6}$`, rc["reference"])
	require.Equal(t, "first truck", rc["note"])
	require.Len(t, rc["lines"], 1)
	pu := r.Body["purchase"].(map[string]any)
	require.Equal(t, "Partially received", pu["status"])
	require.Nil(t, pu["received_at"])
	require.Equal(t, 60.0, pu["items"].([]any)[0].(map[string]any)["received_qty"])
	require.Equal(t, 60, stockOf(t, "a", store, p1.vid))
	again := postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(i1, 60)}, "note": "first truck"}, map[string]string{"Idempotency-Key": "grn-part-0001"})
	require.Equal(t, 201, again.Code)
	require.Equal(t, "true", again.Header.Get("Idempotent-Replayed"))
	require.Equal(t, 60, stockOf(t, "a", store, p1.vid), "replay stocks nothing")

	// Over-receiving, unknown lines, duplicates and zero are refused.
	over := postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(i1, 41)}}, nil)
	require.Equal(t, 400, over.Code, over.Raw)
	require.Contains(t, over.Raw, "At most 40 still to receive")
	require.Equal(t, 400, postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(i1, 1), rline(i1, 1)}}, nil).Code)
	require.Equal(t, 400, postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(999999, 1)}}, nil).Code)
	require.Equal(t, 400, postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(i1, 0)}}, nil).Code)
	require.Equal(t, 400, postReceipt(t, "a", tok, id, map[string]any{"lines": []any{}}, nil).Code)

	// A partly received purchase cannot be cancelled, reopened or set to
	// Partially received by hand; paying and the note still work.
	for _, s := range []string{"Cancelled", "Pending", "Ordered"} {
		x := putPurchase(t, "a", tok, id, map[string]any{"status": s})
		require.Equal(t, 409, x.Code, s+": "+x.Raw)
		require.Equal(t, "INVALID_STATE", x.Body["code"])
	}
	keep := putPurchase(t, "a", tok, id, map[string]any{"status": "Partially received", "paid": 100, "note": "part paid"})
	require.Equal(t, 200, keep.Code, keep.Raw)
	require.Equal(t, "Partially received", keep.Body["status"])

	// Concurrent receipts of the last 40 + 5: exactly one wins per piece.
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for range 6 {
		wg.Go(func() {
			x := postReceipt(t, "a", tok, id, map[string]any{"lines": []any{rline(i1, 40), rline(i2, 5)}}, nil)
			mu.Lock()
			codes[x.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	require.Equal(t, 1, codes[201], codes)
	require.Equal(t, 5, codes[409], codes)
	require.Equal(t, 100, stockOf(t, "a", store, p1.vid))
	require.Equal(t, 5, stockOf(t, "a", store, p2.vid))
	g := get(t, host, fmt.Sprintf("/api/purchases/%v", id), tok)
	require.Equal(t, "Received", g.Body["status"])
	require.NotNil(t, g.Body["received_at"])

	rl := get(t, host, fmt.Sprintf("/api/purchases/%v/receipts", id), tok)
	require.Equal(t, 200, rl.Code, rl.Raw)
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.purchase_receipts WHERE purchase_id = $1`, int64(num(id))).Scan(&n))
	require.Equal(t, 2, n)
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.stock_movements
		WHERE reason = 'purchase_receive' AND ref_type = 'purchase_receipt' AND ref_id IN
		(SELECT id FROM `+schema("a")+`.purchase_receipts WHERE purchase_id = $1)`, int64(num(id))).Scan(&n))
	require.Equal(t, 3, n, "one movement per receipt line")
	require.Empty(t, reconcile(t, "a"))

	// Old path (A17): PUT Received on a partly received purchase receives
	// the rest in one receipt, with today's ledger ref.
	c2 := postPurchase(t, "a", tok, purchaseBody(sup, "PART-2", "Pending", purchaseLine(p1.pid, p1.vid, store, 10, 10)), nil)
	require.Equal(t, 201, c2.Code, c2.Raw)
	j1 := c2.Body["items"].([]any)[0].(map[string]any)["id"]
	require.Equal(t, 201, postReceipt(t, "a", tok, c2.Body["id"], map[string]any{"lines": []any{rline(j1, 3)}}, nil).Code)
	rest := putPurchase(t, "a", tok, c2.Body["id"], map[string]any{"status": "Received"})
	require.Equal(t, 200, rest.Code, rest.Raw)
	require.Equal(t, 10.0, rest.Body["items"].([]any)[0].(map[string]any)["received_qty"])
	require.Equal(t, 110, stockOf(t, "a", store, p1.vid))
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.purchase_receipts WHERE purchase_id = $1`, int64(num(c2.Body["id"]))).Scan(&n))
	require.Equal(t, 2, n)
	full := postReceipt(t, "a", tok, c2.Body["id"], map[string]any{"lines": []any{rline(j1, 1)}}, nil)
	require.Equal(t, 409, full.Code)

	// Created as Received: one receipt for everything.
	c3 := postPurchase(t, "a", tok, purchaseBody(sup, "PART-3", "Received", purchaseLine(p2.pid, p2.vid, store, 2, 10)), nil)
	require.Equal(t, 201, c3.Code, c3.Raw)
	require.Equal(t, 2.0, c3.Body["items"].([]any)[0].(map[string]any)["received_qty"])
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("a")+`.purchase_receipts WHERE purchase_id = $1`, int64(num(c3.Body["id"]))).Scan(&n))
	require.Equal(t, 1, n)

	// Cancelled purchases cannot be received; the list filters the new status.
	c4 := postPurchase(t, "a", tok, purchaseBody(sup, "PART-4", "Pending", purchaseLine(p2.pid, p2.vid, store, 2, 10)), nil)
	require.Equal(t, 200, putPurchase(t, "a", tok, c4.Body["id"], map[string]any{"status": "Cancelled"}).Code)
	k1 := c4.Body["items"].([]any)[0].(map[string]any)["id"]
	require.Equal(t, 409, postReceipt(t, "a", tok, c4.Body["id"], map[string]any{"lines": []any{rline(k1, 1)}}, nil).Code)
	c5 := postPurchase(t, "a", tok, purchaseBody(sup, "PART-5", "Pending", purchaseLine(p2.pid, p2.vid, store, 2, 10)), nil)
	k5 := c5.Body["items"].([]any)[0].(map[string]any)["id"]
	require.Equal(t, 201, postReceipt(t, "a", tok, c5.Body["id"], map[string]any{"lines": []any{rline(k5, 1)}}, nil).Code)
	pl := get(t, host, "/api/purchases?status=Partially%20received&search=PART-5", tok)
	require.Equal(t, 200, pl.Code, pl.Raw)
	require.Len(t, list(t, pl), 1)
	rep := get(t, host, fmt.Sprintf("/api/purchase/report?supplierId=%v", sup), tok)
	require.Equal(t, 200, rep.Code, rep.Raw)
	require.Contains(t, rep.Raw, `"receivedQty":1`)
}

// Receiving needs purchases:approve; A16 is tenant-scoped.
func TestPurchaseReceiptsPermissionAndIsolation(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	store := storeIDs(t, "a", aTok)[0]
	sup := createSupplier(t, "a", aTok, "Receipt Iso A")
	p := stockedProduct(t, "a", aTok, "PUR-RCPT-ISO", store, 0)
	c := postPurchase(t, "a", aTok, purchaseBody(sup, "RCPT-ISO-1", "Pending", purchaseLine(p.pid, p.vid, store, 4, 10)), nil)
	require.Equal(t, 201, c.Code, c.Raw)
	item := c.Body["items"].([]any)[0].(map[string]any)["id"]
	body := map[string]any{"lines": []any{rline(item, 1)}}

	clerk := userWithPerms(t, "a", aTok, "rcpt-clerk@a.test", "purchases:view", "purchases:edit")
	require.Equal(t, 403, postReceipt(t, "a", clerk, c.Body["id"], body, nil).Code)
	require.Equal(t, 403, get(t, tenants["a"].Host, fmt.Sprintf("/api/purchases/%v/receipts", c.Body["id"]), clerk).Code)

	require.Equal(t, 404, postReceipt(t, "c", cTok, c.Body["id"], body, nil).Code)
	require.Equal(t, 404, get(t, tenants["c"].Host, fmt.Sprintf("/api/purchases/%v/receipts", c.Body["id"]), cTok).Code)
	require.Equal(t, 401, postReceipt(t, "c", aTok, c.Body["id"], body, nil).Code)
	require.Equal(t, 0, stockOf(t, "a", store, p.vid))
	require.Equal(t, 201, postReceipt(t, "a", aTok, c.Body["id"], body, nil).Code)
	require.Equal(t, 1, stockOf(t, "a", store, p.vid))
}
