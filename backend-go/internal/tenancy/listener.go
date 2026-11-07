package tenancy

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// Notification channels (TENANCY §5.5). Payloads carry IDs only and are
// hints: they invalidate caches or wake workers, nothing more.
const (
	ChanTenantChanged        = "tenant_changed"
	ChanAuthChanged          = "auth_changed"
	ChanRoleChanged          = "role_changed"
	ChanImpersonationChanged = "impersonation_changed"
	ChanMailDispatch         = "mail_dispatch"
)

// listenAll subscribes to every channel. One constant statement (constsql).
const listenAll = "LISTEN tenant_changed; LISTEN auth_changed; LISTEN role_changed; LISTEN impersonation_changed; LISTEN mail_dispatch"

// ListenerMetrics are optional.
type ListenerMetrics struct {
	Connected  prometheus.Gauge
	Reconnects prometheus.Counter
}

// Listener holds one dedicated, direct connection for LISTEN. It is never
// taken from the pool and must never go through a transaction pooler.
type Listener struct {
	dsn      secrets.Secret
	log      *slog.Logger
	metrics  ListenerMetrics
	mu       sync.RWMutex
	handlers map[string][]func(payload string)
	flushers []func()

	connected  atomic.Bool
	minBackoff time.Duration
	maxBackoff time.Duration

	pidMu sync.Mutex
	pid   uint32
}

// NewListener creates a listener for dsn (LISTEN_DATABASE_URL).
func NewListener(dsn secrets.Secret, log *slog.Logger, m ListenerMetrics) *Listener {
	if log == nil {
		log = slog.Default()
	}
	return &Listener{
		dsn: dsn, log: log, metrics: m, handlers: map[string][]func(string){},
		minBackoff: time.Second, maxBackoff: 30 * time.Second,
	}
}

// On registers a handler for a channel. Call before Run.
func (l *Listener) On(channel string, fn func(payload string)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handlers[channel] = append(l.handlers[channel], fn)
}

// OnReconnect registers a cache flush that runs after every (re)connect,
// because notifications sent while disconnected are lost, and after every
// disconnect, so the short TTL applies to everything cached while down.
func (l *Listener) OnReconnect(fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushers = append(l.flushers, fn)
}

// Healthy reports whether the listener is connected and subscribed.
func (l *Listener) Healthy() bool { return l.connected.Load() }

// TTL returns normal when healthy and degraded otherwise (TENANCY §5.5).
func (l *Listener) TTL(normal, degraded time.Duration) func() time.Duration {
	return func() time.Duration {
		if l.Healthy() {
			return normal
		}
		return degraded
	}
}

// BackendPID returns the server PID of the current connection (tests).
func (l *Listener) BackendPID() uint32 {
	l.pidMu.Lock()
	defer l.pidMu.Unlock()
	return l.pid
}

// Run connects and dispatches notifications until ctx ends, reconnecting
// with backoff.
func (l *Listener) Run(ctx context.Context) {
	backoff := l.minBackoff
	first := true
	for ctx.Err() == nil {
		if !first && l.metrics.Reconnects != nil {
			l.metrics.Reconnects.Inc()
		}
		first = false
		err := l.session(ctx, func() { backoff = l.minBackoff })
		l.setConnected(false)
		// Entries cached while healthy carry the long TTL. Drop them so the
		// short TTL applies to everything while notifications are missing.
		l.flush()
		if ctx.Err() != nil {
			return
		}
		l.log.Warn("listener disconnected", slog.Any("err", err), slog.Duration("retry_in", backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, l.maxBackoff)
	}
}

func (l *Listener) session(ctx context.Context, onUp func()) error {
	cfg, err := pgx.ParseConfig(l.dsn.Reveal())
	if err != nil {
		return errInvalidListenerDSN
	}
	cfg.RuntimeParams["application_name"] = "posd-listener"
	cfg.ConnectTimeout = 5 * time.Second
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = conn.Close(cctx)
	}()

	if _, err := conn.Exec(ctx, listenAll); err != nil {
		return err
	}
	l.pidMu.Lock()
	l.pid = conn.PgConn().PID()
	l.pidMu.Unlock()

	l.setConnected(true)
	l.flush()
	onUp()

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		l.mu.RLock()
		hs := l.handlers[n.Channel]
		l.mu.RUnlock()
		for _, h := range hs {
			h(n.Payload)
		}
	}
}

func (l *Listener) flush() {
	l.mu.RLock()
	flushers := append([]func(){}, l.flushers...)
	l.mu.RUnlock()
	for _, f := range flushers {
		f()
	}
}

func (l *Listener) setConnected(v bool) {
	l.connected.Store(v)
	if l.metrics.Connected != nil {
		if v {
			l.metrics.Connected.Set(1)
		} else {
			l.metrics.Connected.Set(0)
		}
	}
}

type listenerError string

func (e listenerError) Error() string { return string(e) }

const errInvalidListenerDSN = listenerError("tenancy: invalid LISTEN_DATABASE_URL")
