// Command posd runs the HTTP API and background workers.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/app"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Fprintln(os.Stdout, buildinfo.Version, buildinfo.Commit)
		return
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		// Config errors name variables only, never values.
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	log := obs.NewLogger(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", slog.Any("err", err))
		return 1
	}
	if err := a.Run(ctx); err != nil {
		log.Error("exited with error", slog.Any("err", err))
		return 1
	}
	return 0
}
