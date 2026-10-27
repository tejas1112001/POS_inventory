package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func key(t *testing.T) string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(b)
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.LoadFrom(map[string]string{
		"BASE_DOMAIN": "pos.test", "PLATFORM_NAME": "Test", "APP_ENV": "test", "PUBLIC_SCHEME": "http",
		// Nothing listens here: the pool connects lazily, so startup works
		// and /readyz reports the DB as failing.
		"DATABASE_URL":         "postgres://u:p@127.0.0.1:1/x?sslmode=disable&connect_timeout=1",
		"DATABASE_JOBS_URL":    "postgres://u:p@127.0.0.1:1/x?sslmode=disable&connect_timeout=1",
		"JWT_KEYS":             "k1:" + key(t),
		"PLATFORM_JWT_KEYS":    "p1:" + key(t),
		"MASTER_KEYS":          "m1:" + key(t),
		"SHUTDOWN_DRAIN_DELAY": "300ms",
		"STORAGE_FS_ROOT":      t.TempDir(),
		"SHUTDOWN_TIMEOUT":     "2s",
	})
	require.NoError(t, err)
	return cfg
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func get(t *testing.T, c *http.Client, url, host string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Host = host
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServeAndGracefulShutdown(t *testing.T) {
	var logs syncBuf
	cfg := testConfig(t)
	a, err := New(context.Background(), cfg, obs.NewLogger(&logs, "info", "json"))
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	iln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx, ln, iln) }()

	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	t.Cleanup(client.CloseIdleConnections)

	code, body := get(t, client, base+"/healthz", "acme.pos.test")
	require.Equal(t, 200, code)
	require.JSONEq(t, `{"status":"ok"}`, body)

	code, body = get(t, client, base+"/readyz", "acme.pos.test")
	require.Equal(t, 503, code)
	require.Contains(t, body, `"db":"fail"`)
	require.NotContains(t, body, "127.0.0.1", "no error text in readiness output")

	code, _ = get(t, client, base+"/api/anything", "evil.example")
	require.Equal(t, http.StatusMisdirectedRequest, code)

	code, body = get(t, client, "http://"+iln.Addr().String()+"/metrics", "")
	require.Equal(t, 200, code)
	require.Contains(t, body, "pos_http_requests_total")
	require.NotContains(t, body, "acme", "no tenant slugs in metric labels")

	cancel()
	require.Eventually(t, a.Health().Draining, time.Second, 10*time.Millisecond)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	resp, err := client.Get(base + "/healthz")
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "listener closed after shutdown")
	require.True(t, strings.Contains(logs.String(), "shutdown complete"))
}

func TestRunFailsOnBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	cfg := testConfig(t)
	cfg.HTTPAddr = ln.Addr().String()
	a, err := New(context.Background(), cfg, obs.NewLogger(io.Discard, "info", "json"))
	require.NoError(t, err)
	require.Error(t, a.Run(context.Background()))
}
