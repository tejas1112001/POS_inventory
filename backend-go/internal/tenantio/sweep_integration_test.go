//go:build integration

package tenantio_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

type exportRow struct {
	Kind, Status, RequestedBy string
	Files                     bool
}

func exportsOf(t *testing.T, tenantID string) []exportRow {
	t.Helper()
	rows, err := tdb.Admin(t).Query(context.Background(), `SELECT kind, status, requested_by, include_files
		FROM platform.tenant_exports WHERE tenant_id = $1 ORDER BY created_at, id`, tenantID)
	require.NoError(t, err)
	defer rows.Close()
	var out []exportRow
	for rows.Next() {
		var r exportRow
		require.NoError(t, rows.Scan(&r.Kind, &r.Status, &r.RequestedBy, &r.Files))
		out = append(out, r)
	}
	return out
}

func TestSweepQueuesFinalExports(t *testing.T) {
	ctx := context.Background()
	adm := tdb.Admin(t)
	e := exporter()
	gone := newShop(t, "sweep-gone")
	busy := newShop(t, "sweep-busy")
	live := newShop(t, "sweep-live")
	for _, s := range []shop{gone, busy} {
		_, err := adm.Exec(ctx, `UPDATE platform.tenants SET status = 'pending_deletion', deletion_requested_at = now() WHERE id = $1`, s.ID)
		require.NoError(t, err)
	}
	manual := queue(t, busy.ID, false) // another export is waiting

	ids, err := e.QueueFinals(ctx)
	require.NoError(t, err, "a busy tenant is skipped, not an error")
	require.Equal(t, []exportRow{{Kind: "final", Status: "queued", RequestedBy: tenantio.SystemFinal, Files: true}}, exportsOf(t, gone.ID))
	require.Len(t, exportsOf(t, busy.ID), 1, "busy: only the manual export")
	require.Empty(t, exportsOf(t, live.ID), "live tenants get no final export")
	again, err := e.QueueFinals(ctx)
	require.NoError(t, err)
	require.NotContains(t, again, ids[0])
	require.Len(t, exportsOf(t, gone.ID), 1, "one final export at a time")

	// The sweep lists what waits, oldest first.
	queued, err := e.Queued(ctx, 1000)
	require.NoError(t, err)
	require.Less(t, slices.Index(queued, manual), slices.Index(queued, ids[0]))

	// Done: no new final export.
	_, err = e.Run(ctx, ids[0])
	require.NoError(t, err)
	_, err = e.Run(ctx, manual)
	require.NoError(t, err)
	_, err = e.QueueFinals(ctx)
	require.NoError(t, err)
	require.Len(t, exportsOf(t, gone.ID), 1)
	require.Equal(t, "final", exportsOf(t, busy.ID)[1].Kind, "queued once the manual export finished")

	// A failed final export is retried, but not before 6 hours.
	_, err = adm.Exec(ctx, `UPDATE platform.tenant_exports SET status = 'failed', error = 'test' WHERE tenant_id = $1`, gone.ID)
	require.NoError(t, err)
	_, err = e.QueueFinals(ctx)
	require.NoError(t, err)
	require.Len(t, exportsOf(t, gone.ID), 1, "failed just now: wait")
	_, err = adm.Exec(ctx, `UPDATE platform.tenant_exports SET created_at = now() - interval '7 hours' WHERE tenant_id = $1`, gone.ID)
	require.NoError(t, err)
	_, err = e.QueueFinals(ctx)
	require.NoError(t, err)
	rows := exportsOf(t, gone.ID)
	require.Len(t, rows, 2)
	require.Equal(t, "queued", rows[1].Status)
}

func TestSweepFailsStaleExports(t *testing.T) {
	ctx := context.Background()
	s := newShop(t, "sweep-stale")
	id := queue(t, s.ID, false)
	fresh := newShop(t, "sweep-fresh")
	running := queue(t, fresh.ID, false)
	_, err := tdb.Admin(t).Exec(ctx, `UPDATE platform.tenant_exports SET status = 'running',
		started_at = CASE WHEN id = $1 THEN now() - interval '7 hours' ELSE now() END WHERE id = ANY($2)`,
		id, []uuid.UUID{id, running})
	require.NoError(t, err)
	n, err := exporter().FailStale(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))
	var stale, other, msg string
	require.NoError(t, tdb.Admin(t).QueryRow(ctx, `SELECT
			(SELECT status FROM platform.tenant_exports WHERE id = $1), (SELECT status FROM platform.tenant_exports WHERE id = $2),
			(SELECT error FROM platform.tenant_exports WHERE id = $1)`, id, running).Scan(&stale, &other, &msg))
	require.Equal(t, []string{"failed", "interrupted", "running"}, []string{stale, msg, other})
	_, err = tdb.Admin(t).Exec(ctx, `UPDATE platform.tenant_exports SET status = 'failed', error = 'test', finished_at = now() WHERE id = $1`, running)
	require.NoError(t, err)
}
