package postgres

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNewFromDBCloseAndPing(t *testing.T) {
	t.Parallel()

	if _, err := NewFromDB(nil); err == nil {
		t.Fatalf("expected nil db validation error")
	}

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo, err := NewFromDB(db)
	if err != nil {
		t.Fatalf("NewFromDB: %v", err)
	}
	if repo == nil || repo.db != db || repo.ownDB {
		t.Fatalf("unexpected repo: %+v", repo)
	}

	mock.ExpectPing()
	if err := repo.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if err := repo.Close(); err != nil {
		t.Fatalf("Close should be noop for external db: %v", err)
	}

	owned := &Repo{db: db, ownDB: true}
	mock.ExpectClose()
	if err := owned.Close(); err != nil {
		t.Fatalf("owned Close: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
