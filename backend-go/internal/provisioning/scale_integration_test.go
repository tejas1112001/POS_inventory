//go:build integration

package provisioning_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
)

// TestParallelRunOverManySchemas (Phase 3 item 4, TENANCY §8 "50+ schemas"):
// 60 tenants are migrated with 16 workers, twice (the second run only
// re-applies the post-step, as a deploy does). Before the advisory lock in
// db.GrantTenantPrivileges, parallel post-steps failed with "tuple
// concurrently updated" on the shared extensions schema.
func TestParallelRunOverManySchemas(t *testing.T) {
	ctx := context.Background()
	const n = 60
	ids := make([]string, n)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 6)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := provision(t, slug("scale"))
			mu.Lock()
			ids[i] = r.TenantID
			mu.Unlock()
		}()
	}
	wg.Wait()

	for run := 0; run < 2; run++ {
		rep, err := migrate.RunTenants(ctx, tdb.MigratorDSN, migrate.RunOptions{TenantIDs: ids, Parallelism: 16})
		require.NoError(t, err)
		failed := []string{}
		for _, it := range rep.Items {
			if it.Status != "succeeded" {
				failed = append(failed, it.TenantID+": "+it.Status+" "+errString(it.Err))
			}
		}
		require.Empty(t, failed, "run %d", run+1)
		require.Equal(t, "succeeded", rep.Status, "run %d", run+1)
		require.Len(t, rep.Items, n)
	}

	// Every schema is at the latest version and passes the drift check.
	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	var behind int
	require.NoError(t, tdb.Admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.tenants WHERE id = ANY($1) AND schema_version <> $2`, ids, latest).Scan(&behind))
	require.Zero(t, behind)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
