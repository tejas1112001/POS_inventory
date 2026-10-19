//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
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

func TestBootstrapIsIdempotentAndLoginsUseSCRAM(t *testing.T) {
	ctx := context.Background()
	in := migrate.BootstrapInput{AdminDSN: tdb.AdminDSN, MigratorDSN: tdb.MigratorDSN, AppDSN: tdb.AppDSN, JobsDSN: tdb.JobsDSN}
	require.NoError(t, migrate.Bootstrap(ctx, in), "second run")

	for _, dsn := range []string{tdb.AppDSN.Reveal(), tdb.MigratorDSN.Reveal(), tdb.JobsDSN.Reveal()} {
		c, err := pgx.Connect(ctx, dsn)
		require.NoError(t, err, "login with the password the verifier was built from")
		_ = c.Close(ctx)
	}

	admin := tdb.Admin(t)
	rows, err := admin.Query(ctx, `SELECT rolname, rolpassword FROM pg_authid WHERE rolname IN ('pos_app','pos_migrator','pos_jobs')`)
	require.NoError(t, err)
	type r struct{ Name, PW string }
	got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[r])
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, x := range got {
		require.Regexp(t, `^SCRAM-SHA-256\$4096:`, x.PW, x.Name)
	}

	var appPath, csg string
	require.NoError(t, admin.QueryRow(ctx, `SELECT coalesce((SELECT s FROM pg_db_role_setting d, unnest(d.setconfig) s
		WHERE d.setrole = 'pos_app'::regrole AND s LIKE 'search_path=%'), '')`).Scan(&appPath))
	require.Equal(t, `search_path=""`, appPath)
	require.NoError(t, admin.QueryRow(ctx, `SELECT coalesce((SELECT s FROM pg_db_role_setting d, unnest(d.setconfig) s
		WHERE d.setrole = 'pos_migrator'::regrole AND s LIKE 'createrole_self_grant=%'), '')`).Scan(&csg))
	require.Equal(t, `createrole_self_grant=`, csg)
}

func TestBootstrapRejectsWrongRoleOrWeakPassword(t *testing.T) {
	ctx := context.Background()
	in := migrate.BootstrapInput{AdminDSN: tdb.AdminDSN, MigratorDSN: tdb.MigratorDSN, AppDSN: tdb.JobsDSN, JobsDSN: tdb.JobsDSN}
	require.ErrorContains(t, migrate.Bootstrap(ctx, in), "must log in as pos_app")
	in.AppDSN = "postgres://pos_app:short@localhost/pos"
	err := migrate.Bootstrap(ctx, in)
	require.ErrorContains(t, err, "at least 16 characters")
	require.NotContains(t, err.Error(), "short")
}

func TestPlatformUpIsIdempotentAndStatusMatches(t *testing.T) {
	ctx := context.Background()
	res, err := migrate.PlatformUp(ctx, tdb.MigratorDSN, nil)
	require.NoError(t, err)
	require.Empty(t, res, "already applied by testdb.Start")
	cur, latest, err := migrate.PlatformStatus(ctx, tdb.MigratorDSN)
	require.NoError(t, err)
	require.Equal(t, latest, cur)
	embedded, err := migrate.LatestPlatformVersion()
	require.NoError(t, err)
	require.Equal(t, embedded, latest)
}

func TestPlatformObjectsOwnedByOwnerAndPrivileges(t *testing.T) {
	ctx := context.Background()
	admin := tdb.Admin(t)

	var badOwners int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'platform' AND pg_get_userbyid(c.relowner) <> 'pos_owner'`).Scan(&badOwners))
	require.Zero(t, badOwners, "every platform object belongs to pos_owner")

	type check struct {
		role, object, priv string
		want               bool
	}
	for _, c := range []check{
		{"pos_platform", "platform.tenants", "SELECT", true},
		{"pos_platform", "platform.tenants", "UPDATE", true},
		{"pos_platform", "platform.goose_db_version", "SELECT", true},
		{"pos_platform", "platform.goose_db_version", "INSERT", false},
		{"pos_platform", "platform.goose_db_version", "DELETE", false},
		{"pos_app", "platform.tenants", "SELECT", false},
		{"pos_jobs", "platform.tenants", "SELECT", false},
		{"pos_migrator", "platform.tenants", "SELECT", false}, // only via SET ROLE pos_owner
	} {
		var got bool
		require.NoError(t, admin.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, c.role, c.object, c.priv).Scan(&got))
		require.Equal(t, c.want, got, "%s %s on %s", c.role, c.priv, c.object)
	}
	for _, c := range []check{
		{"pos_app", "extensions", "CREATE", false},
		{"pos_platform", "extensions", "CREATE", false},
		{"pos_platform", "extensions", "USAGE", true},
		{"pos_app", "public", "CREATE", false},
		{"pos_app", "platform", "USAGE", false},
	} {
		var got bool
		require.NoError(t, admin.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, $3)`, c.role, c.object, c.priv).Scan(&got))
		require.Equal(t, c.want, got, "%s %s on schema %s", c.role, c.priv, c.object)
	}

	var trialPublic bool
	require.NoError(t, admin.QueryRow(ctx, `SELECT is_public FROM platform.plans WHERE code = 'trial'`).Scan(&trialPublic))
	require.False(t, trialPublic, "the seeded trial plan is hidden (O-3)")
}

func TestTenantsTableConstraints(t *testing.T) {
	ctx := context.Background()
	admin := tdb.Admin(t)
	insert := func(id, schema, role, slug string) error {
		_, err := admin.Exec(ctx, `INSERT INTO platform.tenants (id, schema_name, db_role, slug, name, status, owner_email)
			VALUES ($1, $2, $3, $4, 'x', 'active', 'o@x.test')`, id, schema, role, slug)
		return err
	}
	cases := map[string][4]string{
		"schema mismatch": {"abcdefghij", "t_zzzzzzzzzz", "t_abcdefghij_rw", "okslug"},
		"role mismatch":   {"abcdefghij", "t_abcdefghij", "t_zzzzzzzzzz_rw", "okslug"},
		"bad id":          {"ABCDEFGHIJ", "t_ABCDEFGHIJ", "t_ABCDEFGHIJ_rw", "okslug"},
		"double hyphen":   {"abcdefghij", "t_abcdefghij", "t_abcdefghij_rw", "ok--slug"},
		"short slug":      {"abcdefghij", "t_abcdefghij", "t_abcdefghij_rw", "ab"},
		"uppercase slug":  {"abcdefghij", "t_abcdefghij", "t_abcdefghij_rw", "Case-Shop"},
	}
	for name, c := range cases {
		err := insert(c[0], c[1], c[2], c[3])
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, name)
		require.Equal(t, "23514", pgErr.Code, name)
	}
	require.NoError(t, insert("abcdefghij", "t_abcdefghij", "t_abcdefghij_rw", "case-shop"))
	err := insert("bcdefghijk", "t_bcdefghijk", "t_bcdefghijk_rw", "case-shop")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code, "slugs are unique")
}

func TestPreflightPassesOnPostgres18(t *testing.T) {
	ctx := context.Background()
	checks, err := migrate.Preflight(ctx, tdb.AdminDSN, migrate.PreflightOptions{ManyRoles: 200})
	require.NoError(t, err)
	seen := map[string]string{}
	for _, c := range checks {
		seen[c.ID] = c.Status
		require.NotEqual(t, migrate.Fail, c.Status, "%s %s: %s", c.ID, c.Name, c.Detail)
	}
	for i := 1; i <= 14; i++ {
		require.Contains(t, seen, fmt.Sprintf("M%d", i))
	}
	require.Equal(t, migrate.Skip, seen["M12"], "local container runs without TLS")
	require.Equal(t, migrate.Manual, seen["M13"])

	admin := tdb.Admin(t)
	var leftover int
	require.NoError(t, admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_roles WHERE rolname LIKE 'pf\_%' OR rolname LIKE 't\_pf%')
		+ (SELECT count(*) FROM pg_database WHERE datname LIKE 'pos\_preflight\_%')`).Scan(&leftover))
	require.Zero(t, leftover, "preflight cleans up its roles and scratch database")
}
