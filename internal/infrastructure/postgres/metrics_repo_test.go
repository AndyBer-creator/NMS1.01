package postgres

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSaveMetricWithExec(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectExec(`INSERT INTO metrics \(device_id, oid, value\) VALUES \(\$1, \$2, \$3\)`).
		WithArgs(7, ".1.3.6.1.2.1.1.3.0", "42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.saveMetricWithExec(ctx, db, 7, ".1.3.6.1.2.1.1.3.0", "42"); err != nil {
		t.Fatalf("saveMetricWithExec: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPruneOldMetricPartitionsWithExec(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if _, err := repo.pruneOldMetricPartitionsWithExec(ctx, db, 0); err == nil {
		t.Fatalf("expected validation error for retainMonths < 1")
	}

	mock.ExpectQuery(`SELECT prune_old_metrics_partitions\(\$1\)`).
		WithArgs(3).
		WillReturnRows(sqlmock.NewRows([]string{"prune_old_metrics_partitions"}).AddRow(2))
	dropped, err := repo.pruneOldMetricPartitionsWithExec(ctx, db, 3)
	if err != nil {
		t.Fatalf("pruneOldMetricPartitionsWithExec: %v", err)
	}
	if dropped != 2 {
		t.Fatalf("expected dropped=2, got %d", dropped)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
