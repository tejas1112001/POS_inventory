// Package migrate runs the platform and tenant migration tracks.
package migrate

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// OwnerRole owns every schema and object. Migration sessions run as it.
const OwnerRole = "pos_owner"

// This file is the ONLY place allowed to put "role" and "search_path" into
// connection RuntimeParams (TENANCY §5 CI rule). Startup parameters are part
// of every physical connection, so they survive NO TRANSACTION migrations
// and reconnects, and no SET statement is ever sent.

// PlatformConn opens a single-connection *sql.DB for the platform track:
// role pos_owner, search_path "platform, extensions".
func PlatformConn(dsn secrets.Secret, app string) (*sql.DB, error) {
	return dedicated(dsn, "platform, extensions", app)
}

// TenantConn opens a single-connection *sql.DB for one tenant schema:
// role pos_owner, search_path "t_<id>", extensions.
func TenantConn(dsn secrets.Secret, schema tenancy.SchemaName, app string) (*sql.DB, error) {
	if schema.IsZero() {
		return nil, tenancy.ErrInvalidName
	}
	return dedicated(dsn, schema.SearchPath(), app)
}

func dedicated(dsn secrets.Secret, searchPath, app string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn.Reveal())
	if err != nil {
		return nil, errors.New("migrate: invalid DATABASE_MIGRATOR_URL")
	}
	cfg.RuntimeParams["role"] = OwnerRole
	cfg.RuntimeParams["search_path"] = searchPath
	cfg.RuntimeParams["lock_timeout"] = "5s"     // don't queue behind POS traffic
	cfg.RuntimeParams["statement_timeout"] = "0" // long DDL is expected
	cfg.RuntimeParams["application_name"] = app
	// Migrations are DDL; caching statements would be pointless and unsafe.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

// ExportSettings are the session settings of every export and of the
// restore verification, so the same rows always give the same bytes
// (OPERATIONS §6.2). They are startup parameters, never SET.
var ExportSettings = map[string]string{
	"TimeZone": "UTC", "DateStyle": "ISO", "IntervalStyle": "iso_8601",
	"extra_float_digits": "3", "bytea_output": "hex", "client_encoding": "UTF8",
}

// SnapshotConfig is a read-only connection acting as role on one schema
// (search_path "<schema>", extensions) with ExportSettings. For an export
// the role is the tenant role (login pos_jobs, a member with SET): it
// cannot read another tenant's schema, and nothing it runs can write.
func SnapshotConfig(dsn secrets.Secret, schema tenancy.SchemaName, role tenancy.RoleName, app string) (*pgx.ConnConfig, error) {
	if schema.IsZero() || role.IsZero() {
		return nil, tenancy.ErrInvalidName
	}
	return sessionConfig(dsn, schema.SearchPath(), role.String(), app, true)
}

// sessionConfig is the shared builder for export and restore sessions.
func sessionConfig(dsn secrets.Secret, searchPath, role, app string, readOnly bool) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(dsn.Reveal())
	if err != nil {
		return nil, errors.New("migrate: invalid connection string")
	}
	for k, v := range ExportSettings {
		cfg.RuntimeParams[k] = v
	}
	cfg.RuntimeParams["role"] = role
	cfg.RuntimeParams["search_path"] = searchPath
	cfg.RuntimeParams["statement_timeout"] = "0"
	cfg.RuntimeParams["application_name"] = app
	if readOnly {
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	return cfg, nil
}

// startupParamConfig builds a pgx config whose session role and search_path
// are startup parameters (preflight M6 check).
// ownerPoolConfig is a small pool that runs as pos_owner with a fixed
// search_path (River's migrations).
func ownerPoolConfig(dsn, searchPath, app string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("migrate: invalid connection string")
	}
	cfg.ConnConfig.RuntimeParams["role"] = OwnerRole
	cfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	cfg.MaxConns = 2
	return cfg, nil
}

func startupParamConfig(dsn, role, searchPath string) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("migrate: invalid connection string")
	}
	cfg.RuntimeParams["role"] = role
	cfg.RuntimeParams["search_path"] = searchPath
	return cfg, nil
}
