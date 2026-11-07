//go:build integration

package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

// TestPlatformInviteEmailWorks (S10 item 1): the worker sends the console
// invite with a token it issues at send time (none stored in the outbox),
// and the emailed link starts the account with forced MFA enrolment.
func TestPlatformInviteEmailWorks(t *testing.T) {
	ctx := context.Background()
	to := uniqueAddr("console-invite")
	var uid int64
	require.NoError(t, admin(t).QueryRow(ctx, `INSERT INTO platform.platform_users (email, name, role, password_hash, must_change_password, mfa_enrolled)
		VALUES ($1, 'Invited Admin', 'support', 'unusable', false, false) RETURNING id`, to).Scan(&uid))
	// As the console does: an open (copy) invite and the queued email.
	expires := time.Now().Add(7 * 24 * time.Hour)
	_, err := admin(t).Exec(ctx, `INSERT INTO platform.platform_invites (user_id, token_hash, via, expires_at) VALUES ($1, $2, 'copy', $3)`,
		uid, security.HashToken(security.NewToken(32)), expires)
	require.NoError(t, err)
	_, err = admin(t).Exec(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data)
		VALUES ($1, 'platform_notice', $2, 'Invited Admin', 'platform_invite', jsonb_build_object('userId', $3::bigint, 'expiresAt', $4::timestamptz, 'invitedBy', 'Root'))`,
		"platform-invite-test:"+to, to, uid, expires)
	require.NoError(t, err)
	require.NoError(t, theApp.Worker().EnqueueMail(ctx, nil, true))

	m := waitMail(t, platSrv, to)
	require.Contains(t, m.Header.Get("Subject"), "invited to the")
	require.Contains(t, m.Text, "Root invited you")
	token := linkToken(t, m, "/invite")
	var copies int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.platform_invites WHERE user_id = $1 AND via = 'email'`, uid).Scan(&copies))
	require.Equal(t, 1, copies, "the emailed token was issued at send time")

	acc := do(t, rq{method: "POST", host: adminHost, path: "/api/platform/auth/invite/accept", body: map[string]string{"token": token, "password": "invited-admin-password-1"}})
	require.Equal(t, 200, acc.Code, acc.Raw)
	require.Equal(t, true, acc.Body["mfaEnrollRequired"])
	// Every other open link of this user is now revoked.
	var open int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.platform_invites WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL`, uid).Scan(&open))
	require.Zero(t, open)
}
