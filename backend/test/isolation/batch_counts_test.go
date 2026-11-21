//go:build integration

package isolation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 6: stock counts per batch (design §6, A32).

func TestBatchCounts(t *testing.T) {
	l, tok, store := pharmacyShop(t, "bc-cnt")
	host := tenants[l].Host
	post := func(path string, body any) resp {
		return do(t, rq{method: "POST", host: host, path: path, token: tok, body: body})
	}
	put := func(path string, body any) resp {
		return do(t, rq{method: "PUT", host: host, path: path, token: tok, body: body})
	}
	newCount := func() string {
		c := post("/api/stock-counts", map[string]any{"storeId": store})
		require.Equal(t, 201, c.Code, c.Raw)
		return fmt.Sprintf("/api/stock-counts/%v", c.Body["id"])
	}
	a := stockedProduct(t, l, tok, "BC-PARA", store, 10) // 10 with no batch
	unk := unknownBatch(t, l, a.vid)
	require.Equal(t, 10, batchQty(t, l, unk, store))
	receiveBatches(t, l, tok, a, store, bat("B1", day(100), 3))
	b1, _ := batchByNo(t, l, a.vid, "B1")
	other := stockedProduct(t, l, tok, "BC-OTHER", store, 1)
	plain := stockedProduct(t, l, tok, "BC-PLAIN", store, 4)
	require.Equal(t, 200, setTracking(t, l, tok, plain, false).Code)

	// The count splits the unknown batch into real batches, two of them
	// found on the shelf (one expired).
	path := newCount()
	r := put(path, map[string]any{"lines": []any{
		map[string]any{"variantId": a.vid, "batchId": b1, "qty": 2},
		map[string]any{"variantId": a.vid, "batchNo": "SHELF1", "expiry": day(300), "qty": 5},
		map[string]any{"variantId": a.vid, "batchNo": "SHELF2", "expiry": day(-10), "qty": 4},
	}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Len(t, r.Body["lines"], 3)
	for _, x := range r.Body["lines"].([]any) {
		ln := x.(map[string]any)
		require.Equal(t, true, ln["byBatch"])
		if ln["batchNo"] == "B1" {
			require.EqualValues(t, 3, ln["currentQty"])
			require.EqualValues(t, -1, ln["difference"])
		}
		if ln["batchNo"] == "SHELF2" {
			require.Equal(t, true, ln["expired"])
		}
	}
	s1, _ := batchByNo(t, l, a.vid, "SHELF1")
	s2, _ := batchByNo(t, l, a.vid, "SHELF2")

	// The unknown batch is not counted: completion will set it to 0.
	g := get(t, host, path, tok)
	require.Equal(t, 200, g.Code, g.Raw)
	ub := g.Body["uncountedBatches"].([]any)
	require.Len(t, ub, 1)
	require.Equal(t, true, ub[0].(map[string]any)["unknown"])
	require.EqualValues(t, 10, ub[0].(map[string]any)["currentQty"])

	// Validation.
	bad := func(line map[string]any) resp { return put(path, map[string]any{"lines": []any{line}}) }
	require.Equal(t, 400, bad(map[string]any{"variantId": other.vid, "batchId": b1, "qty": 1}).Code, "another product's batch")
	r = bad(map[string]any{"variantId": a.vid, "batchNo": "B1", "expiry": day(200), "qty": 1})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "another expiry")
	require.Equal(t, 400, bad(map[string]any{"variantId": a.vid, "batchNo": "NOEXP", "qty": 1}).Code)
	require.Equal(t, 400, bad(map[string]any{"variantId": a.vid, "batchNo": "X", "expiry": day(9), "batchId": b1, "qty": 1}).Code)
	require.Equal(t, 400, bad(map[string]any{"variantId": a.vid, "batchNo": "unbatched", "expiry": day(9), "qty": 1}).Code)
	require.Equal(t, 400, bad(map[string]any{"variantId": plain.vid, "batchId": b1, "qty": 1}).Code, "untracked product")

	// A scan of a product counted by batch asks for the batch (it does not
	// land on "No batch"); with the batch it adds; an untracked scan adds.
	r = bad(map[string]any{"code": "BCBC-PARA", "add": 1})
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "BATCH_REQUIRED", codeOf(r))
	require.Equal(t, a.vid, num(r.Body["details"].(map[string]any)["variantId"]))
	require.Equal(t, 200, bad(map[string]any{"code": "BCBC-PARA", "batchId": b1, "add": 1}).Code)
	pr := bad(map[string]any{"code": "BC-PLAIN", "add": 1}) // by SKU
	require.Equal(t, 200, pr.Code, pr.Raw)
	require.Equal(t, 200, put(path, map[string]any{"remove": []any{plain.vid}}).Code, "and take it back out")
	// Put B1 back to 2 for the rest of the test (the scan made it 3).
	require.Equal(t, 200, bad(map[string]any{"variantId": a.vid, "batchId": b1, "qty": 2}).Code)

	// Remove one line by id, then put it back.
	var shelf2Line any
	for _, x := range get(t, host, path, tok).Body["lines"].([]any) {
		if ln := x.(map[string]any); ln["batchNo"] == "SHELF2" {
			shelf2Line = ln["id"]
		}
	}
	r = put(path, map[string]any{"removeLines": []any{shelf2Line}})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Len(t, r.Body["removedLines"], 1)
	require.EqualValues(t, 2, r.Body["lineCount"])
	require.Equal(t, 200, put(path, map[string]any{"lines": []any{map[string]any{"variantId": a.vid, "batchId": s2, "qty": 4}}}).Code)

	// Untracked lines as before.
	require.Equal(t, 200, put(path, map[string]any{"lines": []any{map[string]any{"variantId": plain.vid, "qty": 6}}}).Code)

	r = post(path+"/complete", map[string]any{})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, 2, batchQty(t, l, b1, store))
	require.Equal(t, 5, batchQty(t, l, s1, store))
	require.Equal(t, 4, batchQty(t, l, s2, store))
	require.Equal(t, 0, batchQty(t, l, unk, store))
	require.Equal(t, 11, stockOf(t, l, store, a.vid))
	require.Equal(t, 6, stockOf(t, l, store, plain.vid))
	for _, x := range r.Body["lines"].([]any) {
		if ln := x.(map[string]any); ln["batchNo"] == "B1" {
			require.EqualValues(t, 3, ln["expected"])
			require.EqualValues(t, -1, ln["difference"])
		}
	}
	require.Empty(t, reconcile(t, l))

	// A tracked product counted with no batch line: counted as a whole
	// (decrease earliest expiry first).
	path = newCount()
	require.Equal(t, 200, put(path, map[string]any{"lines": []any{map[string]any{"variantId": a.vid, "qty": 9}}}).Code)
	g = get(t, host, path, tok)
	require.Equal(t, false, g.Body["lines"].([]any)[0].(map[string]any)["byBatch"])
	require.Empty(t, g.Body["uncountedBatches"])
	require.Equal(t, 200, post(path+"/complete", map[string]any{}).Code)
	require.Equal(t, 9, stockOf(t, l, store, a.vid))
	require.Equal(t, 2, batchQty(t, l, s2, store), "the earliest expiry went down")
	require.Empty(t, reconcile(t, l))

	// Batch lines plus a line with no batch: that line counts the unknown batch.
	path = newCount()
	require.Equal(t, 200, put(path, map[string]any{"lines": []any{
		map[string]any{"variantId": a.vid, "batchId": b1, "qty": 2},
		map[string]any{"variantId": a.vid, "qty": 1},
	}}).Code)
	require.Equal(t, 200, post(path+"/complete", map[string]any{}).Code)
	require.Equal(t, 1, batchQty(t, l, unk, store))
	require.Equal(t, 0, batchQty(t, l, s1, store))
	require.Equal(t, 0, batchQty(t, l, s2, store))
	require.Equal(t, 3, stockOf(t, l, store, a.vid))
	require.Empty(t, reconcile(t, l))

	// Module off: batch fields are refused.
	l2, tok2, store2 := pharmacyShop(t, "bc-cnt2")
	c := stockedProduct(t, l2, tok2, "BC-OFF", store2, 2)
	require.Equal(t, 200, putBusiness(t, l2, tok2, map[string]any{"modules": []string{"stock_counts"}}).Code)
	cr := do(t, rq{method: "POST", host: tenants[l2].Host, path: "/api/stock-counts", token: tok2, body: map[string]any{"storeId": store2}})
	require.Equal(t, 201, cr.Code, cr.Raw)
	r = do(t, rq{method: "PUT", host: tenants[l2].Host, path: fmt.Sprintf("/api/stock-counts/%v", cr.Body["id"]), token: tok2,
		body: map[string]any{"lines": []any{map[string]any{"variantId": c.vid, "batchNo": "Z1", "expiry": day(50), "qty": 1}}}})
	require.Equal(t, 403, r.Code, r.Raw)
}
