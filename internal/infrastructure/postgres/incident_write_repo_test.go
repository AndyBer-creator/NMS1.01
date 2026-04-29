package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCreateOrTouchOpenIncident_TouchPath(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE incidents i`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(77)))
	mock.ExpectCommit()

	now := time.Now().UTC()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(77)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(77), nil, nil, "t", "warning", "new", "manual", []byte(`{}`), now, now, nil, nil, nil, nil))

	it, created, err := repo.CreateOrTouchOpenIncident(ctx, nil, "t", "warning", "manual", nil, 10*time.Second)
	if err != nil {
		t.Fatalf("CreateOrTouchOpenIncident: %v", err)
	}
	if it == nil || it.ID != 77 {
		t.Fatalf("unexpected incident: %+v", it)
	}
	if created {
		t.Fatalf("touch path should not report created=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCreateOrTouchOpenIncident_CreatePath(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE incidents i`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO incidents .* RETURNING id`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(88)))
	mock.ExpectCommit()

	now := time.Now().UTC()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(88)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(88), nil, nil, "t", "warning", "new", "manual", []byte(`{}`), now, now, nil, nil, nil, nil))

	it, created, err := repo.CreateOrTouchOpenIncident(ctx, nil, "t", "warning", "manual", nil, 10*time.Second)
	if err != nil {
		t.Fatalf("CreateOrTouchOpenIncident: %v", err)
	}
	if it == nil || it.ID != 88 {
		t.Fatalf("unexpected incident: %+v", it)
	}
	if !created {
		t.Fatalf("create path should report created=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTransitionIncidentStatus(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repo{db: db}
	ctx := context.Background()

	if _, err := repo.TransitionIncidentStatus(ctx, 0, "resolved", "", ""); err == nil {
		t.Fatalf("expected error for invalid incident id")
	}
	if _, err := repo.TransitionIncidentStatus(ctx, 1, "bad", "", ""); err == nil {
		t.Fatalf("expected error for invalid target status")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM incidents WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("new"))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("acknowledged", int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(1), "new", "acknowledged", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	now := time.Now().UTC()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(1), nil, nil, "t", "warning", "acknowledged", "manual", []byte(`{}`), now, now, now, nil, nil, nil))
	it, err := repo.TransitionIncidentStatus(ctx, 1, "acknowledged", "", "")
	if err != nil || it == nil || it.Status != "acknowledged" {
		t.Fatalf("TransitionIncidentStatus success: it=%+v err=%v", it, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM incidents WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("resolved"))
	mock.ExpectRollback()
	if _, err := repo.TransitionIncidentStatus(ctx, 2, "resolved", "", ""); err == nil {
		t.Fatalf("expected already-in-status error")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM incidents WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("new"))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("acknowledged", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(3), "new", "acknowledged", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("in_progress", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(3), "acknowledged", "in_progress", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("resolved", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(3), "in_progress", "resolved", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("closed", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(3), "resolved", "closed", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(3), nil, nil, "t", "warning", "closed", "manual", []byte(`{}`), now, now, now, now, now, nil))
	it, err = repo.TransitionIncidentStatus(ctx, 3, "closed", "", "")
	if err != nil || it == nil || it.Status != "closed" {
		t.Fatalf("expected multi-step success to closed, got it=%+v err=%v", it, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM incidents WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(4)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()
	it, err = repo.TransitionIncidentStatus(ctx, 4, "resolved", "", "")
	if err != nil || it != nil {
		t.Fatalf("not found path: it=%v err=%v", it, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestAssignIncidentAndApplyITSMInboundUpdate(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repo{db: db}
	ctx := context.Background()
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM incidents WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("new"))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs(sql.NullString{String: "ops", Valid: true}, int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(5), "new", "new", "system", "assigned to ops").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(5), nil, "ops", "t", "warning", "new", "manual", []byte(`{}`), now, now, nil, nil, nil, nil))
	it, err := repo.AssignIncident(ctx, 5, "ops", "", "")
	if err != nil || it == nil || it.Assignee == nil || *it.Assignee != "ops" {
		t.Fatalf("AssignIncident: it=%+v err=%v", it, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, device_id, assignee, title, severity, status, source, details`).
		WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at",
		}).AddRow(int64(6), nil, "old", "t", "warning", "new", "manual", []byte(`{}`), now, now, nil, nil, nil))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("acknowledged", int64(6)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(6), "new", "acknowledged", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs(sql.NullString{String: "new-owner", Valid: true}, int64(6)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(6), "acknowledged", "acknowledged", "system", "assigned to new-owner").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(6), nil, "new-owner", "t", "warning", "acknowledged", "manual", []byte(`{}`), now, now, now, nil, nil, nil))
	it, statusChanged, assigneeChanged, err := repo.ApplyITSMInboundUpdate(ctx, 6, "acknowledged", "new-owner", "", "")
	if err != nil || it == nil || !statusChanged || !assigneeChanged {
		t.Fatalf("ApplyITSMInboundUpdate success: it=%+v statusChanged=%t assigneeChanged=%t err=%v", it, statusChanged, assigneeChanged, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, device_id, assignee, title, severity, status, source, details`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at",
		}).AddRow(int64(7), nil, "old", "t", "warning", "new", "manual", []byte(`{}`), now, now, nil, nil, nil))
	mock.ExpectRollback()
	if _, _, _, err := repo.ApplyITSMInboundUpdate(ctx, 7, "closed", "", "", ""); err == nil {
		t.Fatalf("expected invalid transition error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestEscalateAndBulkTransition(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repo{db: db}
	ctx := context.Background()

	if changed, err := repo.EscalateUnackedIncidentsWithFilter(ctx, 0, "ops", "", "", "", "", false); err != nil || changed != 0 {
		t.Fatalf("olderThan<=0 should no-op: changed=%d err=%v", changed, err)
	}
	if changed, err := repo.EscalateUnackedIncidentsWithFilter(ctx, time.Minute, "", "", "", "", "", false); err != nil || changed != 0 {
		t.Fatalf("empty targetAssignee should no-op: changed=%d err=%v", changed, err)
	}
	if _, err := repo.EscalateUnackedIncidentsWithFilter(ctx, time.Minute, "ops", "", "", "bad", "", false); err == nil {
		t.Fatalf("expected invalid severity error")
	}

	mock.ExpectQuery(`WITH candidates AS \(`).
		WithArgs(float64((2 * time.Minute).Seconds()), "ops", "", "", false, "system-escalation", "auto-escalated: ack timeout reached").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))
	changed, err := repo.EscalateUnackedIncidents(ctx, 2*time.Minute, "ops", "", "")
	if err != nil || changed != 3 {
		t.Fatalf("EscalateUnackedIncidents: changed=%d err=%v", changed, err)
	}

	// TransitionIncidentsStatus deduplicates ids and skips non-positive ones.
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM incidents WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("new"))
	mock.ExpectExec(`UPDATE incidents`).
		WithArgs("acknowledged", int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO incident_transitions`).
		WithArgs(int64(9), "new", "acknowledged", "system", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	now := time.Now().UTC()
	mock.ExpectQuery(`FROM incidents i`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "device_id", "assignee", "title", "severity", "status", "source", "details",
			"created_at", "updated_at", "acknowledged_at", "resolved_at", "closed_at", "ip",
		}).AddRow(int64(9), nil, nil, "t", "warning", "acknowledged", "manual", []byte(`{}`), now, now, now, nil, nil, nil))
	items, err := repo.TransitionIncidentsStatus(ctx, []int64{0, 9, 9}, "acknowledged", "", "")
	if err != nil || len(items) != 1 || items[0].ID != 9 {
		t.Fatalf("TransitionIncidentsStatus: items=%+v err=%v", items, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCreateOrTouchOpenIncident_Errors(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repo{db: db}
	ctx := context.Background()

	if _, _, err := repo.CreateOrTouchOpenIncident(ctx, nil, "", "warning", "manual", nil, 0); err == nil {
		t.Fatalf("expected missing title error")
	}
	if _, _, err := repo.CreateOrTouchOpenIncident(ctx, nil, "t", "bad", "manual", nil, 0); err == nil {
		t.Fatalf("expected invalid severity error")
	}

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE incidents i`).
		WillReturnError(errors.New("boom"))
	mock.ExpectRollback()
	if _, _, err := repo.CreateOrTouchOpenIncident(ctx, nil, "t", "warning", "manual", nil, 0); err == nil {
		t.Fatalf("expected update error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

