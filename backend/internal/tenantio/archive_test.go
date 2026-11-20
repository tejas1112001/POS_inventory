package tenantio

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

var testKey = []byte("manifest-key-for-tests-0123456789abcdef")

func identity(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	return id
}

func smallParts(t *testing.T, n int) {
	t.Helper()
	old := partSize
	partSize = n
	t.Cleanup(func() { partSize = old })
}

var t0 = time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)

func header() Manifest {
	return Manifest{TenantID: "abcdefghij", Slug: "shop", SchemaVersion: 9, ExportedAt: t0, AppVersion: "test",
		Settings: map[string]string{"TimeZone": "UTC"}, Excluded: []string{"refresh_tokens"}}
}

type fixture struct {
	tables map[string]string
	rows   map[string]int64
	files  map[string]string
}

func build(t *testing.T, id *age.X25519Identity, f fixture, order []string, fileOrder []string) ([]byte, Manifest, Summary) {
	t.Helper()
	var out bytes.Buffer
	w, err := NewWriter(&out, id.Recipient(), testKey, t0)
	require.NoError(t, err)
	for _, name := range order {
		tw, err := w.Table(name, []string{"id", "name"}, []string{"id"})
		require.NoError(t, err)
		// Write in odd-sized pieces to cross part boundaries.
		data := f.tables[name]
		for len(data) > 0 {
			k := min(7, len(data))
			_, err := tw.Write([]byte(data[:k]))
			require.NoError(t, err)
			data = data[k:]
		}
		require.NoError(t, tw.Finish(f.rows[name]))
	}
	for _, k := range fileOrder {
		require.NoError(t, w.File(k, int64(len(f.files[k])), strings.NewReader(f.files[k])))
	}
	m, sum, err := w.Close(header())
	require.NoError(t, err)
	require.Equal(t, int64(out.Len()), sum.Size)
	h := sha256.Sum256(out.Bytes())
	require.Equal(t, hex.EncodeToString(h[:]), sum.SHA256, "the summary hashes the stored bytes")
	return out.Bytes(), m, sum
}

var fx = fixture{
	tables: map[string]string{
		"roles": "id,name\n1,Owner\n2,Biller\n",
		"users": "id,name\n1,\"Asha, \"\"A\"\"\"\n2,Ravi\n3,\"line\nbreak\"\n",
		"empty": "id,name\n",
	},
	rows:  map[string]int64{"roles": 2, "users": 3, "empty": 0},
	files: map[string]string{"tenants/abcdefghij/logo.png": "\x89PNG fake", "tenants/abcdefghij/empty.bin": ""},
}

func TestArchiveRoundTrip(t *testing.T) {
	smallParts(t, 10)
	id := identity(t)
	data, m, sum := build(t, id, fx, []string{"roles", "users", "empty"}, []string{"tenants/abcdefghij/logo.png", "tenants/abcdefghij/empty.bin"})
	require.Len(t, m.Tables, 3)
	require.Greater(t, m.Tables[1].Parts, 1, "users is cut into parts")
	require.Equal(t, 1, m.Tables[2].Parts, "an empty table still has its header part")
	require.EqualValues(t, 3, m.Tables[1].Rows)

	vm, vsum, err := Verify(bytes.NewReader(data), id, testKey)
	require.NoError(t, err)
	require.Equal(t, m, vm)
	require.Equal(t, sum, vsum)

	got := map[string]string{}
	files := map[string]string{}
	lsum, err := Load(bytes.NewReader(data), id, testKey, vm, func(e TableEntry, r io.Reader) error {
		require.Equal(t, []string{"id", "name"}, e.Columns, "the manifest entry is handed over")
		b, err := io.ReadAll(r)
		got[e.Name] = string(b)
		return err
	}, func(f FileEntry, r io.Reader) error {
		b, err := io.ReadAll(r)
		files[f.Key] = string(b)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, sum, lsum)
	require.Equal(t, fx.tables, got)
	require.Equal(t, fx.files, files)

	// A consumer that stops early still lets the walk finish and verify.
	_, err = Load(bytes.NewReader(data), id, testKey, vm, func(TableEntry, io.Reader) error { return nil }, nil)
	require.NoError(t, err)
}

func TestArchiveRefusesTampering(t *testing.T) {
	smallParts(t, 10)
	id := identity(t)
	data, m, _ := build(t, id, fx, []string{"roles", "users"}, nil)

	flipped := append([]byte(nil), data...)
	flipped[len(flipped)/2] ^= 1
	_, _, err := Verify(bytes.NewReader(flipped), id, testKey)
	require.Error(t, err, "age authenticates every chunk")

	_, _, err = Verify(bytes.NewReader(data[:len(data)-20]), id, testKey)
	require.Error(t, err, "truncated")

	_, _, err = Verify(bytes.NewReader(data), identity(t), testKey)
	require.Error(t, err, "another key cannot decrypt")

	_, _, err = Verify(bytes.NewReader(data), id, []byte("another-manifest-key-0123456789abcdef"))
	require.ErrorIs(t, err, ErrSignature)

	// Load with a manifest that does not match the archive.
	other := m
	other.Tables = append([]TableEntry(nil), m.Tables...)
	other.Tables[0].SHA256 = strings.Repeat("0", 64)
	_, err = Load(bytes.NewReader(data), id, testKey, other, nil, nil)
	require.ErrorIs(t, err, ErrChecksum, "Load only accepts the archive that was verified")
}

// raw builds an archive entry by entry, with a valid signature over the
// given manifest, to test the structural checks.
func raw(t *testing.T, id *age.X25519Identity, entries [][2]string, m *Manifest) []byte {
	t.Helper()
	var out bytes.Buffer
	aw, err := age.Encrypt(&out, id.Recipient())
	require.NoError(t, err)
	zw, err := zstd.NewWriter(aw)
	require.NoError(t, err)
	tw := tar.NewWriter(zw)
	put := func(name, body string) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	for _, e := range entries {
		put(e[0], e[1])
	}
	if m != nil {
		b, err := json.Marshal(m)
		require.NoError(t, err)
		put("manifest.json", string(b))
		put("manifest.sig", hex.EncodeToString(sign(testKey, b)))
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	require.NoError(t, aw.Close())
	return out.Bytes()
}

func manifestFor(tables ...[2]string) *Manifest {
	m := header()
	m.Format = Format
	m.Files = []FileEntry{}
	for _, tb := range tables {
		h := sha256.Sum256([]byte(tb[1]))
		m.Tables = append(m.Tables, TableEntry{Name: tb[0], Columns: []string{"id"}, OrderBy: []string{"id"}, Rows: 1,
			SHA256: hex.EncodeToString(h[:]), Parts: 1})
	}
	return &m
}

func TestArchiveStructureChecks(t *testing.T) {
	id := identity(t)
	ok := [2]string{"tables/roles/00000.csv", "id\n1\n"}
	verify := func(data []byte) error {
		_, _, err := Verify(bytes.NewReader(data), id, testKey)
		return err
	}
	require.NoError(t, verify(raw(t, id, [][2]string{ok}, manifestFor([2]string{"roles", "id\n1\n"}))))

	cases := map[string][]byte{
		"no manifest":      raw(t, id, [][2]string{ok}, nil),
		"unknown entry":    raw(t, id, [][2]string{ok, {"etc/passwd", "x"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"path escape":      raw(t, id, [][2]string{ok, {"files/../../x", "x"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"part gap":         raw(t, id, [][2]string{ok, {"tables/roles/00002.csv", "x"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"part not first":   raw(t, id, [][2]string{{"tables/roles/00001.csv", "id\n1\n"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"table twice":      raw(t, id, [][2]string{ok, {"tables/users/00000.csv", "x"}, {"tables/roles/00000.csv", "y"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"bad table name":   raw(t, id, [][2]string{{"tables/Roles;drop/00000.csv", "x"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"hash differs":     raw(t, id, [][2]string{{"tables/roles/00000.csv", "id\n2\n"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"table not listed": raw(t, id, [][2]string{ok, {"tables/users/00000.csv", "id\n1\n"}}, manifestFor([2]string{"roles", "id\n1\n"})),
		"manifest bad name": raw(t, id, [][2]string{{"tables/roles/00000.csv", "id\n1\n"}}, func() *Manifest {
			m := manifestFor([2]string{"roles", "id\n1\n"})
			m.Tables[0].Columns = []string{"id; drop"}
			return m
		}()),
		"wrong format": raw(t, id, [][2]string{ok}, func() *Manifest { m := manifestFor([2]string{"roles", "id\n1\n"}); m.Format = "other/1"; return m }()),
		"order by unknown": raw(t, id, [][2]string{ok}, func() *Manifest {
			m := manifestFor([2]string{"roles", "id\n1\n"})
			m.Tables[0].OrderBy = []string{"name"}
			return m
		}()),
	}
	for name, data := range cases {
		err := verify(data)
		require.Error(t, err, name)
		require.True(t, errors.Is(err, ErrArchive) || errors.Is(err, ErrChecksum), "%s: %v", name, err)
	}

	// Anything after the signature is refused.
	m := manifestFor([2]string{"roles", "id\n1\n"})
	b, _ := json.Marshal(m)
	after := raw(t, id, [][2]string{ok, {"manifest.json", string(b)}, {"manifest.sig", hex.EncodeToString(sign(testKey, b))}, {"tables/x/00000.csv", "x"}}, nil)
	require.ErrorIs(t, verify(after), ErrArchive)
}

func TestWriterRules(t *testing.T) {
	id := identity(t)
	w, err := NewWriter(io.Discard, id.Recipient(), testKey, t0)
	require.NoError(t, err)
	tw, err := w.Table("roles", []string{"id"}, []string{"id"})
	require.NoError(t, err)
	_, err = w.Table("users", []string{"id"}, []string{"id"})
	require.Error(t, err, "one table at a time")
	require.NoError(t, tw.Finish(0))
	require.Error(t, tw.Finish(0), "finished twice")
	_, err = w.Table("Bad Name", []string{"id"}, []string{"id"})
	require.Error(t, err)
	require.Error(t, w.File("../x", 1, strings.NewReader("x")))
	require.Error(t, w.File("tenants/abcdefghij/short", 5, strings.NewReader("abc")), "shorter than its size")

	_, err = NewWriter(io.Discard, nil, testKey, t0)
	require.Error(t, err)
}
