//go:build integration

package platform_test

// This file sorts last on purpose: the test moves the shared test clock 8
// hours forward (the clock only moves forward because TOTP replay state is
// in the database), which would expire sessions other tests still hold.

import (
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
