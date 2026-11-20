//go:build integration

package platform_test

// This file sorts last on purpose: the test moves the shared test clock 8
// hours forward (the clock only moves forward because TOTP replay state is
// in the database), which would expire sessions other tests still hold.

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSessionAbsoluteLimit (PHASE1_DESIGN §7.2): refreshing every 25
// minutes keeps a session alive (idle limit 30 min) only until 8 hours
// after sign-in; then the refresh is refused and the user signs in again.
func TestSessionAbsoluteLimit(t *testing.T) {
	e := newEnv(t)
	root := rootSession(t, e)
	s := e.mfaLogin(t, rootEmail, newPW, root.secret)
	cookie := s.cookie
	refreshed := 0
	for elapsed := 25 * time.Minute; elapsed < 8*time.Hour; elapsed += 25 * time.Minute {
		e.advance(25 * time.Minute)
		r := e.do(req{method: "POST", path: "/auth/refresh", cookie: cookie, csrf: true})
		require.Equal(t, 200, r.Code, "refresh at %s: %s", elapsed, r.Raw)
		cookie = cookieOf(t, r).Value
		refreshed++
	}
	require.Greater(t, refreshed, 15)
	e.advance(25 * time.Minute) // now past 8 h
	r := e.do(req{method: "POST", path: "/auth/refresh", cookie: cookie, csrf: true})
	require.Equal(t, 401, r.Code, "absolute limit: %s", r.Raw)
}

// TestInviteExpires (S10 item 1): an invite link stops working after 7
// days. In this file because it moves the shared clock 8 days.
func TestInviteExpires(t *testing.T) {
	e := newEnv(t)
	root := e.mfaLogin(t, rootEmail, newPW, rootSession(t, e).secret)
	e.stepUp(t, root)
	r := e.do(req{method: "POST", path: "/platform-users", token: root.token, body: map[string]any{
		"email": "late-joiner@platform.test", "name": "Late Joiner", "role": "support", "reason": "Starts next month"}})
	require.Equal(t, 201, r.Code, r.Raw)
	link := r.Body["inviteUrl"].(string)
	token := link[strings.Index(link, "#token=")+len("#token="):]
	e.advance(8 * 24 * time.Hour)
	acc := e.do(req{method: "POST", path: "/auth/invite/accept", body: map[string]string{"token": token, "password": "late-joiner-password-1"}})
	require.Equal(t, 401, acc.Code, acc.Raw)
}
