package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// ErrScratchExists means a scratch schema of that name already exists.
var ErrScratchExists = fmt.Errorf("migrate: scratch schema already exists")

// CreateScratch creates the scratch role (NOLOGIN; granted to the
// migrator with SET only, never to pos_app or pos_jobs) and the scratch
// schema owned by pos_owner (OPERATIONS §6.3 step 2).
func CreateScratch(ctx context.Context, dsn secrets.Secret, s tenancy.ScratchSchema) error {
	if s.IsZero() {
		return tenancy.ErrInvalidName
	}
	mig, err := pgx.Connect(ctx, dsn.Reveal())
	if err != nil {
		return fmt.Errorf("migrate: connect: %w", err)
	}
	defer func() { _ = mig.Close(context.WithoutCancel(ctx)) }()
	var exists bool
	if err := mig.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)
		OR EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $2)`, s.String(), s.Role()).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrScratchExists
	}
	if err := db.CreateRoleNoLogin(ctx, mig, s.Role()); err != nil {
		return err
	}
	if err := db.GrantRoleSet(ctx, mig, s.Role(), RoleMigrator); err != nil {
		return err
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
	return db.CreateSchema(ctx, owner, s.String(), OwnerRole)
}

// UpScratch runs the tenant migrations on a scratch schema up to
// version (0: the latest) and grants the scratch role its privileges. No
// outbox trigger, no permission sync and nothing recorded in
// platform.tenants: a scratch schema is never a tenant.
func UpScratch(ctx context.Context, dsn secrets.Secret, s tenancy.ScratchSchema, version int64) (int64, error) {
	conn, err := ScratchConn(dsn, s, "posctl-restore")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	p, err := tenantProvider(conn, nil)
	if err != nil {
		return 0, err
	}
	if version > 0 {
		_, err = p.UpTo(ctx, version)
	} else {
		_, err = p.Up(ctx)
	}
	if err != nil {
		return 0, err
	}
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, err
	}
	c, err := conn.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = c.Close() }()
	err = c.Raw(func(dc any) error {
		pc, ok := dc.(interface{ Conn() *pgx.Conn })
		if !ok {
			return fmt.Errorf("migrate: not a pgx connection")
		}
		return db.GrantTenantPrivileges(ctx, pc.Conn(), s.String(), s.Role(), OwnerRole)
	})
	return v, err
}

// DropScratch drops a scratch schema and its role.
func DropScratch(ctx context.Context, dsn secrets.Secret, s tenancy.ScratchSchema) error {
	if s.IsZero() {
		return tenancy.ErrInvalidName
	}
	return dropSchemaAndRole(ctx, dsn, s.String(), s.Role())
}

// DropOldSchema drops a schema a replace restore set aside.
func DropOldSchema(ctx context.Context, dsn secrets.Secret, o tenancy.OldSchema) error {
	if o.IsZero() {
		return tenancy.ErrInvalidName
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
	return db.DropSchemaCascade(ctx, owner, o.String())
}

// OwnerConnConfig is a migrator connection acting as pos_owner with only
// pg_catalog on the search_path (every name schema-qualified): the replace
// restore's transaction.
func OwnerConnConfig(dsn secrets.Secret) (*pgx.ConnConfig, error) { return ownerConfig(dsn) }
