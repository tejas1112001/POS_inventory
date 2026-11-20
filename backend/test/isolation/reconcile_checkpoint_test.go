//go:build integration

package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

func reconcileCheckpointed(t *testing.T, l string) inventory.ReconcileResult {
	t.Helper()
	s, r, err := tenancy.SchemaFor(tenants[l].ID)
	require.NoError(t, err)
	sess := tenancy.NewSessions(tdb.AppPool(t, 1), 0, nil, nil)
	var out inventory.ReconcileResult
	require.NoError(t, sess.InTenantTx(context.Background(), tenancy.Tenant{ID: tenants[l].ID, Schema: s, Role: r},
		tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			var err error
			out, err = inventory.ReconcileCheckpointed(context.Background(), tx, time.Now())
			return err
		}))
	return out
}

func mismatchesFor(res inventory.ReconcileResult, variantID int64) []inventory.Mismatch {
	var out []inventory.Mismatch
	for _, m := range res.Mismatches {
		if m.VariantID == variantID {
			out = append(out, m)
		}
	}
	return out
}

// TestCheckpointedReconcile (ARCHITECTURE_REVIEW M2): the first check is
// full and records checkpoints; later nights add only newer movements; a
// wrong checkpoint costs a re-check but is never reported; real drift is
// reported; checkpoints older than a week trigger a full check again.
func TestCheckpointedReconcile(t *testing.T) {
	ctx := context.Background()
	a := admin(t)
	sc := schema("c")
	tok, _ := ownerLogin(t, "c")
	store := storeIDs(t, "c", tok)[0]
	p := stockedProduct(t, "c", tok, "CKPT-1", store, 9)
	vid := int64(p.vid)

	// Its opening stock is older than the checkpoint lag (one hour).
	_, err := a.Exec(ctx, `UPDATE `+sc+`.stock_movements SET created_at = created_at - interval '3 hours' WHERE variant_id = $1`, vid)
	require.NoError(t, err)
	_, err = a.Exec(ctx, `DELETE FROM `+sc+`.ledger_checkpoint_marks`)
	require.NoError(t, err)

	// 1. No checkpoints: full check, checkpoints recorded.
	res := reconcileCheckpointed(t, "c")
	require.True(t, res.Full)
	require.Empty(t, mismatchesFor(res, vid))
	var cp int64
	require.NoError(t, a.QueryRow(ctx, `SELECT qty FROM `+sc+`.ledger_checkpoints WHERE ledger = 'stock' AND item_id = $1`, vid).Scan(&cp))
	require.Equal(t, int64(9), cp)

	// 2. A sale after the checkpoint: incremental, still in step.
	sold := sell(t, "c", tok, posSale(store, line(p, 2)), nil)
	require.Equal(t, 201, sold.Code, sold.Raw)
	res = reconcileCheckpointed(t, "c")
	require.False(t, res.Full)
	require.Empty(t, mismatchesFor(res, vid))

	// 3. A wrong checkpoint is caught by the whole-ledger re-check.
	_, err = a.Exec(ctx, `UPDATE `+sc+`.ledger_checkpoints SET qty = qty + 4 WHERE ledger = 'stock' AND item_id = $1`, vid)
	require.NoError(t, err)
	res = reconcileCheckpointed(t, "c")
	require.False(t, res.Full)
	require.GreaterOrEqual(t, res.Rechecked, 1)
	require.Empty(t, mismatchesFor(res, vid), "no false report")

	// 4. Stock changed outside the ledger is reported with exact figures.
	_, err = a.Exec(ctx, `UPDATE `+sc+`.inventories SET qty = qty + 2 WHERE variant_id = $1`, vid)
	require.NoError(t, err)
	defer func() { _, _ = a.Exec(ctx, `UPDATE `+sc+`.inventories SET qty = qty - 2 WHERE variant_id = $1`, vid) }()
	res = reconcileCheckpointed(t, "c")
	require.False(t, res.Full)
	got := mismatchesFor(res, vid)
	require.Len(t, got, 1)
	require.Equal(t, 9, got[0].Qty)
	require.Equal(t, 7, got[0].LedgerSum)

	// 5. Checkpoints older than a week: full check, rebuilt (the wrong
	// one is gone).
	_, err = a.Exec(ctx, `UPDATE `+sc+`.ledger_checkpoint_marks SET taken_at = now() - interval '8 days'`)
	require.NoError(t, err)
	res = reconcileCheckpointed(t, "c")
	require.True(t, res.Full)
	require.Len(t, mismatchesFor(res, vid), 1)
	require.NoError(t, a.QueryRow(ctx, `SELECT qty FROM `+sc+`.ledger_checkpoints WHERE ledger = 'stock' AND item_id = $1`, vid).Scan(&cp))
	require.Equal(t, int64(9), cp, "only the backdated opening stock is old enough to be in a checkpoint")
}

// TestReconcileQueuedPerShop: the nightly job queues one reconcile per live
// shop on its own queue.
func TestReconcileQueuedPerShop(t *testing.T) {
	n, err := theApp.Worker().QueueReconciles(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 3)
	var queued int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM river.river_job
		WHERE kind = 'ledger_reconcile_tenant' AND queue = 'tenant_maintenance'`).Scan(&queued))
	require.GreaterOrEqual(t, queued, 3)
}
