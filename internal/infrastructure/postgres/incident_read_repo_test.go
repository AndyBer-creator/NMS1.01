package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetIncidentByIDWithExec(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if got, err := repo.getIncidentByIDWithExec(ctx, db, 0); err != nil || got != nil {
		t.Fatalf("id<=0 should return nil,nil: got=%v err=%v", got, err)
	}

	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(10)).
		WillReturnError(errors.New("query failed"))
	if _, err := repo.getIncidentByIDWithExec(ctx, db, 10); err == nil {
		t.Fatalf("expected query error")
	}

	now := time.Now().UTC()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(
			int64(11), int64(3), "ops", "title", "warning", "new", "manual", []byte(`{}`),
			now, now, nil, nil, nil, "192.0.2.10",
		))
	it, err := repo.getIncidentByIDWithExec(ctx, db, 11)
	if err != nil {
		t.Fatalf("getIncidentByIDWithExec success: %v", err)
	}
	if it == nil || it.ID != 11 || it.DeviceID == nil || *it.DeviceID != 3 || it.Assignee == nil || *it.Assignee != "ops" {
		t.Fatalf("unexpected incident payload: %+v", it)
	}
	if it.DeviceIP == nil || *it.DeviceIP != "192.0.2.10" {
		t.Fatalf("unexpected device_ip: %+v", it.DeviceIP)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListIncidentsPageWithExec(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if _, err := repo.listIncidentsPageWithExec(ctx, db, 100, nil, "bad", "", nil, nil); err == nil {
		t.Fatalf("expected invalid status error")
	}

	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT i\.id, i\.device_id, i\.assignee, i\.title, i\.severity, i\.status, i\.source, i\.details`).
		WithArgs(201).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(1), nil, nil, "a", "warning", "new", "manual", []byte(`{}`), now, now, nil, nil, nil, nil))
	page, err := repo.listIncidentsPageWithExec(ctx, db, 200, nil, "", "", nil, nil)
	if err != nil {
		t.Fatalf("listIncidentsPageWithExec: %v", err)
	}
	if page == nil || len(page.Items) != 1 || page.More {
		t.Fatalf("unexpected page: %+v", page)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListIncidentTransitions(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	if _, err := repo.ListIncidentTransitions(ctx, 0, 10); err == nil {
		t.Fatalf("expected error for invalid incident id")
	}

	now := time.Now().UTC()
	mock.ExpectQuery(`FROM incident_transitions`).
		WithArgs(int64(7), 100).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "incident_id", "from_status", "to_status", "changed_by", "comment", "changed_at",
		}).AddRow(int64(1), int64(7), "new", "acknowledged", "system", "ok", now))

	items, err := repo.ListIncidentTransitions(ctx, 7, -1)
	if err != nil {
		t.Fatalf("ListIncidentTransitions: %v", err)
	}
	if len(items) != 1 || items[0].IncidentID != 7 {
		t.Fatalf("unexpected transitions: %+v", items)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

