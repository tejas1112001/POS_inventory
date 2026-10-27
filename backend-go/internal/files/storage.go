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

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
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
