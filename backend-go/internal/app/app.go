// Package app wires the components together and runs the process lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Server hardening (OPERATIONS §5).
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 32 << 10
)

// App is the running process.
type App struct {
	cfg      *config.Config
	log      *slog.Logger
	metrics  *obs.Metrics
	health   *httpx.Health
	pool     *pgxpool.Pool
	jobsPool *pgxpool.Pool
	http     *http.Server
	internal *http.Server

	sessions *tenancy.Sessions
	registry *tenancy.Registry
	listener *tenancy.Listener
}

// New builds the app. It does not open network listeners.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	a := &App{cfg: cfg, log: log, metrics: obs.NewMetrics(), health: httpx.NewHealth(time.Second)}

	var err error
	a.pool, err = db.NewPool(ctx, cfg.DatabaseURL, db.PoolOptions{
		MaxConns: cfg.DBPoolMax, MinConns: cfg.DBPoolMin, ApplicationName: "posd",
	})
	if err != nil {
		return nil, err
	}
	a.jobsPool, err = db.NewPool(ctx, cfg.DatabaseJobsURL, db.PoolOptions{
		MaxConns: 10, MinConns: 0, ApplicationName: "posd-jobs",
	})
	if err != nil {
		a.pool.Close()
		return nil, err
	}
	a.health.Add("db", a.pool.Ping)
	a.health.Add("jobs_db", a.jobsPool.Ping)

	a.sessions = tenancy.NewSessions(a.pool, cfg.DBStatementTimeout, a.metrics.SessionTampered, log)
	a.listener = tenancy.NewListener(cfg.ListenDatabaseURL, log, tenancy.ListenerMetrics{
		Connected: a.metrics.ListenerUp, Reconnects: a.metrics.ListenerRetries,
	})
	a.registry = tenancy.NewRegistry(a.sessions, a.listener.TTL(tenancy.RegistryTTL, tenancy.RegistryTTLDegraded))
	a.listener.On(tenancy.ChanTenantChanged, func(string) { a.registry.Flush() })
	a.listener.OnReconnect(a.registry.Flush)

	latest, err := migrate.LatestPlatformVersion()
	if err != nil {
		a.closePools()
		return nil, err
	}
	a.health.Add("platform_schema", func(ctx context.Context) error {
		var v int64
		err := a.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
			return tx.QueryRow(ctx, `SELECT coalesce(max(version_id), 0) FROM platform.goose_db_version WHERE is_applied`).Scan(&v)
		})
		if err != nil {
			return err
		}
		if v < latest {
			return fmt.Errorf("platform schema at %d, need %d", v, latest)
		}
		return nil
	})

	spas := map[string]http.Handler{}
	for _, name := range []string{"tenant", "admin", "landing"} {
		dir := ""
		if cfg.StaticDir != "" {
			dir = filepath.Join(cfg.StaticDir, name)
		}
		spa, err := httpx.NewSPA(dir)
		if err != nil {
			log.Warn("static build not served", slog.String("app", name), slog.Any("err", err))
			continue
		}
		if spa != nil {
			spas[name] = spa
		}
	}

	network := httpx.Network{BaseDomain: cfg.BaseDomain, TrustedProxies: cfg.TrustedProxies}
	handler := httpx.NewRouter(httpx.RouterDeps{
		Logger:         log,
		Metrics:        a.metrics,
		Network:        network,
		Health:         a.health,
		HSTS:           cfg.HSTSEnabled,
		RequestTimeout: cfg.RequestTimeout,
		CORSDevOrigins: cfg.CORSDevOrigins,
		TenantResolver: tenancy.Resolve(a.registry),
		TenantSPA:      spas["tenant"],
		AdminSPA:       spas["admin"],
		LandingSPA:     spas["landing"],
	})

	a.http = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", a.metrics.Handler())
	a.internal = &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	return a, nil
}

// Handler exposes the public HTTP handler (tests).
func (a *App) Handler() http.Handler { return a.http.Handler }

// Health exposes the health state (tests).
func (a *App) Health() *httpx.Health { return a.health }

// Sessions exposes the session factory (tests, later steps).
func (a *App) Sessions() *tenancy.Sessions { return a.sessions }

// Registry exposes the tenant registry (tests, later steps).
func (a *App) Registry() *tenancy.Registry { return a.registry }

// Run serves until ctx is cancelled, then shuts down gracefully
// (OPERATIONS §5). It returns an error if shutdown hit its timeout.
func (a *App) Run(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", a.cfg.HTTPAddr)
	if err != nil {
		a.closePools()
		return fmt.Errorf("listen %s: %w", a.cfg.HTTPAddr, err)
	}
	iln, err := lc.Listen(ctx, "tcp", a.cfg.MetricsAddr)
	if err != nil {
		_ = ln.Close()
		a.closePools()
		return fmt.Errorf("listen %s: %w", a.cfg.MetricsAddr, err)
	}
	return a.Serve(ctx, ln, iln)
}

// Serve is Run with listeners supplied by the caller (tests).
func (a *App) Serve(ctx context.Context, ln, internalLn net.Listener) error {
	a.log.Info("posd starting",
		slog.String("version", buildinfo.Version), slog.String("mode", a.cfg.Mode),
		slog.String("env", a.cfg.AppEnv), slog.String("addr", ln.Addr().String()),
		slog.String("metrics_addr", internalLn.Addr().String()))

	lctx, stopListener := context.WithCancel(context.WithoutCancel(ctx))
	var lwg sync.WaitGroup
	lwg.Go(func() { a.listener.Run(lctx) })

	errc := make(chan error, 2)
	go func() { errc <- serve(a.http, ln) }()
	go func() { errc <- serve(a.internal, internalLn) }()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errc:
		a.log.Error("server stopped unexpectedly", slog.Any("err", serveErr))
	}
	shutdownErr := a.shutdown(func() { stopListener(); lwg.Wait() })
	return errors.Join(serveErr, shutdownErr)
}

func serve(s *http.Server, ln net.Listener) error {
	if err := s.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (a *App) shutdown(stopListener func()) error {
	a.log.Info("shutdown: draining", slog.Duration("delay", a.cfg.ShutdownDrainDelay))
	a.health.SetDraining()
	time.Sleep(a.cfg.ShutdownDrainDelay)

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()
	var errs []error
	if err := a.http.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	// River (step 3+) stops here.
	stopListener()
	if err := a.internal.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("metrics shutdown: %w", err))
	}
	a.closePools()
	if len(errs) > 0 {
		a.log.Error("shutdown incomplete", slog.Any("err", errors.Join(errs...)))
		return errors.Join(errs...)
	}
	a.log.Info("shutdown complete")
	return nil
}

func (a *App) closePools() {
	if a.pool != nil {
		a.pool.Close()
	}
	if a.jobsPool != nil {
		a.jobsPool.Close()
	}
}
