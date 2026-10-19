// Package db holds the pgx pool setup, transaction helpers and Postgres
// error mapping. ident.go (step 2) is the only file allowed to build SQL from
// non-constant strings.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// PoolOptions configure a pool.
type PoolOptions struct {
	MaxConns        int32
	MinConns        int32
	ApplicationName string
}

// NewPool creates a lazily connecting pool in QueryExecModeExec, so no
// prepared statement is ever cached on a connection and reused under another
// tenant's search_path (TENANCY §5.2).
func NewPool(ctx context.Context, dsn secrets.Secret, o PoolOptions) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn.Reveal())
	if err != nil {
		// pgx parse errors can include the DSN; never return them.
		return nil, fmt.Errorf("db: invalid connection string for %s", o.ApplicationName)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.ConnConfig.StatementCacheCapacity = 0
	cfg.ConnConfig.DescriptionCacheCapacity = 0
	if o.ApplicationName != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = o.ApplicationName
	}
	if o.MaxConns > 0 {
		cfg.MaxConns = o.MaxConns
	}
	if o.MinConns >= 0 && o.MinConns <= cfg.MaxConns {
		cfg.MinConns = o.MinConns
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool %s: %w", o.ApplicationName, err)
	}
	return pool, nil
}
