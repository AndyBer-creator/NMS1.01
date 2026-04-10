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

func main() {
	timezone.InitFromEnv()
	cfg := config.Load()

	logger, err := applog.NewZapFile("nms-worker")
	if err != nil {
		fmt.Fprintf(os.Stderr, "nms-worker: logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, logger, workerOpts{}); err != nil {
		logger.Fatal("worker stopped", zap.Error(err))
	}
}
