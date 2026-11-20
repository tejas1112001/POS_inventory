//go:build integration

package isolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 4: returns per batch and quarantine (design §4,
// A28, A29).

func quarantined(t *testing.T, l string, batchID int64, store float64) int {
	t.Helper()
	var n int
	err := admin(t).QueryRow(context.Background(), `SELECT coalesce((SELECT quarantined_qty FROM `+schema(l)+`.batch_stock
		WHERE batch_id = $1 AND store_id = $2), 0)`, batchID, int64(store)).Scan(&n)
	require.NoError(t, err)
	return n
}

func batchRet(sale resp, qty int, cond string) map[string]any {
	it := sale.Body["items"].([]any)[0].(map[string]any)
	ri := map[string]any{"orderItemId": it["id"], "quantity": qty}
	if cond != "" {
		ri["condition"] = cond
	}
	return map[string]any{"saleId": sale.Body["id"], "returnItems": []map[string]any{ri}}
}

func retBatches(t *testing.T, r resp) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, b := range r.Body["items"].([]any)[0].(map[string]any)["batches"].([]any) {
		out = append(out, b.(map[string]any))
	}
	return out
}

func TestBatchReturns(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "br-ret")
	a := stockedProduct(t, l, tok, "BR-PARA", store, 0)
	receiveBatches(t, l, tok, a, store, bat("EARLY", day(100), 3), bat("MID", day(200), 4))
	early, _ := batchByNo(t, l, a.vid, "EARLY")
	mid, _ := batchByNo(t, l, a.vid, "MID")

	s := sell(t, l, tok, posSale(store, line(a, 5)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, 0, batchQty(t, l, early, store))
	require.Equal(t, 2, batchQty(t, l, mid, store))

	// The condition is always asked for tracked products (policy has it off).
	r := postReturn(t, l, tok, batchRet(s, 1, ""), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "returnItems[0].condition", errField(r))
	// A batch cannot be named on a return against a sale.
	b := batchRet(s, 1, "resellable")
	b["returnItems"].([]map[string]any)[0]["batchId"] = mid
	r = postReturn(t, l, tok, b, nil)
	require.Equal(t, 400, r.Code, r.Raw)

	// Back to the batches it was sold from, latest expiry first.
	r = postReturn(t, l, tok, batchRet(s, 3, "resellable"), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	bs := retBatches(t, r)
	require.Len(t, bs, 2)
	require.Equal(t, "MID", bs[0]["batchNo"])
	require.Equal(t, 2.0, bs[0]["quantity"])
	require.Equal(t, "stock", bs[0]["bucket"])
	require.Equal(t, "EARLY", bs[1]["batchNo"])
	require.Equal(t, true, r.Body["items"].([]any)[0].(map[string]any)["restocked"])
	require.Equal(t, 4, batchQty(t, l, mid, store))
	require.Equal(t, 1, batchQty(t, l, early, store))
	require.Equal(t, 5, stockOf(t, l, store, a.vid))

	// The batch expired since: resellable units go to quarantine, outside stock.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = current_date - 1 WHERE id = $1`, early)
	require.NoError(t, err)
	r = postReturn(t, l, tok, batchRet(s, 2, "resellable"), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	bs = retBatches(t, r)
	require.Len(t, bs, 1)
	require.Equal(t, "quarantine", bs[0]["bucket"])
	require.Equal(t, false, r.Body["items"].([]any)[0].(map[string]any)["restocked"])
	require.Equal(t, 2, quarantined(t, l, early, store))
	require.Equal(t, 5, stockOf(t, l, store, a.vid), "quarantine is not stock")
	r = postReturn(t, l, tok, batchRet(s, 1, "resellable"), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "RETURN_EXCEEDS_SOLD", codeOf(r))

	// Damaged goes to quarantine even when the batch sells.
	s2 := sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 201, s2.Code, s2.Raw)
	require.Equal(t, "MID", saleBatches(t, s2)[0]["batchNo"], "the expired batch is not sold")
	r = postReturn(t, l, tok, batchRet(s2, 1, "damaged"), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "quarantine", retBatches(t, r)[0]["bucket"])
	require.Equal(t, 1, quarantined(t, l, mid, store))
	require.Equal(t, 3, batchQty(t, l, mid, store))

	// Without a sale: the named batch, or quarantine under the unknown batch.
	un := func(extra map[string]any) map[string]any {
		it := map[string]any{"productId": a.pid, "variantId": a.vid, "quantity": 1, "unitPrice": 10, "condition": "resellable"}
		for k, v := range extra {
			it[k] = v
		}
		return map[string]any{"storeId": store, "returnItems": []map[string]any{it}}
	}
	r = postReturn(t, l, tok, un(map[string]any{"batchId": mid}), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "stock", retBatches(t, r)[0]["bucket"])
	require.Equal(t, 4, batchQty(t, l, mid, store))
	r = postReturn(t, l, tok, un(nil), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "quarantine", retBatches(t, r)[0]["bucket"])
	require.Equal(t, true, retBatches(t, r)[0]["unknown"])
	require.Equal(t, 1, quarantined(t, l, unknownBatch(t, l, a.vid), store))
	unkRet := r.Body["id"]
	other := stockedProduct(t, l, tok, "BR-OTHER", store, 2)
	require.Equal(t, 200, setTracking(t, l, tok, other, true).Code)
	r = postReturn(t, l, tok, un(map[string]any{"batchId": unknownBatch(t, l, other.vid)}), nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "returnItems[0].batchId", errField(r))

	// The return detail keeps the batches.
	g := get(t, tenants[l].Host, fmt.Sprintf("/api/sales/returns/%v", unkRet), tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Equal(t, "quarantine", retBatches(t, g)[0]["bucket"])

	// Exchange: the returned goods follow the same rules.
	require.Equal(t, 200, putPolicy(t, l, tok, map[string]any{"exchange": true}).Code)
	s3 := sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 201, s3.Code, s3.Raw)
	item := s3.Body["items"].([]any)[0].(map[string]any)["id"]
	x := do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/sales/exchanges", token: tok, body: map[string]any{
		"saleId": s3.Body["id"], "returnLines": []map[string]any{{"orderItemId": item, "quantity": 1}}, "newLines": []map[string]any{line(a, 1)}}})
	require.Equal(t, 400, x.Code, x.Raw)
	require.Contains(t, x.Raw, "returnLines[0].condition")
	x = do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/sales/exchanges", token: tok, body: map[string]any{
		"saleId": s3.Body["id"], "returnLines": []map[string]any{{"orderItemId": item, "quantity": 1, "condition": "defective"}},
		"newLines": []map[string]any{line(a, 1)}}})
	require.Equal(t, 201, x.Code, x.Raw)
	require.Equal(t, 2, quarantined(t, l, mid, store))

	// Untracked products return as before (no condition needed).
	plain := stockedProduct(t, l, tok, "BR-PLAIN", store, 3)
	require.Equal(t, 200, setTracking(t, l, tok, plain, false).Code)
	ps := sell(t, l, tok, posSale(store, line(plain, 1)), nil)
	require.Equal(t, 201, ps.Code, ps.Raw)
	r = postReturn(t, l, tok, batchRet(ps, 1, ""), nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Empty(t, retBatches(t, r))
	require.Equal(t, 3, stockOf(t, l, store, plain.vid))
	require.Empty(t, reconcile(t, l))

	// Units in quarantine keep the module on (item 1: closing batches would
	// lose them); they leave by release or write-off.
	r = putBusiness(t, l, tok, map[string]any{"modules": []string{"barcode_labels"}})
	require.Equal(t, 409, r.Code, r.Raw)
	require.Empty(t, reconcile(t, l))
}

func TestBatchQuarantineAndRelease(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "br-quar")
	host := tenants[l].Host
	a := stockedProduct(t, l, tok, "BQ-PARA", store, 0)
	receiveBatches(t, l, tok, a, store, bat("Q1", day(100), 5), bat("Q2", day(200), 2))
	q1, _ := batchByNo(t, l, a.vid, "Q1")
	q2, _ := batchByNo(t, l, a.vid, "Q2")
	move := func(tk, what string, id int64, qty int, reason string) resp {
		return do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/batches/%d/%s", id, what), token: tk,
			body: map[string]any{"storeId": store, "quantity": qty, "reason": reason}})
	}

	r := move(tok, "quarantine", q1, 3, "found crushed on shelf")
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, 2, batchQty(t, l, q1, store))
	require.Equal(t, 3, quarantined(t, l, q1, store))
	require.Equal(t, 4, stockOf(t, l, store, a.vid))
	// Quarantined units never sell: 4 sellable.
	require.Equal(t, 409, sell(t, l, tok, posSale(store, line(a, 5)), nil).Code)

	require.Equal(t, 200, move(tok, "release", q1, 1, "checked, pack is fine").Code)
	require.Equal(t, 3, batchQty(t, l, q1, store))
	require.Equal(t, 2, quarantined(t, l, q1, store))

	// Refusals.
	require.Equal(t, 400, move(tok, "quarantine", q1, 1, "x").Code, "reason too short")
	require.Equal(t, 400, move(tok, "quarantine", q1, 0, "zero units").Code)
	r = move(tok, "quarantine", q2, 3, "more than the batch has")
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", codeOf(r))
	r = move(tok, "release", q1, 5, "more than in quarantine")
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", codeOf(r))
	require.Equal(t, 404, move(tok, "release", 999999, 1, "no such batch").Code)
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = current_date - 1 WHERE id = $1`, q1)
	require.NoError(t, err)
	r = move(tok, "release", q1, 1, "expired, try anyway")
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "BATCH_NOT_SELLABLE", codeOf(r))
	r = move(tok, "quarantine", q1, 3, "expired, off the shelf")
	require.Equal(t, 200, r.Code, r.Raw)

	// Permission: a biller may view batches, not move them.
	createUser(t, l, tok, "biller@br-quar.test", "biller", "biller-pass-12345")
	btok, _ := login(t, l, "biller@br-quar.test", "biller-pass-12345")
	require.Equal(t, 403, move(btok, "quarantine", q2, 1, "biller tries this").Code)

	// The detail shows quarantine per store; the movements are on both ledgers.
	g := get(t, host, fmt.Sprintf("/api/batches/%d", q1), tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Contains(t, g.Raw, `"quarantined":5`)
	var n int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.stock_movements WHERE reason IN ('quarantine','quarantine_release')`).Scan(&n))
	require.Equal(t, 3, n)
	require.Empty(t, reconcile(t, l))

	// Isolation: another shop cannot see or move this batch.
	l2, tok2, _ := pharmacyShop(t, "br-quar2")
	require.Equal(t, 404, do(t, rq{method: "POST", host: tenants[l2].Host, path: fmt.Sprintf("/api/batches/%d/release", q2), token: tok2,
		body: map[string]any{"storeId": store, "quantity": 1, "reason": "cross shop try"}}).Code)
	_ = l2

	// Module off with nothing in quarantine: the routes are refused.
	l3, tok3, store3 := pharmacyShop(t, "br-quar3")
	c := stockedProduct(t, l3, tok3, "BQ-OFF", store3, 2)
	require.Equal(t, 200, putBusiness(t, l3, tok3, map[string]any{"modules": []string{}}).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: tenants[l3].Host, path: "/api/batches/1/quarantine", token: tok3,
		body: map[string]any{"storeId": store3, "quantity": 1, "reason": "module is off"}}).Code)
	r = postReturn(t, l3, tok3, map[string]any{"storeId": store3, "returnItems": []map[string]any{{"productId": c.pid, "variantId": c.vid,
		"quantity": 1, "unitPrice": 10, "batchId": 1}}}, nil)
	require.Equal(t, 403, r.Code, r.Raw)
}
