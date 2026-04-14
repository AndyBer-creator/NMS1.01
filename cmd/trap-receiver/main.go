package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"NMS1/internal/applog"
	"NMS1/internal/config"
	"NMS1/internal/timezone"

	"go.uber.org/zap"
)

func main() {
	timezone.InitFromEnv()

	logger, err := applog.NewZapFile("nms-trap-receiver")
	if err != nil {
		fmt.Fprintf(os.Stderr, "nms-trap-receiver: logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("🚀 SNMP Trap Receiver v1 started")
	if err := config.ValidateRuntimeSecurity(); err != nil {
		logger.Fatal("runtime security validation failed", zap.Error(err))
	}

	dsn := strings.TrimSpace(config.EnvOrFile("DB_DSN"))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger, dsn, trapListenPort()); err != nil {
		logger.Fatal("trap receiver stopped", zap.Error(err))
	}
}
