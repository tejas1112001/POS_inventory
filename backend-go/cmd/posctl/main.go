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
	"syscall"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

const usage = `usage: posctl <command>

commands:
  version                  print the build version
  config check             validate the posd environment (prints variable names only)
  db bootstrap             create roles, schemas and extensions (DATABASE_ADMIN_URL);
                           role passwords come from DATABASE_MIGRATOR_URL, DATABASE_URL,
                           DATABASE_JOBS_URL. Safe to re-run (rotates passwords)
  db preflight [--roles N] check a managed Postgres against OPERATIONS §8 (DATABASE_ADMIN_URL)
  migrate platform up      apply platform migrations (DATABASE_MIGRATOR_URL)
  migrate platform status  show current and latest platform version
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
	return nil
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
