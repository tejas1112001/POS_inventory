//go:build integration

package isolation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Industry Phase 3 item 2: receiving with batch and expiry (design §2, A26).

func day(offset int) string {
	return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
}

func bline(item any, qty int, batches ...map[string]any) map[string]any {
	l := rline(item, qty)
	l["batches"] = batches
	return l
}

// bat is a received batch; a pharmacy needs its MRP (pharmacy plan §6.2).
func bat(no, expiry string, qty int) map[string]any {
	return map[string]any{"batchNo": no, "expiry": expiry, "quantity": qty, "mrp": 500}
}

// pharmacyShop provisions a shop with the pharmacy preset (batch_expiry on).
func pharmacyShop(t *testing.T, slug string) (l, tok string, store float64) {
	t.Helper()
	l = billingTenant(t, slug)
	setModules(t, l, "pharmacy")
	tok, _ = ownerLogin(t, l)
	return l, tok, storeIDs(t, l, tok)[0]
}

func batchByNo(t *testing.T, l string, vid float64, no string) (id int64, expiry string) {
	t.Helper()
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT id, to_char(expiry_date, 'YYYY-MM-DD') FROM `+schema(l)+`.batches
		WHERE variant_id = $1 AND batch_no = $2`, int64(vid), no).Scan(&id, &expiry))
	return id, expiry
}

func TestBatchReceiving(t *testing.T) {
	ctx := context.Background()
	l, tok, store := pharmacyShop(t, "br-recv")
	host := tenants[l].Host
	sup := createSupplier(t, l, tok, "Pharma Distributors")
	a := stockedProduct(t, l, tok, "BR-PARA", store, 0) // tracked (new product, module on)
	plain := stockedProduct(t, l, tok, "BR-BAG", store, 0)
	require.Equal(t, 200, setTracking(t, l, tok, plain, false).Code)

	c := postPurchase(t, l, tok, purchaseBody(sup, "BR-1", "Ordered",
		purchaseLine(a.pid, a.vid, store, 100, 10), purchaseLine(plain.pid, plain.vid, store, 5, 2)), nil)
	require.Equal(t, 201, c.Code, c.Raw)
	id := c.Body["id"]
	items := c.Body["items"].([]any)
	ia, ip := items[0].(map[string]any)["id"], items[1].(map[string]any)["id"]
	require.Equal(t, true, items[0].(map[string]any)["tracksBatches"])
	require.Equal(t, false, items[1].(map[string]any)["tracksBatches"])

	// Tracked lines need batches; they must add up; untracked lines refuse them.
	r := postReceipt(t, l, tok, id, map[string]any{"lines": []any{rline(ia, 10)}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "lines[0].batches")
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 10, bat("B1", day(400), 6))}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "add up to 6")
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ip, 1, bat("X", day(400), 1))}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "does not track batches")
	// Expired refused; bad dates refused; the reserved name refused.
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 1, bat("OLD", day(-1), 1))}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "This batch has expired")
	require.Equal(t, 400, postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 1, bat("D", "13/2030", 1))}}, nil).Code)
	require.Equal(t, 400, postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 1, bat("unbatched", day(400), 1))}}, nil).Code)
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 1, bat("FAR", day(365*12), 1))}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "confirm")

	// Near expiry (within 90 days): 409 with details, then accepted.
	near := bline(ia, 30, bat("B-NEAR", day(20), 10), bat("B-LONG", "12/2030", 15), map[string]any{
		"batchNo": "B-LONG2", "expiry": day(500), "mfgDate": day(-30), "mrp": 45.5, "quantity": 5})
	body := map[string]any{"lines": []any{near, rline(ip, 2)}}
	r = postReceipt(t, l, tok, id, body, nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, "NEAR_EXPIRY", codeOf(r))
	det := r.Body["details"].(map[string]any)
	require.Len(t, det["batches"], 1)
	require.Equal(t, 0, stockOf(t, l, store, a.vid), "nothing received")
	body["acceptNearExpiry"] = true
	r = postReceipt(t, l, tok, id, body, map[string]string{"Idempotency-Key": "br-recv-0001"})
	require.Equal(t, 201, r.Code, r.Raw)
	lines := r.Body["receipt"].(map[string]any)["lines"].([]any)
	bs := lines[0].(map[string]any)["batches"].([]any)
	require.Len(t, bs, 3)
	require.Equal(t, "B-NEAR", bs[0].(map[string]any)["batchNo"], "earliest expiry first")
	require.Equal(t, true, bs[0].(map[string]any)["nearExpiryAccepted"])
	require.Empty(t, lines[1].(map[string]any)["batches"])
	require.Equal(t, 30, stockOf(t, l, store, a.vid))
	nearID, _ := batchByNo(t, l, a.vid, "B-NEAR")
	longID, longExp := batchByNo(t, l, a.vid, "B-LONG")
	require.Equal(t, "2030-12-31", longExp, "MM/YYYY is the month's last day")
	require.Equal(t, 10, batchQty(t, l, nearID, store))
	require.Equal(t, 15, batchQty(t, l, longID, store))
	var cost, mrp *float64
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT cost::float8, mrp::float8 FROM `+schema(l)+`.batches WHERE batch_no = 'B-LONG2'`).Scan(&cost, &mrp))
	require.Equal(t, 10.0, *cost, "cost defaults to the purchase line's")
	require.Equal(t, 45.5, *mrp)
	var accepted string
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT after->'nearExpiryAccepted'->0->>'batchNo' FROM `+schema(l)+`.audit_log
		WHERE action = 'purchase.receive' ORDER BY id DESC LIMIT 1`).Scan(&accepted))
	require.Equal(t, "B-NEAR", accepted)
	again := postReceipt(t, l, tok, id, body, map[string]string{"Idempotency-Key": "br-recv-0001"})
	require.Equal(t, 201, again.Code)
	require.Equal(t, 30, stockOf(t, l, store, a.vid), "replay receives nothing")

	// A known batch number is reused with the same expiry, refused with another.
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 5, bat("B-LONG", "2030-12-01", 5))}}, nil)
	require.Equal(t, 400, r.Code, r.Raw)
	require.Contains(t, r.Raw, "another expiry")
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 5, bat("b-long", "12/2030", 5))}}, nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, 20, batchQty(t, l, longID, store), "same batch (any case)")

	// Receive everything in one click is refused while tracked lines are open.
	pu := putPurchase(t, l, tok, id, map[string]any{"status": "Received"})
	require.Equal(t, 409, pu.Code, pu.Raw)
	require.Contains(t, pu.Raw, "batch and expiry")
	r = postPurchase(t, l, tok, purchaseBody(sup, "BR-2", "Received", purchaseLine(a.pid, a.vid, store, 1, 10)), nil)
	require.Equal(t, 409, r.Code, r.Raw)
	require.Equal(t, 201, postPurchase(t, l, tok, purchaseBody(sup, "BR-3", "Received", purchaseLine(plain.pid, plain.vid, store, 1, 2)), nil).Code,
		"untracked products receive in one click as before")

	// GET receipts shows the batches; the rule of §1.4 holds.
	g := get(t, host, fmt.Sprintf("/api/purchases/%v/receipts", id), tok)
	require.Equal(t, 200, g.Code, g.Raw)
	require.Contains(t, g.Raw, `"batchNo":"B-NEAR"`)
	require.Empty(t, reconcile(t, l))

	// Labels print batch and expiry; a batch of another variant is not found.
	lab := do(t, rq{method: "POST", host: host, path: "/api/labels/pdf", token: tok, body: map[string]any{"layoutId": "a4-3x8",
		"items": []any{map[string]any{"variantId": a.vid, "copies": 2, "batchId": longID}}}})
	require.Equal(t, 200, lab.Code, lab.Raw)
	lab = do(t, rq{method: "POST", host: host, path: "/api/labels/pdf", token: tok, body: map[string]any{"layoutId": "a4-3x8",
		"items": []any{map[string]any{"variantId": plain.vid, "copies": 1, "batchId": longID}}}})
	require.Equal(t, 404, lab.Code, lab.Raw)

	// Module off: batch fields refused, nothing else changes.
	require.Equal(t, 200, putBusiness(t, l, tok, map[string]any{"modules": []string{"barcode_labels", "stock_counts"}}).Code)
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{bline(ia, 1, bat("M", day(400), 1))}}, nil)
	require.Equal(t, 403, r.Code, r.Raw)
	require.Equal(t, "MODULE_OFF", codeOf(r))
	r = postReceipt(t, l, tok, id, map[string]any{"lines": []any{rline(ia, 1)}}, nil)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Empty(t, reconcile(t, l))
}

// Two receipts of different purchases creating the same new batch at once.
func TestBatchReceivingConcurrentSameBatch(t *testing.T) {
	l, tok, store := pharmacyShop(t, "br-race")
	sup := createSupplier(t, l, tok, "Race Pharma")
	a := stockedProduct(t, l, tok, "BR-RACE", store, 0)
	var ids, items []any
	for i := range 6 {
		c := postPurchase(t, l, tok, purchaseBody(sup, fmt.Sprintf("BRR-%d", i), "Ordered", purchaseLine(a.pid, a.vid, store, 4, 10)), nil)
		require.Equal(t, 201, c.Code, c.Raw)
		ids = append(ids, c.Body["id"])
		items = append(items, c.Body["items"].([]any)[0].(map[string]any)["id"])
	}
	var wg sync.WaitGroup
	codes := make([]int, len(ids))
	for i := range ids {
		wg.Go(func() {
			codes[i] = postReceipt(t, l, tok, ids[i], map[string]any{"lines": []any{bline(items[i], 4, bat("SAME-1", "06/2031", 4))}}, nil).Code
		})
	}
	wg.Wait()
	for _, c := range codes {
		require.Equal(t, 201, c)
	}
	bid, _ := batchByNo(t, l, a.vid, "SAME-1")
	require.Equal(t, 24, batchQty(t, l, bid, store))
	require.Equal(t, 24, stockOf(t, l, store, a.vid))
	require.Empty(t, reconcile(t, l))
}
