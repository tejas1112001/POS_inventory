// Package files stores tenant files (images now; receipts and exports
// later) behind a Storage interface, validates and re-encodes images, and
// serves them through the public allow-list or behind login
// (API_CONTRACT §5.1, C-17).
package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/tejas1112001/POS_inventory/backend/internal/secrets"
)

// ErrNotFound means the object does not exist.
var ErrNotFound = errors.New("files: object not found")

// Storage keeps file bytes. Keys look like "tenants/<id>/images/<uuid>.png".
type Storage interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	// PutStream uploads from a reader of unknown length with bounded
	// memory (tenant exports). On error nothing is left under key.
	PutStream(ctx context.Context, key, contentType string, r io.Reader) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every object under prefix ("tenants/<id>/",
	// the deleted-shop purge) and returns how many it removed.
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// validPrefix accepts "a/b/" and deeper: a valid key with a trailing
// slash, never the whole store or one top-level folder.
func validPrefix(prefix string) bool {
	k, ok := strings.CutSuffix(prefix, "/")
	return ok && validKey(k) && strings.Contains(k, "/")
}

func validKey(key string) bool {
	if key == "" || len(key) > 300 || strings.HasPrefix(key, "/") || strings.Contains(key, "..") || path.Clean(key) != key {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '/' && r != '-' && r != '.' && r != '_' {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------- filesystem

// FSStorage stores files under a directory, confined by os.Root.
type FSStorage struct{ root *os.Root }

// NewFSStorage opens (and creates) dir.
func NewFSStorage(dir string) (*FSStorage, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &FSStorage{root: r}, nil
}

// Put writes atomically (temp file + rename).
func (s *FSStorage) Put(_ context.Context, key, _ string, data []byte) error {
	if !validKey(key) {
		return fmt.Errorf("files: invalid key")
	}
	if err := s.root.MkdirAll(path.Dir(key), 0o750); err != nil {
		return err
	}
	tmp := key + ".tmp"
	if err := s.root.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return s.root.Rename(tmp, key)
}

// PutStream writes atomically from a reader.
func (s *FSStorage) PutStream(_ context.Context, key, _ string, r io.Reader) (int64, error) {
	if !validKey(key) {
		return 0, fmt.Errorf("files: invalid key")
	}
	if err := s.root.MkdirAll(path.Dir(key), 0o750); err != nil {
		return 0, err
	}
	tmp := key + ".tmp"
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = s.root.Rename(tmp, key)
	}
	if err != nil {
		_ = s.root.Remove(tmp)
		return 0, err
	}
	return n, nil
}

// Get opens a file.
func (s *FSStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	if !validKey(key) {
		return nil, 0, ErrNotFound
	}
	f, err := s.root.Open(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// Delete removes a file (missing is fine).
func (s *FSStorage) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return nil
	}
	err := s.root.Remove(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// DeletePrefix removes the directory prefix names.
func (s *FSStorage) DeletePrefix(_ context.Context, prefix string) (int, error) {
	if !validPrefix(prefix) {
		return 0, fmt.Errorf("files: invalid prefix")
	}
	dir := strings.TrimSuffix(prefix, "/")
	n := 0
	err := fs.WalkDir(s.root.FS(), dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return n, s.root.RemoveAll(dir)
}

// ------------------------------------------------------------------- S3

// S3Config configures the S3 driver (any S3-compatible service).
type S3Config struct {
	Endpoint  string // e.g. https://s3.ap-south-1.amazonaws.com or http://s3:7070
	Bucket    string
	AccessKey secrets.Secret
	SecretKey secrets.Secret
	Region    string
}

// S3Storage stores files in a bucket.
type S3Storage struct {
	c      *minio.Client
	bucket string
}

// NewS3Storage connects to the endpoint and makes sure the bucket exists.
func NewS3Storage(ctx context.Context, c S3Config) (*S3Storage, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("files: S3_ENDPOINT must be an http(s) URL")
	}
	if c.Bucket == "" {
		return nil, errors.New("files: S3_BUCKET is required")
	}
	cl, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey.Reveal(), c.SecretKey.Reveal(), ""),
		Secure:       u.Scheme == "https",
		Region:       c.Region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("files: s3 client: %w", err)
	}
	ok, err := cl.BucketExists(ctx, c.Bucket)
	if err != nil {
		return nil, fmt.Errorf("files: s3 bucket check: %w", err)
	}
	if !ok {
		if err := cl.MakeBucket(ctx, c.Bucket, minio.MakeBucketOptions{Region: c.Region}); err != nil {
			return nil, fmt.Errorf("files: create bucket: %w", err)
		}
	}
	return &S3Storage{c: cl, bucket: c.Bucket}, nil
}

// Put uploads an object.
func (s *S3Storage) Put(ctx context.Context, key, contentType string, data []byte) error {
	if !validKey(key) {
		return fmt.Errorf("files: invalid key")
	}
	_, err := s.c.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

// PutStream uploads in 16 MiB parts (multipart), so memory stays bounded
// whatever the size. A failed upload is aborted and leaves no object.
func (s *S3Storage) PutStream(ctx context.Context, key, contentType string, r io.Reader) (int64, error) {
	if !validKey(key) {
		return 0, fmt.Errorf("files: invalid key")
	}
	info, err := s.c.PutObject(ctx, s.bucket, key, r, -1,
		minio.PutObjectOptions{ContentType: contentType, PartSize: 16 << 20})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

// Get downloads an object.
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if !validKey(key) {
		return nil, 0, ErrNotFound
	}
	st, err := s.c.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	obj, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	return obj, st.Size, nil
}

// Delete removes an object.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return nil
	}
	return s.c.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// DeletePrefix lists the objects under prefix and removes them in batches.
func (s *S3Storage) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if !validPrefix(prefix) {
		return 0, fmt.Errorf("files: invalid prefix")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listed := s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	toRemove := make(chan minio.ObjectInfo)
	n := 0
	var listErr error
	go func() {
		defer close(toRemove)
		for o := range listed {
			if o.Err != nil {
				listErr = o.Err
				return
			}
			select {
			case toRemove <- o:
				n++
			case <-ctx.Done():
				return
			}
		}
	}()
	var errs []error
	for e := range s.c.RemoveObjects(ctx, s.bucket, toRemove, minio.RemoveObjectsOptions{}) {
		errs = append(errs, fmt.Errorf("%s: %w", e.ObjectName, e.Err))
	}
	if listErr != nil {
		errs = append(errs, listErr)
	}
	return n - len(errs), errors.Join(errs...)
}
