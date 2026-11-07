package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenantio"
)

// Per-tenant export and restore (OPERATIONS §6.2–6.3). Exports read
// DATABASE_JOBS_URL (the snapshot logs in as pos_jobs and acts as the
// tenant role), EXPORT_AGE_RECIPIENT and EXPORT_MANIFEST_KEY. Restores read
// DATABASE_MIGRATOR_URL, EXPORT_AGE_IDENTITY and EXPORT_MANIFEST_KEY. Both
// read the storage settings (STORAGE_DRIVER, STORAGE_FS_ROOT, S3_*).

// storageFromEnv builds the object storage posd uses.
func storageFromEnv(ctx context.Context) (files.Storage, error) {
	switch d := os.Getenv("STORAGE_DRIVER"); d {
	case "", "fs":
		root := os.Getenv("STORAGE_FS_ROOT")
		if root == "" {
			root = "data/files"
		}
		return files.NewFSStorage(root)
	case "s3":
		region := os.Getenv("S3_REGION")
		if region == "" {
			region = "us-east-1"
		}
		return files.NewS3Storage(ctx, files.S3Config{Endpoint: os.Getenv("S3_ENDPOINT"), Bucket: os.Getenv("S3_BUCKET"),
			AccessKey: secrets.Secret(os.Getenv("S3_ACCESS_KEY")), SecretKey: secrets.Secret(os.Getenv("S3_SECRET_KEY")), Region: region})
	default:
		return nil, errors.New("STORAGE_DRIVER must be fs or s3")
	}
}

func manifestKeyEnv() ([]byte, error) {
	k := secrets.Secret(os.Getenv("EXPORT_MANIFEST_KEY"))
	if k.IsZero() {
		return nil, errors.New("EXPORT_MANIFEST_KEY is required")
	}
	if msg := config.CheckManifestKey(k); msg != "" {
		return nil, fmt.Errorf("EXPORT_MANIFEST_KEY %s", msg)
	}
	return []byte(k.Reveal()), nil
}

func exporterFromEnv(ctx context.Context) (*tenantio.Exporter, func(), error) {
	dsn, err := dsnEnv("DATABASE_JOBS_URL")
	if err != nil {
		return nil, nil, err
	}
	rcp, err := config.ParseAgeRecipient(os.Getenv("EXPORT_AGE_RECIPIENT"))
	if err != nil {
		return nil, nil, fmt.Errorf("EXPORT_AGE_RECIPIENT %w", err)
	}
	key, err := manifestKeyEnv()
	if err != nil {
		return nil, nil, err
	}
	store, err := storageFromEnv(ctx)
	if err != nil {
		return nil, nil, err
	}
	pool, err := db.NewPool(ctx, dsn, db.PoolOptions{MaxConns: 2, ApplicationName: "posctl-export"})
	if err != nil {
		return nil, nil, err
	}
	return &tenantio.Exporter{Sessions: tenancy.NewSessions(pool, time.Minute, nil, nil), DSN: dsn, Storage: store,
		Recipient: rcp, ManifestKey: key}, pool.Close, nil
}

func restorerFromEnv(ctx context.Context) (*tenantio.Restorer, error) {
	dsn, err := dsnEnv("DATABASE_MIGRATOR_URL")
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(os.Getenv("EXPORT_AGE_IDENTITY"))
	if raw == "" {
		return nil, errors.New("EXPORT_AGE_IDENTITY is required")
	}
	ids, err := age.ParseIdentities(strings.NewReader(raw))
	if err != nil || len(ids) != 1 {
		return nil, errors.New("EXPORT_AGE_IDENTITY must be one age identity (AGE-SECRET-KEY-1…)")
	}
	key, err := manifestKeyEnv()
	if err != nil {
		return nil, err
	}
	store, err := storageFromEnv(ctx)
	if err != nil {
		return nil, err
	}
	return &tenantio.Restorer{DSN: dsn, Storage: store, Identity: ids[0], ManifestKey: key}, nil
}

// tenant export <tenant_id> [--files]
func tenantExport(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: posctl tenant export <tenant_id> [--files]")
	}
	tenantID := args[0]
	fs := flag.NewFlagSet("tenant export", flag.ContinueOnError)
	withFiles := fs.Bool("files", false, "include the tenant's files")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	e, done, err := exporterFromEnv(ctx)
	if err != nil {
		return err
	}
	defer done()
	var id uuid.UUID
	err = e.Sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var err error
		id, err = tenantio.Queue(ctx, tx, tenantID, tenantio.KindManual, "posctl", *withFiles)
		return err
	})
	if err != nil {
		return describe(err)
	}
	rec, err := e.Run(ctx, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "export %s succeeded\n  object   %s\n  size     %d bytes\n  sha256   %s\n  version  %d\n  tables   %d, rows %d, files %d\n",
		rec.ID, rec.ObjectKey, rec.Summary.Size, rec.Summary.SHA256, rec.SchemaVersion, rec.Tables, rec.Rows, rec.Files)
	if len(rec.Missing) > 0 {
		fmt.Fprintf(stdout, "  missing  %d files (listed in the manifest)\n", len(rec.Missing))
	}
	return nil
}

func printReport(stdout io.Writer, rep tenantio.Report) {
	fmt.Fprintf(stdout, "restore %s (%s) of export %s: ", rep.ID, rep.Mode, rep.ExportID)
	if rep.OK {
		fmt.Fprintln(stdout, "rows and checksums match")
	} else {
		fmt.Fprintln(stdout, "NOT VERIFIED")
	}
	fmt.Fprintf(stdout, "  scratch  %s (version %d, migrated to %d)\n", rep.Scratch, rep.SchemaVersion, rep.MigratedTo)
	for _, c := range rep.Tables {
		mark := "ok"
		if !c.OK {
			mark = "MISMATCH"
		}
		fmt.Fprintf(stdout, "  %-22s %8d rows  %s  %s\n", c.Name, c.RowsRead, c.SHA256Read[:12], mark)
	}
	switch {
	case rep.OldSchema != "":
		fmt.Fprintf(stdout, "  replaced: the live schema is the export; the old one is %s (dropped after 30 days)\n", rep.OldSchema)
	case rep.Kept:
		fmt.Fprintln(stdout, "  kept for 7 days (posctl tenant restore-cleanup drops it after that)")
	}
}

// tenant restore <tenant_id> [--export ID] [--drop] | [--replace --confirm SLUG]
func tenantRestore(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: posctl tenant restore <tenant_id> [--export ID] [--drop] [--replace --confirm SLUG]")
	}
	tenantID := args[0]
	fs := flag.NewFlagSet("tenant restore", flag.ContinueOnError)
	exportArg := fs.String("export", "", "export ID (default: the latest succeeded export)")
	drop := fs.Bool("drop", false, "drop the scratch schema after comparing (a restore test)")
	replace := fs.Bool("replace", false, "replace the live schema (tenant must be read_only)")
	confirm := fs.String("confirm", "", "the tenant's slug, required with --replace")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var exportID uuid.UUID
	if *exportArg != "" {
		var err error
		if exportID, err = uuid.Parse(*exportArg); err != nil {
			return errors.New("--export must be an export ID")
		}
	}
	r, err := restorerFromEnv(ctx)
	if err != nil {
		return err
	}
	var rep tenantio.Report
	switch {
	case *replace:
		rep, err = r.Replace(ctx, tenantID, exportID, *confirm, "posctl")
	default:
		mode := tenantio.ModeScratch
		if *drop {
			mode = tenantio.ModeTest
		}
		var id uuid.UUID
		if id, err = r.Enqueue(ctx, tenantID, exportID, mode, "posctl"); err != nil {
			return err
		}
		rep, err = r.Run(ctx, id)
	}
	if rep.ExportID != uuid.Nil {
		printReport(stdout, rep)
	}
	return err
}

// tenant restore-tests [--weekly]: run queued restore tests (console
// requests); --weekly first queues the weekly ones.
func tenantRestoreTests(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tenant restore-tests", flag.ContinueOnError)
	weekly := fs.Bool("weekly", false, "queue the weekly restore tests first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := restorerFromEnv(ctx)
	if err != nil {
		return err
	}
	if *weekly {
		ids, err := r.ScheduleWeekly(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "queued %d weekly restore tests\n", len(ids))
	}
	ids, err := r.Queued(ctx, 50)
	if err != nil {
		return err
	}
	var failed int
	for _, id := range ids {
		rep, err := r.Run(ctx, id)
		if errors.Is(err, tenantio.ErrNotQueued) {
			continue // taken by another run
		}
		if rep.ExportID != uuid.Nil {
			printReport(stdout, rep)
		}
		if err != nil {
			failed++
			fmt.Fprintf(stdout, "  failed: %v\n", err)
		}
	}
	fmt.Fprintf(stdout, "%d restore tests run, %d failed\n", len(ids), failed)
	if failed > 0 {
		return fmt.Errorf("%d restore tests failed", failed)
	}
	return nil
}

// tenant restore-cleanup: drop kept scratch and old schemas that are due.
func tenantRestoreCleanup(ctx context.Context, stdout io.Writer) error {
	r, err := restorerFromEnv(ctx)
	if err != nil {
		return err
	}
	n, err := r.Cleanup(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "dropped %d schemas\n", n)
	return nil
}
