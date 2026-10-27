package tenancy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
)

// PlatformRole is the role platform transactions run as.
const PlatformRole = "pos_platform"

// platformSearchPath has no "platform" on it: platform queries are always
// schema-qualified, so an unqualified table fails instead of resolving
// somewhere unexpected (TENANCY §5).
const platformSearchPath = "extensions"

// ErrSessionTampered means the role or search_path changed inside a
// transaction. The transaction is rolled back. This should never happen; if
// it does, treat it as a possible SQL injection.
var ErrSessionTampered = errors.New("tenancy: session tampered")

// Access selects a read-only or read-write transaction.
type Access int

// Access modes.
const (
	ReadOnly Access = iota
	ReadWrite
)

// TxOptions tune one transaction.
type TxOptions struct {
	Access           Access
	StatementTimeout time.Duration // 0 = the Sessions default
}

// Tenant is a resolved tenant. Schema and Role come from the registry row,
// never from request input.
type Tenant struct {
	ID        string
	Slug      string
	Name      string
	Status    string
	Schema    SchemaName
	Role      RoleName
	DBCluster string
	PlanID    int64
}

// querier is the subset of pgx.Tx that repositories use (sqlc's DBTX).
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TenantTx is a transaction bound to one tenant's role and search_path.
// Tenant repositories accept only this type.
type TenantTx struct {
	tx     pgx.Tx
	tenant Tenant
}

// Tenant returns the tenant this transaction is bound to.
func (t TenantTx) Tenant() Tenant { return t.tenant }

// Exec runs a statement.
func (t TenantTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

// Query runs a query.
func (t TenantTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

// QueryRow runs a single-row query.
func (t TenantTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

// PlatformTx is a transaction as pos_platform. Platform repositories accept
// only this type, so tenant and platform handles cannot be mixed up.
type PlatformTx struct{ tx pgx.Tx }

// Exec runs a statement.
func (p PlatformTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.tx.Exec(ctx, sql, args...)
}

// Query runs a query.
func (p PlatformTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.tx.Query(ctx, sql, args...)
}

// QueryRow runs a single-row query.
func (p PlatformTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.tx.QueryRow(ctx, sql, args...)
}

var (
	_ querier = TenantTx{}
	_ querier = PlatformTx{}
)

// Sessions opens tenant and platform transactions on the shared pool.
type Sessions struct {
	pool             *pgxpool.Pool
	defaultTimeout   time.Duration
	tampered         prometheus.Counter
	log              *slog.Logger
	lockTimeout      string
	rollbackDeadline time.Duration
}

// NewSessions creates the session factory. tampered may be nil.
func NewSessions(pool *pgxpool.Pool, statementTimeout time.Duration, tampered prometheus.Counter, log *slog.Logger) *Sessions {
	if log == nil {
		log = slog.Default()
	}
	return &Sessions{
		pool: pool, defaultTimeout: statementTimeout, tampered: tampered, log: log,
		lockTimeout: "3s", rollbackDeadline: 5 * time.Second,
	}
}

// InTenantTx runs fn in a transaction as the tenant's role with the tenant's
// search_path, all transaction-local (TENANCY §5 step 7). Before commit it
// checks that role and search_path are unchanged.
func (s *Sessions) InTenantTx(ctx context.Context, t Tenant, o TxOptions, fn func(TenantTx) error) error {
	if t.Schema.IsZero() || t.Role.IsZero() || !ValidTenantID(t.ID) {
		return ErrInvalidName
	}
	return s.run(ctx, o, t.Role.String(), t.Schema.SearchPath(), t.ID, func(tx pgx.Tx) error {
		return fn(TenantTx{tx: tx, tenant: t})
	})
}

// InPlatformTx runs fn in a transaction as pos_platform.
func (s *Sessions) InPlatformTx(ctx context.Context, o TxOptions, fn func(PlatformTx) error) error {
	return s.run(ctx, o, PlatformRole, platformSearchPath, "", func(tx pgx.Tx) error {
		return fn(PlatformTx{tx: tx})
	})
}

func (s *Sessions) run(ctx context.Context, o TxOptions, role, searchPath, tenantID string, fn func(pgx.Tx) error) (err error) {
	access := pgx.ReadWrite
	if o.Access == ReadOnly {
		access = pgx.ReadOnly
	}
	timeout := o.StatementTimeout
	if timeout <= 0 {
		timeout = s.defaultTimeout
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: access})
	if err != nil {
		return fmt.Errorf("tenancy: begin: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Roll back even if ctx was cancelled; if this fails pgx closes the
		// connection and the pool discards it.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.rollbackDeadline)
		defer cancel()
		_ = tx.Rollback(rctx)
	}()

	if err := db.SetLocalRole(ctx, tx, role); err != nil {
		return fmt.Errorf("tenancy: set role: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`SELECT set_config('search_path', $1, true),
		        set_config('app.tenant_id', $2, true),
		        set_config('statement_timeout', $3, true),
		        set_config('lock_timeout', $4, true)`,
		searchPath, tenantID, fmt.Sprintf("%dms", timeout.Milliseconds()), s.lockTimeout); err != nil {
		return fmt.Errorf("tenancy: session settings: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	// Tripwire (THREAT_MODEL §4): the session must still be what we set.
	var gotRole, gotPath string
	if err := tx.QueryRow(ctx, `SELECT current_user::text, current_setting('search_path')`).Scan(&gotRole, &gotPath); err != nil {
		return fmt.Errorf("tenancy: post-check: %w", err)
	}
	if gotRole != role || gotPath != searchPath {
		if s.tampered != nil {
			s.tampered.Inc()
		}
		s.log.ErrorContext(ctx, "session tampered", slog.String("event", "security.session_tampered"),
			slog.String("tenant_id", tenantID), slog.String("expected_role", role), slog.String("got_role", gotRole))
		return ErrSessionTampered
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tenancy: commit: %w", err)
	}
	committed = true
	return nil
}
