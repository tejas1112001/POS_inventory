//go:build integration

package tenantio_test

import (
	"context"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

// TestNightlyExportsAndRetention (S10 item 4, B5): live tenants get a
// nightly export once a night; retention keeps the last 7 nightly and the
// last 4 Sunday ones (India time), the latest verified export, and every
// manual and final export; pruned files are deleted from storage.
func TestNightlyExportsAndRetention(t *testing.T) {
	ctx := context.Background()
	adm := tdb.Admin(t)
	e := exporter()
	live := newShop(t, "nightly-live")
	gone := newShop(t, "nightly-gone")
	_, err := adm.Exec(ctx, `UPDATE platform.tenants SET status = 'pending_deletion', deletion_requested_at = now() WHERE id = $1`, gone.ID)
	require.NoError(t, err)

	_, _, err = e.QueueNightly(ctx)
	require.NoError(t, err)
	require.Equal(t, []exportRow{{Kind: "nightly", Status: "queued", RequestedBy: tenantio.SystemNightly}}, exportsOf(t, live.ID))
	for _, r := range exportsOf(t, gone.ID) {
		require.NotEqual(t, "nightly", r.Kind, "no nightly export for a tenant pending deletion")
	}
	_, _, err = e.QueueNightly(ctx)
	require.NoError(t, err)
	require.Len(t, exportsOf(t, live.ID), 1, "once a night")
	var tonight uuid.UUID
	require.NoError(t, adm.QueryRow(ctx, `SELECT id FROM platform.tenant_exports WHERE tenant_id = $1`, live.ID).Scan(&tonight))
	_, err = e.Run(ctx, tonight)
	require.NoError(t, err)

	// 30 earlier nights, a manual and a final export long ago, and one old
	// nightly export whose restore test passed.
	ist := time.FixedZone("IST", 5*3600+1800)
	type row struct {
		id  uuid.UUID
		at  time.Time
		key string
	}
	var nights []row
	var tonightAt time.Time
	require.NoError(t, adm.QueryRow(ctx, `SELECT created_at FROM platform.tenant_exports WHERE id = $1`, tonight).Scan(&tonightAt))
	nights = append(nights, row{id: tonight, at: tonightAt})
	insert := func(kind string, at time.Time) row {
		r := row{id: uuid.New(), at: at, key: "exports/" + live.ID + "/" + uuid.NewString() + ".tar.zst.age"}
		require.NoError(t, store.Put(ctx, r.key, "application/octet-stream", []byte("old export")))
		_, err := adm.Exec(ctx, `INSERT INTO platform.tenant_exports (id, tenant_id, kind, status, requested_by, object_key, size_bytes,
			sha256, manifest_sha256, created_at, finished_at) VALUES ($1, $2, $3, 'succeeded', 'test', $4, 10, repeat('a', 64), repeat('b', 64), $5, $5)`,
			r.id, live.ID, kind, r.key, at)
		require.NoError(t, err)
		return r
	}
	for i := 1; i <= 30; i++ {
		nights = append(nights, insert("nightly", tonightAt.Add(-time.Duration(i)*24*time.Hour)))
	}
	manual := insert("manual", tonightAt.Add(-60*24*time.Hour))
	final := insert("final", tonightAt.Add(-61*24*time.Hour))
	verified := nights[25]
	_, err = adm.Exec(ctx, `INSERT INTO platform.tenant_restores (id, export_id, tenant_id, mode, status, requested_by, created_at, finished_at)
		VALUES ($1, $2, $3, 'test', 'succeeded', 'test', now(), now())`, uuid.New(), verified.id, live.ID)
	require.NoError(t, err)

	// The keep set, worked out independently.
	sort.Slice(nights, func(i, j int) bool { return nights[i].at.After(nights[j].at) })
	keep := map[uuid.UUID]bool{verified.id: true}
	sundays := 0
	for i, n := range nights {
		if i < 7 {
			keep[n.id] = true
		}
		if n.at.In(ist).Weekday() == time.Sunday {
			sundays++
			if sundays <= 4 {
				keep[n.id] = true
			}
		}
	}

	pruned, err := e.PruneNightly(ctx)
	require.NoError(t, err)
	require.Equal(t, len(nights)-len(keep), pruned)
	for _, n := range nights {
		var status string
		var expired *time.Time
		require.NoError(t, adm.QueryRow(ctx, `SELECT status, expired_at FROM platform.tenant_exports WHERE id = $1`, n.id).Scan(&status, &expired))
		if keep[n.id] {
			require.Equal(t, "succeeded", status, "kept: %s", n.at.In(ist).Format("Mon 2 Jan"))
			continue
		}
		require.Equal(t, "expired", status, "pruned: %s", n.at.In(ist).Format("Mon 2 Jan"))
		require.NotNil(t, expired)
		_, _, err := store.Get(ctx, n.key)
		require.Error(t, err, "the file is deleted")
	}
	for _, r := range []row{manual, final} {
		var status string
		require.NoError(t, adm.QueryRow(ctx, `SELECT status FROM platform.tenant_exports WHERE id = $1`, r.id).Scan(&status))
		require.Equal(t, "succeeded", status, "manual and final exports are never pruned")
		rc, _, err := store.Get(ctx, r.key)
		require.NoError(t, err)
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		require.True(t, strings.HasPrefix(string(b), "old"))
	}
	again, err := e.PruneNightly(ctx)
	require.NoError(t, err)
	require.Zero(t, again)
}

// TestFinalExportGetsRestoreTest (S10 item 4): a final export that
// succeeded gets a restore test queued once.
func TestFinalExportGetsRestoreTest(t *testing.T) {
	ctx := context.Background()
	adm := tdb.Admin(t)
	e := exporter()
	gone := newShop(t, "final-test")
	_, err := adm.Exec(ctx, `UPDATE platform.tenants SET status = 'pending_deletion', deletion_requested_at = now() WHERE id = $1`, gone.ID)
	require.NoError(t, err)
	ids, err := e.QueueFinals(ctx)
	require.NoError(t, err)
	var fin uuid.UUID
	for _, id := range ids {
		var tid string
		require.NoError(t, adm.QueryRow(ctx, `SELECT tenant_id FROM platform.tenant_exports WHERE id = $1`, id).Scan(&tid))
		if tid == gone.ID {
			fin = id
		}
	}
	require.NotEqual(t, uuid.Nil, fin)
	tests, err := e.QueueFinalTests(ctx)
	require.NoError(t, err)
	var mine int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM platform.tenant_restores WHERE tenant_id = $1`, gone.ID).Scan(&mine))
	require.Zero(t, mine, "not before the export succeeded")
	_ = tests
	_, err = e.Run(ctx, fin)
	require.NoError(t, err)
	_, err = e.QueueFinalTests(ctx)
	require.NoError(t, err)
	var mode, status, by string
	require.NoError(t, adm.QueryRow(ctx, `SELECT mode, status, requested_by FROM platform.tenant_restores WHERE tenant_id = $1 AND export_id = $2`,
		gone.ID, fin).Scan(&mode, &status, &by))
	require.Equal(t, []string{"test", "queued", tenantio.SystemFinal}, []string{mode, status, by})
	_, err = e.QueueFinalTests(ctx)
	require.NoError(t, err)
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM platform.tenant_restores WHERE tenant_id = $1`, gone.ID).Scan(&mine))
	require.Equal(t, 1, mine, "queued once")
}
