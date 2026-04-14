package main

import (
	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "calculate and validate rotation without writing updates")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall rotation timeout")
	flag.Parse()

	dsn := config.EnvOrFile("DB_DSN")
	oldKey := config.EnvOrFile("NMS_DB_ENCRYPTION_OLD_KEY")
	newKey := config.EnvOrFile("NMS_DB_ENCRYPTION_KEY")
	if err := validateRotateEnv(dsn, oldKey, newKey); err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	stats, err := postgres.RotateDeviceSNMPSecrets(ctx, db, oldKey, newKey, *dryRun)
	if err != nil {
		log.Fatal(err)
	}
	mode := rotationMode(*dryRun)
	_, _ = fmt.Fprintf(os.Stdout, "db-secret-rotation (%s): scanned=%d updated=%d skipped=%d\n", mode, stats.Scanned, stats.Updated, stats.Skipped)
}

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

func rotationMode(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "applied"
}
