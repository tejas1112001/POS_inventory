package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Queues.
const (
	QueueMail        = "mail"
	QueueReceipts    = "receipts"
	QueueMaintenance = "maintenance"
)

// activeStates makes a job unique while it waits or runs, but not after it
// completed (River's default would block re-inserts for the retention
// period). River requires available, pending, running and scheduled.
var activeStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
	rivertype.JobStateRetryable, rivertype.JobStateScheduled,
}

func unique() river.UniqueOpts { return river.UniqueOpts{ByArgs: true, ByState: activeStates} }

// ------------------------------------------------------------------- args

// MailTenantArgs delivers one tenant's outbox.
type MailTenantArgs struct {
	TenantID string `json:"tenant_id"`
}

// Kind implements river.JobArgs.
func (MailTenantArgs) Kind() string { return "mail_tenant" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (MailTenantArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMail, UniqueOpts: unique(), MaxAttempts: 5}
}

// MailPlatformArgs delivers the platform outbox.
type MailPlatformArgs struct{}

// Kind implements river.JobArgs.
func (MailPlatformArgs) Kind() string { return "mail_platform" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (MailPlatformArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMail, UniqueOpts: unique(), MaxAttempts: 5}
}

// RenderReceiptArgs pre-renders a receipt after a sale change.
type RenderReceiptArgs struct {
	TenantID  string `json:"tenant_id"`
	ReceiptID int64  `json:"receipt_id"`
}

// Kind implements river.JobArgs.
func (RenderReceiptArgs) Kind() string { return "receipt_render" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (RenderReceiptArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueReceipts, UniqueOpts: unique(), MaxAttempts: 3}
}

// maintenanceArgs is one periodic job.
type maintenanceArgs struct {
	Job string `json:"job"`
}

// Kind implements river.JobArgs.
func (maintenanceArgs) Kind() string { return "maintenance" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (maintenanceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMaintenance, UniqueOpts: unique(), MaxAttempts: 3}
}

// Periodic job names.
const (
	JobMailSweep          = "mail_sweep"
	JobPOSLockSweep       = "pos_lock_sweep"
	JobIdempotencyCleanup = "idempotency_cleanup"
	JobLedgerReconcile    = "ledger_reconcile"
	JobBillingLifecycle   = "billing_lifecycle"
	JobBillingReminders   = "billing_reminders"
)

// ---------------------------------------------------------------- workers

type mailTenantWorker struct {
	river.WorkerDefaults[MailTenantArgs]
	w *Worker
}

func (k *mailTenantWorker) Work(ctx context.Context, job *river.Job[MailTenantArgs]) error {
	return k.w.observe("mail_tenant", k.w.mail.DeliverTenant(ctx, job.Args.TenantID))
}

func (k *mailTenantWorker) Timeout(*river.Job[MailTenantArgs]) time.Duration { return 15 * time.Minute }

type mailPlatformWorker struct {
	river.WorkerDefaults[MailPlatformArgs]
	w *Worker
}

func (k *mailPlatformWorker) Work(ctx context.Context, _ *river.Job[MailPlatformArgs]) error {
	return k.w.observe("mail_platform", k.w.mail.DeliverPlatform(ctx))
}

func (k *mailPlatformWorker) Timeout(*river.Job[MailPlatformArgs]) time.Duration {
	return 15 * time.Minute
}

type renderWorker struct {
	river.WorkerDefaults[RenderReceiptArgs]
	w *Worker
}

func (k *renderWorker) Work(ctx context.Context, job *river.Job[RenderReceiptArgs]) error {
	t, err := k.w.cfg.Registry.ByID(ctx, job.Args.TenantID)
	if err != nil {
		return nil // tenant gone or not active: nothing to render
	}
	_, err = k.w.cfg.Receipts.Render(ctx, t, job.Args.ReceiptID)
	if errors.Is(err, receipts.ErrSuperseded) || errors.Is(err, receipts.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return k.w.observe("receipt_render", err)
}

type maintenanceWorker struct {
	river.WorkerDefaults[maintenanceArgs]
	w *Worker
}

func (k *maintenanceWorker) Work(ctx context.Context, job *river.Job[maintenanceArgs]) error {
	m := k.w.maint
	var err error
	switch job.Args.Job {
	case JobMailSweep:
		err = m.SweepMail(ctx)
	case JobPOSLockSweep:
		var n int64
		if n, err = m.SweepPOSLocks(ctx); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "expired POS locks removed", slog.Int64("rows", n))
		}
	case JobIdempotencyCleanup:
		var n int64
		if n, err = m.CleanupIdempotencyKeys(ctx); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "old idempotency keys removed", slog.Int64("rows", n))
		}
	case JobLedgerReconcile:
		_, err = m.ReconcileLedgers(ctx)
	case JobBillingLifecycle, JobBillingReminders:
		if k.w.cfg.Billing == nil || k.w.cfg.Billing.Provider == nil {
			return nil
		}
		var n int
		if job.Args.Job == JobBillingLifecycle {
			n, err = k.w.cfg.Billing.RunLifecycle(ctx)
		} else {
			n, err = k.w.cfg.Billing.RunReminders(ctx)
		}
		if n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "billing job", slog.String("job", job.Args.Job), slog.Int("rows", n))
		}
	default:
		return nil
	}
	return k.w.observe(job.Args.Job, err)
}

func (k *maintenanceWorker) Timeout(*river.Job[maintenanceArgs]) time.Duration {
	return 30 * time.Minute
}

// ---------------------------------------------------------------- client

// dailyAt fires once a day at a fixed UTC time.
type dailyAt struct{ hour, minute int }

// Next implements river.PeriodicSchedule.
func (d dailyAt) Next(now time.Time) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), d.hour, d.minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// Config wires the worker.
type Config struct {
	Pool     *pgxpool.Pool // pos_jobs pool
	Registry *tenancy.Registry
	Receipts *receipts.Service
	Mail     MailConfig
	Log      *slog.Logger
	// RunJobs is false for POSD_MODE=api: the client only inserts jobs.
	RunJobs bool
	// SweepInterval is the mail safety-net sweep (default 1 minute).
	SweepInterval time.Duration
	// Billing runs the lifecycle (hourly) and reminder (daily) jobs; nil
	// or without a provider when billing is off.
	Billing *billing.Service
}

// Worker owns the River client and the job implementations.
type Worker struct {
	cfg     Config
	client  *river.Client[pgx.Tx]
	mail    *Mailer
	maint   *Maintenance
	started atomic.Bool

	mu       sync.Mutex // guards stopping against started
	stopping bool
}

// New builds the River client (not started).
func New(cfg Config) (*Worker, error) {
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = time.Minute
	}
	w := &Worker{cfg: cfg, mail: NewMailer(cfg.Mail)}
	w.maint = &Maintenance{Jobs: cfg.Mail.Jobs, Metrics: cfg.Mail.Metrics, Log: cfg.Log, EnqueueMail: w.EnqueueMail}

	workers := river.NewWorkers()
	river.AddWorker(workers, &mailTenantWorker{w: w})
	river.AddWorker(workers, &mailPlatformWorker{w: w})
	river.AddWorker(workers, &renderWorker{w: w})
	river.AddWorker(workers, &maintenanceWorker{w: w})

	rc := &river.Config{
		Schema:  "river",
		Logger:  slog.New(minLevel{h: cfg.Log.Handler(), min: slog.LevelWarn}).With(slog.String("component", "river")),
		Workers: workers,
	}
	if cfg.RunJobs {
		rc.Queues = map[string]river.QueueConfig{
			QueueMail:          {MaxWorkers: 10}, // tenants in parallel; each job handles one tenant
			QueueReceipts:      {MaxWorkers: 4},
			QueueMaintenance:   {MaxWorkers: 2},
			river.QueueDefault: {MaxWorkers: 1},
		}
		periodic := func(job string, s river.PeriodicSchedule, onStart bool) *river.PeriodicJob {
			return river.NewPeriodicJob(s, func() (river.JobArgs, *river.InsertOpts) {
				return maintenanceArgs{Job: job}, nil
			}, &river.PeriodicJobOpts{ID: job, RunOnStart: onStart})
		}
		rc.PeriodicJobs = []*river.PeriodicJob{
			periodic(JobMailSweep, river.PeriodicInterval(cfg.SweepInterval), true),
			periodic(JobPOSLockSweep, river.PeriodicInterval(5*time.Minute), false),
			periodic(JobIdempotencyCleanup, river.PeriodicInterval(time.Hour), true),
			periodic(JobLedgerReconcile, dailyAt{hour: 21, minute: 0}, false), // 02:30 in India
			periodic(JobBillingLifecycle, river.PeriodicInterval(time.Hour), true),
			periodic(JobBillingReminders, dailyAt{hour: 3, minute: 30}, false), // BILLING_NOTIFY_HOUR: 09:00 in India
		}
	}
	client, err := river.NewClient(riverpgxv5.New(cfg.Pool), rc)
	if err != nil {
		return nil, err
	}
	w.client = client
	return w, nil
}

func (w *Worker) observe(kind string, err error) error {
	if err != nil {
		w.cfg.Mail.Metrics.JobsFailed.WithLabelValues(kind).Inc()
	}
	return err
}

// Run starts the job loops (POSD_MODE all or worker) and retries every 5 s
// if starting fails, so the API never waits on the job runner. ctx must
// outlive the client (cancel it only after Stop). It returns once started
// or when ctx ends.
func (w *Worker) Run(ctx context.Context) {
	if !w.cfg.RunJobs {
		return
	}
	for {
		w.mu.Lock()
		stopping := w.stopping
		w.mu.Unlock()
		if stopping {
			return
		}
		// ctx lives until shutdown: River stops hard when it is cancelled.
		err := w.client.Start(ctx)
		if err == nil {
			w.mu.Lock()
			w.started.Store(true)
			stopping = w.stopping
			w.mu.Unlock()
			if stopping { // Stop ran while we were starting
				sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
				_ = w.client.Stop(sctx)
				cancel()
			}
			return
		}
		w.cfg.Log.Warn("job runner not started, retrying", slog.Any("err", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// Started reports whether the job loops run (readiness).
func (w *Worker) Started() bool { return w.started.Load() }

// Stop lets running jobs finish until ctx ends, then cancels the rest; they
// are retried later because every job is idempotent (OPERATIONS §5).
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	w.stopping = true
	started := w.started.Load()
	w.mu.Unlock()
	if !started {
		return nil
	}
	soft, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := w.client.Stop(soft); err == nil {
		return nil
	}
	return w.client.StopAndCancel(ctx)
}

// EnqueueMail queues delivery jobs; duplicates collapse (unique jobs).
func (w *Worker) EnqueueMail(ctx context.Context, tenantIDs []string, platform bool) error {
	params := make([]river.InsertManyParams, 0, len(tenantIDs)+1)
	for _, id := range tenantIDs {
		if tenancy.ValidTenantID(id) {
			params = append(params, river.InsertManyParams{Args: MailTenantArgs{TenantID: id}})
		}
	}
	if platform {
		params = append(params, river.InsertManyParams{Args: MailPlatformArgs{}})
	}
	if len(params) == 0 {
		return nil
	}
	_, err := w.client.InsertMany(ctx, params)
	return err
}

// MailDispatched handles a mail_dispatch notification (payload: tenant ID).
func (w *Worker) MailDispatched(payload string) {
	if !tenancy.ValidTenantID(payload) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.EnqueueMail(ctx, []string{payload}, false); err != nil {
		w.cfg.Log.Warn("mail job not queued; the sweep will pick it up", slog.String("tenant_id", payload), slog.Any("err", err))
	}
}

// ReceiptChanged queues a receipt pre-render after a sale commits.
func (w *Worker) ReceiptChanged(ctx context.Context, t tenancy.Tenant, receiptID int64) {
	if _, err := w.client.Insert(ctx, RenderReceiptArgs{TenantID: t.ID, ReceiptID: receiptID}, nil); err != nil {
		w.cfg.Log.WarnContext(ctx, "receipt render not queued; it renders on demand", slog.String("tenant_id", t.ID), slog.Any("err", err))
	}
}

// Maintenance exposes the periodic jobs (tests, posctl).
func (w *Worker) Maintenance() *Maintenance { return w.maint }

// Mailer exposes the mail deliverer (tests).
func (w *Worker) Mailer() *Mailer { return w.mail }
