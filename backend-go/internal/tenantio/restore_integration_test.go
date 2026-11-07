//go:build integration

package tenantio_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

// clock moves forward a minute per restore so scratch names never repeat.
var clock = time.Now().UTC()

func restorer() *tenantio.Restorer {
	return &tenantio.Restorer{DSN: tdb.MigratorDSN, Storage: store, Identity: ident, ManifestKey: mkey,
		Now: func() time.Time { return clock }}
}

func restore(t *testing.T, s shop, exportID uuid.UUID, mode string) (tenantio.Report, error) {
	t.Helper()
	clock = clock.Add(time.Minute)
	r := restorer()
	id, err := r.Enqueue(context.Background(), s.ID, exportID, mode, "test")
	require.NoError(t, err)
	return r.Run(context.Background(), id)
}

func schemaExists(t *testing.T, name string) bool {
	t.Helper()
	var ok bool
	require.NoError(t, tdb.Admin(t).QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1) OR EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1 || '_rw')`,
		name).Scan(&ok))
	return ok
}

func dropScratch(t *testing.T, name string) {
	t.Helper()
	s, err := tenancy.ParseScratchSchema(name)
	require.NoError(t, err)
	require.NoError(t, migrate.DropScratch(context.Background(), tdb.MigratorDSN, s))
}

func count(t *testing.T, schema, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, tdb.Admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+pgx.Identifier{schema, table}.Sanitize()).Scan(&n))
	return n
}

func TestRestoreIntoScratchMatchesTheExport(t *testing.T) {
	ctx := context.Background()
	a, b := newShop(t, "rst-a"), newShop(t, "rst-b")
	rec, _ := export(t, a, false)
	rep, err := restore(t, a, rec.ID, tenantio.ModeScratch)
	require.NoError(t, err)
	t.Cleanup(func() { dropScratch(t, rep.Scratch) })

	require.True(t, rep.OK)
	require.True(t, rep.Kept)
	require.Regexp(t, `^r_`+a.ID+`_[0-9]{12}$`, rep.Scratch)
	require.Len(t, rep.Tables, len(tenantio.Tables))
	for _, c := range rep.Tables {
		require.True(t, c.OK, c.Name)
		require.Equal(t, c.Rows, c.RowsLoaded, c.Name)
		require.Equal(t, c.Rows, c.RowsRead, c.Name)
		require.Equal(t, c.SHA256, c.SHA256Read, c.Name)
		require.Equal(t, count(t, a.Schema, c.Name), count(t, rep.Scratch, c.Name), "%s: same rows as live", c.Name)
	}
	require.Equal(t, rec.Rows, rep.Rows)
	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	require.Equal(t, latest, rep.MigratedTo)
	for _, ex := range []string{"refresh_tokens", "auth_tokens", "idempotency_keys", "pos_locks"} {
		require.Zero(t, count(t, rep.Scratch, ex), "%s is empty after a restore", ex)
	}

	adm := tdb.Admin(t)
	// Sequences continue after the restored IDs.
	var maxID, newID int64
	require.NoError(t, adm.QueryRow(ctx, `SELECT max(id) FROM `+pgx.Identifier{rep.Scratch, "customers"}.Sanitize()).Scan(&maxID))
	require.NoError(t, adm.QueryRow(ctx, `INSERT INTO `+pgx.Identifier{rep.Scratch, "customers"}.Sanitize()+` (name) VALUES ('new') RETURNING id`).Scan(&newID))
	require.Greater(t, newID, maxID)

	// The platform record.
	var status, scratch string
	var dropAfter time.Time
	var ok bool
	require.NoError(t, adm.QueryRow(ctx, `SELECT status, scratch_schema, drop_after, (result->>'ok')::bool FROM platform.tenant_restores WHERE id = $1`,
		rep.ID).Scan(&status, &scratch, &dropAfter, &ok))
	require.Equal(t, "succeeded", status)
	require.Equal(t, rep.Scratch, scratch)
	require.True(t, ok)
	require.WithinDuration(t, clock.Add(7*24*time.Hour), dropAfter, time.Minute)

	// The scratch role reaches only its schema; the app roles cannot act as it.
	s, _ := tenancy.ParseScratchSchema(rep.Scratch)
	cfg, err := migrate.ScratchSessionConfig(tdb.MigratorDSN, s, true, "test")
	require.NoError(t, err)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, `SELECT count(*) FROM `+pgx.Identifier{b.Schema, "users"}.Sanitize())
	require.ErrorContains(t, err, "permission denied")
	_, err = conn.Exec(ctx, `SELECT count(*) FROM `+pgx.Identifier{a.Schema, "users"}.Sanitize())
	require.ErrorContains(t, err, "permission denied", "not even the live schema it was restored from")
	for _, member := range []string{migrate.RoleApp, migrate.RoleJobs} {
		var isMember bool
		require.NoError(t, adm.QueryRow(ctx, `SELECT pg_has_role($1, $2, 'MEMBER')`, member, s.Role()).Scan(&isMember))
		require.False(t, isMember, "%s cannot act as the scratch role", member)
	}
	_, err = tenancy.ParseSchemaName(rep.Scratch)
	require.ErrorIs(t, err, tenancy.ErrInvalidName, "the resolver can never serve it")
}

func TestRestoreTestModeDropsTheScratchSchema(t *testing.T) {
	a := newShop(t, "rst-test")
	rec, _ := export(t, a, true)
	rep, err := restore(t, a, uuid.Nil, tenantio.ModeTest) // nil: the latest export
	require.NoError(t, err)
	require.Equal(t, rec.ID, rep.ExportID)
	require.True(t, rep.OK)
	require.False(t, rep.Kept)
	require.False(t, schemaExists(t, rep.Scratch), "schema and role dropped")
	var dropped *time.Time
	require.NoError(t, tdb.Admin(t).QueryRow(context.Background(), `SELECT dropped_at FROM platform.tenant_restores WHERE id = $1`,
		rep.ID).Scan(&dropped))
	require.NotNil(t, dropped)
}

// writes is every tenant table's insert/update/delete counter.
func writes(t *testing.T, except string) map[string]int64 {
	t.Helper()
	rows, err := tdb.Admin(t).Query(context.Background(), `SELECT schemaname || '.' || relname, n_tup_ins + n_tup_upd + n_tup_del
		FROM pg_stat_user_tables WHERE (schemaname LIKE 't\_%' OR schemaname LIKE 'r\_%') AND schemaname <> $1`, except)
	require.NoError(t, err)
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		require.NoError(t, rows.Scan(&k, &n))
		out[k] = n
	}
	require.NoError(t, rows.Err())
	return out
}

func TestRestoreWritesToNoOtherSchema(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "rst-iso")
	newShop(t, "rst-iso-other")
	rec, _ := export(t, a, false)
	// Let every earlier backend's statistics settle.
	time.Sleep(1500 * time.Millisecond)
	before := writes(t, "")
	rep, err := restore(t, a, rec.ID, tenantio.ModeScratch)
	require.NoError(t, err)
	t.Cleanup(func() { dropScratch(t, rep.Scratch) })
	// The scratch schema's own counters show the load, so statistics did flush.
	require.Eventually(t, func() bool {
		var n int64
		_ = tdb.Admin(t).QueryRow(ctx, `SELECT coalesce(sum(n_tup_ins), 0) FROM pg_stat_user_tables WHERE schemaname = $1`, rep.Scratch).Scan(&n)
		return n > 0
	}, 10*time.Second, 200*time.Millisecond)
	after := writes(t, rep.Scratch)
	for k, n := range before {
		require.Equal(t, n, after[k], "%s was written during the restore", k)
	}
}

func TestRestoreRefusesTamperedOrForeignExports(t *testing.T) {
	ctx := context.Background()
	a, b := newShop(t, "tamper-a"), newShop(t, "tamper-b")
	adm := tdb.Admin(t)
	noScratchOf := func(id string) {
		t.Helper()
		var n int
		require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'r\_' || $1 || '\_%'`, id).Scan(&n))
		require.Zero(t, n, "nothing created")
	}

	// The stored object changed.
	rec, data := export(t, a, false)
	data[len(data)/2] ^= 1
	require.NoError(t, store.Put(ctx, rec.ObjectKey, "application/octet-stream", data))
	_, err := restore(t, a, rec.ID, tenantio.ModeTest)
	require.Error(t, err)
	noScratchOf(a.ID)

	// The record does not match the object (an object swapped for another export's).
	rec2, _ := export(t, a, false)
	rec3, data3 := export(t, a, false)
	require.NoError(t, store.Put(ctx, rec2.ObjectKey, "application/octet-stream", data3))
	_, err = restore(t, a, rec2.ID, tenantio.ModeTest)
	require.ErrorIs(t, err, tenantio.ErrChecksum)
	noScratchOf(a.ID)
	var status, msg string
	require.NoError(t, adm.QueryRow(ctx, `SELECT status, error FROM platform.tenant_restores WHERE export_id = $1`, rec2.ID).Scan(&status, &msg))
	require.Equal(t, "failed", status)
	require.Contains(t, msg, "checksum")

	// Another tenant's export cannot be restored as this tenant.
	_, err = restorer().Enqueue(ctx, b.ID, rec3.ID, tenantio.ModeTest, "test")
	require.ErrorContains(t, err, "is not a succeeded export of tenant")
	err = sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tenantio.RequestRestore(ctx, tx, b.ID, rec3.ID, tenantio.ModeTest, "test")
		return err
	})
	require.Error(t, err)

	// A key that is not the export key.
	other, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	r := restorer()
	r.Identity = other
	clock = clock.Add(time.Minute)
	id, err := r.Enqueue(ctx, a.ID, rec3.ID, tenantio.ModeTest, "test")
	require.NoError(t, err)
	_, err = r.Run(ctx, id)
	require.Error(t, err)
	noScratchOf(a.ID)

	// The object is gone.
	require.NoError(t, store.Delete(ctx, rec3.ObjectKey))
	_, err = restore(t, a, rec3.ID, tenantio.ModeTest)
	require.ErrorIs(t, err, files.ErrNotFound)
}

func TestRestoreOfAnOlderSchemaVersion(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "rst-old")
	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	schema, _, err := tenancy.SchemaFor(a.ID)
	require.NoError(t, err)
	conn, err := migrate.TenantConn(tdb.MigratorDSN, schema, "test")
	require.NoError(t, err)
	p, err := migrate.NewTenantProvider(conn)
	require.NoError(t, err)
	_, err = p.DownTo(ctx, latest-1)
	require.NoError(t, err)
	_ = conn.Close()

	rec, _ := export(t, a, false)
	require.Equal(t, latest-1, rec.SchemaVersion)
	rep, err := restore(t, a, rec.ID, tenantio.ModeTest)
	require.NoError(t, err)
	require.Equal(t, latest-1, rep.SchemaVersion, "verified at the export's version")
	require.Equal(t, latest, rep.MigratedTo, "then migrated to the latest")
	require.True(t, rep.OK)
	fmt.Println("restored", len(rep.Tables), "tables at version", rep.SchemaVersion)
}

func TestRestoreComparisonCatchesADifference(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "rst-diff")
	rec, _ := export(t, a, false)
	clock = clock.Add(time.Minute)
	r := restorer()
	tenantio.SetAfterLoad(r, func(ctx context.Context, s tenancy.ScratchSchema) error {
		_, err := tdb.Admin(t).Exec(ctx, `UPDATE `+pgx.Identifier{s.String(), "customers"}.Sanitize()+` SET city = 'changed' WHERE name = 'Plain'`)
		return err
	})
	id, err := r.Enqueue(ctx, a.ID, rec.ID, tenantio.ModeScratch, "test")
	require.NoError(t, err)
	rep, err := r.Run(ctx, id)
	require.ErrorIs(t, err, tenantio.ErrMismatch)
	require.False(t, rep.OK)
	for _, c := range rep.Tables {
		if c.Name == "customers" {
			require.False(t, c.OK)
			require.Equal(t, c.Rows, c.RowsRead, "same rows, different content")
			require.NotEqual(t, c.SHA256, c.SHA256Read)
		} else {
			require.True(t, c.OK, c.Name)
		}
	}
	require.False(t, schemaExists(t, rep.Scratch), "a failed restore keeps nothing, even in scratch mode")
	var bad bool
	require.NoError(t, tdb.Admin(t).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.tenant_restores r,
		jsonb_array_elements(r.result->'tables') e WHERE r.id = $1 AND e->>'name' = 'customers' AND NOT (e->>'ok')::bool)`, id).Scan(&bad))
	require.True(t, bad, "the per-table result is recorded")
}
