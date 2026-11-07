//go:build integration

// Package testdb starts a real PostgreSQL 18 container for integration tests,
// bootstraps roles and schemas and applies platform migrations exactly as
// production does.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Image is the Postgres image used by every integration test.
const Image = "postgres:18.6@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336"

// DB is a running, bootstrapped database.
type DB struct {
	Container   *tcpostgres.PostgresContainer
	AdminDSN    secrets.Secret
	MigratorDSN secrets.Secret
	AppDSN      secrets.Secret
	JobsDSN     secrets.Secret
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start runs the container, bootstraps it and migrates the platform schema.
// Call Stop when done (usually from TestMain).
func Start(ctx context.Context) (*DB, error) {
	adminPw := randHex(16)
	c, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase("pos"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword(adminPw),
		testcontainers.WithCmd("postgres",
			"-c", "shared_preload_libraries=pg_stat_statements",
			"-c", "max_locks_per_transaction=1024",
			"-c", "fsync=off", "-c", "full_page_writes=off"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("testdb: start container: %w", err)
	}
	admin, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, err
	}
	d := &DB{Container: c, AdminDSN: secrets.Secret(admin)}
	d.MigratorDSN = withUser(admin, migrate.RoleMigrator, randHex(16))
	d.AppDSN = withUser(admin, migrate.RoleApp, randHex(16))
	d.JobsDSN = withUser(admin, migrate.RoleJobs, randHex(16))

	if err := migrate.Bootstrap(ctx, migrate.BootstrapInput{
		AdminDSN: d.AdminDSN, MigratorDSN: d.MigratorDSN, AppDSN: d.AppDSN, JobsDSN: d.JobsDSN,
	}); err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("testdb: bootstrap: %w", err)
	}
	if _, err := migrate.PlatformUp(ctx, d.MigratorDSN, nil); err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("testdb: platform migrations: %w", err)
	}
	if err := migrate.SyncPlatformCatalog(ctx, d.MigratorDSN); err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("testdb: catalog: %w", err)
	}
	return d, nil
}

// Stop terminates the container.
func (d *DB) Stop(ctx context.Context) error { return d.Container.Terminate(ctx) }

func withUser(dsn, user, pw string) secrets.Secret {
	u, _ := url.Parse(dsn)
	u.User = url.UserPassword(user, pw)
	return secrets.Secret(u.String())
}

// AppPool opens a pool as pos_app with the production settings.
func (d *DB) AppPool(t testing.TB, maxConns int32) *pgxpool.Pool {
	t.Helper()
	p, err := db.NewPool(context.Background(), d.AppDSN, db.PoolOptions{MaxConns: maxConns, MinConns: 0, ApplicationName: "test-app"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// Admin opens a superuser connection (test inspection only).
func (d *DB) Admin(t testing.TB) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), d.AdminDSN.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TenantSpec describes a test tenant.
type TenantSpec struct {
	Slug   string
	Status string // default "active"
	// ExtraDDL runs inside the tenant schema after the base tables, as the
	// owner role (constant SQL from tests).
	ExtraDDL string
}

// baseTenantDDL is a small stand-in for the tenant migrations of step 3:
// enough tables to test isolation, citext and trigram search.
const baseTenantDDL = `
CREATE TABLE users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL,
  email citext NOT NULL,
  deleted_at timestamptz
);
CREATE UNIQUE INDEX users_email_uq ON users (email) WHERE deleted_at IS NULL;
CREATE TABLE products (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL
);
CREATE INDEX products_name_trgm ON products USING gin (name gin_trgm_ops);
CREATE TABLE audit_log (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  action text NOT NULL
);
`

// CreateTenant creates a registry row, role, schema, base tables and grants
// the way provisioning will (step 3), using the migrator login.
func (d *DB) CreateTenant(ctx context.Context, s TenantSpec) (tenancy.Tenant, error) {
	if s.Status == "" {
		s.Status = "active"
	}
	id := tenancy.NewTenantID()
	schema, role, err := tenancy.SchemaFor(id)
	if err != nil {
		return tenancy.Tenant{}, err
	}

	mig, err := pgx.Connect(ctx, d.MigratorDSN.Reveal())
	if err != nil {
		return tenancy.Tenant{}, err
	}
	defer func() { _ = mig.Close(ctx) }()
	if err := db.CreateRoleNoLogin(ctx, mig, role.String()); err != nil {
		return tenancy.Tenant{}, err
	}
	for _, member := range []string{migrate.RoleApp, migrate.RoleJobs} {
		if err := db.GrantRoleSet(ctx, mig, role.String(), member); err != nil {
			return tenancy.Tenant{}, err
		}
	}

	sqlDB, err := migrate.PlatformConn(d.MigratorDSN, "test-provision")
	if err != nil {
		return tenancy.Tenant{}, err
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO platform.tenants (id, schema_name, db_role, slug, name, status, owner_email)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, schema.String(), role.String(), s.Slug, "Tenant "+s.Slug, s.Status, "owner@"+s.Slug+".test"); err != nil {
		return tenancy.Tenant{}, err
	}

	ownerConn, err := migrate.TenantConn(d.MigratorDSN, schema, "test-provision")
	if err != nil {
		return tenancy.Tenant{}, err
	}
	defer func() { _ = ownerConn.Close() }()
	conn, err := ownerConn.Conn(ctx)
	if err != nil {
		return tenancy.Tenant{}, err
	}
	defer func() { _ = conn.Close() }()
	err = conn.Raw(func(dc any) error {
		pc := dc.(interface{ Conn() *pgx.Conn }).Conn()
		if err := db.CreateSchema(ctx, pc, schema.String(), migrate.OwnerRole); err != nil {
			return err
		}
		if _, err := pc.Exec(ctx, baseTenantDDL); err != nil {
			return err
		}
		if s.ExtraDDL != "" {
			if _, err := pc.Exec(ctx, s.ExtraDDL); err != nil {
				return err
			}
		}
		return db.GrantTenantPrivileges(ctx, pc, schema.String(), role.String(), migrate.OwnerRole)
	})
	if err != nil {
		return tenancy.Tenant{}, err
	}
	return tenancy.Tenant{ID: id, Slug: s.Slug, Name: "Tenant " + s.Slug, Status: s.Status,
		Schema: schema, Role: role, DBCluster: "default"}, nil
}

// Eventually polls cond until it is true or the timeout passes.
func Eventually(t testing.TB, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", msg)
}
