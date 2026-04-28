package postgres

import (
	"context"
	"errors"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepoInTx_CommitsOnNilError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}

	mock.ExpectBegin()
	mock.ExpectCommit()

	if err := repo.InTx(context.Background(), func(tx *sql.Tx) error {
		// We don't need to execute queries here; we only test lifecycle.
		return nil
	}); err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRepoInTx_RollsBackOnError(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}

	mock.ExpectBegin()
	mock.ExpectRollback()

	wantErr := errors.New("boom")
	if err := repo.InTx(context.Background(), func(tx *sql.Tx) error {
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("InTx: got %v want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRepoInTx_RejectsNilFunc(t *testing.T) {
	t.Parallel()
	repo := &Repo{}
	if err := repo.InTx(context.Background(), nil); err == nil {
		t.Fatalf("expected error for nil tx func")
	}
}

