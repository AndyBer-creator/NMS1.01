package main

import (
	"NMS1/internal/applog"
	"NMS1/internal/config"
	"NMS1/internal/timezone"
	"context"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	timezone.InitFromEnv()
	cfg := config.Load()
	if err := config.ValidateRuntimeSecurity(); err != nil {
		panic(err.Error())
	}
	logger := applog.MustNewZapFile("nms-api")
	defer func() { _ = logger.Sync() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, logger, nil); err != nil {
		logger.Fatal("server stopped", zap.Error(err))
	}
}
