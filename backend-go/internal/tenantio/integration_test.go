//go:build integration

package tenantio_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

var (
	tdb      *testdb.DB
	sessions *tenancy.Sessions
	seq      atomic.Int64
	mkey     = []byte("manifest-key-integration-0123456789abcdef")
	ident    *age.X25519Identity
	store    *files.FSStorage
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if tdb, err = testdb.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pool, err := db.NewPool(ctx, tdb.JobsDSN, db.PoolOptions{MaxConns: 4, ApplicationName: "test-jobs"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sessions = tenancy.NewSessions(pool, 30*time.Second, nil, nil)
	ident, _ = age.GenerateX25519Identity()
	dir, _ := os.MkdirTemp("", "tenantio-store-")
	store, _ = files.NewFSStorage(dir)
	code := m.Run()
	pool.Close()
	_ = os.RemoveAll(dir)
	_ = tdb.Stop(ctx)
	os.Exit(code)
}

func exporter() *tenantio.Exporter {
	return &tenantio.Exporter{Sessions: sessions, DSN: tdb.JobsDSN, Storage: store, Recipient: ident.Recipient(), ManifestKey: mkey}
}

type shop struct {
	ID, Slug, Schema, Marker string
}

// newShop provisions a tenant with the real migrations and seeds rows
// that exercise CSV quoting, NULL vs empty, JSON, inet, bytea, numeric and
// timestamps, plus rows in the tables an export must leave out.
func newShop(t *testing.T, prefix string) shop {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("%s-%d", prefix, seq.Add(1))
	res, err := provisioning.New(tdb.MigratorDSN, "https", "pos.test", nil).Provision(ctx, provisioning.Request{
		Slug: slug, Name: "Shop " + slug, OwnerEmail: "owner@" + slug + ".test", OwnerName: "Asha"})
	require.NoError(t, err)
	s := shop{ID: res.TenantID, Slug: slug, Schema: "t_" + res.TenantID, Marker: "mk" + strings.ReplaceAll(slug, "-", "")}
	adm := tdb.Admin(t)
	q := func(sql string, args ...any) {
		t.Helper()
		_, err := adm.Exec(ctx, strings.ReplaceAll(sql, "@s", s.Schema), args...)
		require.NoError(t, err)
	}
	q(`INSERT INTO @s.customers (name, email, phone, address, city, country, status, marketing_consent)
		VALUES ($1, NULL, '', E'line one\nline "two", end', 'चाय ☕', 'IN', 'Active', true),
		       ('Plain', 'plain@example.test', NULL, NULL, '', NULL, 'Active', false)`, `Asha "Q", `+s.Marker)
	q(`INSERT INTO @s.suppliers (name, phone, address, status) VALUES ($1, E'tab\there', 'C:\path\', 'Active')`, "Supplier "+s.Marker)
	q(`INSERT INTO @s.audit_log (at, actor_type, actor_id, action, entity, entity_id, before, after, ip, user_agent)
		VALUES ('2026-10-24 18:30:15.123456+05:30', 'user', 1, 'test.seed', 'customer', '1',
		        '{"b": [1, 2.50, null, "x"], "a": "é"}', NULL, '203.0.113.7', $1)`, "agent "+s.Marker)
	q(`INSERT INTO @s.files (id, kind, purpose, content_type, size_bytes, sha256, storage_key)
		VALUES ($1, 'image', 'tenant_logo', 'image/png', 3, decode(repeat('ab', 32), 'hex'), $2)`,
		uuid.New(), "tenants/"+s.ID+"/images/logo-"+s.Marker+".png")
	q(`UPDATE @s.settings SET receipt_footer = E'Thanks!\nVisit again', max_discount_pct_without_override = 12.50`)
	// Rows that must never leave the database.
	q(`INSERT INTO @s.refresh_tokens (id, user_id, family_id, token_hash, remember, expires_at, absolute_expires_at, user_agent)
		VALUES ($1, 1, $1, '\xdeadbeef', true, now() + interval '1 day', now() + interval '2 days', $2)`, uuid.New(), "session-"+s.Marker)
	q(`INSERT INTO @s.auth_tokens (user_id, purpose, token_hash, expires_at) VALUES (1, 'reset', $1, now() + interval '1 hour')`,
		[]byte("reset-"+s.Marker))
	q(`INSERT INTO @s.idempotency_keys (user_id, key, request_hash, status, response) VALUES (1, $1, '\x01', 200, $2)`,
		"idem-"+s.Marker, `{"marker": "idem-`+s.Marker+`"}`)
	return s
}

func queue(t *testing.T, tenantID string, withFiles bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, sessions.InPlatformTx(context.Background(), tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var err error
		id, err = tenantio.Queue(context.Background(), tx, tenantID, tenantio.KindManual, "test", withFiles)
		return err
	}))
	return id
}

// export runs one export and returns its record and stored bytes.
func export(t *testing.T, s shop, withFiles bool) (tenantio.Record, []byte) {
	t.Helper()
	rec, err := exporter().Run(context.Background(), queue(t, s.ID, withFiles))
	require.NoError(t, err)
	rc, _, err := store.Get(context.Background(), rec.ObjectKey)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	return rec, b
}

// contents decrypts an archive into table name → CSV and key → bytes.
func contents(t *testing.T, data []byte) (tenantio.Manifest, map[string]string, map[string]string) {
	t.Helper()
	m, _, err := tenantio.Verify(bytes.NewReader(data), ident, mkey)
	require.NoError(t, err)
	tables, fls := map[string]string{}, map[string]string{}
	_, err = tenantio.Load(bytes.NewReader(data), ident, mkey, m, func(e tenantio.TableEntry, r io.Reader) error {
		b, err := io.ReadAll(r)
		tables[e.Name] = string(b)
		return err
	}, func(f tenantio.FileEntry, r io.Reader) error {
		b, err := io.ReadAll(r)
		fls[f.Key] = string(b)
		return err
	})
	require.NoError(t, err)
	return m, tables, fls
}

func TestExportHoldsOneTenantAndNoCredentials(t *testing.T) {
	ctx := context.Background()
	a, b := newShop(t, "exp-a"), newShop(t, "exp-b")
	rec, data := export(t, a, false)
	m, tables, _ := contents(t, data)

	require.Equal(t, a.ID, m.TenantID)
	require.Equal(t, a.Slug, m.Slug)
	latest, err := migrate.LatestTenantVersion()
	require.NoError(t, err)
	require.Equal(t, latest, m.SchemaVersion)
	require.Equal(t, migrate.ExportSettings, m.Settings)
	require.ElementsMatch(t, []string{"auth_tokens", "goose_db_version", "idempotency_keys", "pos_locks", "refresh_tokens"}, m.Excluded)
	var names []string
	for _, e := range m.Tables {
		names = append(names, e.Name)
	}
	require.ElementsMatch(t, tenantio.Tables, names, "every known table, nothing else")
	pos := map[string]int{}
	for i, n := range names {
		pos[n] = i
	}
	require.Less(t, pos["roles"], pos["users"], "parents before children")
	require.Less(t, pos["sales"], pos["order_items"])

	all := strings.Join(func() []string {
		var v []string
		for _, s := range tables {
			v = append(v, s)
		}
		return v
	}(), "\n")
	require.Contains(t, all, a.Marker)
	require.NotContains(t, all, b.Marker, "nothing from the other tenant")
	for _, secret := range []string{"session-" + a.Marker, "reset-" + a.Marker, "idem-" + a.Marker} {
		require.NotContains(t, all, secret, "excluded tables never leave the database")
	}
	require.NotContains(t, string(data), a.Marker, "the stored object is encrypted")
	require.Contains(t, tables["customers"], `"line one`+"\n"+`line ""two"", end"`, "CSV keeps quotes and newlines")
	require.Contains(t, tables["audit_log"], "2026-10-24 13:00:15.123456+00", "timestamps in UTC with microseconds")

	adm := tdb.Admin(t)
	for _, e := range m.Tables {
		var n int64
		require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{a.Schema, e.Name}.Sanitize()).Scan(&n))
		require.Equal(t, n, e.Rows, e.Name)
	}
	require.Equal(t, m.Rows(), rec.Rows)

	var status, sha string
	var size int64
	require.NoError(t, adm.QueryRow(ctx, `SELECT status, sha256, size_bytes FROM platform.tenant_exports WHERE id = $1`, rec.ID).
		Scan(&status, &sha, &size))
	require.Equal(t, "succeeded", status)
	require.Equal(t, rec.Summary.SHA256, sha)
	require.EqualValues(t, len(data), size)
	require.Equal(t, "exports/"+a.ID+"/"+rec.ID.String()+".tar.zst.age", rec.ObjectKey)

	// Same data, same table hashes (the ciphertext differs every time).
	_, data2 := export(t, a, false)
	m2, _, _ := contents(t, data2)
	require.Equal(t, m.Tables, m2.Tables, "deterministic per-table hashes")
	require.NotEqual(t, data, data2)
}

func TestSnapshotConnectionIsConfined(t *testing.T) {
	ctx := context.Background()
	a, b := newShop(t, "conf-a"), newShop(t, "conf-b")
	schema, role, err := tenancy.SchemaFor(a.ID)
	require.NoError(t, err)
	cfg, err := migrate.SnapshotConfig(tdb.JobsDSN, schema, role, "test")
	require.NoError(t, err)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	var who string
	require.NoError(t, conn.QueryRow(ctx, `SELECT current_user`).Scan(&who))
	require.Equal(t, role.String(), who)
	_, err = conn.Exec(ctx, `SELECT count(*) FROM `+pgx.Identifier{b.Schema, "users"}.Sanitize())
	require.ErrorContains(t, err, "permission denied", "another tenant's schema is out of reach")
	_, err = conn.Exec(ctx, `SELECT count(*) FROM platform.tenants`)
	require.ErrorContains(t, err, "permission denied", "so is the platform schema")
	_, err = conn.Exec(ctx, `UPDATE users SET name = name`)
	require.ErrorContains(t, err, "read-only", "nothing can be written")
}

func TestExportRefusesUnknownTablesAndLeavesNothing(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "unk")
	adm := tdb.Admin(t)
	_, err := adm.Exec(ctx, `CREATE TABLE `+pgx.Identifier{a.Schema, "surprise"}.Sanitize()+` (id int PRIMARY KEY)`)
	require.NoError(t, err)
	_, err = adm.Exec(ctx, `GRANT SELECT ON `+pgx.Identifier{a.Schema, "surprise"}.Sanitize()+` TO `+pgx.Identifier{"t_" + a.ID + "_rw"}.Sanitize())
	require.NoError(t, err)
	id := queue(t, a.ID, false)
	_, err = exporter().Run(ctx, id)
	require.ErrorContains(t, err, `"surprise" is neither exported nor excluded`)
	var status, msg string
	require.NoError(t, adm.QueryRow(ctx, `SELECT status, error FROM platform.tenant_exports WHERE id = $1`, id).Scan(&status, &msg))
	require.Equal(t, "failed", status)
	require.Contains(t, msg, "surprise")
	_, _, err = store.Get(ctx, tenantio.ObjectKey(a.ID, id))
	require.ErrorIs(t, err, files.ErrNotFound, "a failed export leaves no object")
	_, err = exporter().Run(ctx, id)
	require.ErrorIs(t, err, tenantio.ErrNotQueued, "a finished export does not run again")
}

func TestOneExportAtATime(t *testing.T) {
	ctx := context.Background()
	a := newShop(t, "busy")
	queue(t, a.ID, false)
	err := sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tenantio.Queue(ctx, tx, a.ID, tenantio.KindManual, "test", false)
		return err
	})
	require.ErrorIs(t, err, tenantio.ErrBusy)
	err = sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tenantio.Queue(ctx, tx, "zzzzzzzzzz", tenantio.KindManual, "test", false)
		return err
	})
	require.Error(t, err, "unknown tenant")

	// A running export that stopped long ago is failed so a new one can start.
	adm := tdb.Admin(t)
	_, err = adm.Exec(ctx, `UPDATE platform.tenant_exports SET status = 'running', started_at = now() - interval '7 hours'
		WHERE tenant_id = $1 AND status = 'queued'`, a.ID)
	require.NoError(t, err)
	queue(t, a.ID, false)
	var interrupted int
	require.NoError(t, adm.QueryRow(ctx, `SELECT count(*) FROM platform.tenant_exports WHERE tenant_id = $1 AND error = 'interrupted'`,
		a.ID).Scan(&interrupted))
	require.Equal(t, 1, interrupted)
}

func TestExportFilesStayInsideTheTenant(t *testing.T) {
	ctx := context.Background()
	a, b := newShop(t, "files-a"), newShop(t, "files-b")
	adm := tdb.Admin(t)
	logoA := "tenants/" + a.ID + "/images/logo-" + a.Marker + ".png"
	secretB := "tenants/" + b.ID + "/images/private-" + b.Marker + ".png"
	require.NoError(t, store.Put(ctx, logoA, "image/png", []byte("PNG-A")))
	require.NoError(t, store.Put(ctx, secretB, "image/png", []byte("PNG-B-SECRET")))
	// A tampered row in A pointing at B's object, and a row whose object is gone.
	_, err := adm.Exec(ctx, `INSERT INTO `+pgx.Identifier{a.Schema, "files"}.Sanitize()+` (id, kind, purpose, content_type, size_bytes, sha256, storage_key)
		VALUES ($1, 'image', 'product_image', 'image/png', 3, decode(repeat('ab', 32), 'hex'), $2),
		       ($3, 'image', 'product_image', 'image/png', 3, decode(repeat('ab', 32), 'hex'), $4)`,
		uuid.New(), secretB, uuid.New(), "tenants/"+a.ID+"/images/gone.png")
	require.NoError(t, err)

	_, data := export(t, a, true)
	m, _, fls := contents(t, data)
	require.Equal(t, map[string]string{logoA: "PNG-A"}, fls)
	require.ElementsMatch(t, []string{"outside-tenant-prefix", "tenants/" + a.ID + "/images/gone.png"}, m.Missing)
	require.NotContains(t, strings.Join(func() []string {
		var v []string
		for _, s := range fls {
			v = append(v, s)
		}
		return v
	}(), ""), "PNG-B-SECRET")
}
