//go:build integration

package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// The same image as the dev stack (deploy/docker-compose.yml).
const versityImage = "versity/versitygw:v1.9.0@sha256:eb7f786fcd91e72194e48fcf1ac16302d686bf8e3a71434bdbc067a6efd3fdc7"

func TestS3StorageAgainstVersityGateway(t *testing.T) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        versityImage,
			Cmd:          []string{"posix", "/tmp"},
			Env:          map[string]string{"ROOT_ACCESS_KEY_ID": "testaccess", "ROOT_SECRET_ACCESS_KEY": "testsecretkey123"},
			ExposedPorts: []string{"7070/tcp"},
			WaitingFor:   wait.ForListeningPort("7070/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	endpoint, err := c.PortEndpoint(ctx, "7070/tcp", "http")
	require.NoError(t, err)

	s, err := NewS3Storage(ctx, S3Config{Endpoint: endpoint, Bucket: "posd-test", AccessKey: secrets.Secret("testaccess"),
		SecretKey: secrets.Secret("testsecretkey123"), Region: "us-east-1"})
	require.NoError(t, err, "bucket created on first use")
	_, err = NewS3Storage(ctx, S3Config{Endpoint: endpoint, Bucket: "posd-test", AccessKey: secrets.Secret("testaccess"),
		SecretKey: secrets.Secret("testsecretkey123"), Region: "us-east-1"})
	require.NoError(t, err, "existing bucket is fine")

	key := "tenants/abcdefghij/images/00000000-0000-4000-8000-000000000001.png"
	require.NoError(t, s.Put(ctx, key, "image/png", []byte("png-bytes")))
	rc, size, err := s.Get(ctx, key)
	require.NoError(t, err)
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	require.Equal(t, int64(9), size)
	require.Equal(t, "png-bytes", string(b))
	require.NoError(t, s.Delete(ctx, key))
	_, _, err = s.Get(ctx, key)
	require.ErrorIs(t, err, ErrNotFound)

	// Streaming past one 16 MiB part (multipart), then a failing source.
	big := bytes.Repeat([]byte("0123456789abcdef"), (17<<20)/16)
	skey := "exports/abcdefghij/00000000-0000-4000-8000-000000000002.tar.zst.age"
	n, err := s.PutStream(ctx, skey, "application/octet-stream", bytes.NewReader(big))
	require.NoError(t, err)
	require.EqualValues(t, len(big), n)
	rc, size, err = s.Get(ctx, skey)
	require.NoError(t, err)
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	require.EqualValues(t, len(big), size)
	require.True(t, bytes.Equal(big, got))
	fkey := "exports/abcdefghij/00000000-0000-4000-8000-000000000003.tar.zst.age"
	_, err = s.PutStream(ctx, fkey, "application/octet-stream", io.MultiReader(bytes.NewReader(big), failing{}))
	require.Error(t, err)
	_, _, err = s.Get(ctx, fkey)
	require.ErrorIs(t, err, ErrNotFound, "an aborted upload leaves no object")

	_, err = NewS3Storage(ctx, S3Config{Endpoint: endpoint, Bucket: "posd-test", AccessKey: secrets.Secret("testaccess"),
		SecretKey: secrets.Secret("wrong-secret"), Region: "us-east-1"})
	require.Error(t, err, "bad credentials fail at startup")
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("source failed") }
