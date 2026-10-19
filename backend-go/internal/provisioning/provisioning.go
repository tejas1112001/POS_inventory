// Package provisioning creates tenants: registry row, role, schema,
// migrations, grants, seed data, owner user, trial subscription and welcome
// email (TENANCY §6). Every step is idempotent, so a failed run can be
// retried; failures in steps 3–8 are compensated in reverse.
package provisioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Steps (TENANCY §6).
const (
	StepValidate   = 1
	StepRegister   = 2
	StepSchema     = 3
	StepMigrate    = 4
	StepGrants     = 5
	StepSeed       = 6
	StepOwner      = 7
	StepTrial      = 8
	StepActivate   = 9
	StepWelcomeMsg = 10
)

// Request is a provisioning request from the Super Admin console or posctl.
type Request struct {
	Slug       string
	Name       string
	OwnerEmail string
	OwnerName  string
	PlanCode   string // default "trial"
	Currency   string // default INR
	Locale     string // default en-IN
	Timezone   string // default Asia/Kolkata
}

// options are the non-secret request fields replayed on retry.
type options struct {
	OwnerName string `json:"owner_name"`
	PlanCode  string `json:"plan_code"`
	Currency  string `json:"currency"`
	Locale    string `json:"locale"`
	Timezone  string `json:"timezone"`
}

// Result describes a provisioned tenant.
type Result struct {
	TenantID    string
	Slug        string
	OwnerUserID int64
	Status      string
	TenantURL   string
}

// Orchestrator runs provisioning. All DDL runs as pos_migrator acting as
// pos_owner; tenant seed data is written through a tenant connection.
type Orchestrator struct {
	dsn          secrets.Secret
	log          *slog.Logger
	publicScheme string
	baseDomain   string
	now          func() time.Time
	failAfter    func(step int) error // tests: inject a failure after a step
}

// New creates an orchestrator. migratorDSN logs in as pos_migrator.
func New(migratorDSN secrets.Secret, publicScheme, baseDomain string, log *slog.Logger) *Orchestrator {
	if log == nil {
		log = slog.Default()
	}
	return &Orchestrator{dsn: migratorDSN, log: log, publicScheme: publicScheme, baseDomain: baseDomain, now: time.Now}
}

// SetFailAfter installs a fault-injection hook (tests only).
func (o *Orchestrator) SetFailAfter(f func(step int) error) { o.failAfter = f }

var (
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	localePattern   = regexp.MustCompile(`^en-[A-Z]{2}$`)
	planPattern     = regexp.MustCompile(`^[a-z0-9_]{2,40}$`)
)

// validate is step 1. It returns field errors, never partial state.
func (o *Orchestrator) validate(req *Request) error {
	fields := map[string]string{}
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	req.OwnerEmail = strings.TrimSpace(req.OwnerEmail)
	req.OwnerName = strings.TrimSpace(req.OwnerName)
	if req.PlanCode == "" {
		req.PlanCode = "trial"
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}
	if req.Locale == "" {
		req.Locale = "en-IN"
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Kolkata"
	}
	if req.OwnerName == "" {
		req.OwnerName = "Owner"
	}

	switch {
	case !httpx.ValidSlug(req.Slug):
		fields["slug"] = "3-30 lowercase letters, digits and single hyphens"
	case tenancy.ReservedSlugs[req.Slug]:
		fields["slug"] = "is reserved"
	}
	if l := len([]rune(req.Name)); l < 1 || l > 120 {
		fields["name"] = "1-120 characters"
	}
	if strings.ContainsAny(req.Name+req.OwnerName, "\r\n\x00") {
		fields["name"] = "must not contain control characters"
	}
	if a, err := mail.ParseAddress(req.OwnerEmail); err != nil || a.Address != req.OwnerEmail || len(req.OwnerEmail) > 254 {
		fields["ownerEmail"] = "must be a plain email address"
	}
	if len([]rune(req.OwnerName)) > 120 {
		fields["ownerName"] = "at most 120 characters"
	}
	if !planPattern.MatchString(req.PlanCode) {
		fields["plan"] = "unknown plan"
	}
	if !currencyPattern.MatchString(req.Currency) {
		fields["currency"] = "ISO 4217 code such as INR"
	}
	if !localePattern.MatchString(req.Locale) {
		fields["locale"] = "English number format such as en-IN"
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil || req.Timezone == "Local" {
		fields["timezone"] = "IANA time zone such as Asia/Kolkata"
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

// Provision creates a new tenant.
func (o *Orchestrator) Provision(ctx context.Context, req Request) (Result, error) {
	if err := o.validate(&req); err != nil {
		return Result{}, err
	}
	pconn, err := migrate.PlatformConn(o.dsn, "posctl-provision")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = pconn.Close() }()

	// Step 1 (database part): reserved list in the DB, plan exists.
	var reserved bool
	if err := pconn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM platform.reserved_slugs WHERE slug = $1)`, req.Slug).Scan(&reserved); err != nil {
		return Result{}, err
	}
	if reserved {
		return Result{}, apperr.Validation(map[string]string{"slug": "is reserved"})
	}
	var planID int64
	err = pconn.QueryRowContext(ctx, `SELECT id FROM platform.plans WHERE code = $1 AND is_active`, req.PlanCode).Scan(&planID)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, apperr.Validation(map[string]string{"plan": "unknown plan"})
	}
	if err != nil {
		return Result{}, err
	}

	// Step 2: registry row + run record, one transaction.
	id := tenancy.NewTenantID()
	t, err := migrate.TargetFor(id)
	if err != nil {
		return Result{}, err
	}
	optsJSON, _ := json.Marshal(options{req.OwnerName, req.PlanCode, req.Currency, req.Locale, req.Timezone})
	var runID int64
	err = withSQLTx(ctx, pconn, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform.tenants (id, schema_name, db_role, slug, name, status, plan_id, owner_email)
			VALUES ($1, $2, $3, $4, $5, 'provisioning', $6, $7)`,
			id, t.Schema.String(), t.Role.String(), req.Slug, req.Name, planID, req.OwnerEmail); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `INSERT INTO platform.provisioning_runs (tenant_id, step, request) VALUES ($1, 2, $2) RETURNING id`,
			id, string(optsJSON)).Scan(&runID)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Result{}, apperr.Conflict(apperr.CodeConflict, "Slug is already taken")
	}
	if err != nil {
		return Result{}, err
	}
	if err := o.inject(StepRegister); err != nil {
		o.fail(ctx, pconn, runID, t, StepRegister, err)
		return Result{TenantID: id, Status: "failed"}, err
	}
	return o.run(ctx, pconn, runID, t, req, planID)
}

// Retry resumes a failed (or stuck) tenant from step 3 with the original
// request options. Every step checks whether it is already done.
func (o *Orchestrator) Retry(ctx context.Context, tenantID string) (Result, error) {
	t, err := migrate.TargetFor(tenantID)
	if err != nil {
		return Result{}, apperr.NotFound()
	}
	pconn, err := migrate.PlatformConn(o.dsn, "posctl-provision")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = pconn.Close() }()

	var (
		req     Request
		status  string
		planID  sql.NullInt64
		optsRaw []byte
	)
	err = pconn.QueryRowContext(ctx, `SELECT t.slug::text, t.name, t.owner_email::text, t.status, t.plan_id,
			coalesce((SELECT r.request FROM platform.provisioning_runs r WHERE r.tenant_id = t.id ORDER BY r.id DESC LIMIT 1), '{}')
		FROM platform.tenants t WHERE t.id = $1`, tenantID).Scan(&req.Slug, &req.Name, &req.OwnerEmail, &status, &planID, &optsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, apperr.NotFound()
	}
	if err != nil {
		return Result{}, err
	}
	if status != "failed" && status != "provisioning" {
		return Result{}, apperr.Conflict(apperr.CodeInvalidState, "Only failed or stuck tenants can be retried")
	}
	var opts options
	_ = json.Unmarshal(optsRaw, &opts)
	req.OwnerName, req.PlanCode, req.Currency, req.Locale, req.Timezone = opts.OwnerName, opts.PlanCode, opts.Currency, opts.Locale, opts.Timezone
	if err := o.validate(&req); err != nil {
		return Result{}, err
	}
	if !planID.Valid {
		return Result{}, apperr.Conflict(apperr.CodeInvalidState, "Tenant has no plan")
	}

	var runID int64
	err = withSQLTx(ctx, pconn, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE platform.provisioning_runs SET status = 'discarded', finished_at = now()
			WHERE tenant_id = $1 AND status = 'running'`, tenantID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE platform.tenants SET status = 'provisioning', updated_at = now() WHERE id = $1`, tenantID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `INSERT INTO platform.provisioning_runs (tenant_id, step, request, attempts)
			VALUES ($1, 2, $2, 1 + (SELECT count(*) FROM platform.provisioning_runs WHERE tenant_id = $1)) RETURNING id`,
			tenantID, string(optsRaw)).Scan(&runID)
	})
	if err != nil {
		return Result{}, err
	}
	return o.run(ctx, pconn, runID, t, req, planID.Int64)
}

func (o *Orchestrator) inject(step int) error {
	if o.failAfter != nil {
		return o.failAfter(step)
	}
	return nil
}

// run executes steps 3–10.
func (o *Orchestrator) run(ctx context.Context, pconn *sql.DB, runID int64, t migrate.Target, req Request, planID int64) (Result, error) {
	res := Result{TenantID: t.ID, Slug: req.Slug, TenantURL: o.publicScheme + "://" + req.Slug + "." + o.baseDomain}

	steps := []struct {
		n  int
		fn func() error
	}{
		{StepSchema, func() error { return migrate.CreateTenantSchema(ctx, o.dsn, t) }},
		{StepMigrate, func() error {
			r := migrate.UpTenant(ctx, o.dsn, t, nil) // includes the post-step
			return r.Err
		}},
		{StepGrants, func() error { return nil }}, // applied by UpTenant's post-step
		{StepSeed, func() error { return o.seed(ctx, t, req) }},
		{StepOwner, func() error {
			uid, err := o.createOwner(ctx, t, req)
			res.OwnerUserID = uid
			return err
		}},
		{StepTrial, func() error {
			_, err := pconn.ExecContext(ctx, `INSERT INTO platform.subscriptions (tenant_id, plan_id, status, trial_end)
				SELECT $1, p.id, 'trialing', now() + make_interval(days => p.trial_days)
				FROM platform.plans p WHERE p.id = $2
				ON CONFLICT (tenant_id) DO NOTHING`, t.ID, planID)
			return err
		}},
	}
	for _, s := range steps {
		if err := s.fn(); err != nil {
			o.fail(ctx, pconn, runID, t, s.n, err)
			return Result{TenantID: t.ID, Status: "failed"}, fmt.Errorf("provisioning step %d: %w", s.n, err)
		}
		_, _ = pconn.ExecContext(ctx, `UPDATE platform.provisioning_runs SET step = $2 WHERE id = $1`, runID, s.n)
		if err := o.inject(s.n); err != nil {
			o.fail(ctx, pconn, runID, t, s.n, err)
			return Result{TenantID: t.ID, Status: "failed"}, err
		}
	}

	// Step 9: live. The tenants trigger sends tenant_changed, which flushes
	// every registry cache (including negative entries for this slug).
	if _, err := pconn.ExecContext(ctx, `UPDATE platform.tenants SET status = 'trialing', updated_at = now() WHERE id = $1`, t.ID); err != nil {
		o.fail(ctx, pconn, runID, t, StepActivate, err)
		return Result{TenantID: t.ID, Status: "failed"}, err
	}
	_, _ = pconn.ExecContext(ctx, `UPDATE platform.provisioning_runs SET step = 9 WHERE id = $1`, runID)

	// Step 10: welcome email. A failure here never fails provisioning.
	data, _ := json.Marshal(map[string]any{"tenant_id": t.ID, "user_id": res.OwnerUserID, "tenant_url": res.TenantURL})
	if _, err := pconn.ExecContext(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data)
		VALUES ($1, 'platform_notice', $2, $3, 'tenant_welcome', $4) ON CONFLICT (idempotency_key) DO NOTHING`,
		"tenant:"+t.ID+":welcome", req.OwnerEmail, req.OwnerName, string(data)); err != nil {
		o.log.WarnContext(ctx, "welcome email not queued", slog.String("tenant_id", t.ID), slog.Any("err", err))
	}
	_, _ = pconn.ExecContext(ctx, `UPDATE platform.provisioning_runs SET step = 10, status = 'succeeded', finished_at = now() WHERE id = $1`, runID)
	res.Status = "trialing"
	o.log.InfoContext(ctx, "tenant provisioned", slog.String("tenant_id", t.ID), slog.String("slug", req.Slug))
	return res, nil
}

// fail compensates steps 3–8 in reverse and marks the tenant failed.
func (o *Orchestrator) fail(ctx context.Context, pconn *sql.DB, runID int64, t migrate.Target, step int, cause error) {
	cctx := context.WithoutCancel(ctx)
	if step >= StepSchema {
		if _, err := pconn.ExecContext(cctx, `DELETE FROM platform.subscriptions WHERE tenant_id = $1`, t.ID); err != nil {
			o.log.Error("compensation: delete subscription", slog.String("tenant_id", t.ID), slog.Any("err", err))
		}
		_, _ = pconn.ExecContext(cctx, `DELETE FROM platform.mail_dispatch WHERE tenant_id = $1`, t.ID)
		if err := migrate.DropTenantSchema(cctx, o.dsn, t); err != nil {
			o.log.Error("compensation: drop schema/role", slog.String("tenant_id", t.ID), slog.Any("err", err))
		}
	}
	msg := sanitize(cause)
	_, _ = pconn.ExecContext(cctx, `UPDATE platform.tenants SET status = 'failed', schema_version = 0, updated_at = now() WHERE id = $1`, t.ID)
	_, _ = pconn.ExecContext(cctx, `UPDATE platform.provisioning_runs SET status = 'failed', step = $2, last_error = $3, finished_at = now() WHERE id = $1`,
		runID, step, msg)
	o.log.Error("provisioning failed", slog.String("tenant_id", t.ID), slog.Int("step", step), slog.String("err", msg))
}

const (
	qSeedSettings = `INSERT INTO settings (id, business_name, currency, locale, timezone)
		VALUES (1, $1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`
	qSeedRoles = `INSERT INTO roles (name, description, color, is_system, system_key) VALUES
		('Owner', 'Full access including billing', '#7c3aed', true, 'owner'),
		('Admin', 'Full access except billing', '#2563eb', true, 'admin'),
		('Biller', 'Point of sale', '#16a34a', true, 'biller')
		ON CONFLICT (system_key) DO NOTHING`
	qSeedBiller = `INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r JOIN permissions p ON (p.module || ':' || p.action) = ANY($1)
		WHERE r.system_key = 'biller' ON CONFLICT DO NOTHING`
	qSeedStore = `INSERT INTO stores (name, slug) VALUES ('Main Store', 'main')
		ON CONFLICT (slug) WHERE deleted_at IS NULL DO NOTHING`
	qSeedWalkIn = `WITH s AS (SELECT default_customer_id FROM settings WHERE id = 1),
		ins AS (INSERT INTO customers (name) SELECT 'Walk-in Customer' WHERE (SELECT default_customer_id FROM s) IS NULL RETURNING id)
		UPDATE settings SET default_customer_id = (SELECT id FROM ins) WHERE id = 1 AND EXISTS (SELECT 1 FROM ins)`
	qOwnerUser = `INSERT INTO users (name, email, role_id, status)
		SELECT $1, $2, r.id, 'Active' FROM roles r WHERE r.system_key = 'owner'
		ON CONFLICT (email) WHERE deleted_at IS NULL DO NOTHING`
	qOwnerID = `SELECT id FROM users WHERE email = $1 AND deleted_at IS NULL`
)

// seed is step 6: settings, system roles and their permissions, the
// Walk-in Customer and the default store (O-2). No demo data.
func (o *Orchestrator) seed(ctx context.Context, t migrate.Target, req Request) error {
	return o.inTenant(ctx, t, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, qSeedSettings, req.Name, req.Currency, req.Locale, req.Timezone); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qSeedRoles); err != nil {
			return err
		}
		// Owner/admin sets come from the same rule the post-step re-applies.
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_id)
			SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
			WHERE r.system_key = 'owner' OR (r.system_key = 'admin' AND p.module <> 'billing')
			ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		keys := make([]string, 0, len(rbac.BillerDefaults))
		for k := range rbac.BillerDefaults {
			keys = append(keys, k)
		}
		if _, err := tx.Exec(ctx, qSeedBiller, keys); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qSeedStore); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, qSeedWalkIn)
		return err
	})
}

// createOwner is step 7. The owner has no password yet; they set one from
// an invite link (issued by the Super Admin console or the welcome email
// worker, never stored in clear).
func (o *Orchestrator) createOwner(ctx context.Context, t migrate.Target, req Request) (int64, error) {
	var id int64
	err := o.inTenant(ctx, t, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, qOwnerUser, req.OwnerName, req.OwnerEmail); err != nil {
			return err
		}
		return tx.QueryRow(ctx, qOwnerID, req.OwnerEmail).Scan(&id)
	})
	return id, err
}

func (o *Orchestrator) inTenant(ctx context.Context, t migrate.Target, fn func(pgx.Tx) error) error {
	conn, err := migrate.TenantConn(o.dsn, t.Schema, "posctl-provision")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	c, err := conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.Raw(func(dc any) error {
		pc, ok := dc.(interface{ Conn() *pgx.Conn })
		if !ok {
			return errors.New("provisioning: not a pgx connection")
		}
		return pgx.BeginFunc(ctx, pc.Conn(), fn)
	})
}

// ReapStuck marks runs stuck in "running" for longer than maxAge as failed
// (and compensates), so the Super Admin can retry them.
func (o *Orchestrator) ReapStuck(ctx context.Context, maxAge time.Duration) ([]string, error) {
	pconn, err := migrate.PlatformConn(o.dsn, "posctl-provision")
	if err != nil {
		return nil, err
	}
	defer func() { _ = pconn.Close() }()
	rows, err := pconn.QueryContext(ctx, `SELECT r.id, r.tenant_id, r.step FROM platform.provisioning_runs r
		JOIN platform.tenants t ON t.id = r.tenant_id
		WHERE r.status = 'running' AND t.status = 'provisioning' AND r.started_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(maxAge.Seconds())))
	if err != nil {
		return nil, err
	}
	list, err := scanStuck(rows)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, s := range list {
		t, err := migrate.TargetFor(s.id)
		if err != nil {
			continue
		}
		o.fail(ctx, pconn, s.run, t, max(s.step, StepSchema), errors.New("stuck: reaped"))
		ids = append(ids, s.id)
	}
	return ids, nil
}

type stuck struct {
	run  int64
	id   string
	step int
}

func scanStuck(rows *sql.Rows) ([]stuck, error) {
	defer func() { _ = rows.Close() }()
	var list []stuck
	for rows.Next() {
		var s stuck
		if err := rows.Scan(&s.run, &s.id, &s.step); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

func withSQLTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func sanitize(err error) string {
	s := err.Error()
	if i := strings.Index(s, "postgres://"); i >= 0 {
		s = s[:i] + "[dsn]"
	}
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
