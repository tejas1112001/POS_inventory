package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/db"
	"github.com/tejas1112001/POS_inventory/backend/internal/secrets"
)

// qNotInGroup lists the given roles that exist but are not yet members of
// pos_tenant (roles created before the group, ARCHITECTURE_REVIEW H1).
const qNotInGroup = `SELECT r FROM unnest($1::text[]) AS r
	WHERE CASE WHEN EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r)
		THEN NOT pg_has_role(r, 'pos_tenant', 'USAGE') ELSE false END
	ORDER BY r`

// EnsureTenantGroup creates the pos_tenant group if needed and makes each
// role a member, one short statement at a time. It logs in as pos_migrator
// (dsn). Tenant migrations call it once before their parallel phase, so
// the post-steps never change a shared catalog row.
func EnsureTenantGroup(ctx context.Context, dsn secrets.Secret, roles ...string) error {
	mig, err := pgx.Connect(ctx, dsn.Reveal())
	if err != nil {
		return fmt.Errorf("migrate: connect: %w", err)
	}
	defer func() { _ = mig.Close(context.WithoutCancel(ctx)) }()
	return ensureTenantGroup(ctx, mig, roles)
}

func ensureTenantGroup(ctx context.Context, mig *pgx.Conn, roles []string) error {
	if err := db.EnsureTenantGroup(ctx, mig, OwnerRole); err != nil {
		return fmt.Errorf("migrate: tenant group: %w", err)
	}
	if len(roles) == 0 {
		return nil
	}
	rows, err := mig.Query(ctx, qNotInGroup, roles)
	if err != nil {
		return err
	}
	missing, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, r := range missing {
		if err := db.GrantTenantGroup(ctx, mig, r); err != nil {
			return fmt.Errorf("migrate: add %s to the tenant group: %w", r, err)
		}
	}
	return nil
}
