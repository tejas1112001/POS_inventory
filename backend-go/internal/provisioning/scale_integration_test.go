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
// re-applies the post-step, as a deploy does). Parallel post-steps used to
// grant USAGE on the shared extensions schema per tenant, which failed with
// "tuple concurrently updated" and, once serialised by a global lock, with
// lock timeouts (ARCHITECTURE_REVIEW H1). Tenant roles now get it through
// the pos_tenant group, so the post-steps touch no shared catalog row.
// Two roles are first put back in the state of a role created before the
// group (direct grant, no membership): the run must add them to it.
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

	admin := tdb.Admin(t)
	for _, id := range ids[:2] {
		role := "t_" + id + "_rw"
		_, err := admin.Exec(ctx, "REVOKE pos_tenant FROM "+role)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "GRANT USAGE ON SCHEMA extensions TO "+role)
		require.NoError(t, err)
	}

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
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.tenants WHERE id = ANY($1) AND schema_version <> $2`, ids, latest).Scan(&behind))
	require.Zero(t, behind)

	// Every tenant role is in the group; the group holds USAGE on
	// extensions and nothing that reaches a tenant schema.
	var outside int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM unnest($1::text[]) id
		WHERE NOT pg_has_role('t_' || id || '_rw', 'pos_tenant', 'USAGE')`, ids).Scan(&outside))
	require.Zero(t, outside)
	var groupUsage, groupTenant bool
	require.NoError(t, admin.QueryRow(ctx, `SELECT has_schema_privilege('pos_tenant', 'extensions', 'USAGE'),
		has_schema_privilege('pos_tenant', 't_' || $1, 'USAGE')`, ids[0]).Scan(&groupUsage, &groupTenant))
	require.True(t, groupUsage)
	require.False(t, groupTenant)

	// A role that still has the old direct grant can be dropped.
	tgt, err := migrate.TargetFor(ids[0])
	require.NoError(t, err)
	require.NoError(t, migrate.DropTenantSchema(ctx, tdb.MigratorDSN, tgt))
	var gone bool
	require.NoError(t, admin.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, tgt.Role.String()).Scan(&gone))
	require.True(t, gone)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
