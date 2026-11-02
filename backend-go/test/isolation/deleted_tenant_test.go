//go:build integration

package isolation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDeletedTenantEndsSessions (S10 item 4): once a tenant is deleted its
// host no longer answers (no sign-in, no session works) and the worker
// revokes its refresh tokens. Other tenants keep theirs.
func TestDeletedTenantEndsSessions(t *testing.T) {
	ctx := context.Background()
	l := billingTenant(t, "deleted-shop")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	require.Equal(t, 200, get(t, host, "/api/auth/me", tok).Code)
	_, bcookie := ownerLogin(t, "b")
	var bOpen int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("b")+`.refresh_tokens WHERE revoked_at IS NULL`).Scan(&bOpen))
	require.NotEmpty(t, bcookie)

	_, err := admin(t).Exec(ctx, `UPDATE platform.tenants SET status = 'deleted', deleted_at = now() WHERE id = $1`, tenants[l].ID)
	require.NoError(t, err)
	theApp.Registry().Flush()
	require.NotEqual(t, 200, get(t, host, "/api/auth/me", tok).Code, "the host is gone")
	r := do(t, rq{method: "POST", host: host, path: "/api/auth/login", body: map[string]any{"email": tenants[l].OwnerEmail, "password": ownerPW}})
	require.NotEqual(t, 200, r.Code)

	n, err := theApp.Worker().Maintenance().EndDeletedSessions(ctx)
	require.NoError(t, err)
	require.Positive(t, n)
	var open int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema(l)+`.refresh_tokens WHERE revoked_at IS NULL`).Scan(&open))
	require.Zero(t, open)
	var bAfter int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM `+schema("b")+`.refresh_tokens WHERE revoked_at IS NULL`).Scan(&bAfter))
	require.Equal(t, bOpen, bAfter, "other tenants keep their sessions")
}
