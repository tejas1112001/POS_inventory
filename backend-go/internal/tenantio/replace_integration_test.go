//go:build integration

package tenantio_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

func setTenantStatus(t *testing.T, s shop, status string) {
	t.Helper()
	_, err := tdb.Admin(t).Exec(context.Background(), `UPDATE platform.tenants SET status = $2 WHERE id = $1`, s.ID, status)
	require.NoError(t, err)
}

func TestReplaceRestoreSwapsTheSchemaAndEndsEverySession(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "repl")
	other := newShop(t, "repl-other")
	adm := tdb.Admin(t)
	tbl := func(schema, table string) string { return pgx.Identifier{schema, table}.Sanitize() }
	_, err := adm.Exec(ctx, `INSERT INTO `+tbl(a.Schema, "email_outbox")+` (idempotency_key, kind, to_addr, template, template_data)
		VALUES ('waiting-at-export', 'customer', 'c@example.test', 'receipt', '{}')`)
	if err != nil {
		t.Logf("outbox seed skipped: %v", err)
	}
	setTenantStatus(t, a, "read_only")
	rec, _ := export(t, a, false)
	var versionBefore int
	require.NoError(t, adm.QueryRow(ctx, `SELECT token_version FROM `+tbl(a.Schema, "users")+` WHERE id = 1`).Scan(&versionBefore))

	// Changes after the export, and a live session.
	_, err = adm.Exec(ctx, `INSERT INTO `+tbl(a.Schema, "customers")+` (name) VALUES ('after-export')`)
	require.NoError(t, err)
	otherBefore := count(t, other.Schema, "customers")

	r := restorer()
	clock = clock.Add(time.Minute)
	_, err = r.Replace(ctx, a.ID, rec.ID, "wrong-slug", "test")
	require.ErrorIs(t, err, tenantio.ErrConfirm)
	setTenantStatus(t, a, "active")
	_, err = r.Replace(ctx, a.ID, rec.ID, a.Slug, "test")
	require.ErrorIs(t, err, tenantio.ErrNotReadOnly)
	setTenantStatus(t, a, "read_only")

	listen, err := pgx.Connect(ctx, tdb.AdminDSN.Reveal())
	require.NoError(t, err)
	defer func() { _ = listen.Close(ctx) }()
	_, err = listen.Exec(ctx, `LISTEN auth_changed`)
	require.NoError(t, err)

	rep, err := r.Replace(ctx, a.ID, rec.ID, a.Slug, "test")
	require.NoError(t, err)
	require.True(t, rep.OK)
	require.Regexp(t, `^t_`+a.ID+`_old_[0-9]{12}$`, rep.OldSchema)

	// The live schema is the export; the old one keeps the later change.
	var after int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+tbl(a.Schema, "customers")+` WHERE name = 'after-export'`).Scan(&after))
	require.Zero(t, after, "back to the export")
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+tbl(rep.OldSchema, "customers")+` WHERE name = 'after-export'`).Scan(&after))
	require.Equal(t, 1, after, "the set-aside schema keeps it")
	require.Equal(t, otherBefore, count(t, other.Schema, "customers"), "another tenant untouched")

	// Every session ends.
	var versionAfter int
	require.NoError(t, adm.QueryRow(ctx, `SELECT token_version FROM `+tbl(a.Schema, "users")+` WHERE id = 1`).Scan(&versionAfter))
	require.Equal(t, versionBefore+1, versionAfter, "outstanding access tokens are invalid")
	require.Zero(t, count(t, a.Schema, "refresh_tokens"))
	require.Zero(t, count(t, a.Schema, "auth_tokens"), "invite and reset links are gone")
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := listen.WaitForNotification(wctx)
	require.NoError(t, err)
	require.Equal(t, a.ID+":1", n.Payload, "auth caches are told")

	// Privileges: the tenant role has the new schema, not the old one; the
	// scratch role is gone.
	schema, role, err := tenancy.SchemaFor(a.ID)
	require.NoError(t, err)
	cfg, err := migrate.SnapshotConfig(tdb.JobsDSN, schema, role, "test")
	require.NoError(t, err)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	var users int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users))
	require.Positive(t, users)
	_, err = conn.Exec(ctx, `SELECT count(*) FROM `+tbl(rep.OldSchema, "users"))
	require.ErrorContains(t, err, "permission denied", "the old schema is out of reach")
	var roleLeft bool
	require.NoError(t, adm.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, rep.Scratch+"_rw").Scan(&roleLeft))
	require.False(t, roleLeft, "scratch role dropped")

	// The schema matches the reference, outbox trigger bound to this tenant.
	drift, err := migrate.Drift(ctx, tdb.MigratorDSN)
	require.NoError(t, err)
	for _, d := range drift {
		if d.TenantID == a.ID {
			require.True(t, d.Matches, "drift: %v %v", d.Missing, d.Extra)
		}
	}

	// Waiting emails are not sent again; audit and owner notice.
	var waiting int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+tbl(a.Schema, "email_outbox")+` WHERE status IN ('pending','sending','failed')`).Scan(&waiting))
	require.Zero(t, waiting)
	var tenantAudit, platformAudit, mails int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+tbl(a.Schema, "audit_log")+` WHERE action = 'tenant.restore' AND actor_type = 'system'`).Scan(&tenantAudit))
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log WHERE action = 'tenant.restore_replace' AND target_tenant = $1`, a.ID).Scan(&platformAudit))
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'tenant_restored' AND to_addr = $1 AND template_data->>'tenant_id' = $2`,
		"owner@"+a.Slug+".test", a.ID).Scan(&mails))
	require.Equal(t, []int{1, 1, 1}, []int{tenantAudit, platformAudit, mails})
	var sv int64
	require.NoError(t, adm.QueryRow(ctx, `SELECT schema_version FROM platform.tenants WHERE id = $1`, a.ID).Scan(&sv))
	require.Equal(t, rep.MigratedTo, sv)

	// Clean-up drops the old schema when its time comes.
	var dropAfter time.Time
	require.NoError(t, adm.QueryRow(ctx, `SELECT drop_after FROM platform.tenant_restores WHERE id = $1`, rep.ID).Scan(&dropAfter))
	require.WithinDuration(t, clock.Add(tenantio.OldSchemaKeep), dropAfter, time.Minute)
	n0, err := r.Cleanup(ctx)
	require.NoError(t, err)
	require.Zero(t, n0, "not yet")
	later := *r
	later.Now = func() time.Time { return dropAfter.Add(time.Minute) }
	dropped, err := later.Cleanup(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, dropped, 1)
	require.False(t, schemaExists(t, rep.OldSchema))
	require.Positive(t, count(t, a.Schema, "users"), "the live schema stays")
}

func TestCleanupDropsKeptScratchSchemas(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "clean")
	rec, _ := export(t, a, false)
	rep, err := restore(t, a, rec.ID, tenantio.ModeScratch)
	require.NoError(t, err)
	require.True(t, schemaExists(t, rep.Scratch))
	r := restorer()
	r.Now = func() time.Time { return clock.Add(8 * 24 * time.Hour) }
	_, err = r.Cleanup(ctx)
	require.NoError(t, err)
	require.False(t, schemaExists(t, rep.Scratch), "schema and role dropped after the keep time")
	var dropped *time.Time
	require.NoError(t, tdb.Admin(t).QueryRow(ctx, `SELECT dropped_at FROM platform.tenant_restores WHERE id = $1`, rep.ID).Scan(&dropped))
	require.NotNil(t, dropped)
}
