package postgres

import (
	"database/sql"
	"testing"
)

func TestSecretProtector_EncryptDecryptRoundTrip(t *testing.T) {
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "itest-db-key")
	t.Setenv("NMS_DB_ENCRYPTION_KEY_FILE", "")
	p, err := newSecretProtectorFromEnv()
	if err != nil {
		t.Fatalf("newSecretProtectorFromEnv: %v", err)
	}
	plain, enc, err := p.splitSecretForStorage("secret-value")
	if err != nil {
		t.Fatalf("splitSecretForStorage: %v", err)
	}
	if plain.Valid {
		t.Fatal("plain storage must be empty when encryption is enabled")
	}
	if !enc.Valid || enc.String == "" {
		t.Fatal("encrypted payload expected")
	}
	got, err := p.mergeSecretFromStorage("", enc)
	if err != nil {
		t.Fatalf("mergeSecretFromStorage: %v", err)
	}
	if got != "secret-value" {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}

func TestSecretProtector_PlaintextFallbackWhenDisabled(t *testing.T) {
	t.Setenv("NMS_DB_ENCRYPTION_KEY", "")
	t.Setenv("NMS_DB_ENCRYPTION_KEY_FILE", "")
	p, err := newSecretProtectorFromEnv()
	if err != nil {
		t.Fatalf("newSecretProtectorFromEnv: %v", err)
	}
	plain, enc, err := p.splitSecretForStorage("public")
	if err != nil {
		t.Fatalf("splitSecretForStorage: %v", err)
	}
	if !plain.Valid || plain.String != "public" {
		t.Fatalf("expected plaintext storage, got %+v", plain)
	}
	if enc.Valid {
		t.Fatalf("encrypted storage must be empty, got %+v", enc)
	}
	got, err := p.mergeSecretFromStorage(plain.String, sql.NullString{})
	if err != nil {
		t.Fatalf("mergeSecretFromStorage: %v", err)
	}
	if got != "public" {
		t.Fatalf("merge fallback mismatch: got %q", got)
	}
}

func TestSecretProtector_MergeEncryptedWhenDisabled(t *testing.T) {
	p, err := newSecretProtector("")
	if err != nil {
		t.Fatalf("newSecretProtector: %v", err)
	}

	_, err = p.mergeSecretFromStorage("", sql.NullString{String: encryptedSecretPrefix + "AQID", Valid: true})
	if err == nil {
		t.Fatal("expected error for encrypted payload with disabled protector")
	}
}
