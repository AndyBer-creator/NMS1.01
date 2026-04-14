package postgres

import (
	"database/sql"
	"testing"
)

func TestRotateSecretValue_ReencryptsExistingCiphertext(t *testing.T) {
	oldP, err := newSecretProtector("old-key")
	if err != nil {
		t.Fatalf("new old protector: %v", err)
	}
	newP, err := newSecretProtector("new-key")
	if err != nil {
		t.Fatalf("new new protector: %v", err)
	}
	oldCipher, err := oldP.encrypt("secret-value")
	if err != nil {
		t.Fatalf("old encrypt: %v", err)
	}

	plainOut, encOut, changed, err := rotateSecretValue(oldP, newP, "", sql.NullString{String: oldCipher, Valid: true})
	if err != nil {
		t.Fatalf("rotateSecretValue: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for re-encrypted value")
	}
	if plainOut.Valid {
		t.Fatal("plaintext column must be empty when encryption is enabled")
	}
	if !encOut.Valid || encOut.String == "" {
		t.Fatal("encrypted column must be set")
	}
	if encOut.String == oldCipher {
		t.Fatal("ciphertext must change after rotation")
	}
	got, err := newP.decrypt(encOut.String)
	if err != nil {
		t.Fatalf("new decrypt: %v", err)
	}
	if got != "secret-value" {
		t.Fatalf("unexpected decrypted value: %q", got)
	}
}

func TestRotateSecretValue_EncryptsLegacyPlaintext(t *testing.T) {
	oldP, err := newSecretProtector("old-key")
	if err != nil {
		t.Fatalf("new old protector: %v", err)
	}
	newP, err := newSecretProtector("new-key")
	if err != nil {
		t.Fatalf("new new protector: %v", err)
	}

	plainOut, encOut, changed, err := rotateSecretValue(oldP, newP, "legacy-secret", sql.NullString{})
	if err != nil {
		t.Fatalf("rotateSecretValue: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for legacy plaintext")
	}
	if plainOut.Valid {
		t.Fatal("legacy plaintext must be moved to encrypted column")
	}
	if !encOut.Valid {
		t.Fatal("encrypted column must be filled")
	}
	got, err := newP.decrypt(encOut.String)
	if err != nil {
		t.Fatalf("new decrypt: %v", err)
	}
	if got != "legacy-secret" {
		t.Fatalf("unexpected decrypted value: %q", got)
	}
}
