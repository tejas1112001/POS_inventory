//go:build integration

package platform_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTenantContactsNotesOverview (UI S9): contacts (one primary), an
// append-only notes timeline, the read-only overview and list sorting;
// support staff may read but not change; every change is audited.
func TestTenantContactsNotesOverview(t *testing.T) {
	e := newEnv(t)
	// A fresh sign-in: earlier tests move the clock past the shared session.
	root := e.mfaLogin(t, rootEmail, newPW, rootSession(t, e).secret)
	id := e.newTenant(t, root, "extras-shop")
	base := "/tenants/" + id

	// Contacts.
	bad := e.do(req{method: "POST", path: base + "/contacts", token: root.token, body: map[string]any{"name": "No Way To Reach"}})
	require.Equal(t, 400, bad.Code, "email or phone is required")
	a := e.do(req{method: "POST", path: base + "/contacts", token: root.token, body: map[string]any{"name": "Asha", "role": "Owner", "email": "asha@extras.test", "isPrimary": true}})
	require.Equal(t, 201, a.Code, a.Raw)
	b := e.do(req{method: "POST", path: base + "/contacts", token: root.token, body: map[string]any{"name": "Bilal", "role": "Accountant", "phone": "+91 90000 00000", "isPrimary": true}})
	require.Equal(t, 201, b.Code, b.Raw)
	l := e.do(req{method: "GET", path: base + "/contacts", token: root.token})
	require.Equal(t, 200, l.Code, l.Raw)
	require.Contains(t, l.Raw, `"name":"Bilal","role":"Accountant"`)
	require.Regexp(t, `^\[\{"id":\d+,"name":"Bilal"`, l.Raw, "the new primary comes first; only one primary")
	require.Contains(t, l.Raw, `"name":"Asha","role":"Owner","email":"asha@extras.test","phone":"","isPrimary":false`)
	ed := e.do(req{method: "PUT", path: fmt.Sprintf("%s/contacts/%v", base, a.Body["id"]), token: root.token, body: map[string]any{"name": "Asha K", "role": "Owner", "email": "asha@extras.test"}})
	require.Equal(t, 200, ed.Code, ed.Raw)
	require.Equal(t, 204, e.do(req{method: "DELETE", path: fmt.Sprintf("%s/contacts/%v", base, b.Body["id"]), token: root.token}).Code)
	require.Equal(t, 404, e.do(req{method: "DELETE", path: fmt.Sprintf("%s/contacts/%v", base, b.Body["id"]), token: root.token}).Code)

	// Notes: newest first, cannot be changed even by the app's database role.
	for _, body := range []string{"Called about GST setup", "Asked for a demo of returns"} {
		r := e.do(req{method: "POST", path: base + "/notes", token: root.token, body: map[string]any{"body": body}})
		require.Equal(t, 201, r.Code, r.Raw)
	}
	require.Equal(t, 400, e.do(req{method: "POST", path: base + "/notes", token: root.token, body: map[string]any{"body": "  "}}).Code)
	n := e.do(req{method: "GET", path: base + "/notes", token: root.token})
	require.Equal(t, 200, n.Code)
	require.Regexp(t, `^\[\{"id":\d+,"body":"Asked for a demo of returns","authorEmail":"root@platform.test"`, n.Raw)
	ctx := context.Background()
	pool := tdb.AppPool(t, 1)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SET LOCAL ROLE pos_platform`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE platform.tenant_notes SET body = 'x'`)
	require.Error(t, err, "the app role cannot rewrite notes")
	require.NoError(t, tx.Rollback(ctx))
	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SET LOCAL ROLE pos_platform`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM platform.tenant_notes`)
	require.Error(t, err, "the app role cannot delete notes")
	require.NoError(t, tx.Rollback(ctx))

	// Overview.
	o := e.do(req{method: "GET", path: base + "/overview", token: root.token})
	require.Equal(t, 200, o.Code, o.Raw)
	require.EqualValues(t, 1, o.Body["contactCount"])
	require.EqualValues(t, 2, o.Body["noteCount"])
	require.Greater(t, o.Body["latestSchema"].(float64), float64(0))
	require.NotNil(t, o.Body["limits"])
	require.Equal(t, 404, e.do(req{method: "GET", path: "/tenants/not-a-tenant/overview", token: root.token}).Code)

	// Sorting: a known key works, an unknown one falls back, never errors.
	for _, s := range []string{"name", "oldest", "activity", "storage", "newest", "x; DROP TABLE"} {
		require.Equal(t, 200, e.do(req{method: "GET", path: "/tenants?sort=" + url.QueryEscape(s), token: root.token}).Code, s)
	}

	// Support staff: read only.
	sec := addUser(t, e, "support-extras@platform.test", "support", "support-extras-pass-1")
	sup := e.mfaLogin(t, "support-extras@platform.test", "support-extras-pass-1", sec)
	require.Equal(t, 200, e.do(req{method: "GET", path: base + "/contacts", token: sup.token}).Code)
	require.Equal(t, 200, e.do(req{method: "GET", path: base + "/notes", token: sup.token}).Code)
	require.Equal(t, 403, e.do(req{method: "POST", path: base + "/notes", token: sup.token, body: map[string]any{"body": "hi"}}).Code)
	require.Equal(t, 403, e.do(req{method: "POST", path: base + "/contacts", token: sup.token, body: map[string]any{"name": "X", "phone": "1"}}).Code)

	// Audit: every change, without contact details or note text.
	var actions []string
	rows, err := tdb.Admin(t).Query(ctx, `SELECT action || ' ' || coalesce(after::text, before::text, '') FROM platform.platform_audit_log
		WHERE target_tenant = $1 AND action LIKE 'tenant.contact%' OR target_tenant = $1 AND action = 'tenant.note_add' ORDER BY id`, id)
	require.NoError(t, err)
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		actions = append(actions, s)
	}
	require.Len(t, actions, 6)
	for _, s := range actions {
		require.NotContains(t, s, "asha@extras.test")
		require.NotContains(t, s, "90000")
		require.NotContains(t, s, "GST setup")
	}
}
