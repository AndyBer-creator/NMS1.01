package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSettingsReadWriteHelpers(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &Repo{db: db, protector: &secretProtector{enabled: false}}
	ctx := context.Background()

	// getIntSettingWithExec success.
	mock.ExpectQuery(`SELECT value FROM nms_settings WHERE key = \$1`).
		WithArgs("k1").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("42"))
	gotInt := repo.getIntSettingWithExec(ctx, db, "k1", 10, clampWorkerPollIntervalSec)
	if gotInt != 42 {
		t.Fatalf("getIntSettingWithExec: got %d want 42", gotInt)
	}

	// getIntSettingWithExec fallback on parse error.
	mock.ExpectQuery(`SELECT value FROM nms_settings WHERE key = \$1`).
		WithArgs("k2").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("abc"))
	gotInt = repo.getIntSettingWithExec(ctx, db, "k2", 3, clampSNMPTimeoutSeconds)
	if gotInt != clampSNMPTimeoutSeconds(3) {
		t.Fatalf("getIntSettingWithExec fallback: got %d", gotInt)
	}

	// setIntSettingWithExec.
	mock.ExpectExec(`INSERT INTO nms_settings`).
		WithArgs("k3", "7").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.setIntSettingWithExec(ctx, db, "k3", 7); err != nil {
		t.Fatalf("setIntSettingWithExec: %v", err)
	}

	// getStringSettingWithExec.
	mock.ExpectQuery(`SELECT value FROM nms_settings WHERE key = \$1`).
		WithArgs("k4").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("  hello  "))
	if got := repo.getStringSettingWithExec(ctx, db, "k4"); got != "hello" {
		t.Fatalf("getStringSettingWithExec: got %q", got)
	}

	// setStringSettingWithExec trims.
	mock.ExpectExec(`INSERT INTO nms_settings`).
		WithArgs("k5", "v").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.setStringSettingWithExec(ctx, db, "k5", "  v  "); err != nil {
		t.Fatalf("setStringSettingWithExec: %v", err)
	}

	// getSecretSettingWithExec plain path.
	mock.ExpectQuery(`SELECT value, value_enc FROM nms_settings WHERE key = \$1`).
		WithArgs("k6").
		WillReturnRows(sqlmock.NewRows([]string{"value", "value_enc"}).AddRow(" secret ", nil))
	sec, err := repo.getSecretSettingWithExec(ctx, db, "k6")
	if err != nil {
		t.Fatalf("getSecretSettingWithExec: %v", err)
	}
	if sec != "secret" {
		t.Fatalf("getSecretSettingWithExec: got %q", sec)
	}

	// hasSecretSettingWithExec true on plaintext.
	mock.ExpectQuery(`SELECT value, value_enc FROM nms_settings WHERE key = \$1`).
		WithArgs("k7").
		WillReturnRows(sqlmock.NewRows([]string{"value", "value_enc"}).AddRow("x", nil))
	if !repo.hasSecretSettingWithExec(ctx, db, "k7") {
		t.Fatalf("hasSecretSettingWithExec expected true")
	}

	// setSecretSettingWithExec plain path when encryption disabled.
	mock.ExpectExec(`INSERT INTO nms_settings`).
		WithArgs("k8", "plain", sql.NullString{}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.setSecretSettingWithExec(ctx, db, "k8", " plain "); err != nil {
		t.Fatalf("setSecretSettingWithExec plain: %v", err)
	}

	encProtector, err := newSecretProtector("test-key")
	if err != nil {
		t.Fatalf("newSecretProtector: %v", err)
	}
	repo.protector = encProtector

	// setSecretSettingWithExec encrypted path.
	mock.ExpectExec(`INSERT INTO nms_settings`).
		WithArgs("k9", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.setSecretSettingWithExec(ctx, db, "k9", "secret"); err != nil {
		t.Fatalf("setSecretSettingWithExec encrypted: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSetSecretSettingWithExec_EncryptedArgLooksVersioned(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	protector, err := newSecretProtector("k")
	if err != nil {
		t.Fatalf("newSecretProtector: %v", err)
	}
	repo := &Repo{db: db, protector: protector}

	mock.ExpectExec(`INSERT INTO nms_settings`).
		WithArgs("token", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.setSecretSettingWithExec(context.Background(), db, "token", "value"); err != nil {
		t.Fatalf("setSecretSettingWithExec: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}

	// Minimal check that prefix contract still used by encrypt().
	ciphertext, err := protector.encrypt("abc")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(ciphertext, encryptedSecretPrefix) {
		t.Fatalf("encrypted payload should have prefix %q: %q", encryptedSecretPrefix, ciphertext)
	}
}

