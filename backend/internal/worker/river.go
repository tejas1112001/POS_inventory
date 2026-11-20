package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/tejas1112001/POS_inventory/backend/internal/billing"
	"github.com/tejas1112001/POS_inventory/backend/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenantio"
)

// Queues.
const (
	QueueMail        = "mail"
	QueueReceipts    = "receipts"
	QueueMaintenance = "maintenance"
	QueueExports     = "exports"
	// QueueTenantMaintenance runs per-shop maintenance jobs.
	QueueTenantMaintenance = "tenant_maintenance"
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

// ExportArgs runs one queued tenant export (OPERATIONS §6.2).
type ExportArgs struct {
	ExportID uuid.UUID `json:"export_id"`
}

// Kind implements river.JobArgs.
func (ExportArgs) Kind() string { return "tenant_export" }

// InsertOpts implements river.JobArgsWithInsertOpts. One attempt: a
// failed export is recorded as failed and is requested again by a person
// (or, for a final export, by the sweep).
func (ExportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueExports, UniqueOpts: unique(), MaxAttempts: 1}
}

// ReconcileTenantArgs checks one shop's stock against its ledgers
// (ARCHITECTURE_REVIEW M2). The nightly ledger_reconcile job queues one per
// live shop, so shops run in parallel, each with its own time limit and
// retries, and the work no longer has to fit one job's 30 minutes.
type ReconcileTenantArgs struct {
	TenantID string `json:"tenant_id"`
}

// Kind implements river.JobArgs.
func (ReconcileTenantArgs) Kind() string { return "ledger_reconcile_tenant" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ReconcileTenantArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueTenantMaintenance, UniqueOpts: unique(), MaxAttempts: 3}
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
	JobDeletionClock      = "deletion_clock"
	JobExportSweep        = "export_sweep"
	JobUsageSnapshot      = "usage_snapshot"
	JobBillingReconcile   = "billing_reconcile"
	JobNightlyExports     = "nightly_exports"
	JobLowStockEmail      = "low_stock_email"
	JobExpiryDigest       = "expiry_digest"
	JobTenantPurge        = "tenant_purge"
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

type exportWorker struct {
	river.WorkerDefaults[ExportArgs]
	w *Worker
}

func (k *exportWorker) Work(ctx context.Context, job *river.Job[ExportArgs]) error {
	e := k.w.cfg.Exporter
	if !e.Enabled() {
		return nil // stays queued until exports are configured
	}
	rec, err := e.Run(ctx, job.Args.ExportID)
	if errors.Is(err, tenantio.ErrNotQueued) {
		return nil // already run (a duplicate job or the sweep)
	}
	if err == nil {
		k.w.cfg.Log.InfoContext(ctx, "tenant export succeeded", slog.String("tenant_id", rec.TenantID),
			slog.String("export_id", rec.ID.String()), slog.Int64("bytes", rec.Summary.Size), slog.Int64("rows", rec.Rows))
	}
	// A failure is already recorded on the export (and logged); a retry
	// would find it not queued.
	_ = k.w.observe("tenant_export", err)
	return nil
}

// Timeout is below the export's stale limit (6 h).
func (k *exportWorker) Timeout(*river.Job[ExportArgs]) time.Duration { return 3 * time.Hour }

type reconcileTenantWorker struct {
	river.WorkerDefaults[ReconcileTenantArgs]
	w *Worker
}

func (k *reconcileTenantWorker) Work(ctx context.Context, job *river.Job[ReconcileTenantArgs]) error {
	t, err := k.w.cfg.Registry.ByID(ctx, job.Args.TenantID)
	if err != nil {
		return nil // tenant gone or not active: nothing to check
	}
	_, err = k.w.maint.ReconcileTenant(ctx, t)
	return k.w.observe("ledger_reconcile_tenant", err)
}

// Timeout is a little above the reconcile's own limit.
func (k *reconcileTenantWorker) Timeout(*river.Job[ReconcileTenantArgs]) time.Duration {
	return ReconcileTimeout + time.Minute
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
		var n int
		if n, err = k.w.QueueReconciles(ctx); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "stock reconcile queued", slog.Int("tenants", n))
		}
	case JobUsageSnapshot:
		_, err = m.SnapshotUsage(ctx)
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
	case JobBillingReconcile:
		if k.w.cfg.Billing == nil || k.w.cfg.Billing.Provider == nil {
			return nil
		}
		var r billing.ReconcileResult
		if r, err = k.w.cfg.Billing.RunReconcile(ctx); err == nil && (r.Drift > 0 || r.FetchErrors > 0 || r.WebhooksQuiet) {
			k.w.cfg.Log.WarnContext(ctx, "billing reconcile", slog.Int("checked", r.Checked), slog.Int("drift", r.Drift),
				slog.Int("resolved", r.Resolved), slog.Int("fetch_errors", r.FetchErrors), slog.Bool("webhooks_quiet", r.WebhooksQuiet))
		}
	case JobDeletionClock:
		// Runs also with billing off: restricted_at may predate that.
		if k.w.cfg.Billing == nil {
			return nil
		}
		var n int
		if n, err = k.w.cfg.Billing.RunDeletionClock(ctx); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "tenants moved to pending_deletion", slog.Int("tenants", n))
		}
		if err == nil {
			if n, err = k.w.cfg.Billing.RunDeletionWarnings(ctx); n > 0 {
				k.w.cfg.Log.InfoContext(ctx, "deletion warnings sent", slog.Int("tenants", n))
			}
		}
		if err == nil {
			_, err = m.EndDeletedSessions(ctx)
		}
	case JobTenantPurge:
		var n int
		if n, err = k.w.cfg.Purger.Run(ctx); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "deleted tenants purged", slog.Int("tenants", n))
		}
	case JobExportSweep:
		err = k.w.SweepExports(ctx)
	case JobNightlyExports:
		err = k.w.NightlyExports(ctx)
	case JobLowStockEmail:
		var n int
		if n, err = m.LowStockEmails(ctx, time.Now()); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "low-stock emails queued", slog.Int("emails", n))
			_ = k.w.EnqueueMail(ctx, nil, false)
		}
	case JobExpiryDigest:
		var n int
		if n, err = m.ExpiryDigests(ctx, time.Now()); n > 0 {
			k.w.cfg.Log.InfoContext(ctx, "expiry emails queued", slog.Int("emails", n))
			_ = k.w.EnqueueMail(ctx, nil, false)
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
	// ExportSweepInterval is the export sweep (default 10 minutes).
	ExportSweepInterval time.Duration
	// Billing runs the lifecycle (hourly) and reminder (daily) jobs; nil
	// or without a provider when billing is off.
	Billing *billing.Service
	// Exporter runs tenant exports; nil or not enabled when exports are
	// not configured.
	Exporter *tenantio.Exporter
	// Purger drops deleted shops after the retention window
	// (ARCHITECTURE_REVIEW M4); nil or not enabled when off.
	Purger *tenantio.Purger
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
	if cfg.ExportSweepInterval <= 0 {
		cfg.ExportSweepInterval = 10 * time.Minute
	}
	w := &Worker{cfg: cfg, mail: NewMailer(cfg.Mail)}
	w.maint = &Maintenance{Jobs: cfg.Mail.Jobs, Metrics: cfg.Mail.Metrics, Log: cfg.Log, EnqueueMail: w.EnqueueMail}

	workers := river.NewWorkers()
	river.AddWorker(workers, &mailTenantWorker{w: w})
	river.AddWorker(workers, &mailPlatformWorker{w: w})
	river.AddWorker(workers, &renderWorker{w: w})
	river.AddWorker(workers, &maintenanceWorker{w: w})
	river.AddWorker(workers, &exportWorker{w: w})
	river.AddWorker(workers, &reconcileTenantWorker{w: w})

	rc := &river.Config{
		Schema:  "river",
		Logger:  slog.New(minLevel{h: cfg.Log.Handler(), min: slog.LevelWarn}).With(slog.String("component", "river")),
		Workers: workers,
	}
	if cfg.RunJobs {
		rc.Queues = map[string]river.QueueConfig{
			QueueMail:              {MaxWorkers: 10}, // tenants in parallel; each job handles one tenant
			QueueReceipts:          {MaxWorkers: 2},  // PDF rendering is CPU-heavy: leave the API threads free (Phase 3 load test)
			QueueMaintenance:       {MaxWorkers: 2},
			QueueExports:           {MaxWorkers: 1}, // one export at a time per node: they are long, streamed reads
			QueueTenantMaintenance: {MaxWorkers: 2}, // per-shop reconciles; the jobs pool has 10 connections
			river.QueueDefault:     {MaxWorkers: 1},
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
			periodic(JobUsageSnapshot, river.PeriodicInterval(15*time.Minute), true),
			periodic(JobLedgerReconcile, dailyAt{hour: 21, minute: 0}, false), // 02:30 in India
			periodic(JobBillingLifecycle, river.PeriodicInterval(time.Hour), true),
			periodic(JobBillingReconcile, river.PeriodicInterval(time.Hour), false),
			periodic(JobBillingReminders, dailyAt{hour: 3, minute: 30}, false), // BILLING_NOTIFY_HOUR: 09:00 in India
			periodic(JobDeletionClock, river.PeriodicInterval(time.Hour), false),
			periodic(JobExportSweep, river.PeriodicInterval(cfg.ExportSweepInterval), true),
			periodic(JobNightlyExports, dailyAt{hour: 19, minute: 30}, false),   // 01:00 in India
			periodic(JobLowStockEmail, river.PeriodicInterval(time.Hour), true), // from 08:00 shop time, once a day
			periodic(JobExpiryDigest, river.PeriodicInterval(time.Hour), false), // likewise, when turned on
		}
		if cfg.Purger.Enabled() {
			rc.PeriodicJobs = append(rc.PeriodicJobs, periodic(JobTenantPurge, dailyAt{hour: 22, minute: 0}, false)) // 03:30 in India
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

// EnqueueExport queues the job that runs a queued export. If this fails,
// the export sweep picks the export up.
func (w *Worker) EnqueueExport(ctx context.Context, id uuid.UUID) error {
	_, err := w.client.Insert(ctx, ExportArgs{ExportID: id}, nil)
	return err
}

// SweepExports fails stale exports, queues final exports of tenants
// pending deletion, and queues a job for every queued export (duplicates
// collapse: the jobs are unique by export).
func (w *Worker) SweepExports(ctx context.Context) error {
	e := w.cfg.Exporter
	if !e.Enabled() {
		return nil
	}
	if n, err := e.FailStale(ctx); err != nil {
		return err
	} else if n > 0 {
		w.cfg.Log.WarnContext(ctx, "stale tenant exports marked failed", slog.Int64("exports", n))
	}
	finals, err := e.QueueFinals(ctx)
	if len(finals) > 0 {
		w.cfg.Log.InfoContext(ctx, "final exports queued", slog.Int("exports", len(finals)))
	}
	if err != nil {
		return err
	}
	// A final export must pass a restore test before deletion can be
	// finalised (S10 item 4); posctl tenant restore-tests runs it.
	tests, err := e.QueueFinalTests(ctx)
	if len(tests) > 0 {
		w.cfg.Log.InfoContext(ctx, "final export restore tests queued", slog.Int("restores", len(tests)))
	}
	if err != nil {
		return err
	}
	ids, err := e.Queued(ctx, 100)
	if err != nil || len(ids) == 0 {
		return err
	}
	params := make([]river.InsertManyParams, 0, len(ids))
	for _, id := range ids {
		params = append(params, river.InsertManyParams{Args: ExportArgs{ExportID: id}})
	}
	_, err = w.client.InsertMany(ctx, params)
	return err
}

// NightlyExports queues tonight's export of every live tenant and prunes
// nightly exports past retention (S10 item 4). The export sweep runs the
// queued exports.
func (w *Worker) NightlyExports(ctx context.Context) error {
	e := w.cfg.Exporter
	if !e.Enabled() {
		return nil
	}
	queued, busy, err := e.QueueNightly(ctx)
	w.cfg.Log.InfoContext(ctx, "nightly exports queued", slog.Int("exports", len(queued)), slog.Int("busy", busy))
	if err != nil {
		return err
	}
	pruned, err := e.PruneNightly(ctx)
	if pruned > 0 {
		w.cfg.Log.InfoContext(ctx, "old nightly exports pruned", slog.Int("exports", pruned))
	}
	if err != nil {
		return err
	}
	return w.SweepExports(ctx)
}

// QueueReconciles queues one stock reconcile per live shop (duplicates of
// a job still waiting collapse) and returns how many shops it covered.
func (w *Worker) QueueReconciles(ctx context.Context) (int, error) {
	ids, err := w.maint.TenantIDs(ctx)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	params := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		params[i] = river.InsertManyParams{Args: ReconcileTenantArgs{TenantID: id}}
	}
	for start := 0; start < len(params); start += 500 {
		if _, err := w.client.InsertMany(ctx, params[start:min(start+500, len(params))]); err != nil {
			return start, err
		}
	}
	return len(ids), nil
}

// Maintenance exposes the periodic jobs (tests, posctl).
func (w *Worker) Maintenance() *Maintenance { return w.maint }

// Mailer exposes the mail deliverer (tests).
func (w *Worker) Mailer() *Mailer { return w.mail }
