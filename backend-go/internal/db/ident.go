package db

// This is the ONLY file allowed to send SQL that is not a compile-time
// constant (constsql exemption, TENANCY §4). Every identifier reaching it has
// already been validated by a typed constructor (tenancy.ParseRoleName,
// tenancy.ParseSchemaName) and is quoted here with pgx.Identifier.

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// tenantIDPattern guards the tenant ID literal in the outbox trigger.
var tenantIDPattern = regexp.MustCompile(`^[a-z0-9]{10}$`)

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

// RevokeSchemaUsage removes every privilege a role holds on a schema
// (needed before DROP ROLE; run as the schema owner).
func RevokeSchemaUsage(ctx context.Context, c Execer, schema, role string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	r, err := quote(role)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "REVOKE ALL ON SCHEMA "+s+" FROM "+r)
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

// AppendOnlyTables are tenant tables the tenant role may read and insert
// into but never update or delete: the audit log, the stock ledger, return
// lines (a deleted return line would allow returning more than sold) and
// stock adjustments (corrections are new adjustments).
var AppendOnlyTables = []string{"audit_log", "stock_movements", "return_items", "stock_adjustments"}

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
	// The audit log, the stock ledger and return lines are append-only for
	// the tenant role.
	for _, table := range AppendOnlyTables {
		var exists bool
		if err := queryRowScan(ctx, c, `SELECT to_regclass($1) IS NOT NULL`, []any{schema + "." + table}, &exists); err != nil {
			return err
		}
		if exists {
			if _, err := c.Exec(ctx, "REVOKE UPDATE, DELETE, TRUNCATE ON "+s+"."+table+" FROM "+r); err != nil {
				return err
			}
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

// CreateOutboxTrigger creates (or replaces) the tenant-bound function
// mark_outbox_pending() in the tenant's schema and its trigger on
// email_outbox (EMAIL_AND_RECEIPTS §4.2, O-1). The tenant ID is a literal
// fixed at creation, never read at run time. Must run as pos_owner.
func CreateOutboxTrigger(ctx context.Context, c Execer, schema, role, tenantID string) error {
	if !tenantIDPattern.MatchString(tenantID) || schema != "t_"+tenantID || role != "t_"+tenantID+"_rw" {
		return ErrBadIdentifier
	}
	s, err := quote(schema)
	if err != nil {
		return err
	}
	r, err := quote(role)
	if err != nil {
		return err
	}
	lit := "'" + tenantID + "'" // validated [a-z0-9]{10}: safe as a literal
	stmts := []string{
		"CREATE OR REPLACE FUNCTION " + s + `.mark_outbox_pending() RETURNS trigger
  LANGUAGE plpgsql SECURITY DEFINER
  SET search_path = pg_catalog, platform
AS $fn$
BEGIN
  INSERT INTO platform.mail_dispatch (tenant_id) VALUES (` + lit + `)
  ON CONFLICT (tenant_id) DO UPDATE
    SET next_attempt_at = LEAST(platform.mail_dispatch.next_attempt_at, now());
  PERFORM pg_notify('mail_dispatch', ` + lit + `);
  RETURN NULL;
END $fn$`,
		"ALTER FUNCTION " + s + ".mark_outbox_pending() OWNER TO pos_owner",
		"REVOKE ALL ON FUNCTION " + s + ".mark_outbox_pending() FROM PUBLIC",
		"GRANT EXECUTE ON FUNCTION " + s + ".mark_outbox_pending() TO " + r,
		"DROP TRIGGER IF EXISTS email_outbox_mark ON " + s + ".email_outbox",
		"CREATE TRIGGER email_outbox_mark AFTER INSERT ON " + s + ".email_outbox FOR EACH STATEMENT EXECUTE FUNCTION " + s + ".mark_outbox_pending()",
	}
	for _, q := range stmts {
		if _, err := c.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func quoteList(names []string) (string, error) {
	if len(names) == 0 {
		return "", ErrBadIdentifier
	}
	out := make([]string, len(names))
	for i, n := range names {
		q, err := quote(n)
		if err != nil {
			return "", err
		}
		out[i] = q
	}
	return strings.Join(out, ", "), nil
}

// CopyOutSQL is the export query of one table (OPERATIONS §6.2): exactly
// these columns, sorted by the primary key, as CSV with a header. The
// table resolves through the session's search_path (one schema).
func CopyOutSQL(table string, columns, orderBy []string) (string, error) {
	t, err := quote(table)
	if err != nil {
		return "", err
	}
	c, err := quoteList(columns)
	if err != nil {
		return "", err
	}
	o, err := quoteList(orderBy)
	if err != nil {
		return "", err
	}
	return "COPY (SELECT " + c + " FROM " + t + " ORDER BY " + o + ") TO STDOUT WITH (FORMAT csv, HEADER true)", nil
}

// CopyInSQL loads one table from an export's CSV. HEADER MATCH makes the
// server check the column names against the header line. COPY writes the
// identity values as given (like OVERRIDING SYSTEM VALUE).
func CopyInSQL(table string, columns []string) (string, error) {
	t, err := quote(table)
	if err != nil {
		return "", err
	}
	c, err := quoteList(columns)
	if err != nil {
		return "", err
	}
	return "COPY " + t + " (" + c + ") FROM STDIN WITH (FORMAT csv, HEADER MATCH)", nil
}

// TruncateTables empties tables of one schema in a single statement (a
// restore empties the rows its migrations seeded before loading). Run as
// the schema owner.
func TruncateTables(ctx context.Context, c Execer, schema string, tables []string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	parts := make([]string, len(tables))
	for i, t := range tables {
		q, err := quote(t)
		if err != nil {
			return err
		}
		parts[i] = s + "." + q
	}
	_, err = c.Exec(ctx, "TRUNCATE "+strings.Join(parts, ", "))
	return err
}

// ResetIdentity moves an identity column's sequence past the largest
// value present, so new rows after a restore get fresh IDs. Run as the
// schema owner.
func ResetIdentity(ctx context.Context, c Execer, schema, table, column string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	t, err := quote(table)
	if err != nil {
		return err
	}
	col, err := quote(column)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "SELECT setval(pg_get_serial_sequence($1, $2), coalesce(max("+col+"), 0) + 1, false) FROM "+s+"."+t,
		s+"."+t, column)
	return err
}

// RenameSchema renames a schema (replace restore). Run as its owner.
func RenameSchema(ctx context.Context, c Execer, from, to string) error {
	f, err := quote(from)
	if err != nil {
		return err
	}
	t, err := quote(to)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "ALTER SCHEMA "+f+" RENAME TO "+t)
	return err
}

// RevokeTenantPrivileges undoes GrantTenantPrivileges for a role on a
// schema (a scratch role after its schema became the live one).
func RevokeTenantPrivileges(ctx context.Context, c Execer, schema, role, owner string) error {
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
	for _, q := range []string{
		"REVOKE ALL ON ALL TABLES IN SCHEMA " + s + " FROM " + r,
		"REVOKE ALL ON ALL SEQUENCES IN SCHEMA " + s + " FROM " + r,
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + o + " IN SCHEMA " + s + " REVOKE ALL ON TABLES FROM " + r,
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + o + " IN SCHEMA " + s + " REVOKE ALL ON SEQUENCES FROM " + r,
		"REVOKE ALL ON SCHEMA " + s + " FROM " + r,
		"REVOKE ALL ON SCHEMA extensions FROM " + r,
	} {
		if _, err := c.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// RevokeTenantSessions ends every session of a restored tenant (OPERATIONS
// §6.3 step 8): token_version + 1 kills outstanding access tokens, and an
// auth_changed / role_changed notification per user and role drops the
// cached states. refresh_tokens and auth_tokens are already empty (never
// exported). Run in the replace transaction.
func RevokeTenantSessions(ctx context.Context, c Execer, schema, tenantID string) error {
	if !tenantIDPattern.MatchString(tenantID) || schema != "t_"+tenantID {
		return ErrBadIdentifier
	}
	s, err := quote(schema)
	if err != nil {
		return err
	}
	for _, q := range []string{
		"UPDATE " + s + ".users SET token_version = token_version + 1",
		"SELECT count(pg_notify('auth_changed', $1 || ':' || id)) FROM " + s + ".users",
		"SELECT count(pg_notify('role_changed', $1 || ':' || id)) FROM " + s + ".roles",
	} {
		args := []any{}
		if q[0] == 'S' {
			args = append(args, tenantID)
		}
		if _, err := c.Exec(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// CancelRestoredOutbox cancels emails that were waiting at export time: the
// shop's state moved back, and resending could duplicate what was sent
// after the export.
func CancelRestoredOutbox(ctx context.Context, c Execer, schema string) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "UPDATE "+s+".email_outbox SET status = 'cancelled', last_error = 'Restored from a backup; not sent again.'"+
		" WHERE status IN ('pending','sending','failed')")
	return err
}

// WriteTenantAudit adds a system entry to a tenant's audit log from an
// operator path (replace restore).
func WriteTenantAudit(ctx context.Context, c Execer, schema, action, entity, entityID string, after []byte) error {
	s, err := quote(schema)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "INSERT INTO "+s+".audit_log (actor_type, action, entity, entity_id, after) VALUES ('system', $1, $2, $3, $4)",
		action, entity, entityID, after)
	return err
}

// ExecEmbeddedMigration runs one statement from a migration file embedded in
// the binary (db/migrations). Used only by the tenant runner's --dry-run,
// inside a transaction that is rolled back. Never pass request data here.
func ExecEmbeddedMigration(ctx context.Context, tx *sql.Tx, stmt string) error {
	_, err := tx.ExecContext(ctx, stmt)
	return err
}
