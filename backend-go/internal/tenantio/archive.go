// Package tenantio exports one tenant's schema to an encrypted archive and
// restores it into a scratch schema (OPERATIONS §6.2–6.3).
//
// Archive layout: age( zstd( tar ) ). The tar holds, in this order:
//
//	tables/<table>/<part>.csv   COPY … TO STDOUT (FORMAT csv, HEADER), cut
//	                            into parts of at most partSize bytes, so the
//	                            export streams with constant memory
//	files/<key>                 tenant files (optional)
//	manifest.json               what is in the archive, with hashes
//	manifest.sig                hex HMAC-SHA256 of the manifest
//
// The manifest comes last because it carries the hashes computed while the
// tables streamed. A reader therefore verifies the whole archive (one pass)
// before anything is loaded (a second pass).
package tenantio

import (
	"archive/tar"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"regexp"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

// Format names the archive layout; a reader refuses any other.
const Format = "posd-tenant-export/1"

// partSize is the largest table part (a var for tests).
var partSize = 4 << 20

const (
	maxManifest = 16 << 20
	maxTables   = 300
	maxColumns  = 300
	maxFiles    = 200_000
	maxFileSize = 64 << 20
	sigPrefix   = Format + "\n"
)

// Errors a reader returns. Archive and checksum errors never include
// archive content.
var (
	ErrArchive   = errors.New("tenantio: malformed archive")
	ErrSignature = errors.New("tenantio: manifest signature does not match")
	ErrChecksum  = errors.New("tenantio: checksum mismatch")
)

var (
	identRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	tenantRe  = regexp.MustCompile(`^[a-z0-9]{10}$`)
	fileKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9/._-]{0,299}$`)
	partRe    = regexp.MustCompile(`^tables/([a-z][a-z0-9_]{0,62})/([0-9]{5})\.csv$`)
	sha256Re  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// TableEntry is one exported table. Columns and OrderBy are the exact
// export query, so a restore can re-run it and compare.
type TableEntry struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	OrderBy []string `json:"order_by"`
	Rows    int64    `json:"rows"`
	SHA256  string   `json:"sha256"`
	Parts   int      `json:"parts"`
}

// FileEntry is one tenant file.
type FileEntry struct {
	Key    string `json:"key"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes an archive.
type Manifest struct {
	Format        string            `json:"format"`
	TenantID      string            `json:"tenant_id"`
	Slug          string            `json:"slug"`
	SchemaVersion int64             `json:"schema_version"`
	ExportedAt    time.Time         `json:"exported_at"`
	AppVersion    string            `json:"app_version"`
	Settings      map[string]string `json:"output_settings"`
	Tables        []TableEntry      `json:"tables"`
	Excluded      []string          `json:"excluded"`
	Files         []FileEntry       `json:"files"`
	// Missing lists file keys that were referenced but not in storage
	// (outsidePrefix for a key outside the tenant's own prefix).
	Missing []string `json:"missing_files"`
}

// outsidePrefix marks a file row whose key is not under tenants/<id>/.
const outsidePrefix = "outside-tenant-prefix"

// Table returns the manifest entry for a table.
func (m Manifest) Table(name string) (TableEntry, bool) {
	for _, t := range m.Tables {
		if t.Name == name {
			return t, true
		}
	}
	return TableEntry{}, false
}

// Rows is the total row count.
func (m Manifest) Rows() int64 {
	var n int64
	for _, t := range m.Tables {
		n += t.Rows
	}
	return n
}

// validate checks every name in the manifest before any of it is used to
// build SQL or paths.
func (m Manifest) validate() error {
	if m.Format != Format || !tenantRe.MatchString(m.TenantID) || m.SchemaVersion < 1 || len(m.Tables) == 0 ||
		len(m.Tables) > maxTables || len(m.Files) > maxFiles {
		return ErrArchive
	}
	seen := map[string]bool{}
	for _, t := range m.Tables {
		if !identRe.MatchString(t.Name) || seen[t.Name] || len(t.Columns) == 0 || len(t.Columns) > maxColumns ||
			len(t.OrderBy) == 0 || t.Rows < 0 || t.Parts < 1 || !sha256Re.MatchString(t.SHA256) {
			return ErrArchive
		}
		seen[t.Name] = true
		cols := map[string]bool{}
		for _, c := range t.Columns {
			if !identRe.MatchString(c) || cols[c] {
				return ErrArchive
			}
			cols[c] = true
		}
		for _, c := range t.OrderBy {
			if !cols[c] {
				return ErrArchive
			}
		}
	}
	for _, e := range m.Excluded {
		if !identRe.MatchString(e) || seen[e] {
			return ErrArchive
		}
	}
	for _, k := range m.Missing {
		if k != outsidePrefix && !validFileKey(k) {
			return ErrArchive
		}
	}
	keys := map[string]bool{}
	for _, f := range m.Files {
		if !validFileKey(f.Key) || keys[f.Key] || f.Size < 0 || f.Size > maxFileSize || !sha256Re.MatchString(f.SHA256) {
			return ErrArchive
		}
		keys[f.Key] = true
	}
	return nil
}

func validFileKey(k string) bool {
	return fileKeyRe.MatchString(k) && !strings.Contains(k, "..") && !strings.Contains(k, "//") && !strings.HasSuffix(k, "/")
}

// sign is the manifest signature.
func sign(key, manifest []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(sigPrefix))
	m.Write(manifest)
	return m.Sum(nil)
}

// --------------------------------------------------------------- writer

// Summary describes the stored (encrypted) archive.
type Summary struct {
	Size           int64
	SHA256         string // of the encrypted bytes
	ManifestSHA256 string
}

type hashCounter struct {
	h hash.Hash
	n int64
}

func (c *hashCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return c.h.Write(p)
}

func (c *hashCounter) sum() string { return hex.EncodeToString(c.h.Sum(nil)) }

// Writer builds an archive. It is not safe for concurrent use.
type Writer struct {
	key     []byte
	out     *hashCounter
	aw      io.WriteCloser
	zw      *zstd.Encoder
	tw      *tar.Writer
	modTime time.Time
	tables  []TableEntry
	files   []FileEntry
	open    *TableWriter
	err     error
}

// NewWriter encrypts to the recipient and writes to dst.
func NewWriter(dst io.Writer, to age.Recipient, manifestKey []byte, now time.Time) (*Writer, error) {
	if to == nil || len(manifestKey) == 0 {
		return nil, errors.New("tenantio: recipient and manifest key are required")
	}
	out := &hashCounter{h: sha256.New()}
	aw, err := age.Encrypt(io.MultiWriter(dst, out), to)
	if err != nil {
		return nil, err
	}
	zw, err := zstd.NewWriter(aw, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	return &Writer{key: manifestKey, out: out, aw: aw, zw: zw, tw: tar.NewWriter(zw), modTime: now.UTC().Truncate(time.Second)}, nil
}

func (w *Writer) entry(name string, data []byte) error {
	if err := w.tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: w.modTime,
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return err
	}
	_, err := w.tw.Write(data)
	return err
}

// TableWriter receives one table's CSV stream.
type TableWriter struct {
	w     *Writer
	entry TableEntry
	h     hash.Hash
	buf   bytes.Buffer
}

// Table starts a table. The previous table must be finished.
func (w *Writer) Table(name string, columns, orderBy []string) (*TableWriter, error) {
	if w.err != nil {
		return nil, w.err
	}
	if w.open != nil || len(w.files) > 0 || !identRe.MatchString(name) {
		return nil, errors.New("tenantio: tables must be written one at a time, before files")
	}
	tw := &TableWriter{w: w, h: sha256.New(),
		entry: TableEntry{Name: name, Columns: append([]string(nil), columns...), OrderBy: append([]string(nil), orderBy...)}}
	w.open = tw
	return tw, nil
}

func (t *TableWriter) flush() error {
	if err := t.w.entry(fmt.Sprintf("tables/%s/%05d.csv", t.entry.Name, t.entry.Parts), t.buf.Bytes()); err != nil {
		t.w.err = err
		return err
	}
	t.entry.Parts++
	t.buf.Reset()
	return nil
}

// Write implements io.Writer: bytes are hashed and cut into parts.
func (t *TableWriter) Write(p []byte) (int, error) {
	if t.w.err != nil {
		return 0, t.w.err
	}
	t.h.Write(p)
	n := 0
	for len(p) > 0 {
		room := partSize - t.buf.Len()
		k := min(room, len(p))
		t.buf.Write(p[:k])
		p, n = p[k:], n+k
		if t.buf.Len() == partSize {
			if err := t.flush(); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// Finish ends the table with its row count (from the COPY command tag).
// A table always has at least one part (the header line at least).
func (t *TableWriter) Finish(rows int64) error {
	if t.w.open != t {
		return errors.New("tenantio: table already finished")
	}
	if t.buf.Len() > 0 || t.entry.Parts == 0 {
		if err := t.flush(); err != nil {
			return err
		}
	}
	t.entry.Rows = rows
	t.entry.SHA256 = hex.EncodeToString(t.h.Sum(nil))
	t.w.tables = append(t.w.tables, t.entry)
	t.w.open = nil
	return nil
}

// File adds one tenant file of the given size.
func (w *Writer) File(key string, size int64, r io.Reader) error {
	if w.err != nil {
		return w.err
	}
	if w.open != nil || !validFileKey(key) || size < 0 || size > maxFileSize {
		return errors.New("tenantio: invalid file entry")
	}
	h := sha256.New()
	if err := w.tw.WriteHeader(&tar.Header{Name: "files/" + key, Mode: 0o600, Size: size, ModTime: w.modTime,
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		w.err = err
		return err
	}
	n, err := io.Copy(io.MultiWriter(w.tw, h), io.LimitReader(r, size))
	if err == nil && n != size {
		err = fmt.Errorf("tenantio: file %s is shorter than its size", key)
	}
	if err != nil {
		w.err = err
		return err
	}
	w.files = append(w.files, FileEntry{Key: key, Size: size, SHA256: hex.EncodeToString(h.Sum(nil))})
	return nil
}

// Close writes the manifest (header fields from m; tables and files from
// what was written) and its signature, and closes every layer. The
// destination is complete only when Close returns nil.
func (w *Writer) Close(m Manifest) (Manifest, Summary, error) {
	if w.err != nil {
		return Manifest{}, Summary{}, w.err
	}
	if w.open != nil {
		return Manifest{}, Summary{}, errors.New("tenantio: a table is not finished")
	}
	m.Format, m.Tables, m.Files = Format, w.tables, w.files
	if m.Files == nil {
		m.Files = []FileEntry{}
	}
	if m.Excluded == nil {
		m.Excluded = []string{}
	}
	if m.Missing == nil {
		m.Missing = []string{}
	}
	m.ExportedAt = m.ExportedAt.UTC()
	if err := m.validate(); err != nil {
		return Manifest{}, Summary{}, fmt.Errorf("tenantio: manifest: %w", err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	if err := w.entry("manifest.json", raw); err != nil {
		return Manifest{}, Summary{}, err
	}
	if err := w.entry("manifest.sig", []byte(hex.EncodeToString(sign(w.key, raw)))); err != nil {
		return Manifest{}, Summary{}, err
	}
	for _, c := range []io.Closer{w.tw, w.zw, w.aw} {
		if err := c.Close(); err != nil {
			return Manifest{}, Summary{}, err
		}
	}
	ms := sha256.Sum256(raw)
	return m, Summary{Size: w.out.n, SHA256: w.out.sum(), ManifestSHA256: hex.EncodeToString(ms[:])}, nil
}

// --------------------------------------------------------------- reader

// reader walks an archive and enforces its structure.
type reader struct {
	src     *hashCounter
	ar      io.Reader
	zr      *zstd.Decoder
	tr      *tar.Reader
	pending *tar.Header // the next header, already read
	done    bool
}

func openReader(src io.Reader, id age.Identity) (*reader, error) {
	if id == nil {
		return nil, errors.New("tenantio: identity is required")
	}
	hc := &hashCounter{h: sha256.New()}
	ar, err := age.Decrypt(io.TeeReader(src, hc), id)
	if err != nil {
		return nil, fmt.Errorf("tenantio: decrypt: %w", err)
	}
	zr, err := zstd.NewReader(ar, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(256<<20),
		zstd.WithDecoderMaxWindow(64<<20))
	if err != nil {
		return nil, err
	}
	r := &reader{src: hc, ar: ar, zr: zr, tr: tar.NewReader(zr)}
	return r, nil
}

func (r *reader) close() { r.zr.Close() }

// next returns the next entry header (validated as a plain file).
func (r *reader) next() (*tar.Header, error) {
	if r.pending != nil {
		h := r.pending
		r.pending = nil
		return h, nil
	}
	if r.done {
		return nil, io.EOF
	}
	h, err := r.tr.Next()
	if errors.Is(err, io.EOF) {
		r.done = true
		return nil, io.EOF
	}
	if err != nil {
		return nil, ErrArchive
	}
	if h.Typeflag != tar.TypeReg || h.Size < 0 {
		return nil, ErrArchive
	}
	return h, nil
}

// finish reads every remaining byte, so that age authenticates the final
// chunk and the hash covers the whole object.
func (r *reader) finish() (Summary, error) {
	if _, err := r.next(); !errors.Is(err, io.EOF) {
		return Summary{}, ErrArchive // something after manifest.sig
	}
	if _, err := io.Copy(io.Discard, r.zr); err != nil {
		return Summary{}, ErrArchive
	}
	if _, err := io.Copy(io.Discard, r.ar); err != nil {
		return Summary{}, fmt.Errorf("tenantio: decrypt: %w", err)
	}
	return Summary{Size: r.src.n, SHA256: r.src.sum()}, nil
}

// tableStream reads one table's parts as one stream, hashing it.
type tableStream struct {
	r     *reader
	name  string
	part  int
	cur   io.Reader
	h     hash.Hash
	err   error
	ended bool
}

func (s *tableStream) Read(p []byte) (int, error) {
	for {
		if s.err != nil {
			return 0, s.err
		}
		if s.ended {
			return 0, io.EOF
		}
		n, err := s.cur.Read(p)
		if n > 0 {
			s.h.Write(p[:n])
			return n, nil
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			s.err = ErrArchive
			return 0, s.err
		}
		// This part is done; is the next entry the next part?
		h, err := s.r.next()
		if errors.Is(err, io.EOF) {
			s.err = ErrArchive // the manifest must follow
			return 0, s.err
		}
		if err != nil {
			s.err = err
			return 0, err
		}
		if m := partRe.FindStringSubmatch(h.Name); m != nil && m[1] == s.name {
			if m[2] != fmt.Sprintf("%05d", s.part+1) || h.Size > int64(partSize) {
				s.err = ErrArchive
				return 0, s.err
			}
			s.part++
			s.cur = io.LimitReader(s.r.tr, h.Size)
			continue
		}
		s.r.pending = h
		s.ended = true
		return 0, io.EOF
	}
}

// walk reads the archive once: tables in order (each through onTable,
// which must read the stream to the end), then files (onFile), then the
// manifest and its signature. It returns the verified manifest.
func walk(src io.Reader, id age.Identity, manifestKey []byte, expect *Manifest,
	onTable func(TableEntry, io.Reader) error, onFile func(FileEntry, io.Reader) error) (Manifest, Summary, error) {
	r, err := openReader(src, id)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	defer r.close()
	type got struct {
		name   string
		parts  int
		sha256 string
	}
	var tables []got
	var files []FileEntry
	var raw, sig []byte
	maxPart := int64(partSize)
	for {
		h, err := r.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, Summary{}, err
		}
		switch {
		case partRe.MatchString(h.Name):
			m := partRe.FindStringSubmatch(h.Name)
			if raw != nil || len(files) > 0 || m[2] != "00000" || h.Size > maxPart || len(tables) >= maxTables {
				return Manifest{}, Summary{}, ErrArchive
			}
			for _, t := range tables {
				if t.name == m[1] {
					return Manifest{}, Summary{}, ErrArchive // a table appears twice
				}
			}
			s := &tableStream{r: r, name: m[1], cur: io.LimitReader(r.tr, h.Size), h: sha256.New()}
			entry := TableEntry{Name: m[1]}
			if expect != nil {
				var ok bool
				if entry, ok = expect.Table(m[1]); !ok {
					return Manifest{}, Summary{}, ErrArchive
				}
			}
			if onTable != nil {
				if err := onTable(entry, s); err != nil {
					return Manifest{}, Summary{}, err
				}
			}
			if _, err := io.Copy(io.Discard, s); err != nil {
				return Manifest{}, Summary{}, err
			}
			tables = append(tables, got{name: m[1], parts: s.part + 1, sha256: hex.EncodeToString(s.h.Sum(nil))})
		case strings.HasPrefix(h.Name, "files/"):
			key := strings.TrimPrefix(h.Name, "files/")
			if raw != nil || !validFileKey(key) || h.Size > maxFileSize || len(files) >= maxFiles {
				return Manifest{}, Summary{}, ErrArchive
			}
			fh := sha256.New()
			body := io.TeeReader(io.LimitReader(r.tr, h.Size), fh)
			fe := FileEntry{Key: key, Size: h.Size}
			if onFile != nil {
				if err := onFile(fe, body); err != nil {
					return Manifest{}, Summary{}, err
				}
			}
			if _, err := io.Copy(io.Discard, body); err != nil {
				return Manifest{}, Summary{}, ErrArchive
			}
			fe.SHA256 = hex.EncodeToString(fh.Sum(nil))
			files = append(files, fe)
		case h.Name == "manifest.json":
			if raw != nil || h.Size > maxManifest {
				return Manifest{}, Summary{}, ErrArchive
			}
			if raw, err = io.ReadAll(io.LimitReader(r.tr, h.Size)); err != nil {
				return Manifest{}, Summary{}, ErrArchive
			}
		case h.Name == "manifest.sig":
			if raw == nil || sig != nil || h.Size != 64 {
				return Manifest{}, Summary{}, ErrArchive
			}
			b, err := io.ReadAll(io.LimitReader(r.tr, 64))
			if err != nil {
				return Manifest{}, Summary{}, ErrArchive
			}
			if sig, err = hex.DecodeString(string(b)); err != nil {
				return Manifest{}, Summary{}, ErrArchive
			}
			sum, err := r.finish()
			if err != nil {
				return Manifest{}, Summary{}, err
			}
			if !hmac.Equal(sig, sign(manifestKey, raw)) {
				return Manifest{}, Summary{}, ErrSignature
			}
			var m Manifest
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err := d.Decode(&m); err != nil || m.validate() != nil {
				return Manifest{}, Summary{}, ErrArchive
			}
			if len(m.Tables) != len(tables) || len(m.Files) != len(files) {
				return Manifest{}, Summary{}, ErrChecksum
			}
			for i, t := range m.Tables {
				g := tables[i]
				if g.name != t.Name || g.parts != t.Parts || g.sha256 != t.SHA256 {
					return Manifest{}, Summary{}, ErrChecksum
				}
			}
			for i, f := range m.Files {
				if files[i] != f {
					return Manifest{}, Summary{}, ErrChecksum
				}
			}
			ms := sha256.Sum256(raw)
			sum.ManifestSHA256 = hex.EncodeToString(ms[:])
			return m, sum, nil
		default:
			return Manifest{}, Summary{}, ErrArchive
		}
	}
	return Manifest{}, Summary{}, ErrArchive // no manifest.sig
}

// Verify reads the whole archive and checks its structure, every table's
// and file's hash, and the manifest signature. Nothing is trusted before it
// returns nil.
func Verify(src io.Reader, id age.Identity, manifestKey []byte) (Manifest, Summary, error) {
	return walk(src, id, manifestKey, nil, nil, nil)
}

// Load reads an archive that Verify accepted as m and hands each table's
// CSV stream to onTable (and each file to onFile, if set). The archive is
// checked again on the way; the caller must compare the returned summary
// with Verify's, so an object changed in between is caught.
func Load(src io.Reader, id age.Identity, manifestKey []byte, m Manifest,
	onTable func(TableEntry, io.Reader) error, onFile func(FileEntry, io.Reader) error) (Summary, error) {
	got, sum, err := walk(src, id, manifestKey, &m, onTable, onFile)
	if err != nil {
		return Summary{}, err
	}
	a, errA := json.Marshal(got)
	b, errB := json.Marshal(m)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		return Summary{}, ErrChecksum // not the archive that was verified
	}
	return sum, nil
}
