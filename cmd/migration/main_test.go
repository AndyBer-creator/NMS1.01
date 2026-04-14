package main

import "testing"

func TestMigrationDSN(t *testing.T) {
	t.Setenv("DB_DSN", "")
	t.Setenv("DB_DSN_FILE", "")
	if _, err := migrationDSN(); err == nil {
		t.Fatal("expected error when DB_DSN is missing")
	}

	t.Setenv("DB_DSN", "host=localhost port=5432 user=u password=p dbname=n sslmode=disable")
	dsn, err := migrationDSN()
	if err != nil {
		t.Fatalf("migrationDSN() unexpected error: %v", err)
	}
	if dsn == "" {
		t.Fatal("migrationDSN() returned empty dsn")
	}
}
