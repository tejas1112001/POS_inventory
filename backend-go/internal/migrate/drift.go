package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// qFingerprint lists a schema's structure with the schema name and tenant ID
// replaced by placeholders, so two healthy tenants produce identical lines.
// A trigger function carrying another tenant's ID shows up as a difference.
const qFingerprint = `
SELECT line FROM (
  SELECT 'col ' || c.table_name || '.' || c.column_name || ' ' || c.data_type || ' ' || c.is_nullable
         || ' ' || coalesce(replace(c.column_default, $1, '<schema>'), '') AS line
    FROM information_schema.columns c WHERE c.table_schema = $1
  UNION ALL
  SELECT 'idx ' || replace(i.indexdef, $1, '<schema>')
    FROM pg_indexes i WHERE i.schemaname = $1
  UNION ALL
  SELECT 'con ' || cl.relname || '.' || co.conname || ' ' || replace(pg_get_constraintdef(co.oid), $1, '<schema>')
    FROM pg_constraint co JOIN pg_class cl ON cl.oid = co.conrelid
    JOIN pg_namespace n ON n.oid = co.connamespace WHERE n.nspname = $1
  UNION ALL
  SELECT 'fn ' || p.proname || ' secdef=' || p.prosecdef || ' owner=' || pg_get_userbyid(p.proowner)
         || ' cfg=' || coalesce(array_to_string(p.proconfig, ','), '')
         || ' src=' || md5(replace(p.prosrc, $2, '<tenant>'))
    FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = $1
  UNION ALL
  SELECT 'trg ' || c.relname || '.' || t.tgname || ' ' || replace(pg_get_triggerdef(t.oid), $1, '<schema>')
    FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND NOT t.tgisinternal
) x
WHERE line NOT LIKE 'col goose_db_version.%' AND line NOT LIKE 'idx %goose_db_version%'
  AND line NOT LIKE 'con goose_db_version.%'
ORDER BY line`

// DriftItem is one tenant's drift status.
type DriftItem struct {
	TenantID    string
	Version     int64
	Latest      int64
	Fingerprint string
	Matches     bool
	Missing     []string // in the reference, not in the tenant
	Extra       []string // in the tenant, not in the reference
}

func fingerprint(ctx context.Context, conn *sql.DB, schema, tenantID string) ([]string, string, error) {
	rows, err := conn.QueryContext(ctx, qFingerprint, schema, tenantID)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	h := sha256.New()
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, "", err
		}
		lines = append(lines, l)
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return lines, hex.EncodeToString(h.Sum(nil)), rows.Err()
}

// ReferenceFingerprint migrates a scratch schema to the latest version,
// fingerprints it and drops it.
func ReferenceFingerprint(ctx context.Context, dsn secrets.Secret) ([]string, string, error) {
	id := tenancy.NewTenantID()
	t, err := TargetFor(id)
	if err != nil {
		return nil, "", err
	}
	if err := CreateTenantSchema(ctx, dsn, t); err != nil {
		return nil, "", err
	}
	defer func() { _ = DropTenantSchema(context.WithoutCancel(ctx), dsn, t) }()
	if r := UpTenant(ctx, dsn, t, nil); r.Err != nil {
		return nil, "", r.Err
	}
	pconn, err := PlatformConn(dsn, "posctl-drift")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = pconn.Close() }()
	return fingerprint(ctx, pconn, t.Schema.String(), t.ID)
}

// Drift compares every live tenant with a freshly migrated reference.
func Drift(ctx context.Context, dsn secrets.Secret) ([]DriftItem, error) {
	latest, err := LatestTenantVersion()
	if err != nil {
		return nil, err
	}
	refLines, refHash, err := ReferenceFingerprint(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("drift: reference: %w", err)
	}
	pconn, err := PlatformConn(dsn, "posctl-drift")
	if err != nil {
		return nil, err
	}
	defer func() { _ = pconn.Close() }()
	targets, err := listTargets(ctx, pconn, RunOptions{})
	if err != nil {
		return nil, err
	}
	var out []DriftItem
	for _, t := range targets {
		item := DriftItem{TenantID: t.ID, Latest: latest}
		if err := pconn.QueryRowContext(ctx, `SELECT schema_version FROM platform.tenants WHERE id = $1`, t.ID).Scan(&item.Version); err != nil {
			return nil, err
		}
		lines, h, err := fingerprint(ctx, pconn, t.Schema.String(), t.ID)
		if err != nil {
			return nil, err
		}
		item.Fingerprint = h
		item.Matches = h == refHash
		if !item.Matches {
			item.Missing, item.Extra = diffLines(refLines, lines)
		}
		out = append(out, item)
	}
	return out, nil
}

func diffLines(ref, got []string) (missing, extra []string) {
	in := func(set []string) map[string]bool {
		m := make(map[string]bool, len(set))
		for _, s := range set {
			m[s] = true
		}
		return m
	}
	r, g := in(ref), in(got)
	for _, s := range ref {
		if !g[s] {
			missing = append(missing, s)
		}
	}
	for _, s := range got {
		if !r[s] {
			extra = append(extra, s)
		}
	}
	return
}

// CreateTenantSchema creates the tenant role (granted to pos_app and
// pos_jobs) and the schema owned by pos_owner. Idempotent.
func CreateTenantSchema(ctx context.Context, dsn secrets.Secret, t Target) error {
	mig, err := pgx.Connect(ctx, dsn.Reveal())
	if err != nil {
		return fmt.Errorf("migrate: connect: %w", err)
	}
	defer func() { _ = mig.Close(context.WithoutCancel(ctx)) }()
	var exists bool
	if err := mig.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, t.Role.String()).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if err := db.CreateRoleNoLogin(ctx, mig, t.Role.String()); err != nil {
			return err
		}
	}
	for _, member := range []string{RoleApp, RoleJobs} {
		if err := db.GrantRoleSet(ctx, mig, t.Role.String(), member); err != nil {
			return err
		}
	}
	cfg, err := ownerConfig(dsn)
	if err != nil {
		return err
	}
	owner, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = owner.Close(context.WithoutCancel(ctx)) }()
	return db.CreateSchema(ctx, owner, t.Schema.String(), OwnerRole)
}

// DropTenantSchema removes a tenant's schema and role (compensation,
// scratch schemas). Never used on live tenants without step-up (step 4+).
func DropTenantSchema(ctx context.Context, dsn secrets.Secret, t Target) error {
	cfg, err := ownerConfig(dsn)
	if err != nil {
		return err
	}
	owner, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	if err := db.DropSchemaCascade(ctx, owner, t.Schema.String()); err != nil {
		_ = owner.Close(ctx)
		return err
	}
	// The role's grant on the shared extensions schema would block DROP ROLE.
	var exists bool
	if err := owner.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, t.Role.String()).Scan(&exists); err != nil {
		_ = owner.Close(ctx)
		return err
	}
	if exists {
		if err := db.RevokeSchemaUsage(ctx, owner, "extensions", t.Role.String()); err != nil {
			_ = owner.Close(ctx)
			return err
		}
	}
	_ = owner.Close(ctx)
	mig, err := pgx.Connect(ctx, dsn.Reveal())
	if err != nil {
		return err
	}
	defer func() { _ = mig.Close(context.WithoutCancel(ctx)) }()
	return db.DropRole(ctx, mig, t.Role.String())
}

// ownerConfig returns a migrator config acting as pos_owner with only
// pg_catalog on the search_path (DDL here is always schema-qualified).
func ownerConfig(dsn secrets.Secret) (*pgx.ConnConfig, error) {
	return startupParamConfig(dsn.Reveal(), OwnerRole, "pg_catalog")
}

// String renders a drift item for the CLI.
func (d DriftItem) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s version %d/%d ", d.TenantID, d.Version, d.Latest)
	if d.Matches && d.Version == d.Latest {
		b.WriteString("ok")
		return b.String()
	}
	if !d.Matches {
		fmt.Fprintf(&b, "DRIFT: %d missing, %d extra", len(d.Missing), len(d.Extra))
	} else {
		b.WriteString("behind")
	}
	return b.String()
}
