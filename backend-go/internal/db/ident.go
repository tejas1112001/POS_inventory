package db

// This is the ONLY file allowed to send SQL that is not a compile-time
// constant (constsql exemption, TENANCY §4). Every identifier reaching it has
// already been validated by a typed constructor (tenancy.ParseRoleName,
// tenancy.ParseSchemaName) and is quoted here with pgx.Identifier.

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// identPattern is a final guard on every identifier that reaches this file:
// lowercase letters, digits and underscores, starting with a letter.
var identPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// ErrBadIdentifier means an identifier failed the final guard.
var ErrBadIdentifier = errors.New("db: invalid identifier")

// Execer is satisfied by pgx.Tx, *pgx.Conn and *pgxpool.Conn.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func quote(name string) (string, error) {
	if !identPattern.MatchString(name) {
		return "", ErrBadIdentifier
	}
	return pgx.Identifier{name}.Sanitize(), nil
}

// SetLocalRole runs SET LOCAL ROLE <role>. SET takes no bind parameters, so
// the validated, quoted name is part of the statement text.
func SetLocalRole(ctx context.Context, tx Execer, role string) error {
	q, err := quote(role)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "SET LOCAL ROLE "+q)
	return err
}

// CreateSchema creates a schema owned by owner if it does not exist.
func CreateSchema(ctx context.Context, c Execer, schema, owner string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	o, err := quote(owner)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+s+" AUTHORIZATION "+o)
	return err
}

// CreateRoleNoLogin creates a NOLOGIN role.
func CreateRoleNoLogin(ctx context.Context, c Execer, role string) error {
	r, err := quote(role)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "CREATE ROLE "+r+" NOLOGIN")
	return err
}

// CreateDatabase creates a database (preflight scratch DB only).
func CreateDatabase(ctx context.Context, c Execer, name string) error {
	n, err := quote(name)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "CREATE DATABASE "+n)
	return err
}

// DropDatabase drops a database, disconnecting other sessions.
func DropDatabase(ctx context.Context, c Execer, name string) error {
	n, err := quote(name)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "DROP DATABASE IF EXISTS "+n+" WITH (FORCE)")
	return err
}

// DropSchemaCascade drops a schema and everything in it.
func DropSchemaCascade(ctx context.Context, c Execer, schema string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+s+" CASCADE")
	return err
}

// DropRole drops a role if it exists.
func DropRole(ctx context.Context, c Execer, role string) error {
	r, err := quote(role)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "DROP ROLE IF EXISTS "+r)
	return err
}

// GrantRoleSet grants role to member WITH INHERIT FALSE, SET TRUE (PG ≥ 16):
// the member may SET ROLE to it but does not inherit its privileges.
func GrantRoleSet(ctx context.Context, c Execer, role, member string) error {
	r, err := quote(role)
	if err != nil {
		return err
	}
	m, err := quote(member)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "GRANT "+r+" TO "+m+" WITH INHERIT FALSE, SET TRUE")
	return err
}

// GrantTenantPrivileges gives a tenant role exactly what TENANCY §5.3 lists:
// USAGE on its schema and extensions, DML on its tables, sequence use,
// default privileges for tables created later by owner, and insert/select
// only on audit_log if it exists. Idempotent.
func GrantTenantPrivileges(ctx context.Context, c Execer, schema, role, owner string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	r, err := quote(role)
	if err != nil {
		return err
	}
	o, err := quote(owner)
	if err != nil {
		return err
	}
	stmts := []string{
		"REVOKE ALL ON SCHEMA " + s + " FROM PUBLIC",
		"GRANT USAGE ON SCHEMA " + s + " TO " + r,
		"GRANT USAGE ON SCHEMA extensions TO " + r,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA " + s + " TO " + r,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA " + s + " TO " + r,
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + o + " IN SCHEMA " + s + " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + r,
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + o + " IN SCHEMA " + s + " GRANT USAGE, SELECT ON SEQUENCES TO " + r,
	}
	for _, q := range stmts {
		if _, err := c.Exec(ctx, q); err != nil {
			return err
		}
	}
	// audit_log is append-only for the tenant role.
	var hasAudit bool
	if err := queryRowScan(ctx, c, `SELECT to_regclass($1) IS NOT NULL`, []any{schema + ".audit_log"}, &hasAudit); err != nil {
		return err
	}
	if hasAudit {
		if _, err := c.Exec(ctx, "REVOKE UPDATE, DELETE, TRUNCATE ON "+s+".audit_log FROM "+r); err != nil {
			return err
		}
	}
	return nil
}

// queryRowScan works with any Execer that can also query.
func queryRowScan(ctx context.Context, c Execer, sql string, args []any, dst ...any) error {
	type querier interface {
		QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	}
	q, ok := c.(querier)
	if !ok {
		return errors.New("db: connection cannot query")
	}
	return q.QueryRow(ctx, sql, args...).Scan(dst...)
}
