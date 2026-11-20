package a

import (
	"context"
	"fmt"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (int, error)
	Query(ctx context.Context, sql string, args ...any) (int, error)
	QueryRow(ctx context.Context, sql string, args ...any) int
}

type Batch struct{}

func (b *Batch) Queue(sql string, args ...any) {}

type Other struct{}

func (Other) Exec(n int) {} // not SQL: first param is not a string

const listUsers = "SELECT id FROM users"

func f(ctx context.Context, db DBTX, b *Batch, name string) {
	_, _ = db.Exec(ctx, "DELETE FROM sessions WHERE expires_at < now()")
	_, _ = db.Query(ctx, listUsers)
	_ = db.QueryRow(ctx, listUsers+" WHERE id = $1", 1)
	b.Queue("SELECT 1")

	_, _ = db.Exec(ctx, "SET LOCAL ROLE "+name)                 // want `constsql: SQL passed to Exec must be a constant`
	_, _ = db.Query(ctx, fmt.Sprintf("SELECT * FROM %s", name)) // want `constsql: SQL passed to Query must be a constant`
	q := listUsers
	_ = db.QueryRow(ctx, q) // want `constsql: SQL passed to QueryRow must be a constant`
	b.Queue(name)           // want `constsql: SQL passed to Queue must be a constant`

	Other{}.Exec(3)
}

// Wrapper is a query method that forwards its SQL unchanged: allowed.
type Wrapper struct{ db DBTX }

func (w Wrapper) Exec(ctx context.Context, sql string, args ...any) (int, error) {
	return w.db.Exec(ctx, sql, args...)
}

// Query modifies the SQL before forwarding: still reported.
func (w Wrapper) Query(ctx context.Context, sql string, args ...any) (int, error) {
	return w.db.Query(ctx, sql+" LIMIT 1", args...) // want `constsql: SQL passed to Query must be a constant`
}

// helper is not a query method, so forwarding is reported.
func helper(ctx context.Context, db DBTX, sql string) {
	_, _ = db.Exec(ctx, sql) // want `constsql: SQL passed to Exec must be a constant`
}

func useWrapper(ctx context.Context, w Wrapper, name string) {
	_, _ = w.Exec(ctx, listUsers)
	_, _ = w.Exec(ctx, "DROP "+name) // want `constsql: SQL passed to Exec must be a constant`
}
