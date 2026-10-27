// Command posctl is the operator CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

const usage = `usage: posctl <command>

commands:
  version                  print the build version
  config check             validate the posd environment (prints variable names only)
  db bootstrap             create roles, schemas and extensions (DATABASE_ADMIN_URL);
                           role passwords come from DATABASE_MIGRATOR_URL, DATABASE_URL,
                           DATABASE_JOBS_URL. Safe to re-run (rotates passwords)
  db preflight [--roles N] check a managed Postgres against OPERATIONS §8 (DATABASE_ADMIN_URL)
  migrate platform up      apply platform migrations and sync the permission catalog
  migrate platform status  show current and latest platform version
  migrate tenants up [--dry-run] [--parallelism N] [--tenants id,id] [--resume RUN_ID]
                           migrate tenant schemas (TENANCY §8)
  migrate drift            compare every tenant schema with a fresh reference
  tenant provision --slug S --name N --owner-email E [--owner-name O] [--plan P]
                   [--currency INR] [--locale en-IN] [--timezone Asia/Kolkata]
  tenant retry <tenant_id> resume a failed provisioning run
  tenant reap [--older-than 15m]  fail and compensate runs stuck in provisioning
  tenant export <tenant_id> [--files]
                           encrypted per-tenant export to object storage (OPERATIONS §6.2);
                           reads DATABASE_JOBS_URL, EXPORT_AGE_RECIPIENT, EXPORT_MANIFEST_KEY
  tenant restore <tenant_id> [--export ID] [--drop]
                           restore into a scratch schema and compare row counts and
                           checksums (kept 7 days; --drop: a restore test)
  tenant restore <tenant_id> --replace --confirm SLUG [--export ID]
                           replace the live schema (tenant must be read_only); every
                           session ends. Restores read DATABASE_MIGRATOR_URL,
                           EXPORT_AGE_IDENTITY, EXPORT_MANIFEST_KEY
  tenant restore-tests [--weekly]  run queued restore tests (--weekly: queue the weekly ones)
  tenant restore-cleanup   drop kept scratch schemas and replaced schemas that are due
  bootstrap-superadmin     create the first super_admin from BOOTSTRAP_ADMIN_EMAIL,
                           BOOTSTRAP_ADMIN_PASSWORD (and optional BOOTSTRAP_ADMIN_NAME);
                           refuses if any platform user exists

Every command that touches the database reads DATABASE_MIGRATOR_URL unless
noted. Output never includes connection strings or secrets.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cmd := func(n int) string {
		if len(args) > n {
			return args[n]
		}
		return ""
	}
	var err error
	switch {
	case cmd(0) == "version":
		fmt.Fprintln(stdout, buildinfo.Version, buildinfo.Commit)
		return 0
	case cmd(0) == "config" && cmd(1) == "check":
		if _, err = config.Load(); err == nil {
			fmt.Fprintln(stdout, "config ok")
		}
	case cmd(0) == "db" && cmd(1) == "bootstrap":
		err = dbBootstrap(ctx, stdout)
	case cmd(0) == "db" && cmd(1) == "preflight":
		err = dbPreflight(ctx, args[2:], stdout)
	case cmd(0) == "migrate" && cmd(1) == "platform" && cmd(2) == "up":
		err = migratePlatformUp(ctx, stdout)
	case cmd(0) == "migrate" && cmd(1) == "platform" && cmd(2) == "status":
		err = migratePlatformStatus(ctx, stdout)
	case cmd(0) == "migrate" && cmd(1) == "tenants" && cmd(2) == "up":
		err = migrateTenantsUp(ctx, args[3:], stdout)
	case cmd(0) == "migrate" && cmd(1) == "drift":
		err = migrateDrift(ctx, stdout)
	case cmd(0) == "tenant" && cmd(1) == "provision":
		err = tenantProvision(ctx, args[2:], stdout)
	case cmd(0) == "tenant" && cmd(1) == "retry" && cmd(2) != "":
		err = tenantRetry(ctx, cmd(2), stdout)
	case cmd(0) == "tenant" && cmd(1) == "reap":
		err = tenantReap(ctx, args[2:], stdout)
	case cmd(0) == "tenant" && cmd(1) == "export":
		err = tenantExport(ctx, args[2:], stdout)
	case cmd(0) == "tenant" && cmd(1) == "restore":
		err = tenantRestore(ctx, args[2:], stdout)
	case cmd(0) == "tenant" && cmd(1) == "restore-tests":
		err = tenantRestoreTests(ctx, args[2:], stdout)
	case cmd(0) == "tenant" && cmd(1) == "restore-cleanup":
		err = tenantRestoreCleanup(ctx, stdout)
	case cmd(0) == "bootstrap-superadmin":
		err = bootstrapSuperAdmin(ctx, stdout)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", obs.RedactString(err.Error()))
		return 1
	}
	return 0
}

// dsnEnv reads a DSN from the environment and checks it without echoing it.
func dsnEnv(name string) (secrets.Secret, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	prod := os.Getenv("APP_ENV") == config.EnvProduction
	if msg := config.CheckDSN(v, prod); msg != "" {
		return "", fmt.Errorf("%s %s", name, msg)
	}
	return secrets.Secret(v), nil
}

func dbBootstrap(ctx context.Context, stdout io.Writer) error {
	var in migrate.BootstrapInput
	var errs []error
	for _, f := range []struct {
		name string
		dst  *secrets.Secret
	}{
		{"DATABASE_ADMIN_URL", &in.AdminDSN}, {"DATABASE_MIGRATOR_URL", &in.MigratorDSN},
		{"DATABASE_URL", &in.AppDSN}, {"DATABASE_JOBS_URL", &in.JobsDSN},
	} {
		v, err := dsnEnv(f.name)
		errs = append(errs, err)
		*f.dst = v
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := migrate.Bootstrap(ctx, in); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "bootstrap ok: roles pos_owner, pos_migrator, pos_app, pos_platform, pos_jobs; schemas extensions, platform")
	return nil
}

func dbPreflight(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	roles := fs.Int("roles", 2000, "roles and schemas to create for M5")
	if err := fs.Parse(args); err != nil {
		return err
	}
	admin, err := dsnEnv("DATABASE_ADMIN_URL")
	if err != nil {
		return err
	}
	checks, err := migrate.Preflight(ctx, admin, migrate.PreflightOptions{ManyRoles: *roles})
	failed := 0
	for _, c := range checks {
		fmt.Fprintf(stdout, "%-4s %-6s %s", c.ID, c.Status, c.Name)
		if c.Detail != "" {
			fmt.Fprintf(stdout, "  (%s)", obs.RedactString(c.Detail))
		}
		fmt.Fprintln(stdout)
		if c.Status == migrate.Fail {
			failed++
		}
	}
	if err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d preflight checks failed", failed)
	}
	return nil
}

func migratePlatformUp(ctx context.Context, stdout io.Writer) error {
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return err
	}
	log := obs.NewLogger(os.Stderr, "info", "text")
	res, err := migrate.PlatformUp(ctx, dsn, log.With(slog.String("track", "platform")))
	for _, r := range res {
		fmt.Fprintf(stdout, "applied %05d %s (%s)\n", r.Version, r.Name, r.Duration)
	}
	if err != nil {
		return err
	}
	if len(res) == 0 {
		fmt.Fprintln(stdout, "platform schema is up to date")
	}
	if err := migrate.SyncPlatformCatalog(ctx, dsn); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "permission catalog synced")
	return nil
}

func migrateTenantsUp(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("migrate tenants up", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "report and roll back")
	par := fs.Int("parallelism", 8, "tenants in parallel")
	ids := fs.String("tenants", "", "comma-separated tenant IDs")
	resume := fs.Int64("resume", 0, "resume a run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return err
	}
	var list []string
	for _, id := range strings.Split(*ids, ",") {
		if id = strings.TrimSpace(id); id != "" {
			list = append(list, id)
		}
	}
	rep, err := migrate.RunTenants(ctx, dsn, migrate.RunOptions{
		DryRun: *dry, Parallelism: *par, TenantIDs: list, ResumeRunID: *resume,
		Log: obs.NewLogger(os.Stderr, "warn", "text"),
	})
	if err != nil {
		return err
	}
	for _, it := range rep.Items {
		line := fmt.Sprintf("%s %-9s %d -> %d (%s)", it.TenantID, it.Status, it.From, it.To, it.Duration.Round(time.Millisecond))
		if len(it.Pending) > 0 {
			line += fmt.Sprintf(" pending %v", it.Pending)
		}
		if it.Err != nil {
			line += " error: " + obs.RedactString(it.Err.Error())
		}
		fmt.Fprintln(stdout, line)
	}
	fmt.Fprintf(stdout, "run %d: %s (target version %d, %d tenants)\n", rep.RunID, rep.Status, rep.Target, len(rep.Items))
	if rep.Status != "succeeded" {
		return fmt.Errorf("run %d ended %s; retry with --resume %d", rep.RunID, rep.Status, rep.RunID)
	}
	return nil
}

func migrateDrift(ctx context.Context, stdout io.Writer) error {
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return err
	}
	items, err := migrate.Drift(ctx, dsn)
	if err != nil {
		return err
	}
	bad := 0
	for _, it := range items {
		fmt.Fprintln(stdout, it.String())
		for _, m := range it.Missing {
			fmt.Fprintln(stdout, "    missing:", m)
		}
		for _, x := range it.Extra {
			fmt.Fprintln(stdout, "    extra:  ", x)
		}
		if !it.Matches || it.Version != it.Latest {
			bad++
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d tenants drifted or behind", bad)
	}
	fmt.Fprintf(stdout, "%d tenants match the reference\n", len(items))
	return nil
}

func orchestrator() (*provisioning.Orchestrator, error) {
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return nil, err
	}
	base := os.Getenv("BASE_DOMAIN")
	if base == "" {
		return nil, errors.New("BASE_DOMAIN is required")
	}
	scheme := os.Getenv("PUBLIC_SCHEME")
	if scheme == "" {
		scheme = "https"
	}
	return provisioning.New(dsn, scheme, base, obs.NewLogger(os.Stderr, "info", "text")), nil
}

func tenantProvision(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tenant provision", flag.ContinueOnError)
	var r provisioning.Request
	fs.StringVar(&r.Slug, "slug", "", "subdomain")
	fs.StringVar(&r.Name, "name", "", "business name")
	fs.StringVar(&r.OwnerEmail, "owner-email", "", "owner email")
	fs.StringVar(&r.OwnerName, "owner-name", "", "owner name")
	fs.StringVar(&r.PlanCode, "plan", "", "plan code (default trial)")
	fs.StringVar(&r.Currency, "currency", "", "ISO 4217 (default INR)")
	fs.StringVar(&r.Locale, "locale", "", "number format (default en-IN)")
	fs.StringVar(&r.Timezone, "timezone", "", "IANA zone (default Asia/Kolkata)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o, err := orchestrator()
	if err != nil {
		return err
	}
	res, err := o.Provision(ctx, r)
	if err != nil {
		return describe(err)
	}
	fmt.Fprintf(stdout, "tenant %s provisioned: %s (status %s, owner user %d)\n", res.TenantID, res.TenantURL, res.Status, res.OwnerUserID)
	return nil
}

func tenantRetry(ctx context.Context, id string, stdout io.Writer) error {
	o, err := orchestrator()
	if err != nil {
		return err
	}
	res, err := o.Retry(ctx, id)
	if err != nil {
		return describe(err)
	}
	fmt.Fprintf(stdout, "tenant %s provisioned: %s (status %s)\n", res.TenantID, res.TenantURL, res.Status)
	return nil
}

func tenantReap(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tenant reap", flag.ContinueOnError)
	age := fs.Duration("older-than", 15*time.Minute, "minimum run age")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o, err := orchestrator()
	if err != nil {
		return err
	}
	ids, err := o.ReapStuck(ctx, *age)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "reaped %d stuck runs %v\n", len(ids), ids)
	return nil
}

// describe turns field errors into a readable message.
func describe(err error) error {
	if e, ok := apperr.As(err); ok && len(e.Fields) > 0 {
		var parts []string
		for k, v := range e.Fields {
			parts = append(parts, k+": "+v)
		}
		sort.Strings(parts)
		return fmt.Errorf("%s (%s)", e.Message, strings.Join(parts, "; "))
	}
	return err
}

func migratePlatformStatus(ctx context.Context, stdout io.Writer) error {
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return err
	}
	cur, latest, err := migrate.PlatformStatus(ctx, dsn)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "platform: current %d, latest %d\n", cur, latest)
	return nil
}

func bootstrapSuperAdmin(ctx context.Context, stdout io.Writer) error {
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return err
	}
	email, pw := os.Getenv("BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || pw == "" {
		return errors.New("BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	id, err := platform.BootstrapSuperAdmin(ctx, dsn, email, os.Getenv("BOOTSTRAP_ADMIN_NAME"), pw,
		security.NewHasher(security.PlatformArgon2, 1))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "super_admin created (id %d). Sign in at admin.<BASE_DOMAIN>: you will set a new password and enrol TOTP.\n", id)
	fmt.Fprintln(stdout, "Remove BOOTSTRAP_ADMIN_PASSWORD from the environment now.")
	return nil
}
