//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 3: selling by batch (design §3, A27).

// receiveBatches stocks a tracked product through a purchase receipt.
func receiveBatches(t *testing.T, l, tok string, p stocked, store float64, bs ...map[string]any) {
	t.Helper()
	sup := createSupplier(t, l, tok, fmt.Sprintf("Sup %s %d", p.sku, len(bs)))
	total := 0
	for _, b := range bs {
		total += b["quantity"].(int)
	}
	c := postPurchase(t, l, tok, purchaseBody(sup, fmt.Sprintf("RB-%s-%d", p.sku, total), "Ordered", purchaseLine(p.pid, p.vid, store, total, 10)), nil)
	require.Equal(t, 201, c.Code, c.Raw)
	item := c.Body["items"].([]any)[0].(map[string]any)["id"]
	r := postReceipt(t, l, tok, c.Body["id"], map[string]any{"lines": []any{bline(item, total, bs...)}, "acceptNearExpiry": true}, nil)
	require.Equal(t, 201, r.Code, r.Raw)
}

func saleBatches(t *testing.T, sale resp) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, b := range sale.Body["items"].([]any)[0].(map[string]any)["batches"].([]any) {
		out = append(out, b.(map[string]any))
	}
	return out
}

func pick(id int64, qty int) map[string]any { return map[string]any{"batchId": id, "quantity": qty} }

func TestBatchSellingFEFO(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "bs-fefo")
	host := tenants[l].Host
	a := stockedProduct(t, l, tok, "BS-PARA", store, 2) // 2 unbatched
	receiveBatches(t, l, tok, a, store, bat("LATE", day(300), 5), bat("EARLY", day(100), 3), bat("MID", day(200), 4))
	early, _ := batchByNo(t, l, a.vid, "EARLY")
	mid, _ := batchByNo(t, l, a.vid, "MID")
	late, _ := batchByNo(t, l, a.vid, "LATE")
	unk := unknownBatch(t, l, a.vid)

	// Quote shows the allocation before payment.
	q := do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: tok, body: posSale(store, line(a, 5))})
	require.Equal(t, 200, q.Code, q.Raw)
	ql := q.Body["lines"].([]any)[0].(map[string]any)
	require.Len(t, ql["batches"], 2)
	require.Nil(t, q.Body["batchProblem"])

	// FEFO across batches, line split: 3 EARLY + 2 MID.
	s := sell(t, l, tok, posSale(store, line(a, 5)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	bs := saleBatches(t, s)
	require.Len(t, bs, 2)
	require.Equal(t, "EARLY", bs[0]["batchNo"])
	require.Equal(t, 3.0, bs[0]["quantity"])
	require.Equal(t, "MID", bs[1]["batchNo"])
	require.Equal(t, false, bs[0]["chosen"])
	require.Equal(t, 0, batchQty(t, l, early, store))
	require.Equal(t, 2, batchQty(t, l, mid, store))
	require.Empty(t, s.Body["items"].([]any)[0].(map[string]any)["warnings"])

	// Chosen batch (any cashier, Q12): recorded as chosen.
	createUser(t, l, tok, "cashier@bs-fefo.test", "biller", "cashier-pass-12345")
	ctok, _ := login(t, l, "cashier@bs-fefo.test", "cashier-pass-12345")
	sl := line(a, 2)
	sl["batches"] = []any{pick(late, 1)}
	s = sell(t, l, ctok, posSale(store, sl), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	bs = saleBatches(t, s)
	require.Len(t, bs, 2)
	require.Equal(t, 4, batchQty(t, l, late, store), "1 chosen from LATE")
	require.Equal(t, 1, batchQty(t, l, mid, store), "the rest earliest first")
	var chosen int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.order_item_batches WHERE chosen`).Scan(&chosen))
	require.Equal(t, 1, chosen)

	// Chosen: more than the batch has, a batch of another product, more than the line.
	sl["batches"] = []any{pick(late, 9)}
	sl["quantity"] = 9
	r := sell(t, l, tok, posSale(store, sl), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", codeOf(r))
	b := stockedProduct(t, l, tok, "BS-OTHER", store, 3)
	ol := line(b, 1)
	ol["batches"] = []any{pick(late, 1)}
	require.Equal(t, 400, sell(t, l, tok, posSale(store, ol), nil).Code)
	sl = line(a, 1)
	sl["batches"] = []any{pick(late, 2)}
	require.Equal(t, 400, sell(t, l, tok, posSale(store, sl), nil).Code)

	// Expired and recalled batches never sell; the shortage says why.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = current_date - 1 WHERE id = $1`, mid)
	require.NoError(t, err)
	sl = line(a, 1)
	sl["batches"] = []any{pick(mid, 1)}
	r = sell(t, l, tok, posSale(store, sl), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "BATCH_NOT_SELLABLE", codeOf(r))
	require.Equal(t, "expired", r.Body["details"].(map[string]any)["reason"])
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET status = 'recalled', recalled_at = now() WHERE id = $1`, late)
	require.NoError(t, err)
	// Sellable now: only the unknown batch (2). Plain stock is 2 + 1 + 4 = 7.
	r = sell(t, l, tok, posSale(store, line(a, 3)), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", codeOf(r))
	require.Contains(t, r.Raw, `"unsellable"`)
	q = do(t, rq{method: "POST", host: host, path: "/api/sales/quote", token: tok, body: posSale(store, line(a, 3))})
	require.Equal(t, 200, q.Code, q.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", q.Body["batchProblem"].(map[string]any)["code"])

	// The unknown batch sells last, with a warning.
	s = sell(t, l, tok, posSale(store, line(a, 2)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	it := s.Body["items"].([]any)[0].(map[string]any)
	require.Len(t, it["warnings"], 1)
	require.Equal(t, true, saleBatches(t, s)[0]["unknown"])
	require.Equal(t, 0, batchQty(t, l, unk, store))

	// Void returns each unit to its batch; edit is refused (Q13).
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET status = 'available', recalled_at = NULL, expiry_date = CASE WHEN id = $2 THEN current_date + 150 ELSE expiry_date END WHERE id IN ($1, $2)`, late, mid)
	require.NoError(t, err)
	s = sell(t, l, tok, posSale(store, line(a, 3)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	sid := s.Body["id"]
	require.Equal(t, 0, batchQty(t, l, mid, store))
	ed := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/sales/%v", sid), token: tok, body: posSale(store, line(a, 1))})
	require.Equal(t, 409, ed.Code, ed.Raw)
	require.Contains(t, ed.Raw, "Return or void")
	v := do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/sales/%v?reason=test", sid), token: tok})
	require.Equal(t, 204, v.Code, v.Raw)
	require.Equal(t, 1, batchQty(t, l, mid, store))
	require.Equal(t, 4, batchQty(t, l, late, store))

	// The receipt keeps the batch details at sale time after a correction.
	s = sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, 200, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/batches/%d", mid), token: tok,
		body: map[string]any{"batchNo": "MID-FIXED", "reason": "label typo"}}).Code)
	g := get(t, host, fmt.Sprintf("/api/sales/%v", s.Body["id"]), tok)
	require.Equal(t, "MID", saleBatches(t, g)[0]["batchNo"])
	pdf := get(t, host, fmt.Sprintf("/api/sales/%v/receipt", s.Body["id"]), tok)
	require.Equal(t, 200, pdf.Code, "the receipt (with its batch line) renders")
	require.Empty(t, reconcile(t, l))

	// Untracked products and module off: sold as before; batches refused.
	plain := stockedProduct(t, l, tok, "BS-PLAIN", store, 5)
	require.Equal(t, 200, setTracking(t, l, tok, plain, false).Code)
	pl := line(plain, 1)
	pl["batches"] = []any{pick(mid, 1)}
	require.Equal(t, 400, sell(t, l, tok, posSale(store, pl), nil).Code)
	s = sell(t, l, tok, posSale(store, line(plain, 1)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Empty(t, saleBatches(t, s))
	require.Equal(t, 200, putBusiness(t, l, tok, map[string]any{"modules": []string{"barcode_labels", "stock_counts"}}).Code)
	require.Equal(t, 403, sell(t, l, tok, posSale(store, pl), nil).Code)
	require.Equal(t, 201, sell(t, l, tok, posSale(store, line(a, 1)), nil).Code)
	require.Empty(t, reconcile(t, l))
}

func TestBatchSellingMinDaysAndTimeZone(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "bs-days")
	a := stockedProduct(t, l, tok, "BS-DAYS", store, 0)
	receiveBatches(t, l, tok, a, store, bat("SOON", day(10), 2), bat("LATER", day(400), 2))
	// minDaysToSell 30: SOON is not sellable.
	_, err := admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET min_days_to_sell = 30`)
	require.NoError(t, err)
	s := sell(t, l, tok, posSale(store, line(a, 2)), nil)
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, "LATER", saleBatches(t, s)[0]["batchNo"])
	r := sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Contains(t, r.Raw, "expires within 30 days")
	// Expiry is today in the shop's time zone: still sellable (expired = before today).
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.settings SET min_days_to_sell = 0, timezone = 'Pacific/Kiritimati'`)
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = (now() AT TIME ZONE 'Pacific/Kiritimati')::date WHERE batch_no = 'SOON'`)
	require.NoError(t, err)
	require.Equal(t, 201, sell(t, l, tok, posSale(store, line(a, 1)), nil).Code)
	_, err = admin(t).Exec(ctx, `UPDATE `+schema(l)+`.batches SET expiry_date = (now() AT TIME ZONE 'Pacific/Kiritimati')::date - 1 WHERE batch_no = 'SOON'`)
	require.NoError(t, err)
	r = sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Empty(t, reconcile(t, l))
}

// Ten tills selling one batch, and two batches in opposite order.
func TestBatchSellingConcurrency(t *testing.T) {
	l, tok, store := pharmacyShop(t, "bs-race")
	a := stockedProduct(t, l, tok, "BS-RA", store, 0)
	b := stockedProduct(t, l, tok, "BS-RB", store, 0)
	receiveBatches(t, l, tok, a, store, bat("A1", day(100), 6))
	receiveBatches(t, l, tok, b, store, bat("B1", day(100), 6))
	a1, _ := batchByNo(t, l, a.vid, "A1")
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := range 10 {
		wg.Go(func() {
			la, lb := line(a, 1), line(b, 1)
			la["batches"] = []any{pick(a1, 1)}
			lines := []map[string]any{la, lb}
			if i%2 == 1 {
				lines[0], lines[1] = lines[1], lines[0]
			}
			r := sell(t, l, tok, posSale(store, lines...), nil)
			mu.Lock()
			codes[r.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	require.Equal(t, 6, codes[201], codes)
	require.Equal(t, 4, codes[409], codes)
	require.Equal(t, 0, batchQty(t, l, a1, store))
	require.Empty(t, reconcile(t, l))
}
