package main

import (
	"NMS1/internal/applog"
	"NMS1/internal/config"
	"NMS1/internal/timezone"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	timezone.InitFromEnv()
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}
	if err := config.ValidateRuntimeSecurityFor(config.RuntimeSecurityRoleAPI); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "runtime security validation failed: %v\n", err)
		os.Exit(1)
	}
	logger := applog.MustNewZapFile("nms-api")
	defer func() { _ = logger.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, logger, nil); err != nil {
		logger.Fatal("server stopped", zap.Error(err))
	}
}
