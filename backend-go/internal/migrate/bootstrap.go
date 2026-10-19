package migrate

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// Login roles and the DSN that sets each one's password.
const (
	RoleMigrator = "pos_migrator"
	RoleApp      = "pos_app"
	RoleJobs     = "pos_jobs"
)

// BootstrapInput carries the admin DSN and the login DSNs whose passwords
// become the role passwords.
type BootstrapInput struct {
	AdminDSN    secrets.Secret // superuser / rds_superuser; used only here and in preflight
	MigratorDSN secrets.Secret // must log in as pos_migrator
	AppDSN      secrets.Secret // must log in as pos_app
	JobsDSN     secrets.Secret // must log in as pos_jobs
}

// The statements below are constants (constsql). Each is idempotent.
const (
	bsCreateRoles = `DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pos_owner') THEN CREATE ROLE pos_owner NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pos_platform') THEN CREATE ROLE pos_platform NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pos_migrator') THEN CREATE ROLE pos_migrator LOGIN CREATEROLE; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pos_app') THEN CREATE ROLE pos_app LOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pos_jobs') THEN CREATE ROLE pos_jobs LOGIN; END IF;
END $$`
	bsRoleAttrs = `DO $$
BEGIN
  ALTER ROLE pos_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  ALTER ROLE pos_platform NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  ALTER ROLE pos_migrator LOGIN NOSUPERUSER NOCREATEDB CREATEROLE NOBYPASSRLS;
  ALTER ROLE pos_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  ALTER ROLE pos_jobs LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  -- Empty default search_path: a query outside a tenant/platform session fails.
  ALTER ROLE pos_app SET search_path = '';
  ALTER ROLE pos_jobs SET search_path = '';
  -- The migrator must not inherit every tenant role it creates (OPERATIONS M4).
  ALTER ROLE pos_migrator SET createrole_self_grant = '';
END $$`
	bsMemberships = `DO $$
BEGIN
  GRANT pos_owner TO pos_migrator WITH INHERIT FALSE, SET TRUE;
  GRANT pos_platform TO pos_app WITH INHERIT FALSE, SET TRUE;
  GRANT pos_platform TO pos_jobs WITH INHERIT FALSE, SET TRUE;
  -- A non-superuser admin (RDS) must be able to SET ROLE pos_owner for
  -- CREATE SCHEMA ... AUTHORIZATION pos_owner below.
  IF NOT (SELECT rolsuper FROM pg_roles WHERE rolname = current_user) THEN
    EXECUTE format('GRANT pos_owner TO %I WITH INHERIT FALSE, SET TRUE', current_user);
  END IF;
END $$`
	bsDatabase = `DO $$
BEGIN
  EXECUTE format('REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC', current_database());
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO pos_migrator, pos_app, pos_jobs', current_database());
  EXECUTE format('GRANT CREATE ON DATABASE %I TO pos_owner', current_database());
END $$`
	bsSchemas = `DO $$
BEGIN
  REVOKE CREATE ON SCHEMA public FROM PUBLIC;
  CREATE SCHEMA IF NOT EXISTS extensions AUTHORIZATION pos_owner;
  REVOKE CREATE ON SCHEMA extensions FROM PUBLIC;
  GRANT USAGE ON SCHEMA extensions TO pos_platform;
  CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA extensions;
  CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA extensions;
  CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA extensions;
  CREATE SCHEMA IF NOT EXISTS platform AUTHORIZATION pos_owner;
  REVOKE ALL ON SCHEMA platform FROM PUBLIC;
END $$`

	bsStashVerifier = `SELECT set_config('pos.bootstrap_verifier', $1, true)`
	// These read a SCRAM verifier from a transaction-local setting; no
	// credential is embedded (gosec G101 false positives).
	bsPwMigrator = `DO $$ BEGIN EXECUTE format('ALTER ROLE pos_migrator PASSWORD %L', current_setting('pos.bootstrap_verifier')); END $$` //nolint:gosec // G101: no credential
	bsPwApp      = `DO $$ BEGIN EXECUTE format('ALTER ROLE pos_app PASSWORD %L', current_setting('pos.bootstrap_verifier')); END $$`      //nolint:gosec // G101: no credential
	bsPwJobs     = `DO $$ BEGIN EXECUTE format('ALTER ROLE pos_jobs PASSWORD %L', current_setting('pos.bootstrap_verifier')); END $$`     //nolint:gosec // G101: no credential
)

// Bootstrap creates the roles, schemas and extensions the design needs
// (TENANCY §5.3, §5.4). It is idempotent and safe to re-run, e.g. to rotate
// the login passwords. Passwords are sent as SCRAM verifiers, never in clear.
func Bootstrap(ctx context.Context, in BootstrapInput) error {
	logins := []struct {
		role string
		dsn  secrets.Secret
	}{{RoleMigrator, in.MigratorDSN}, {RoleApp, in.AppDSN}, {RoleJobs, in.JobsDSN}}
	verifiers := map[string]string{}
	for _, l := range logins {
		pw, err := passwordFor(l.dsn, l.role)
		if err != nil {
			return err
		}
		v, err := db.SCRAMVerifier(pw)
		if err != nil {
			return err
		}
		verifiers[l.role] = v
	}

	conn, err := pgx.Connect(ctx, in.AdminDSN.Reveal())
	if err != nil {
		return errors.New("bootstrap: cannot connect with DATABASE_ADMIN_URL")
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	var ver int
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&ver); err != nil {
		return err
	}
	if ver < 160000 {
		return fmt.Errorf("bootstrap: PostgreSQL 16 or newer required, server is %d", ver)
	}

	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		steps := []struct {
			name string
			run  func() error
		}{
			{"roles", func() error { _, err := tx.Exec(ctx, bsCreateRoles); return err }},
			{"role attributes", func() error { _, err := tx.Exec(ctx, bsRoleAttrs); return err }},
			{"memberships", func() error { _, err := tx.Exec(ctx, bsMemberships); return err }},
			{"database grants", func() error { _, err := tx.Exec(ctx, bsDatabase); return err }},
			{"schemas", func() error { _, err := tx.Exec(ctx, bsSchemas); return err }},
		}
		for _, s := range steps {
			if err := s.run(); err != nil {
				return fmt.Errorf("bootstrap %s: %w", s.name, err)
			}
		}
		for _, l := range logins {
			if _, err := tx.Exec(ctx, bsStashVerifier, verifiers[l.role]); err != nil {
				return err
			}
			var err error
			switch l.role {
			case RoleMigrator:
				_, err = tx.Exec(ctx, bsPwMigrator)
			case RoleApp:
				_, err = tx.Exec(ctx, bsPwApp)
			case RoleJobs:
				_, err = tx.Exec(ctx, bsPwJobs)
			}
			if err != nil {
				return fmt.Errorf("bootstrap: set password for %s: %w", l.role, err)
			}
		}
		_, err := tx.Exec(ctx, bsStashVerifier, "")
		return err
	})
}

// passwordFor checks that dsn logs in as role and returns its password.
func passwordFor(dsn secrets.Secret, role string) (string, error) {
	u, err := url.Parse(dsn.Reveal())
	if err != nil || u.User == nil {
		return "", fmt.Errorf("bootstrap: the DSN for %s is not a valid postgres URL", role)
	}
	if u.User.Username() != role {
		return "", fmt.Errorf("bootstrap: the DSN for %s must log in as %s", role, role)
	}
	pw, ok := u.User.Password()
	if !ok || len(pw) < 16 {
		return "", fmt.Errorf("bootstrap: the DSN for %s needs a generated password of at least 16 characters", role)
	}
	return pw, nil
}
