package main

import (
	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"context"
	"database/sql"
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
	if dsn == "" {
		log.Fatal("DB_DSN must be set")
	}
	oldKey := config.EnvOrFile("NMS_DB_ENCRYPTION_OLD_KEY")
	newKey := config.EnvOrFile("NMS_DB_ENCRYPTION_KEY")
	if oldKey == "" {
		log.Fatal("NMS_DB_ENCRYPTION_OLD_KEY must be set for rotation")
	}
	if newKey == "" {
		log.Fatal("NMS_DB_ENCRYPTION_KEY must be set for rotation")
	}
	if oldKey == newKey {
		log.Fatal("old and new DB encryption keys must differ")
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
	mode := "applied"
	if *dryRun {
		mode = "dry-run"
	}
	_, _ = fmt.Fprintf(os.Stdout, "db-secret-rotation (%s): scanned=%d updated=%d skipped=%d\n", mode, stats.Scanned, stats.Updated, stats.Skipped)
}
