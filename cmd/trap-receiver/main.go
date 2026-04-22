package main

import (
	"context"
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

	logger := applog.MustNewZapFile("nms-trap-receiver")
	defer func() { _ = logger.Sync() }()

	logger.Info("🚀 SNMP Trap Receiver v1 started")
	if err := config.ValidateRuntimeSecurityFor(config.RuntimeSecurityRoleTrapReceiver); err != nil {
		logger.Fatal("runtime security validation failed", zap.Error(err))
	}

	dsn := strings.TrimSpace(config.EnvOrFile("DB_DSN"))
	grpcTarget := strings.TrimSpace(config.EnvOrFile("NMS_TRAP_GRPC_TARGET"))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, logger, dsn, grpcTarget, trapListenPort()); err != nil {
		logger.Fatal("trap receiver stopped", zap.Error(err))
	}
}
