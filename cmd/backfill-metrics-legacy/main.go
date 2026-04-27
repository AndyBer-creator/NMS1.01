package main

import (
	"NMS1/internal/applog"
	"NMS1/internal/config"
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

func main() {
	batchSize := flag.Int("batch-size", 10000, "rows per batch")
	maxBatches := flag.Int("max-batches", 0, "optional safety cap for batches (0 = unlimited)")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall timeout")
	dryRun := flag.Bool("dry-run", false, "compute remaining rows without writing")
	finalize := flag.Bool("finalize", false, "drop metrics_legacy after verifying no remaining rows")
	flag.Parse()

	logger := applog.MustNewZapFile("nms-backfill-metrics-legacy")
	defer func() { _ = logger.Sync() }()

	if err := config.ValidateRuntimeSecurityFor(config.RuntimeSecurityRoleWorker); err != nil {
		logger.Fatal("validate runtime security", zap.Error(err))
	}
	if *batchSize <= 0 {
		logger.Fatal("invalid batch size", zap.Int("batch_size", *batchSize))
	}
	if *maxBatches < 0 {
		logger.Fatal("invalid max batches", zap.Int("max_batches", *maxBatches))
	}

	dsn := config.EnvOrFile("DB_DSN")
	if dsn == "" {
		logger.Fatal("DB_DSN must be set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logger.Fatal("open db", zap.Error(err))
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	exists, err := metricsLegacyExists(ctx, db)
	if err != nil {
		logger.Fatal("check metrics_legacy", zap.Error(err))
	}
	if !exists {
		logger.Info("metrics_legacy does not exist; nothing to backfill")
		return
	}

	remainingBefore, err := remainingRows(ctx, db)
	if err != nil {
		logger.Fatal("count remaining rows", zap.Error(err))
	}
	logger.Info("metrics legacy backfill start",
		zap.Int64("remaining_before", remainingBefore),
		zap.Int("batch_size", *batchSize),
		zap.Int("max_batches", *maxBatches),
		zap.Bool("dry_run", *dryRun),
		zap.Bool("finalize", *finalize))

	var copiedTotal int64
	var batches int
	if !*dryRun {
		for {
			if *maxBatches > 0 && batches >= *maxBatches {
				logger.Info("max batches reached", zap.Int("batches", batches))
				break
			}
			n, berr := copyBatch(ctx, db, *batchSize)
			if berr != nil {
				logger.Fatal("copy batch failed", zap.Error(berr), zap.Int("batch", batches+1))
			}
			if n == 0 {
				break
			}
			batches++
			copiedTotal += n
			logger.Info("batch copied", zap.Int("batch", batches), zap.Int64("rows", n), zap.Int64("copied_total", copiedTotal))
		}
	}

	remainingAfter, err := remainingRows(ctx, db)
	if err != nil {
		logger.Fatal("count remaining rows after backfill", zap.Error(err))
	}

	if *finalize {
		if remainingAfter > 0 {
			logger.Fatal("cannot finalize: remaining rows are not zero", zap.Int64("remaining_after", remainingAfter))
		}
		if err := dropLegacyTable(ctx, db); err != nil {
			logger.Fatal("drop metrics_legacy", zap.Error(err))
		}
		logger.Info("metrics_legacy dropped")
	}

	mode := "applied"
	if *dryRun {
		mode = "dry-run"
	}
	logger.Info("metrics legacy backfill completed",
		zap.String("mode", mode),
		zap.Int("batches", batches),
		zap.Int64("copied", copiedTotal),
		zap.Int64("remaining_after", remainingAfter))
	_, _ = fmt.Fprintf(os.Stdout, "metrics-legacy-backfill (%s): batches=%d copied=%d remaining=%d\n", mode, batches, copiedTotal, remainingAfter)
}

func metricsLegacyExists(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema='public' AND table_name='metrics_legacy'
		)`).
		Scan(&exists)
	return exists, err
}

func remainingRows(ctx context.Context, db *sql.DB) (int64, error) {
	var remaining int64
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM metrics_legacy ml
		WHERE NOT EXISTS (
			SELECT 1
			FROM metrics m
			WHERE m.id = ml.id
			  AND m."timestamp" = ml."timestamp"
		)`).
		Scan(&remaining)
	return remaining, err
}

func copyBatch(ctx context.Context, db *sql.DB, batchSize int) (int64, error) {
	if batchSize <= 0 {
		return 0, errors.New("batch size must be positive")
	}
	var copied int64
	err := db.QueryRowContext(ctx, `
		WITH batch AS (
			SELECT ml.id, ml.device_id, ml.oid, ml.value, ml."timestamp"
			FROM metrics_legacy ml
			WHERE NOT EXISTS (
				SELECT 1
				FROM metrics m
				WHERE m.id = ml.id
				  AND m."timestamp" = ml."timestamp"
			)
			ORDER BY ml.id
			LIMIT $1
		),
		ins AS (
			INSERT INTO metrics (id, device_id, oid, value, "timestamp")
			SELECT id, device_id, oid, value, "timestamp"
			FROM batch
			ON CONFLICT DO NOTHING
			RETURNING 1
		)
		SELECT COUNT(*) FROM ins`, batchSize).
		Scan(&copied)
	return copied, err
}

func dropLegacyTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS metrics_legacy CASCADE`)
	return err
}
