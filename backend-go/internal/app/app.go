// Package app wires the components together and runs the process lifecycle.
package app

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/go-chi/chi/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/adjustments"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/authtoken"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/catalog"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/products"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/purchases"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/reports"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/sales"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/shop"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantapi"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/users"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/worker"
)

// Server hardening (OPERATIONS §5).
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 32 << 10
)

// Option adjusts how the app is built (tests).
type Option func(*options)

type options struct {
	mailGuard        *mailer.Guard
	mailRoots        *x509.CertPool
	sweepInterval    time.Duration
	tenantLoginLimit int
}

// WithSweepInterval shortens the mail safety-net sweep (tests).
func WithSweepInterval(d time.Duration) Option {
	return func(o *options) { o.sweepInterval = d }
}

// WithTenantLoginLimit raises the per-tenant login rate limit (default
// 100 a minute) so a test suite that signs in very often is not limited.
// Never used by posd.
func WithTenantLoginLimit(n int) Option {
	return func(o *options) { o.tenantLoginLimit = n }
}

// WithMailTesting replaces the tenant SMTP guard and the trusted CA roots,
// so tests can reach an in-process SMTP server. Never used by posd.
func WithMailTesting(g mailer.Guard, roots *x509.CertPool) Option {
	return func(o *options) { o.mailGuard, o.mailRoots = &g, roots }
}

// App is the running process.
type App struct {
	opts     options
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
	tenant   *tenantapi.API
	catalog  *catalog.API
	worker   *worker.Worker
	billing  *billing.Service
}

// New builds the app. It does not open network listeners.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, opts ...Option) (*App, error) {
	a := &App{cfg: cfg, log: log, metrics: obs.NewMetrics(), health: httpx.NewHealth(time.Second)}
	for _, o := range opts {
		o(&a.opts)
	}

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

	env, err := envelope(cfg)
	if err != nil {
		a.closePools()
		return nil, err
	}
	storage, err := newStorage(ctx, cfg)
	if err != nil {
		a.closePools()
		return nil, err
	}
	a.billing = &billing.Service{Sessions: a.sessions, Provider: billingProvider(cfg, log),
		Cfg:    billing.Config{GraceDays: cfg.BillingGraceDays, TrialChargeWait: billing.DefaultConfig.TrialChargeWait},
		MaxAge: cfg.WebhookMaxAge, Metrics: a.metrics, Log: log}
	tenantAPI, authSvc, err := a.tenantAPI(cfg, log, network, env, storage)
	if err != nil {
		a.closePools()
		return nil, err
	}
	if a.worker, err = a.newWorker(cfg, log, env, storage, authSvc); err != nil {
		a.closePools()
		return nil, err
	}
	a.tenant = tenantAPI
	platformHTTP, err := a.platformAPI(cfg, log, network, env, authSvc)
	if err != nil {
		a.closePools()
		return nil, err
	}
	var mountErr error
	handler := httpx.NewRouter(httpx.RouterDeps{
		Logger:         log,
		Metrics:        a.metrics,
		Network:        network,
		Health:         a.health,
		HSTS:           cfg.HSTSEnabled,
		RequestTimeout: cfg.RequestTimeout,
		CORSDevOrigins: cfg.CORSDevOrigins,
		TenantResolver: tenancy.Resolve(a.registry, network, cfg.BaseDomain),
		Platform:       func(r chi.Router) { platformHTTP.Mount(r) },
		TenantRoot: func(r chi.Router) {
			r.Get("/files/t/{id}", a.catalog.PublicFile)
			r.Head("/files/t/{id}", a.catalog.PublicFile)
		},
		Tenant: func(r chi.Router) {
			if err := tenantAPI.Mount(r); err != nil {
				mountErr = err
			}
		},
		TenantSPA:  spas["tenant"],
		AdminSPA:   spas["admin"],
		LandingSPA: spas["landing"],
	})

	if mountErr != nil {
		a.closePools()
		return nil, mountErr // a tenant route without an RBAC requirement
	}

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

func envelope(cfg *config.Config) (*secrets.Envelope, error) {
	keks := make([]secrets.KEK, 0, len(cfg.MasterKeys))
	for _, k := range cfg.MasterKeys {
		keks = append(keks, secrets.KEK{ID: k.ID, Key: k.Bytes()})
	}
	return secrets.NewEnvelope(keks)
}

func newStorage(ctx context.Context, cfg *config.Config) (files.Storage, error) {
	if cfg.StorageDriver == "s3" {
		return files.NewS3Storage(ctx, files.S3Config{
			Endpoint: cfg.S3Endpoint, Bucket: cfg.S3Bucket, AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Region: cfg.S3Region,
		})
	}
	return files.NewFSStorage(cfg.StorageFSRoot)
}

func (a *App) tenantAPI(cfg *config.Config, log *slog.Logger, network httpx.Network, env *secrets.Envelope, storage files.Storage) (*tenantapi.API, *auth.Service, error) {
	signer, err := authtoken.NewSigner(cfg.JWTKeys, authtoken.AudTenant, cfg.AccessTokenTTL)
	if err != nil {
		return nil, nil, err
	}
	states := auth.NewStates(a.sessions, a.listener.Healthy, log)
	a.listener.On(tenancy.ChanAuthChanged, states.InvalidateUser)
	a.listener.On(tenancy.ChanRoleChanged, states.InvalidateRole)
	a.listener.OnReconnect(states.Flush)
	hasher := security.NewHasher(security.TenantArgon2, max(4, runtime.NumCPU()))
	authSvc := auth.NewService(auth.Config{
		Sessions: a.sessions, States: states, Signer: signer, Hasher: hasher, Envelope: env,
		RefreshRemember: cfg.RefreshTTLRemember, RefreshSession: cfg.RefreshTTLSession,
		PublicScheme: cfg.PublicScheme, BaseDomain: cfg.BaseDomain, Log: log,
	})
	planSvc := plans.NewService(a.sessions)
	fileSvc := files.NewService(storage, a.sessions, planSvc, log)
	productAPI := products.New(products.Deps{Sessions: a.sessions, Files: fileSvc, Plans: planSvc, MaxRows: cfg.MaxUnpagedRows})
	purchaseAPI := purchases.New(purchases.Deps{Sessions: a.sessions, MaxRows: cfg.MaxUnpagedRows})
	adjustmentAPI := adjustments.New(adjustments.Deps{Sessions: a.sessions, MaxRows: cfg.MaxUnpagedRows})
	reportAPI := reports.New(reports.Deps{Sessions: a.sessions, Plans: planSvc, MaxRows: cfg.MaxUnpagedRows})
	shopAPI := shop.New(shop.Deps{Sessions: a.sessions, Plans: planSvc, Network: network, Metrics: a.metrics})
	salesAPI := sales.New(sales.Deps{Sessions: a.sessions, MaxRows: cfg.MaxUnpagedRows,
		OnReceipt: func(ctx context.Context, t tenancy.Tenant, receiptID int64) {
			if a.worker != nil {
				a.worker.ReceiptChanged(ctx, t, receiptID)
			}
		}})
	receiptSvc := receipts.NewService(a.sessions, storage, log)
	receiptSvc.Rendered = func(d time.Duration) { a.metrics.ReceiptRender.Observe(d.Seconds()) }
	receiptAPI := receipts.NewAPI(receiptSvc, a.sessions)
	settingsAPI := settings.New(settings.Deps{Sessions: a.sessions, Files: fileSvc, Plans: planSvc, Envelope: env,
		Guard: a.tenantMailGuard(), RootCAs: a.opts.mailRoots, MailDomain: cfg.MailDomain()})
	a.catalog = catalog.New(catalog.Deps{Sessions: a.sessions, Files: fileSvc, Plans: planSvc, MaxRows: cfg.MaxUnpagedRows,
		ShowcaseProducts: products.ShowcaseProducts(cfg.MaxUnpagedRows)})
	api := tenantapi.New(tenantapi.Deps{
		Auth: authSvc, Users: users.NewService(a.sessions, authSvc, hasher, planSvc, cfg.MaxUnpagedRows),
		States: states, Signer: signer, Sessions: a.sessions, Network: network, Metrics: a.metrics, MaxRows: cfg.MaxUnpagedRows,
		LoginPerTenant: a.opts.tenantLoginLimit,
		Modules:        []interface{ Mount(*rbac.Router) }{a.catalog, productAPI, salesAPI, purchaseAPI, adjustmentAPI, reportAPI, shopAPI, settingsAPI, receiptAPI},
	})
	return api, authSvc, nil
}

// newWorker builds the job runner. It uses the pos_jobs pool for its own
// sessions; in POSD_MODE=api it only inserts jobs.
func (a *App) newWorker(cfg *config.Config, log *slog.Logger, env *secrets.Envelope, storage files.Storage, authSvc *auth.Service) (*worker.Worker, error) {
	jobs := tenancy.NewSessions(a.jobsPool, cfg.DBStatementTimeout, a.metrics.SessionTampered, log)
	rcpt := receipts.NewService(jobs, storage, log)
	rcpt.Rendered = func(d time.Duration) { a.metrics.ReceiptRender.Observe(d.Seconds()) }
	var platformSender mailer.Sender
	if cfg.SMTPHost != "" {
		// Operator-configured relay: private addresses are allowed (Mailpit,
		// an internal relay), TLS as configured.
		platformSender = mailer.NewSMTP(mailer.Server{Host: cfg.SMTPHost, Port: cfg.SMTPPort, TLS: mailer.TLSMode(cfg.SMTPTLS),
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword},
			mailer.Options{Guard: mailer.Guard{AllowPrivate: true}, HELO: cfg.MailDomain()})
	} else {
		log.Warn("SMTP_HOST is not set: platform mail stays queued")
	}
	guard, roots := a.tenantMailGuard(), a.opts.mailRoots
	w, err := worker.New(worker.Config{
		Pool: a.jobsPool, Registry: a.registry, Receipts: rcpt, Log: log,
		RunJobs: cfg.Mode != "api", SweepInterval: a.opts.sweepInterval,
		Mail: worker.MailConfig{
			Jobs: jobs, Registry: a.registry, Envelope: env, Receipts: rcpt, Plans: plans.NewService(jobs),
			Platform: platformSender,
			TenantSender: func(s mailer.Server) mailer.Sender {
				return mailer.NewSMTP(s, mailer.Options{Guard: guard, RootCAs: roots, HELO: cfg.MailDomain()})
			},
			OwnerInvite: func(ctx context.Context, t tenancy.Tenant) (string, time.Time, error) {
				return authSvc.OwnerInviteLink(ctx, t, auth.Meta{RequestID: "worker"})
			},
			PublicScheme: cfg.PublicScheme, BaseDomain: cfg.BaseDomain, PlatformName: cfg.PlatformName,
			MailFrom: cfg.MailFromAddress, MailDomain: cfg.MailDomain(), Metrics: a.metrics, Log: log,
		},
	})
	if err != nil {
		return nil, err
	}
	a.listener.On(tenancy.ChanMailDispatch, w.MailDispatched)
	if cfg.Mode != "api" {
		a.health.Add("jobs", func(context.Context) error {
			if w.Started() {
				return nil
			}
			return errors.New("job runner not started")
		})
	}
	return w, nil
}

// Worker exposes the job runner (tests).
func (a *App) Worker() *worker.Worker { return a.worker }

// tenantMailGuard is the SSRF guard for tenant SMTP servers.
func (a *App) tenantMailGuard() mailer.Guard {
	if a.opts.mailGuard != nil {
		return *a.opts.mailGuard
	}
	return mailer.Guard{Deny: a.cfg.SMTPDenyCIDRs}
}

func (a *App) platformAPI(cfg *config.Config, log *slog.Logger, network httpx.Network, env *secrets.Envelope, authSvc *auth.Service) (*platform.HTTP, error) {
	signer, err := authtoken.NewSigner(cfg.PlatformJWTKeys, authtoken.AudPlatform, cfg.PlatformAccessTokenTTL)
	if err != nil {
		return nil, err
	}
	var prov *provisioning.Orchestrator
	if !cfg.DatabaseMigratorURL.IsZero() {
		prov = provisioning.New(cfg.DatabaseMigratorURL, cfg.PublicScheme, cfg.BaseDomain, log)
	}
	svc := platform.NewService(platform.Config{
		BaseDomain: cfg.BaseDomain, PlatformName: cfg.PlatformName,
		SessionMax: cfg.PlatformSessionMax, IdleTimeout: cfg.PlatformIdleTimeout, SecureCookies: true,
		Provisioner: prov, Registry: a.registry, Log: log,
		Hasher:   security.NewHasher(security.PlatformArgon2, 4),
		Envelope: env, Signer: signer, Sessions: a.sessions,
		IssueOwnerInvite: func(ctx context.Context, id string, m platform.Meta) (string, time.Time, error) {
			t, err := a.registry.ByID(ctx, id)
			if err != nil {
				return "", time.Time{}, err
			}
			return authSvc.OwnerInviteLink(ctx, t, auth.Meta{IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
		},
	})
	h := platform.NewHTTP(svc, network, cfg.PlatformIPAllowlist, a.metrics, cfg.PublicScheme)
	if a.billing.Provider != nil {
		h.SetWebhook(a.billing.WebhookHandler())
	}
	return h, nil
}

// billingProvider builds the configured provider; nil when billing is off.
// The fake signs its webhooks with RAZORPAY_WEBHOOK_SECRET, or with a
// random secret when none is set (then no outside webhook verifies).
func billingProvider(cfg *config.Config, log *slog.Logger) billing.Provider {
	switch cfg.BillingProvider {
	case config.BillingRazorpay:
		return billing.NewRazorpay(billing.RazorpayConfig{KeyID: cfg.RazorpayKeyID, KeySecret: cfg.RazorpayKeySecret.Reveal(),
			WebhookSecret: cfg.RazorpayWebhookSecret.Reveal()})
	case config.BillingFake:
		secret := []byte(cfg.RazorpayWebhookSecret.Reveal())
		if len(secret) == 0 {
			secret = []byte(security.NewToken(32))
			log.Warn("fake billing provider with a random webhook secret; set RAZORPAY_WEBHOOK_SECRET to send test webhooks")
		}
		return billing.NewFake(secret, "")
	}
	return nil
}

// Billing exposes the billing service (tests).
func (a *App) Billing() *billing.Service { return a.billing }

// Handler exposes the public HTTP handler (tests).
func (a *App) Handler() http.Handler { return a.http.Handler }

// Health exposes the health state (tests).
func (a *App) Health() *httpx.Health { return a.health }

// TenantRoutes lists every declared tenant route and its requirement
// (isolation tests walk them all).
func (a *App) TenantRoutes() map[string]rbac.Requirement { return a.tenant.Router().Declarations() }

// ListenerHealthy reports whether the LISTEN connection is up (tests).
func (a *App) ListenerHealthy() bool { return a.listener.Healthy() }

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
	wctx, stopWorkerStart := context.WithCancel(context.WithoutCancel(ctx))
	lwg.Go(func() { a.worker.Run(wctx) })

	errc := make(chan error, 2)
	go func() { errc <- serve(a.http, ln) }()
	go func() { errc <- serve(a.internal, internalLn) }()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errc:
		a.log.Error("server stopped unexpectedly", slog.Any("err", serveErr))
	}
	shutdownErr := a.shutdown(func() { stopWorkerStart(); stopListener(); lwg.Wait() })
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
	if err := a.worker.Stop(ctx); err != nil {
		errs = append(errs, fmt.Errorf("job runner shutdown: %w", err))
	}
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
