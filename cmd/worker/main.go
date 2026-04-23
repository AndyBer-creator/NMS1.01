package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"NMS1/internal/applog"
	"NMS1/internal/config"
	"NMS1/internal/timezone"

	"go.uber.org/zap"
)

// main starts worker loops and blocks until process shutdown.
func main() {
	timezone.InitFromEnv()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nms-worker config load failed: %v\n", err)
		os.Exit(1)
	}
	if err := config.ValidateRuntimeSecurityFor(config.RuntimeSecurityRoleWorker); err != nil {
		fmt.Fprintf(os.Stderr, "nms-worker: %v\n", err)
		os.Exit(1)
	}

	logger := applog.MustNewZapFile("nms-worker")
	defer func() { _ = logger.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, logger, workerOpts{}); err != nil {
		logger.Fatal("worker stopped", zap.Error(err))
	}
}
