//go:build integration

package platform_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

func tokenOf(t *testing.T, url string) string {
	t.Helper()
	i := strings.Index(url, "/invite#token=")
	require.Positive(t, i, url)
	return url[i+len("/invite#token="):]
}

// enrolAfter finishes the forced MFA enrolment from an enroll challenge.
func (e *env) enrolAfter(t *testing.T, challenge string) session {
	t.Helper()
	r := e.do(req{method: "POST", path: "/auth/mfa/enroll", body: map[string]string{"challengeId": challenge}})
	require.Equal(t, 200, r.Code, r.Raw)
	secret := secretFromURI(t, r.Body["otpauthUri"].(string))
	e.advance(31 * time.Second)
	r = e.do(req{method: "POST", path: "/auth/mfa/enroll/confirm", body: map[string]string{"challengeId": challenge, "code": security.TOTPCode(secret, e.clock())}})
	require.Equal(t, 200, r.Code, r.Raw)
	return session{token: r.Body["accessToken"].(string), cookie: cookieOf(t, r).Value, secret: secret}
}

// TestPlatformUsers (S10 item 1): invite links (emailed + copy once, 7
// days, token only hashed), forced MFA, role change, deactivate and
// reactivate, MFA reset; step-up, reason, guards, audit, notices.
func TestPlatformUsers(t *testing.T) {
	e := newEnvWith(t, func(c *platform.Config) { c.InboxEmail = "ops-users@platform.test" })
	root := e.mfaLogin(t, rootEmail, newPW, rootSession(t, e).secret)
	add := func(body map[string]any) resp {
		return e.do(req{method: "POST", path: "/platform-users", token: root.token, body: body})
	}
	ok := map[string]any{"email": "new-admin@platform.test", "name": "New Admin", "role": "billing_admin", "reason": "Joins the billing team"}

	require.Equal(t, 403, add(ok).Code, "step-up first")
	e.stepUp(t, root)
	for _, bad := range []map[string]any{
		{"email": "not-an-email", "name": "X", "role": "support", "reason": "A valid reason here"},
		{"email": "x@platform.test", "name": "X", "role": "owner", "reason": "A valid reason here"},
		{"email": "x@platform.test", "name": "X", "role": "support", "reason": "short"},
	} {
		require.Equal(t, 400, add(bad).Code, "%v", bad)
	}
	r := add(ok)
	require.Equal(t, 201, r.Code, r.Raw)
	require.Equal(t, "no-store", r.Header.Get("Cache-Control"))
	link := r.Body["inviteUrl"].(string)
	require.True(t, strings.HasPrefix(link, "https://admin.pos.test/invite#token="), link)
	uid := r.Body["user"].(map[string]any)["id"]
	require.Equal(t, true, r.Body["user"].(map[string]any)["invitePending"])
	require.Equal(t, 409, add(ok).Code, "same email twice")

	ctx := context.Background()
	admin := tdb.Admin(t)
	var stored int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_invites WHERE token_hash = $1`, security.HashToken(tokenOf(t, link))).Scan(&stored))
	require.Equal(t, 1, stored, "only the hash is stored")
	var leaked int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template_data::text LIKE '%' || $1 || '%'`, tokenOf(t, link)).Scan(&leaked))
	require.Zero(t, leaked, "the token is not in the outbox")
	var invites int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'platform_invite' AND to_addr = 'new-admin@platform.test'`).Scan(&invites))
	require.Equal(t, 1, invites, "the invite email is queued")

	// The user cannot sign in before accepting.
	require.Equal(t, 401, e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": "new-admin@platform.test", "password": "anything-at-all-123"}}).Code)

	// Re-inviting revokes the first link.
	ri := e.do(req{method: "POST", path: fmt.Sprintf("/platform-users/%v/invite", uid), token: root.token, body: map[string]any{"reason": "First email was lost"}})
	require.Equal(t, 200, ri.Code, ri.Raw)
	accept := func(token, pw string) resp {
		return e.do(req{method: "POST", path: "/auth/invite/accept", body: map[string]string{"token": token, "password": pw}})
	}
	require.Equal(t, 401, accept(tokenOf(t, link), "a-long-enough-password-77").Code, "revoked by the re-invite")
	link = ri.Body["inviteUrl"].(string)
	require.Equal(t, 400, accept(tokenOf(t, link), "short").Code, "password rules")
	acc := accept(tokenOf(t, link), "new-admin-password-2026")
	require.Equal(t, 200, acc.Code, acc.Raw)
	require.Equal(t, true, acc.Body["mfaEnrollRequired"], "MFA is forced")
	require.Equal(t, 401, accept(tokenOf(t, link), "new-admin-password-2026").Code, "a link works once")
	newbie := e.enrolAfter(t, acc.Body["challengeId"].(string))
	require.Equal(t, 200, e.do(req{method: "GET", path: "/me", token: newbie.token}).Code)
	require.Equal(t, 409, e.do(req{method: "POST", path: fmt.Sprintf("/platform-users/%v/invite", uid), token: root.token, body: map[string]any{"reason": "Not needed any more"}}).Code, "already set up")

	act := func(path string, body map[string]any) resp {
		return e.do(req{method: "POST", path: fmt.Sprintf("/platform-users/%v/%s", uid, path), token: root.token, body: body})
	}
	// Role change: reason needed; the user is signed out.
	require.Equal(t, 400, act("role", map[string]any{"role": "support", "reason": "no"}).Code)
	require.Equal(t, 204, act("role", map[string]any{"role": "support", "reason": "Moves to the support desk"}).Code)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: newbie.token}).Code, "sessions ended")
	newbie = e.mfaLogin(t, "new-admin@platform.test", "new-admin-password-2026", newbie.secret)
	me := e.do(req{method: "GET", path: "/me", token: newbie.token})
	require.Equal(t, "support", me.Body["role"])

	// Deactivate: signed out, cannot sign in; reactivate.
	require.Equal(t, 204, act("deactivate", map[string]any{"reason": "Left the company today"}).Code)
	require.Equal(t, 401, e.do(req{method: "GET", path: "/me", token: newbie.token}).Code)
	require.Equal(t, 401, e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": "new-admin@platform.test", "password": "new-admin-password-2026"}}).Code)
	require.Equal(t, 409, act("deactivate", map[string]any{"reason": "Left the company today"}).Code, "already deactivated")
	require.Equal(t, 204, act("reactivate", map[string]any{"reason": "Came back as a contractor"}).Code)

	// Reset MFA: the next sign-in enrols again.
	require.Equal(t, 204, act("reset-mfa", map[string]any{"reason": "Lost their phone yesterday"}).Code)
	lr := e.do(req{method: "POST", path: "/auth/login", body: map[string]string{"email": "new-admin@platform.test", "password": "new-admin-password-2026"}})
	require.Equal(t, 200, lr.Code, lr.Raw)
	require.Equal(t, true, lr.Body["mfaEnrollRequired"])
	var codes int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_recovery_codes WHERE user_id = $1`, uid).Scan(&codes))
	require.Zero(t, codes, "recovery codes removed")

	// Never yourself.
	var rootID int64
	require.NoError(t, admin.QueryRow(ctx, `SELECT id FROM platform.platform_users WHERE email = $1`, rootEmail).Scan(&rootID))
	for _, p := range []string{"role", "deactivate", "reset-mfa"} {
		r := e.do(req{method: "POST", path: fmt.Sprintf("/platform-users/%d/%s", rootID, p), token: root.token, body: map[string]any{"role": "support", "reason": "Trying to change myself"}})
		require.Equal(t, 409, r.Code, p)
	}

	// The list, notices (user + inbox) and audit.
	l := e.do(req{method: "GET", path: "/platform-users", token: root.token})
	require.Equal(t, 200, l.Code)
	require.Contains(t, l.Raw, `"email":"new-admin@platform.test"`)
	var notices int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.email_outbox WHERE template = 'platform_user_notice'
		AND to_addr IN ('new-admin@platform.test', 'ops-users@platform.test')`).Scan(&notices))
	require.Equal(t, 8, notices, "4 changes x (user + inbox)")
	var audits int
	require.NoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM platform.platform_audit_log WHERE target = $1`, fmt.Sprintf("platform_user:%v", uid)).Scan(&audits))
	require.Equal(t, 6, audits, "invite, reinvite, role, deactivate, reactivate, mfa reset")

	// Other roles may not open the page.
	sec := addUser(t, e, "support-pu@platform.test", "support", "support-pu-pass-12345")
	sup := e.mfaLogin(t, "support-pu@platform.test", "support-pu-pass-12345", sec)
	require.Equal(t, 403, e.do(req{method: "GET", path: "/platform-users", token: sup.token}).Code)
}
