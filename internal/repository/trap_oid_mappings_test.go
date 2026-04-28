package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"NMS1/internal/domain"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeTrapMappingHelpers(t *testing.T) {
	t.Parallel()

	if got, err := normalizeTrapMappingSignalKind(" LINK_DOWN "); err != nil || got != "link_down" {
		t.Fatalf("normalizeTrapMappingSignalKind: got=%q err=%v", got, err)
	}
	if _, err := normalizeTrapMappingSignalKind("bad"); err == nil {
		t.Fatalf("expected invalid signal kind error")
	}

	if got, err := normalizeTrapMappingSeverity(" Warning "); err != nil || got != "warning" {
		t.Fatalf("normalizeTrapMappingSeverity: got=%q err=%v", got, err)
	}
	if _, err := normalizeTrapMappingSeverity("bad"); err == nil {
		t.Fatalf("expected invalid severity error")
	}
}

func TestTrapOIDMappingsCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("list mappings with filters", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := NewTrapsRepo(db)
		enabled := true
		mock.ExpectQuery(`FROM trap_oid_mappings WHERE vendor = \$1 AND enabled = \$2 ORDER BY priority DESC, id ASC`).
			WithArgs("cisco", true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "vendor", "oid_pattern", "signal_kind", "title", "severity", "is_recovery", "priority", "enabled", "created_at",
			}).AddRow(1, "cisco", ".1.3.6.*", "link_down", "Link down", "critical", false, 200, true, now))

		items, err := repo.ListOIDMappings(ctx, " Cisco ", &enabled)
		if err != nil {
			t.Fatalf("ListOIDMappings: %v", err)
		}
		if len(items) != 1 || items[0].Vendor != "cisco" {
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

		repo := NewTrapsRepo(db)
		if _, err := repo.CreateOIDMapping(ctx, nil); err == nil {
			t.Fatalf("expected nil input validation error")
		}

		mock.ExpectQuery(`INSERT INTO trap_oid_mappings`).
			WithArgs("generic", ".1.3.6.1.6.3.1.1.5.3", "link_down", "Link down", "critical", false, 100, true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "vendor", "oid_pattern", "signal_kind", "title", "severity", "is_recovery", "priority", "enabled", "created_at",
			}).AddRow(2, "generic", ".1.3.6.1.6.3.1.1.5.3", "link_down", "Link down", "critical", false, 100, true, now))

		out, err := repo.CreateOIDMapping(ctx, &domain.TrapOIDMapping{
			OIDPattern: ".1.3.6.1.6.3.1.1.5.3",
			SignalKind: "link_down",
			Title:      "Link down",
			Severity:   "critical",
			Enabled:    true,
		})
		if err != nil {
			t.Fatalf("CreateOIDMapping: %v", err)
		}
		if out == nil || out.ID != 2 || out.Priority != 100 || out.Vendor != "generic" {
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

		repo := NewTrapsRepo(db)
		if _, err := repo.UpdateOIDMapping(ctx, 0, &domain.TrapOIDMapping{}); err == nil {
			t.Fatalf("expected id validation error")
		}

		in := &domain.TrapOIDMapping{
			Vendor:     "Cisco",
			OIDPattern: ".1.3.6.1.4.1.*",
			SignalKind: "generic",
			Title:      "Generic trap",
			Severity:   "info",
			IsRecovery: true,
			Priority:   150,
			Enabled:    false,
		}

		mock.ExpectQuery(`UPDATE trap_oid_mappings`).
			WithArgs("cisco", ".1.3.6.1.4.1.*", "generic", "Generic trap", "info", true, 150, false, int64(5)).
			WillReturnError(sql.ErrNoRows)
		out, err := repo.UpdateOIDMapping(ctx, 5, in)
		if err != nil || out != nil {
			t.Fatalf("expected nil,nil for missing row, got out=%v err=%v", out, err)
		}

		mock.ExpectQuery(`UPDATE trap_oid_mappings`).
			WithArgs("cisco", ".1.3.6.1.4.1.*", "generic", "Generic trap", "info", true, 150, false, int64(6)).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "vendor", "oid_pattern", "signal_kind", "title", "severity", "is_recovery", "priority", "enabled", "created_at",
			}).AddRow(6, "cisco", ".1.3.6.1.4.1.*", "generic", "Generic trap", "info", true, 150, false, now))
		out, err = repo.UpdateOIDMapping(ctx, 6, in)
		if err != nil || out == nil || out.ID != 6 {
			t.Fatalf("UpdateOIDMapping: out=%+v err=%v", out, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("delete mapping validates and returns affected flag", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })

		repo := NewTrapsRepo(db)
		if _, err := repo.DeleteOIDMapping(ctx, 0); err == nil {
			t.Fatalf("expected id validation error")
		}

		mock.ExpectExec(`DELETE FROM trap_oid_mappings WHERE id = \$1`).
			WithArgs(int64(9)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		deleted, err := repo.DeleteOIDMapping(ctx, 9)
		if err != nil || !deleted {
			t.Fatalf("DeleteOIDMapping: deleted=%v err=%v", deleted, err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})
}
