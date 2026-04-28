package postgres

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestResolveOpenIncidentsBySource(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if changed, err := repo.ResolveOpenIncidentsBySource(ctx, 0, "manual", "", ""); err != nil || changed != 0 {
		t.Fatalf("deviceID<=0 should no-op: changed=%d err=%v", changed, err)
	}
	if _, err := repo.ResolveOpenIncidentsBySource(ctx, 1, "bad", "", ""); err == nil {
		t.Fatalf("expected invalid source error")
	}

	mock.ExpectQuery(`WITH candidates AS \(`).
		WithArgs(5, "polling", "system", "auto-resolved").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	changed, err := repo.ResolveOpenIncidentsBySource(ctx, 5, "polling", "", "")
	if err != nil || changed != 2 {
		t.Fatalf("ResolveOpenIncidentsBySource: changed=%d err=%v", changed, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

