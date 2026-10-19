//go:build integration

package provisioning_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	dbfiles "github.com/tejas1112001/POS_inventory/backend-go/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

var tdb *testdb.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if tdb, err = testdb.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = tdb.Stop(ctx)
	os.Exit(code)
}

var seq atomic.Int64

func slug(prefix string) string { return fmt.Sprintf("%s-%d", prefix, seq.Add(1)) }

func orch() *provisioning.Orchestrator {
	return provisioning.New(tdb.MigratorDSN, "https", "pos.test", nil)
}

func provision(t *testing.T, s string) provisioning.Result {
	t.Helper()
	res, err := orch().Provision(context.Background(), provisioning.Request{
		Slug: s, Name: "Shop " + s, OwnerEmail: "owner@" + s + ".test", OwnerName: "Asha",
	})
	require.NoError(t, err)
	return res
}

func count(t *testing.T, c *pgx.Conn, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, c.QueryRow(context.Background(), q, args...).Scan(&n))
	return n
}

func TestProvisionHappyPath(t *testing.T) {
	ctx := context.Background()
	admin := tdb.Admin(t)
	s := slug("happy")
	res := provision(t, s)
	require.Equal(t, "trialing", res.Status)
	require.Equal(t, "https://"+s+".pos.test", res.TenantURL)
	require.Positive(t, res.OwnerUserID)
	schema := "t_" + res.TenantID

	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	var status string
	var version int64
	require.NoError(t, admin.QueryRow(ctx, `SELECT status, schema_version FROM platform.tenants WHERE id = $1`, res.TenantID).Scan(&status, &version))
	require.Equal(t, "trialing", status)
	require.Equal(t, latest, version)

	// Seed data (O-2): settings, system roles, Walk-in Customer, default store.
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM `+schema+`.settings WHERE business_name = $1 AND currency = 'INR' AND default_customer_id IS NOT NULL`, "Shop "+s))
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM `+schema+`.customers c JOIN `+schema+`.settings st ON st.default_customer_id = c.id WHERE c.name = 'Walk-in Customer'`))
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM `+schema+`.stores WHERE slug = 'main'`))
	require.Equal(t, 3, count(t, admin, `SELECT count(*) FROM `+schema+`.roles WHERE is_system`))
	require.Equal(t, len(rbac.Catalog), count(t, admin, `SELECT count(*) FROM `+schema+`.permissions`))

	perms := func(key string) int {
		return count(t, admin, `SELECT count(*) FROM `+schema+`.role_permissions rp JOIN `+schema+`.roles r ON r.id = rp.role_id WHERE r.system_key = $1`, key)
	}
	billing := 0
	for _, p := range rbac.Catalog {
		if p.Module == "billing" {
			billing++
		}
	}
	require.Equal(t, len(rbac.Catalog), perms("owner"))
	require.Equal(t, len(rbac.Catalog)-billing, perms("admin"))
	require.Equal(t, len(rbac.BillerDefaults), perms("biller"))

	// Owner: active, owner role, no password yet.
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM `+schema+`.users u JOIN `+schema+`.roles r ON r.id = u.role_id
		WHERE u.id = $1 AND u.status = 'Active' AND u.password_hash IS NULL AND r.system_key = 'owner'`, res.OwnerUserID))

	// Trial subscription on the hidden plan, ~14 days.
	var trialEnd time.Time
	require.NoError(t, admin.QueryRow(ctx, `SELECT s.trial_end FROM platform.subscriptions s JOIN platform.plans p ON p.id = s.plan_id
		WHERE s.tenant_id = $1 AND s.status = 'trialing' AND p.code = 'trial'`, res.TenantID).Scan(&trialEnd))
	require.WithinDuration(t, time.Now().Add(14*24*time.Hour), trialEnd, time.Hour)

	// Welcome email queued once; run recorded.
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM platform.email_outbox WHERE idempotency_key = $1 AND template = 'tenant_welcome'`, "tenant:"+res.TenantID+":welcome"))
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM platform.provisioning_runs WHERE tenant_id = $1 AND status = 'succeeded' AND step = 10`, res.TenantID))

	// The app can resolve and use it.
	sess := tenancy.NewSessions(tdb.AppPool(t, 2), 5*time.Second, nil, nil)
	tn, err := tenancy.NewRegistry(sess, nil).BySlug(ctx, s)
	require.NoError(t, err)
	require.NoError(t, sess.InTenantTx(ctx, tn, tenancy.TxOptions{}, func(tx tenancy.TenantTx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		require.Equal(t, 1, n)
		return nil
	}))
}

func TestGrantMatrix(t *testing.T) {
	ctx := context.Background()
	admin := tdb.Admin(t)
	res := provision(t, slug("grants"))
	other := provision(t, slug("grants"))
	role, schema := "t_"+res.TenantID+"_rw", "t_"+res.TenantID

	rows, err := admin.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = $1 AND tablename <> 'goose_db_version'`, schema)
	require.NoError(t, err)
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.NotEmpty(t, tables)
	for _, tbl := range tables {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			want := tbl != "audit_log" || (priv != "UPDATE" && priv != "DELETE")
			var got bool
			require.NoError(t, admin.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, role, schema+"."+tbl, priv).Scan(&got))
			require.Equal(t, want, got, "%s %s on %s", role, priv, tbl)
			require.NoError(t, admin.QueryRow(ctx, `SELECT has_table_privilege('pos_app', $1, $2)`, schema+"."+tbl, priv).Scan(&got))
			require.False(t, got, "pos_app has no direct privilege on %s", tbl)
		}
	}
	var got bool
	require.NoError(t, admin.QueryRow(ctx, `SELECT has_table_privilege($1, 'platform.tenants', 'SELECT')`, role).Scan(&got))
	require.False(t, got)
	require.NoError(t, admin.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'USAGE')`, role, "t_"+other.TenantID).Scan(&got))
	require.False(t, got)
	require.NoError(t, admin.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'CREATE')`, role, schema).Scan(&got))
	require.False(t, got, "tenant role cannot create objects in its own schema")
	require.NoError(t, admin.QueryRow(ctx, `SELECT has_table_privilege($1, 'platform.mail_dispatch', 'INSERT')`, role).Scan(&got))
	require.False(t, got)
}

func TestValidationAndConflicts(t *testing.T) {
	ctx := context.Background()
	o := orch()
	s := slug("dup")
	provision(t, s)
	_, err := o.Provision(ctx, provisioning.Request{Slug: s, Name: "x", OwnerEmail: "a@b.test"})
	e, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeConflict, e.Code)

	admin := tdb.Admin(t)
	_, err = admin.Exec(ctx, `INSERT INTO platform.reserved_slugs (slug) VALUES ('blocked-db')`)
	require.NoError(t, err)

	for name, req := range map[string]provisioning.Request{
		"reserved code": {Slug: "admin", Name: "x", OwnerEmail: "a@b.test"},
		"reserved db":   {Slug: "blocked-db", Name: "x", OwnerEmail: "a@b.test"},
		"bad slug":      {Slug: "Bad--Slug", Name: "x", OwnerEmail: "a@b.test"},
		"bad email":     {Slug: slug("ok"), Name: "x", OwnerEmail: "Asha <a@b.test>"},
		"bad currency":  {Slug: slug("ok"), Name: "x", OwnerEmail: "a@b.test", Currency: "rupee"},
		"bad timezone":  {Slug: slug("ok"), Name: "x", OwnerEmail: "a@b.test", Timezone: "Mars/Base"},
		"unknown plan":  {Slug: slug("ok"), Name: "x", OwnerEmail: "a@b.test", PlanCode: "gold"},
		"no name":       {Slug: slug("ok"), Name: " ", OwnerEmail: "a@b.test"},
	} {
		_, err := o.Provision(ctx, req)
		e, ok := apperr.As(err)
		require.True(t, ok, name)
		require.Equal(t, apperr.CodeValidationFailed, e.Code, name)
	}
}

func orphanCheck(t *testing.T, id string) {
	t.Helper()
	admin := tdb.Admin(t)
	require.Zero(t, count(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, "t_"+id), "no orphan schema")
	require.Zero(t, count(t, admin, `SELECT count(*) FROM pg_roles WHERE rolname = $1`, "t_"+id+"_rw"), "no orphan role")
	require.Zero(t, count(t, admin, `SELECT count(*) FROM platform.subscriptions WHERE tenant_id = $1`, id))
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM platform.tenants WHERE id = $1 AND status = 'failed'`, id))
	require.Zero(t, count(t, admin, `SELECT count(*) FROM platform.tenants WHERE status = 'provisioning'`), "nothing left in provisioning")
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM platform.provisioning_runs WHERE tenant_id = $1 AND status = 'failed' AND last_error IS NOT NULL`, id))
}

func TestFaultInjectionAtEveryStepThenRetry(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected failure")
	for step := provisioning.StepRegister; step <= provisioning.StepTrial; step++ {
		t.Run(fmt.Sprintf("step%d", step), func(t *testing.T) {
			o := orch()
			o.SetFailAfter(func(s int) error {
				if s == step {
					return injected
				}
				return nil
			})
			s := slug(fmt.Sprintf("fault%d", step))
			res, err := o.Provision(ctx, provisioning.Request{Slug: s, Name: "Fault", OwnerEmail: "o@" + s + ".test", Currency: "USD"})
			require.ErrorIs(t, err, injected)
			require.Equal(t, "failed", res.Status)
			orphanCheck(t, res.TenantID)

			retried, err := orch().Retry(ctx, res.TenantID)
			require.NoError(t, err)
			require.Equal(t, "trialing", retried.Status)
			admin := tdb.Admin(t)
			require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM t_`+res.TenantID+`.settings WHERE currency = 'USD'`), "retry replays the original options")
			require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM platform.subscriptions WHERE tenant_id = $1`, res.TenantID))
		})
	}
	_, err := orch().Retry(ctx, "zzzzzzzzzz")
	e, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeNotFound, e.Code)
}

func TestRetryRefusesLiveTenant(t *testing.T) {
	res := provision(t, slug("live"))
	_, err := orch().Retry(context.Background(), res.TenantID)
	e, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidState, e.Code)
}

func tenantFor(t *testing.T, res provisioning.Result) tenancy.Tenant {
	t.Helper()
	s, r, err := tenancy.SchemaFor(res.TenantID)
	require.NoError(t, err)
	return tenancy.Tenant{ID: res.TenantID, Slug: res.Slug, Schema: s, Role: r}
}

func TestOutboxTriggerMarksOnlyOwnTenant(t *testing.T) { // EMAIL §4.2 tests
	ctx := context.Background()
	a := tenantFor(t, provision(t, slug("mailA")))
	b := tenantFor(t, provision(t, slug("mailB")))
	admin := tdb.Admin(t)
	_, err := admin.Exec(ctx, `DELETE FROM platform.mail_dispatch`)
	require.NoError(t, err)

	listener, err := pgx.Connect(ctx, tdb.AdminDSN.Reveal())
	require.NoError(t, err)
	defer func() { _ = listener.Close(ctx) }()
	_, err = listener.Exec(ctx, `LISTEN mail_dispatch`)
	require.NoError(t, err)

	sess := tenancy.NewSessions(tdb.AppPool(t, 2), 5*time.Second, nil, nil)
	err = sess.InTenantTx(ctx, a, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		// Changing app.tenant_id must not change which tenant is marked.
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO email_outbox (idempotency_key, kind, to_addr, template, template_data)
			VALUES ('test:1', 'test', 'x@y.test', 't', '{}')`)
		return err
	})
	require.NoError(t, err)

	rows, err := admin.Query(ctx, `SELECT tenant_id FROM platform.mail_dispatch`)
	require.NoError(t, err)
	marked, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Equal(t, []string{a.ID}, marked)

	wctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	n, err := listener.WaitForNotification(wctx)
	require.NoError(t, err)
	require.Equal(t, a.ID, n.Payload)

	// A rolled-back insert marks nothing.
	_, _ = admin.Exec(ctx, `DELETE FROM platform.mail_dispatch`)
	err = sess.InTenantTx(ctx, a, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO email_outbox (idempotency_key, kind, to_addr, template, template_data)
			VALUES ('test:2', 'test', 'x@y.test', 't', '{}')`); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	require.Error(t, err)
	require.Zero(t, count(t, admin, `SELECT count(*) FROM platform.mail_dispatch`))

	// Tenant A cannot touch platform.mail_dispatch or call B's function.
	for name, q := range map[string]string{
		"insert mail_dispatch": `INSERT INTO platform.mail_dispatch (tenant_id) VALUES ('` + b.ID + `')`,
		"call B function":      `SELECT "` + b.Schema.String() + `".mark_outbox_pending()`,
	} {
		err := sess.InTenantTx(ctx, a, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			_, err := tx.Exec(ctx, q)
			return err
		})
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, name)
		require.Contains(t, []string{"42501", "0A000"}, pgErr.Code, name)
	}
}

func TestDriftDetectsTampering(t *testing.T) {
	ctx := context.Background()
	a := provision(t, slug("drift"))
	b := provision(t, slug("drift"))
	items, err := migrate.Drift(ctx, tdb.MigratorDSN)
	require.NoError(t, err)
	byID := map[string]migrate.DriftItem{}
	for _, it := range items {
		byID[it.TenantID] = it
	}
	require.True(t, byID[a.TenantID].Matches, "%v %v", byID[a.TenantID].Missing, byID[a.TenantID].Extra)
	require.True(t, byID[b.TenantID].Matches)

	admin := tdb.Admin(t)
	// Function in A carrying B's ID, and a dropped index in B.
	_, err = admin.Exec(ctx, `CREATE OR REPLACE FUNCTION t_`+a.TenantID+`.mark_outbox_pending() RETURNS trigger
		LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, platform AS $fn$
		BEGIN
		  INSERT INTO platform.mail_dispatch (tenant_id) VALUES ('`+b.TenantID+`')
		  ON CONFLICT (tenant_id) DO UPDATE SET next_attempt_at = LEAST(platform.mail_dispatch.next_attempt_at, now());
		  PERFORM pg_notify('mail_dispatch', '`+b.TenantID+`');
		  RETURN NULL;
		END $fn$`)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, `DROP INDEX t_`+b.TenantID+`.customers_phone_idx`)
	require.NoError(t, err)

	items, err = migrate.Drift(ctx, tdb.MigratorDSN)
	require.NoError(t, err)
	for _, it := range items {
		byID[it.TenantID] = it
	}
	require.False(t, byID[a.TenantID].Matches, "wrong tenant literal flagged")
	require.False(t, byID[b.TenantID].Matches, "missing index flagged")
	require.NotEmpty(t, byID[b.TenantID].Missing)

	// The runner's post-step repairs the function.
	ta, err := migrate.TargetFor(a.TenantID)
	require.NoError(t, err)
	require.NoError(t, migrate.UpTenant(ctx, tdb.MigratorDSN, ta, nil).Err)
	items, err = migrate.Drift(ctx, tdb.MigratorDSN)
	require.NoError(t, err)
	for _, it := range items {
		if it.TenantID == a.TenantID {
			require.True(t, it.Matches)
		}
	}
}

func TestRunTenantsDryRunLockAndResume(t *testing.T) {
	ctx := context.Background()
	a := provision(t, slug("run"))
	b := provision(t, slug("run"))
	ids := []string{a.TenantID, b.TenantID}

	extra := fstest.MapFS{}
	copyEmbedded(t, extra)
	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	next := latest + 1
	extra[fmt.Sprintf("%05d_add_marker.sql", next)] = &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE marker (id int);
-- +goose Down
DROP TABLE marker;
`)}
	restore := migrate.SetTenantMigrationsForTest(extra)
	defer restore()
	admin := tdb.Admin(t)

	// Dry run: reports pending, changes nothing.
	rep, err := migrate.RunTenants(ctx, tdb.MigratorDSN, migrate.RunOptions{DryRun: true, TenantIDs: ids, Parallelism: 2})
	require.NoError(t, err)
	require.Equal(t, "succeeded", rep.Status)
	for _, it := range rep.Items {
		require.Equal(t, []int64{next}, it.Pending)
	}
	require.Zero(t, count(t, admin, `SELECT count(*) FROM pg_tables WHERE tablename = 'marker'`))

	// Hold A's lock: A is skipped, B migrates, the run is partial.
	lock, err := pgx.Connect(ctx, tdb.MigratorDSN.Reveal())
	require.NoError(t, err)
	_, err = lock.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('migrate:' || $1, 0))`, a.TenantID)
	require.NoError(t, err)
	rep, err = migrate.RunTenants(ctx, tdb.MigratorDSN, migrate.RunOptions{TenantIDs: ids, Parallelism: 2})
	require.NoError(t, err)
	require.Equal(t, "partial", rep.Status)
	status := map[string]string{}
	for _, it := range rep.Items {
		status[it.TenantID] = it.Status
	}
	require.Equal(t, "skipped", status[a.TenantID])
	require.Equal(t, "succeeded", status[b.TenantID])
	_ = lock.Close(ctx)

	// Resume reprocesses only A.
	rep2, err := migrate.RunTenants(ctx, tdb.MigratorDSN, migrate.RunOptions{ResumeRunID: rep.RunID})
	require.NoError(t, err)
	require.Equal(t, "succeeded", rep2.Status)
	require.Len(t, rep2.Items, 1)
	require.Equal(t, a.TenantID, rep2.Items[0].TenantID)
	require.Equal(t, 2, count(t, admin, `SELECT count(*) FROM pg_tables WHERE tablename = 'marker' AND schemaname = ANY($1)`,
		[]string{"t_" + a.TenantID, "t_" + b.TenantID}))
	require.Equal(t, 2, count(t, admin, `SELECT count(*) FROM platform.tenants WHERE id = ANY($1) AND schema_version = $2`, ids, next))
	require.Equal(t, "succeeded", func() string {
		var s string
		require.NoError(t, admin.QueryRow(ctx, `SELECT status FROM platform.migration_runs WHERE id = $1`, rep.RunID).Scan(&s))
		return s
	}())
}

func TestNoTransactionMigrationAndReconnectStayInTenantSchema(t *testing.T) { // TENANCY §8 tests
	ctx := context.Background()
	res := provision(t, slug("notx"))
	tg, err := migrate.TargetFor(res.TenantID)
	require.NoError(t, err)

	files := fstest.MapFS{}
	copyEmbedded(t, files)
	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	files[fmt.Sprintf("%05d_concurrent_index.sql", latest+1)] = &fstest.MapFile{Data: []byte(`-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY users_name_idx ON users (name);
-- +goose Down
DROP INDEX CONCURRENTLY users_name_idx;
`)}
	files[fmt.Sprintf("%05d_after_reconnect.sql", latest+2)] = &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE after_reconnect (id int);
-- +goose Down
DROP TABLE after_reconnect;
`)}
	restore := migrate.SetTenantMigrationsForTest(files)
	defer restore()

	conn, err := migrate.TenantConn(tdb.MigratorDSN, tg.Schema, "test-notx")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	p, err := migrate.NewTenantProvider(conn)
	require.NoError(t, err)
	_, err = p.UpByOne(ctx) // the NO TRANSACTION migration
	require.NoError(t, err)

	admin := tdb.Admin(t)
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM pg_indexes WHERE indexname = 'users_name_idx' AND schemaname = $1`, tg.Schema.String()))

	// Kill the migration connection between two migrations.
	var pid int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = admin.Exec(ctx, `SELECT pg_terminate_backend($1)`, pid)
	require.NoError(t, err)
	if _, err = p.UpByOne(ctx); err != nil {
		_, err = p.UpByOne(ctx) // database/sql re-dials with the same startup parameters
	}
	require.NoError(t, err)

	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM pg_tables WHERE tablename = 'after_reconnect' AND schemaname = $1`, tg.Schema.String()))
	require.Equal(t, 1, count(t, admin, `SELECT count(*) FROM pg_tables WHERE tablename = 'after_reconnect'`), "nowhere else")
	require.Zero(t, count(t, admin, `SELECT count(*) FROM pg_indexes WHERE indexname = 'users_name_idx' AND schemaname <> $1`, tg.Schema.String()))
	require.Zero(t, count(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN ('public', 'platform', 'extensions') AND c.relname IN ('after_reconnect', 'users_name_idx', 'goose_db_version')
		AND NOT (n.nspname = 'platform' AND c.relname = 'goose_db_version')`))
}

func copyEmbedded(t *testing.T, dst fstest.MapFS) {
	t.Helper()
	entries, err := dbfiles.TenantMigrations.ReadDir("migrations/tenant")
	require.NoError(t, err)
	for _, e := range entries {
		b, err := dbfiles.TenantMigrations.ReadFile("migrations/tenant/" + e.Name())
		require.NoError(t, err)
		dst[e.Name()] = &fstest.MapFile{Data: b}
	}
}
