package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"

	dbfiles "github.com/tejas1112001/POS_inventory/backend-go/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Target is one tenant to migrate.
type Target struct {
	ID     string
	Schema tenancy.SchemaName
	Role   tenancy.RoleName
}

// TargetFor validates a tenant ID and derives its schema and role.
func TargetFor(id string) (Target, error) {
	s, r, err := tenancy.SchemaFor(id)
	if err != nil {
		return Target{}, err
	}
	return Target{ID: id, Schema: s, Role: r}, nil
}

// ItemResult is the outcome for one tenant.
type ItemResult struct {
	TenantID string
	Status   string // succeeded, failed, skipped
	From, To int64
	Pending  []int64 // dry run: versions that would be applied
	Duration time.Duration
	Err      error
}

// ErrLocked means another run holds the tenant's migration lock.
var ErrLocked = errors.New("migrate: tenant is locked by another run")

// tenantOverride replaces the embedded tenant migrations (tests only).
var (
	tenantOverrideMu sync.RWMutex
	tenantOverride   fs.FS
)

// SetTenantMigrationsForTest swaps the tenant migration files and returns a
// restore function. Production code never calls it.
func SetTenantMigrationsForTest(f fs.FS) (restore func()) {
	tenantOverrideMu.Lock()
	prev := tenantOverride
	tenantOverride = f
	tenantOverrideMu.Unlock()
	return func() {
		tenantOverrideMu.Lock()
		tenantOverride = prev
		tenantOverrideMu.Unlock()
	}
}

func tenantFS() (fs.FS, error) {
	tenantOverrideMu.RLock()
	f := tenantOverride
	tenantOverrideMu.RUnlock()
	if f != nil {
		return f, nil
	}
	return fs.Sub(dbfiles.TenantMigrations, "migrations/tenant")
}

// NewTenantProvider returns a goose provider for the tenant track on a
// TenantConn. UpTenant is the normal entry point.
func NewTenantProvider(conn *sql.DB) (*goose.Provider, error) { return tenantProvider(conn, nil) }

// LatestTenantVersion is the newest embedded tenant migration.
func LatestTenantVersion() (int64, error) {
	sub, err := tenantFS()
	if err != nil {
		return 0, err
	}
	return latestIn(sub)
}

func latestIn(fsys fs.FS) (int64, error) {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return 0, err
	}
	var latest int64
	for _, f := range files {
		var v int64
		if _, err := fmt.Sscanf(f, "%d_", &v); err == nil && v > latest {
			latest = v
		}
	}
	return latest, nil
}

// withTenantLock holds pg_try_advisory_lock('migrate:<id>') on its own
// connection for the duration of fn (TENANCY §8). If the lock connection
// dies, ctx passed to fn is cancelled.
func withTenantLock(ctx context.Context, dsn secrets.Secret, id string, fn func(context.Context) error) error {
	lc, err := pgx.Connect(ctx, dsn.Reveal())
	if err != nil {
		return fmt.Errorf("migrate: lock connection: %w", err)
	}
	defer func() { _ = lc.Close(context.WithoutCancel(ctx)) }()
	var ok bool
	if err := lc.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended('migrate:' || $1, 0))`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrLocked
	}
	defer func() {
		_, _ = lc.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended('migrate:' || $1, 0))`, id)
	}()

	// Watch the lock connection; if it drops, abort the tenant run.
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-rctx.Done():
				return
			case <-t.C:
				if err := lc.Ping(rctx); err != nil && rctx.Err() == nil {
					cancel()
					return
				}
			}
		}
	}()
	err = fn(rctx)
	cancel()
	<-done
	return err
}

func tenantProvider(conn *sql.DB, log *slog.Logger) (*goose.Provider, error) {
	sub, err := tenantFS()
	if err != nil {
		return nil, err
	}
	opts := []goose.ProviderOption{goose.WithTableName(VersionTable), goose.WithDisableGlobalRegistry(true)}
	if log != nil {
		opts = append(opts, goose.WithSlog(log))
	}
	return goose.NewProvider(goose.DialectPostgres, conn, sub, opts...)
}

// UpTenant brings one tenant schema to the latest version, then runs
// the post-step (grants, outbox trigger, permission sync) and records the
// version in platform.tenants. The schema must already exist.
func UpTenant(ctx context.Context, dsn secrets.Secret, t Target, log *slog.Logger) ItemResult {
	start := time.Now()
	res := ItemResult{TenantID: t.ID}
	err := withTenantLock(ctx, dsn, t.ID, func(ctx context.Context) error {
		conn, err := TenantConn(dsn, t.Schema, "posctl-migrate")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		p, err := tenantProvider(conn, log)
		if err != nil {
			return err
		}
		if res.From, err = p.GetDBVersion(ctx); err != nil {
			return err
		}
		if _, err := p.Up(ctx); err != nil {
			return err
		}
		if res.To, err = p.GetDBVersion(ctx); err != nil {
			return err
		}
		if err := PostStep(ctx, conn, t); err != nil {
			return fmt.Errorf("post-step: %w", err)
		}
		return recordSchemaVersion(ctx, dsn, t.ID, res.To)
	})
	res.Duration = time.Since(start)
	switch {
	case errors.Is(err, ErrLocked):
		res.Status, res.Err = "skipped", err
	case err != nil:
		res.Status, res.Err = "failed", err
	default:
		res.Status = "succeeded"
	}
	return res
}

const (
	qSyncPermissions = `INSERT INTO permissions (module, action, label)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[])
		ON CONFLICT (module, action) DO UPDATE SET label = EXCLUDED.label`
	qGrantOwnerAll = `INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.system_key = 'owner'
		ON CONFLICT DO NOTHING`
	qGrantAdminAll = `INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.system_key = 'admin' AND p.module <> 'billing'
		ON CONFLICT DO NOTHING`
)

// PostStep re-applies the grant set, re-creates the tenant-bound outbox
// trigger and syncs the permission catalog. Idempotent. conn must be a
// TenantConn for t (role pos_owner, tenant search_path).
func PostStep(ctx context.Context, conn *sql.DB, t Target) error {
	c, err := conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.Raw(func(dc any) error {
		pc, ok := dc.(interface{ Conn() *pgx.Conn })
		if !ok {
			return errors.New("migrate: not a pgx connection")
		}
		return pgx.BeginFunc(ctx, pc.Conn(), func(tx pgx.Tx) error {
			if err := db.GrantTenantPrivileges(ctx, tx, t.Schema.String(), t.Role.String(), OwnerRole); err != nil {
				return err
			}
			if err := db.CreateOutboxTrigger(ctx, tx, t.Schema.String(), t.Role.String(), t.ID); err != nil {
				return err
			}
			mods, acts, labels := catalogArrays()
			if _, err := tx.Exec(ctx, qSyncPermissions, mods, acts, labels); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, qGrantOwnerAll); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, qGrantAdminAll)
			return err
		})
	})
}

func catalogArrays() (mods, acts, labels []string) {
	for _, p := range rbac.Catalog {
		mods = append(mods, p.Module)
		acts = append(acts, p.Action)
		labels = append(labels, p.Label)
	}
	return
}

// SyncPlatformCatalog upserts rbac.Catalog into platform.permission_catalog.
func SyncPlatformCatalog(ctx context.Context, dsn secrets.Secret) error {
	conn, err := PlatformConn(dsn, "posctl-migrate")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	mods, acts, labels := catalogArrays()
	_, err = conn.ExecContext(ctx, `INSERT INTO platform.permission_catalog (module, action, label)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[])
		ON CONFLICT (module, action) DO UPDATE SET label = EXCLUDED.label`, mods, acts, labels)
	return err
}

func recordSchemaVersion(ctx context.Context, dsn secrets.Secret, id string, v int64) error {
	conn, err := PlatformConn(dsn, "posctl-migrate")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.ExecContext(ctx, `UPDATE platform.tenants SET schema_version = $2, updated_at = now() WHERE id = $1`, id, v)
	return err
}

// RunOptions control a tenant migration run.
type RunOptions struct {
	DryRun      bool
	Parallelism int
	TenantIDs   []string // empty = every eligible tenant
	ResumeRunID int64    // reprocess only failed/pending items of that run
	Log         *slog.Logger
}

// RunReport summarizes a run.
type RunReport struct {
	RunID  int64
	Status string // succeeded, partial, failed
	Target int64
	Items  []ItemResult
}

// RunTenants migrates every eligible tenant (or the selected ones) with
// bounded parallelism. One tenant's failure never stops the others
// (TENANCY §8). Small tenants go first.
func RunTenants(ctx context.Context, dsn secrets.Secret, o RunOptions) (RunReport, error) {
	if o.Parallelism <= 0 {
		o.Parallelism = 8
	}
	if o.Parallelism > 64 {
		o.Parallelism = 64
	}
	latest, err := LatestTenantVersion()
	if err != nil {
		return RunReport{}, err
	}
	pconn, err := PlatformConn(dsn, "posctl-migrate")
	if err != nil {
		return RunReport{}, err
	}
	defer func() { _ = pconn.Close() }()

	targets, err := listTargets(ctx, pconn, o)
	if err != nil {
		return RunReport{}, err
	}

	rep := RunReport{Target: latest}
	if o.ResumeRunID > 0 {
		rep.RunID = o.ResumeRunID
		if _, err := pconn.ExecContext(ctx, `UPDATE platform.migration_runs SET status = 'running', finished_at = NULL WHERE id = $1`, rep.RunID); err != nil {
			return rep, err
		}
	} else if err := pconn.QueryRowContext(ctx, `INSERT INTO platform.migration_runs (target_version, dry_run, parallelism)
		VALUES ($1, $2, $3) RETURNING id`, latest, o.DryRun, o.Parallelism).Scan(&rep.RunID); err != nil {
		return rep, err
	}
	for _, t := range targets {
		if _, err := pconn.ExecContext(ctx, `INSERT INTO platform.migration_run_items (run_id, tenant_id) VALUES ($1, $2)
			ON CONFLICT (run_id, tenant_id) DO UPDATE SET status = 'pending', error = NULL`, rep.RunID, t.ID); err != nil {
			return rep, err
		}
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, o.Parallelism)
	)
	for _, t := range targets {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			_, _ = pconn.ExecContext(ctx, `UPDATE platform.migration_run_items SET status = 'running' WHERE run_id = $1 AND tenant_id = $2`, rep.RunID, t.ID)
			var r ItemResult
			if o.DryRun {
				r = DryRunTenant(ctx, dsn, t)
			} else {
				r = UpTenant(ctx, dsn, t, o.Log)
			}
			var errText *string
			if r.Err != nil {
				s := sanitizeErr(r.Err)
				errText = &s
			}
			_, _ = pconn.ExecContext(context.WithoutCancel(ctx), `UPDATE platform.migration_run_items
				SET status = $3, from_version = $4, to_version = $5, duration_ms = $6, error = $7
				WHERE run_id = $1 AND tenant_id = $2`, rep.RunID, t.ID, r.Status, r.From, r.To, r.Duration.Milliseconds(), errText)
			mu.Lock()
			rep.Items = append(rep.Items, r)
			mu.Unlock()
		})
	}
	wg.Wait()

	failed := 0
	for _, r := range rep.Items {
		if r.Status != "succeeded" {
			failed++
		}
	}
	switch {
	case failed == 0:
		rep.Status = "succeeded"
	case failed == len(rep.Items):
		rep.Status = "failed"
	default:
		rep.Status = "partial"
	}
	sort.Slice(rep.Items, func(i, j int) bool { return rep.Items[i].TenantID < rep.Items[j].TenantID })
	_, err = pconn.ExecContext(context.WithoutCancel(ctx), `UPDATE platform.migration_runs SET status = $2, finished_at = now() WHERE id = $1`, rep.RunID, rep.Status)
	return rep, err
}

func listTargets(ctx context.Context, pconn *sql.DB, o RunOptions) ([]Target, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case o.ResumeRunID > 0:
		rows, err = pconn.QueryContext(ctx, `SELECT t.id FROM platform.migration_run_items i JOIN platform.tenants t ON t.id = i.tenant_id
			WHERE i.run_id = $1 AND i.status IN ('pending','running','failed','skipped')
			  AND t.status NOT IN ('deleted','failed','provisioning')
			ORDER BY t.storage_bytes, t.id`, o.ResumeRunID)
	case len(o.TenantIDs) > 0:
		rows, err = pconn.QueryContext(ctx, `SELECT id FROM platform.tenants
			WHERE id = ANY($1) AND status NOT IN ('deleted','failed','provisioning')
			ORDER BY storage_bytes, id`, o.TenantIDs)
	default:
		// Provisioning tenants are migrated by the orchestrator itself.
		rows, err = pconn.QueryContext(ctx, `SELECT id FROM platform.tenants
			WHERE status NOT IN ('deleted','failed','provisioning')
			ORDER BY storage_bytes, id`)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Target
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		t, err := TargetFor(id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DryRunTenant reports pending versions and executes each pending
// transactional migration inside a transaction that is rolled back. NO
// TRANSACTION migrations are listed but never executed.
func DryRunTenant(ctx context.Context, dsn secrets.Secret, t Target) ItemResult {
	start := time.Now()
	res := ItemResult{TenantID: t.ID}
	err := withTenantLock(ctx, dsn, t.ID, func(ctx context.Context) error {
		conn, err := TenantConn(dsn, t.Schema, "posctl-migrate-dry-run")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		p, err := tenantProvider(conn, nil)
		if err != nil {
			return err
		}
		if res.From, err = p.GetDBVersion(ctx); err != nil {
			return err
		}
		res.To = res.From
		sub, err := tenantFS()
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		for _, src := range p.ListSources() {
			if src.Version <= res.From {
				continue
			}
			res.Pending = append(res.Pending, src.Version)
			b, err := fs.ReadFile(sub, baseName(src.Path))
			if err != nil {
				return err
			}
			parsed, err := splitUp(string(b))
			if err != nil {
				return err
			}
			if parsed.NoTransaction {
				continue
			}
			for _, stmt := range parsed.Statements {
				if err := db.ExecEmbeddedMigration(ctx, tx, stmt); err != nil {
					return fmt.Errorf("version %d: %w", src.Version, err)
				}
			}
			res.To = src.Version
		}
		return nil // rollback via defer
	})
	res.Duration = time.Since(start)
	switch {
	case errors.Is(err, ErrLocked):
		res.Status, res.Err = "skipped", err
	case err != nil:
		res.Status, res.Err = "failed", err
	default:
		res.Status = "succeeded"
	}
	return res
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// sanitizeErr keeps error text short and free of connection strings.
func sanitizeErr(err error) string {
	s := err.Error()
	if i := strings.Index(s, "postgres://"); i >= 0 {
		s = s[:i] + "[dsn]"
	}
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
