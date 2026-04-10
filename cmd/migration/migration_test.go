package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationsDirectoryPresent(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// go test для этого пакета выполняется из cmd/migration
	dir := filepath.Join(wd, "..", "..", "migrations")
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("migrations dir: %v", err)
	}
	if !st.IsDir() {
		t.Fatal("migrations is not a directory")
	}
}
