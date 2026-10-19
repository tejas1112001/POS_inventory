package migrate

import (
	"context"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/tejas1112001/POS_inventory/backend-go/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// VersionTable is goose's version table, created in each schema's own
// search_path (platform.goose_db_version, t_<id>.goose_db_version).
const VersionTable = "goose_db_version"

// lockID turns a lock name into a stable advisory-lock key.
func lockID(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return int64(h.Sum64() & 0x7fffffffffffffff) // #nosec G115 -- masked to 63 bits
}

// Result is one applied migration.
type Result struct {
	Version  int64
	Name     string
	Duration string
}

// PlatformUp applies pending platform migrations under the advisory lock
// "migrate:platform".
func PlatformUp(ctx context.Context, migratorDSN secrets.Secret, log *slog.Logger) ([]Result, error) {
	p, closeFn, err := platformProvider(migratorDSN, log)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	res, err := p.Up(ctx)
	out := make([]Result, 0, len(res))
	for _, r := range res {
		out = append(out, Result{Version: r.Source.Version, Name: r.Source.Path, Duration: r.Duration.String()})
	}
	if err != nil {
		return out, fmt.Errorf("migrate platform: %w", err)
	}
	return out, nil
}

// PlatformStatus reports the current and latest platform versions.
func PlatformStatus(ctx context.Context, migratorDSN secrets.Secret) (current, latest int64, err error) {
	p, closeFn, err := platformProvider(migratorDSN, nil)
	if err != nil {
		return 0, 0, err
	}
	defer closeFn()
	current, err = p.GetDBVersion(ctx)
	if err != nil {
		return 0, 0, err
	}
	srcs := p.ListSources()
	if len(srcs) > 0 {
		latest = srcs[len(srcs)-1].Version
	}
	return current, latest, nil
}

// LatestPlatformVersion is the newest embedded platform migration.
func LatestPlatformVersion() (int64, error) {
	sub, err := fs.Sub(db.PlatformMigrations, "migrations/platform")
	if err != nil {
		return 0, err
	}
	files, err := fs.Glob(sub, "*.sql")
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

func platformProvider(migratorDSN secrets.Secret, log *slog.Logger) (*goose.Provider, func(), error) {
	conn, err := PlatformConn(migratorDSN, "posctl-migrate")
	if err != nil {
		return nil, nil, err
	}
	sub, err := fs.Sub(db.PlatformMigrations, "migrations/platform")
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(lockID("migrate:platform")))
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	opts := []goose.ProviderOption{
		goose.WithTableName(VersionTable),
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
	}
	if log != nil {
		opts = append(opts, goose.WithSlog(log))
	}
	p, err := goose.NewProvider(goose.DialectPostgres, conn, sub, opts...)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return p, func() { _ = conn.Close() }, nil
}
