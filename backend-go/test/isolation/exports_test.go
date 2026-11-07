//go:build integration

package isolation

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

// markCustomer adds a customer whose name is a marker to a tenant's schema.
func markCustomer(t *testing.T, l, marker string) {
	t.Helper()
	_, err := admin(t).Exec(context.Background(), `INSERT INTO t_`+tenants[l].ID+`.customers (name, status) VALUES ($1, 'Active')`, marker)
	require.NoError(t, err)
}

type exportState struct {
	ID, Kind, Status, Key, Err string
}

// waitExport waits until the tenant's newest export of a kind has finished.
func waitExport(t *testing.T, l, kind string) exportState {
	t.Helper()
	var s exportState
	// Exports run one at a time and other tests queue some too.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var key, msg *string
		err := admin(t).QueryRow(context.Background(), `SELECT id::text, kind, status, object_key, error FROM platform.tenant_exports
			WHERE tenant_id = $1 AND kind = $2 ORDER BY created_at DESC LIMIT 1`, tenants[l].ID, kind).Scan(&s.ID, &s.Kind, &s.Status, &key, &msg)
		if err == nil && (s.Status == "succeeded" || s.Status == "failed") {
			if key != nil {
				s.Key = *key
			}
			if msg != nil {
				s.Err = *msg
			}
			return s
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("export of %s not finished: %+v; %s", l, s, jobDiagnostics(context.Background()))
	return s
}

// archiveTables decrypts a stored export and returns its manifest and
// tables as CSV text.
func archiveTables(t *testing.T, key string) (tenantio.Manifest, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(storageDir, filepath.FromSlash(key)))
	require.NoError(t, err)
	m, _, err := tenantio.Verify(bytes.NewReader(data), exportIdent, []byte(exportKey))
	require.NoError(t, err)
	tables := map[string]string{}
	_, err = tenantio.Load(bytes.NewReader(data), exportIdent, []byte(exportKey), m, func(e tenantio.TableEntry, r io.Reader) error {
		b, err := io.ReadAll(r)
		tables[e.Name] = string(b)
		return err
	}, nil)
	require.NoError(t, err)
	return m, tables
}

// The worker takes the final export of a tenant pending deletion: only
// that tenant's rows, encrypted to the platform key, never credentials.
func TestFinalExportByTheWorker(t *testing.T) {
	ctx := context.Background()
	gone := billingTenant(t, "exp-final")
	markCustomer(t, gone, "marker-exp-final-mine")
	markCustomer(t, "c", "marker-exp-final-other")
	setTenant(t, gone, `status = 'pending_deletion', deletion_requested_at = now()`)

	require.NoError(t, theApp.Worker().SweepExports(ctx))
	s := waitExport(t, gone, tenantio.KindFinal)
	require.Equal(t, "succeeded", s.Status, s.Err)
	require.True(t, strings.HasPrefix(s.Key, "exports/"+tenants[gone].ID+"/"))
	m, tables := archiveTables(t, s.Key)
	require.Equal(t, tenants[gone].ID, m.TenantID)
	require.Contains(t, tables["customers"], "marker-exp-final-mine")
	require.NotContains(t, tables["customers"], "marker-exp-final-other", "never another tenant's rows")
	require.NotContains(t, tables, "refresh_tokens")
	require.NotContains(t, tables, "auth_tokens")

	// The sweep does not take a second final export.
	require.NoError(t, theApp.Worker().SweepExports(ctx))
	time.Sleep(200 * time.Millisecond)
	var finals int
	require.NoError(t, admin(t).QueryRow(ctx, `SELECT count(*) FROM platform.tenant_exports WHERE tenant_id = $1 AND kind = 'final'`,
		tenants[gone].ID).Scan(&finals))
	require.Equal(t, 1, finals)
}
