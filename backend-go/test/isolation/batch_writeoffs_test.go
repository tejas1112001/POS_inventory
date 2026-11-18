//go:build integration

package isolation

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 5: write-offs, recalls and the trace report
// (design §5, A30, A31).

func woLine(batch int64, qty int, from string) map[string]any {
	return map[string]any{"batchId": batch, "quantity": qty, "from": from}
}

func postWriteOff(t *testing.T, l, tok string, store float64, reason string, hdr map[string]string, lines ...map[string]any) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: "/api/write-offs", token: tok, header: hdr,
		body: map[string]any{"storeId": store, "reason": reason, "note": "shelf check", "lines": lines}})
}

func batchAction(t *testing.T, l, tok string, id int64, what string, body map[string]any) resp {
	t.Helper()
	return do(t, rq{method: "POST", host: tenants[l].Host, path: fmt.Sprintf("/api/batches/%d/%s", id, what), token: tok, body: body})
}

func TestBatchWriteOffs(t *testing.T) {
	l, tok, store := pharmacyShop(t, "bw-wo")
	host := tenants[l].Host
	a := stockedProduct(t, l, tok, "BW-PARA", store, 0)
	receiveBatches(t, l, tok, a, store, bat("W1", day(100), 5), bat("W2", day(200), 3))
	w1, _ := batchByNo(t, l, a.vid, "W1")
	w2, _ := batchByNo(t, l, a.vid, "W2")
	require.Equal(t, 200, batchAction(t, l, tok, w1, "quarantine", map[string]any{"storeId": store, "quantity": 2, "reason": "crushed box"}).Code)

	// Validation.
	require.Equal(t, 400, postWriteOff(t, l, tok, store, "lost", nil, woLine(w1, 1, "stock")).Code)
	r := do(t, rq{method: "POST", host: host, path: "/api/write-offs", token: tok,
		body: map[string]any{"storeId": store, "reason": "other", "lines": []any{woLine(w1, 1, "stock")}}})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "note", errField(r))
	require.Equal(t, 400, postWriteOff(t, l, tok, store, "expired", nil, woLine(w1, 1, "stock"), woLine(w1, 1, "stock")).Code)
	require.Equal(t, 400, postWriteOff(t, l, tok, store, "expired", nil, woLine(w1, 1, "shelf")).Code)
	require.Equal(t, 400, postWriteOff(t, l, tok, store, "expired", nil).Code)

	// Never more than is there; nothing moves.
	r = postWriteOff(t, l, tok, store, "damaged", nil, woLine(w1, 3, "quarantine"))
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", codeOf(r))
	r = postWriteOff(t, l, tok, store, "damaged", nil, woLine(w2, 4, "stock"))
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "INSUFFICIENT_STOCK", codeOf(r))

	// From stock and from quarantine, one document; idempotent.
	key := map[string]string{"Idempotency-Key": "wo-0001-key"}
	r = postWriteOff(t, l, tok, store, "damaged", key, woLine(w1, 2, "stock"), woLine(w1, 1, "quarantine"))
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "WO-000001", r.Body["reference"])
	require.Equal(t, 3.0, r.Body["units"])
	require.Len(t, r.Body["lines"], 2)
	again := postWriteOff(t, l, tok, store, "damaged", key, woLine(w1, 2, "stock"), woLine(w1, 1, "quarantine"))
	require.Equal(t, 201, again.Code, again.Raw)
	require.Equal(t, r.Body["id"], again.Body["id"])
	require.Equal(t, 1, batchQty(t, l, w1, store))
	require.Equal(t, 1, quarantined(t, l, w1, store))
	require.Equal(t, 4, stockOf(t, l, store, a.vid), "1 W1 + 3 W2; quarantine is not stock")

	// List and detail.
	g := get(t, host, fmt.Sprintf("/api/write-offs?batchId=%d", w1), tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Equal(t, 1.0, g.Body["total"])
	require.Equal(t, 0.0, get(t, host, fmt.Sprintf("/api/write-offs?batchId=%d", w2), tok).Body["total"])
	require.Equal(t, 200, get(t, host, fmt.Sprintf("/api/write-offs/%v", r.Body["id"]), tok).Code)
	require.Equal(t, 404, get(t, host, "/api/write-offs/999999", tok).Code)

	// Untracked products have no batches to write off.
	plain := stockedProduct(t, l, tok, "BW-PLAIN", store, 2)
	require.Equal(t, 200, setTracking(t, l, tok, plain, false).Code)
	require.Equal(t, 400, postWriteOff(t, l, tok, store, "damaged", nil, woLine(unknownBatchOrZero(t, l, plain.vid), 1, "stock")).Code)

	// Permissions: billers see write-offs, cannot make them.
	createUser(t, l, tok, "biller@bw-wo.test", "biller", "biller-pass-12345")
	btok, _ := login(t, l, "biller@bw-wo.test", "biller-pass-12345")
	require.Equal(t, 403, postWriteOff(t, l, btok, store, "damaged", nil, woLine(w2, 1, "stock")).Code)
	require.Equal(t, 200, get(t, host, "/api/write-offs", btok).Code)

	// Isolation: another shop sees none of it.
	l2, tok2, _ := pharmacyShop(t, "bw-wo2")
	require.Equal(t, 0.0, get(t, tenants[l2].Host, "/api/write-offs", tok2).Body["total"])
	require.Equal(t, 404, get(t, tenants[l2].Host, fmt.Sprintf("/api/write-offs/%v", r.Body["id"]), tok2).Code)
	require.Empty(t, reconcile(t, l))
}

// unknownBatchOrZero is the variant's unknown batch, or a batch id of another
// variant when it has none (the write-off is refused either way).
func unknownBatchOrZero(t *testing.T, l string, vid float64) int64 {
	t.Helper()
	var id int64
	_ = admin(t).QueryRow(t.Context(), `SELECT coalesce((SELECT id FROM `+schema(l)+`.batches WHERE variant_id = $1 LIMIT 1), 1)`, int64(vid)).Scan(&id)
	return id
}

func TestBatchRecallAndTrace(t *testing.T) {
	l, tok, store := pharmacyShop(t, "bw-rc")
	host := tenants[l].Host
	a := stockedProduct(t, l, tok, "BW-RC", store, 0)
	receiveBatches(t, l, tok, a, store, bat("R1", day(100), 8))
	r1, _ := batchByNo(t, l, a.vid, "R1")
	cust := newCustomer(t, l, tok, "Asha Rao")
	named := saleTo(t, l, tok, store, cust, a, 2, "")
	walk := sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 201, walk.Code, walk.Raw)
	require.Equal(t, 200, batchAction(t, l, tok, r1, "quarantine", map[string]any{"storeId": store, "quantity": 1, "reason": "dented tin"}).Code)

	// Recall: reason needed; billers may not.
	require.Equal(t, 400, batchAction(t, l, tok, r1, "recall", map[string]any{"reason": "x"}).Code)
	createUser(t, l, tok, "biller@bw-rc.test", "biller", "biller-pass-12345")
	btok, _ := login(t, l, "biller@bw-rc.test", "biller-pass-12345")
	require.Equal(t, 403, batchAction(t, l, btok, r1, "recall", map[string]any{"reason": "biller tries"}).Code)
	r := batchAction(t, l, tok, r1, "recall", map[string]any{"reason": "supplier notice 42"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "recalled", r.Body["status"])
	require.Equal(t, 409, batchAction(t, l, tok, r1, "recall", map[string]any{"reason": "supplier notice 42"}).Code)

	// It stops selling at once; release from quarantine is refused.
	s := sell(t, l, tok, posSale(store, line(a, 1)), nil)
	require.Equal(t, 409, s.Code, s.Raw)
	require.Contains(t, s.Raw, "recalled")
	q := batchAction(t, l, tok, r1, "release", map[string]any{"storeId": store, "quantity": 1, "reason": "try to release"})
	require.Equal(t, 409, q.Code, q.Raw)
	require.Equal(t, "BATCH_NOT_SELLABLE", codeOf(q))

	// A return of a recalled batch goes to quarantine; the rest is written off.
	ret := postReturn(t, l, tok, map[string]any{"saleId": named["id"], "returnItems": []map[string]any{{
		"orderItemId": named["items"].([]any)[0].(map[string]any)["id"], "quantity": 1, "condition": "resellable"}}}, nil)
	require.Equal(t, 201, ret.Code, ret.Raw)
	require.Equal(t, "quarantine", retBatches(t, ret)[0]["bucket"])
	wo := postWriteOff(t, l, tok, store, "recalled", nil, woLine(r1, 4, "stock"), woLine(r1, 2, "quarantine"))
	require.Equal(t, 201, wo.Code, wo.Raw)
	require.Equal(t, 0, batchQty(t, l, r1, store))
	require.Equal(t, 0, quarantined(t, l, r1, store))

	// Trace: every sale (named customers only), returns, write-offs.
	tr := get(t, host, fmt.Sprintf("/api/batches/%d/trace", r1), tok)
	require.Equal(t, 200, tr.Code, tr.Raw)
	sales := tr.Body["sales"].([]any)
	require.Len(t, sales, 2)
	byRef := map[any]map[string]any{}
	for _, x := range sales {
		m := x.(map[string]any)
		byRef[m["reference"]] = m
	}
	require.Equal(t, "Asha Rao", byRef[named["reference"]]["customerName"])
	require.Nil(t, byRef[walk.Body["reference"]]["customerName"], "walk-in is not named")
	require.Equal(t, 3.0, tr.Body["sold"])
	require.Equal(t, 1.0, tr.Body["returned"])
	require.Equal(t, 6.0, tr.Body["writtenOff"])
	require.Len(t, tr.Body["writeOffs"], 2, "one write-off, a line from stock and one from quarantine")

	// Lifting the recall: same permission, a reason; the unknown batch is never recalled.
	require.Equal(t, 403, batchAction(t, l, btok, r1, "lift-recall", map[string]any{"reason": "biller tries"}).Code)
	r = batchAction(t, l, tok, r1, "lift-recall", map[string]any{"reason": "supplier withdrew notice"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "available", r.Body["status"])
	require.Equal(t, 409, batchAction(t, l, tok, r1, "lift-recall", map[string]any{"reason": "not recalled now"}).Code)
	b := stockedProduct(t, l, tok, "BW-UNK", store, 2)
	require.Equal(t, 409, batchAction(t, l, tok, unknownBatch(t, l, b.vid), "recall", map[string]any{"reason": "no batch here"}).Code)

	// Isolation: another shop cannot trace or recall it.
	l2, tok2, _ := pharmacyShop(t, "bw-rc2")
	require.Equal(t, 404, get(t, tenants[l2].Host, fmt.Sprintf("/api/batches/%d/trace", r1), tok2).Code)
	require.Equal(t, 404, batchAction(t, l2, tok2, r1, "recall", map[string]any{"reason": "cross shop"}).Code)
	require.Empty(t, reconcile(t, l))
}

// A recall racing ten tills: every sale either finished before the recall
// or was refused; none sells the batch after it.
func TestBatchRecallConcurrency(t *testing.T) {
	l, tok, store := pharmacyShop(t, "bw-race")
	a := stockedProduct(t, l, tok, "BW-RACE", store, 0)
	receiveBatches(t, l, tok, a, store, bat("RR", day(100), 20))
	rr, _ := batchByNo(t, l, a.vid, "RR")
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := range 11 {
		wg.Go(func() {
			if i == 5 {
				require.Equal(t, 200, batchAction(t, l, tok, rr, "recall", map[string]any{"reason": "race recall"}).Code)
				return
			}
			r := sell(t, l, tok, posSale(store, line(a, 1)), nil)
			mu.Lock()
			codes[r.Code]++
			mu.Unlock()
		})
	}
	wg.Wait()
	require.Equal(t, 10, codes[201]+codes[409], codes)
	require.Equal(t, 20-codes[201], batchQty(t, l, rr, store))
	require.Equal(t, 409, sell(t, l, tok, posSale(store, line(a, 1)), nil).Code, "after the recall nothing sells")
	tr := get(t, tenants[l].Host, fmt.Sprintf("/api/batches/%d/trace", rr), tok)
	require.Equal(t, float64(codes[201]), tr.Body["sold"])
	require.Empty(t, reconcile(t, l))
}
