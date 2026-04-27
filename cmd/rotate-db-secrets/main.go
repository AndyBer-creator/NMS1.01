package main

import (
	"NMS1/internal/applog"
	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

// main rotates encrypted device secrets in the database.
func main() {
	dryRun := flag.Bool("dry-run", false, "calculate and validate rotation without writing updates")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall rotation timeout")
	flag.Parse()

	logger := applog.MustNewZapFile("nms-rotate-db-secrets")
	defer func() { _ = logger.Sync() }()

	if err := config.ValidateRuntimeSecurityFor(config.RuntimeSecurityRoleWorker); err != nil {
		logger.Fatal("validate runtime security", zap.Error(err))
	}

	dsn := config.EnvOrFile("DB_DSN")
	oldKey := config.EnvOrFile("NMS_DB_ENCRYPTION_OLD_KEY")
	newKey := config.EnvOrFile("NMS_DB_ENCRYPTION_KEY")
	if err := validateRotateEnv(dsn, oldKey, newKey); err != nil {
		logger.Fatal("validate rotation env", zap.Error(err))
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logger.Fatal("open database", zap.Error(err))
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	stats, err := postgres.RotateDeviceSNMPSecrets(ctx, db, oldKey, newKey, *dryRun)
	if err != nil {
		logger.Fatal("rotate device secrets", zap.Error(err))
	}
	mode := rotationMode(*dryRun)
	logger.Info("db secret rotation completed",
		zap.String("mode", mode),
		zap.Int("scanned", stats.Scanned),
		zap.Int("updated", stats.Updated),
		zap.Int("skipped", stats.Skipped),
	)
	_, _ = fmt.Fprintf(os.Stdout, "db-secret-rotation (%s): scanned=%d updated=%d skipped=%d\n", mode, stats.Scanned, stats.Updated, stats.Skipped)
}

// validateRotateEnv ensures required rotation inputs are present and valid.
func validateRotateEnv(dsn, oldKey, newKey string) error {
	if dsn == "" {
		return errors.New("DB_DSN must be set")
	}
	if oldKey == "" {
		return errors.New("NMS_DB_ENCRYPTION_OLD_KEY must be set for rotation")
	}
	if newKey == "" {
		return errors.New("NMS_DB_ENCRYPTION_KEY must be set for rotation")
	}
	if oldKey == newKey {
		return errors.New("old and new DB encryption keys must differ")
	}
	return nil
}

// rotationMode returns a human-readable mode for logs and stdout.
func rotationMode(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "applied"
}
