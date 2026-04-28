package main

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMetricsLegacyExists(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT EXISTS \(`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	got, err := metricsLegacyExists(context.Background(), db)
	if err != nil {
		t.Fatalf("metricsLegacyExists: %v", err)
	}
	if !got {
		t.Fatalf("metricsLegacyExists: expected true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRemainingRows(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT COUNT\(\*\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(123)))

	got, err := remainingRows(context.Background(), db)
	if err != nil {
		t.Fatalf("remainingRows: %v", err)
	}
	if got != 123 {
		t.Fatalf("remainingRows: got %d want %d", got, 123)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCopyBatch(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := copyBatch(context.Background(), db, 0); err == nil {
		t.Fatalf("expected error for non-positive batch size")
	}

	mock.ExpectQuery(`WITH batch AS \(`).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(7)))

	got, err := copyBatch(context.Background(), db, 10)
	if err != nil {
		t.Fatalf("copyBatch: %v", err)
	}
	if got != 7 {
		t.Fatalf("copyBatch: got %d want %d", got, 7)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestDropLegacyTable(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`DROP TABLE IF EXISTS metrics_legacy CASCADE`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := dropLegacyTable(context.Background(), db); err != nil {
		t.Fatalf("dropLegacyTable: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

