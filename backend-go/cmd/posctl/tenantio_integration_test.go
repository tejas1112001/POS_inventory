//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

var tdb *testdb.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if tdb, err = testdb.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = tdb.Stop(ctx)
	os.Exit(code)
}

func TestExportAndRestoreCommands(t *testing.T) {
	ctx := context.Background()
	res, err := provisioning.New(tdb.MigratorDSN, "https", "pos.test", nil).Provision(ctx, provisioning.Request{
		Slug: "cli-shop", Name: "CLI Shop", OwnerEmail: "owner@cli-shop.test", OwnerName: "Asha"})
	require.NoError(t, err)
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	key := "Mk-cli-" + "q7Zr3pX9wL2vN8kT5bY1cH4jR6sD"
	t.Setenv("DATABASE_JOBS_URL", tdb.JobsDSN.Reveal())
	t.Setenv("DATABASE_MIGRATOR_URL", tdb.MigratorDSN.Reveal())
	t.Setenv("STORAGE_DRIVER", "fs")
	t.Setenv("STORAGE_FS_ROOT", t.TempDir())
	t.Setenv("EXPORT_AGE_RECIPIENT", id.Recipient().String())
	t.Setenv("EXPORT_AGE_IDENTITY", id.String())
	t.Setenv("EXPORT_MANIFEST_KEY", key)
	secretsOut := []string{id.String(), key, tdb.JobsDSN.Reveal(), tdb.MigratorDSN.Reveal()}
	posctl := func(args ...string) (int, string) {
		var out, errb bytes.Buffer
		code := run(ctx, args, &out, &errb)
		all := out.String() + errb.String()
		for _, s := range secretsOut {
			require.NotContains(t, all, s, "posctl never prints keys or connection strings")
		}
		return code, all
	}

	code, out := posctl("tenant", "export", res.TenantID)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "succeeded")
	exportID := regexp.MustCompile(`export ([0-9a-f-]{36}) succeeded`).FindStringSubmatch(out)[1]

	code, out = posctl("tenant", "restore", res.TenantID, "--export", exportID, "--drop")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "rows and checksums match")
	require.Contains(t, out, "customers")
	require.NotContains(t, out, "MISMATCH")

	code, out = posctl("tenant", "restore-tests", "--weekly")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "queued 1 weekly restore tests")
	require.Contains(t, out, "1 restore tests run, 0 failed")

	code, out = posctl("tenant", "restore", res.TenantID, "--replace", "--confirm", "cli-shop")
	require.Equal(t, 1, code)
	require.Contains(t, out, "must be read_only")
	code, out = posctl("tenant", "restore-cleanup")
	require.Equal(t, 0, code, out)

	t.Setenv("EXPORT_AGE_IDENTITY", "")
	code, out = posctl("tenant", "restore", res.TenantID)
	require.Equal(t, 1, code)
	require.Contains(t, out, "EXPORT_AGE_IDENTITY is required")
	code, out = posctl("tenant", "export")
	require.Equal(t, 1, code)
	require.Contains(t, out, "usage")
}
