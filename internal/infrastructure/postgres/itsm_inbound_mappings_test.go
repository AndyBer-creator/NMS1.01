package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"NMS1/internal/domain"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeITSMMappingHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeITSMProvider("  "); got != "generic" {
		t.Fatalf("expected generic provider, got %q", got)
	}
	if got := normalizeITSMProvider(" ServiceNow "); got != "servicenow" {
		t.Fatalf("expected lowercased provider, got %q", got)
	}

	if got, err := normalizeMappedIncidentStatus(" resolved "); err != nil || got != "resolved" {
		t.Fatalf("normalizeMappedIncidentStatus: got=%q err=%v", got, err)
	}
	if _, err := normalizeMappedIncidentStatus("bad"); err == nil {
		t.Fatalf("expected invalid mapped status error")
	}
}

func TestITSMInboundMappingsCRUDAndResolve(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("list mappings with filters", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		enabled := true
		mock.ExpectQuery(`FROM itsm_inbound_mappings WHERE lower\(provider\) = \$1 AND enabled = \$2 ORDER BY priority ASC, id ASC`).
			WithArgs("jira", true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "provider", "external_status", "external_priority", "external_owner",
				"mapped_status", "mapped_assignee", "enabled", "priority", "created_at",
			}).AddRow(1, "jira", "open", "p1", "team-a", "new", "noc", true, 10, now))

		items, err := repo.ListITSMInboundMappings(ctx, " Jira ", &enabled)
		if err != nil {
			t.Fatalf("ListITSMInboundMappings: %v", err)
		}
		if len(items) != 1 || items[0].Provider != "jira" {
			t.Fatalf("unexpected items: %+v", items)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("create mapping validates and inserts defaults", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		if _, err := repo.CreateITSMInboundMapping(ctx, nil); err == nil {
			t.Fatalf("expected nil input validation error")
		}
		if _, err := repo.CreateITSMInboundMapping(ctx, &domain.ITSMInboundMapping{}); err == nil {
			t.Fatalf("expected empty mapping validation error")
		}

		mock.ExpectQuery(`INSERT INTO itsm_inbound_mappings`).
			WithArgs("generic", "open", "p1", "team-a", "acknowledged", "noc", true, 100).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "provider", "external_status", "external_priority", "external_owner",
				"mapped_status", "mapped_assignee", "enabled", "priority", "created_at",
			}).AddRow(2, "generic", "open", "p1", "team-a", "acknowledged", "noc", true, 100, now))

		out, err := repo.CreateITSMInboundMapping(ctx, &domain.ITSMInboundMapping{
			ExternalStatus:   " OPEN ",
			ExternalPriority: " P1 ",
			ExternalOwner:    " Team-A ",
			MappedStatus:     " acknowledged ",
			MappedAssignee:   " noc ",
			Enabled:          true,
		})
		if err != nil {
			t.Fatalf("CreateITSMInboundMapping: %v", err)
		}
		if out == nil || out.ID != 2 || out.Priority != 100 {
			t.Fatalf("unexpected mapping: %+v", out)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("update mapping handles missing row and success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		in := &domain.ITSMInboundMapping{
			Provider:         "ServiceNow",
			ExternalStatus:   "In Progress",
			ExternalPriority: "P2",
			ExternalOwner:    "ops",
			MappedStatus:     "in_progress",
			MappedAssignee:   "duty",
			Enabled:          false,
			Priority:         20,
		}

		mock.ExpectQuery(`UPDATE itsm_inbound_mappings`).
			WithArgs("servicenow", "in progress", "p2", "ops", "in_progress", "duty", false, 20, int64(5)).
			WillReturnError(sql.ErrNoRows)
		out, err := repo.UpdateITSMInboundMapping(ctx, 5, in)
		if err != nil || out != nil {
			t.Fatalf("expected nil,nil for missing row, got out=%v err=%v", out, err)
		}

		mock.ExpectQuery(`UPDATE itsm_inbound_mappings`).
			WithArgs("servicenow", "in progress", "p2", "ops", "in_progress", "duty", false, 20, int64(6)).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "provider", "external_status", "external_priority", "external_owner",
				"mapped_status", "mapped_assignee", "enabled", "priority", "created_at",
			}).AddRow(6, "servicenow", "in progress", "p2", "ops", "in_progress", "duty", false, 20, now))
		out, err = repo.UpdateITSMInboundMapping(ctx, 6, in)
		if err != nil || out == nil || out.ID != 6 {
			t.Fatalf("UpdateITSMInboundMapping: out=%+v err=%v", out, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("delete and resolve mapping", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := &Repo{db: db}
		mock.ExpectExec(`DELETE FROM itsm_inbound_mappings WHERE id = \$1`).
			WithArgs(int64(9)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		deleted, err := repo.DeleteITSMInboundMapping(ctx, 9)
		if err != nil || !deleted {
			t.Fatalf("DeleteITSMInboundMapping: deleted=%v err=%v", deleted, err)
		}

		mock.ExpectQuery(`SELECT id, provider, external_status, external_priority, external_owner`).
			WithArgs("jira", "open", "p1", "team-a").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "provider", "external_status", "external_priority", "external_owner",
				"mapped_status", "mapped_assignee", "enabled", "priority", "created_at",
			}).AddRow(11, "jira", "open", "p1", "team-a", "new", "noc", true, 10, now))
		resolved, err := repo.ResolveITSMInboundMapping(ctx, "Jira", " OPEN ", " P1 ", " Team-A ")
		if err != nil || resolved == nil || resolved.ID != 11 {
			t.Fatalf("ResolveITSMInboundMapping: resolved=%+v err=%v", resolved, err)
		}

		mock.ExpectQuery(`SELECT id, provider, external_status, external_priority, external_owner`).
			WithArgs("generic", "", "", "").
			WillReturnError(sql.ErrNoRows)
		resolved, err = repo.ResolveITSMInboundMapping(ctx, "", "", "", "")
		if err != nil || resolved != nil {
			t.Fatalf("expected nil,nil for missing mapping, got resolved=%v err=%v", resolved, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})
}
