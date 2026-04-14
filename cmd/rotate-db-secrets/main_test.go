package main

import "testing"

func TestValidateRotateEnv(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		oldKey  string
		newKey  string
		wantErr bool
	}{
		{name: "ok", dsn: "host=localhost", oldKey: "old", newKey: "new", wantErr: false},
		{name: "missing dsn", dsn: "", oldKey: "old", newKey: "new", wantErr: true},
		{name: "missing old", dsn: "host=localhost", oldKey: "", newKey: "new", wantErr: true},
		{name: "missing new", dsn: "host=localhost", oldKey: "old", newKey: "", wantErr: true},
		{name: "same keys", dsn: "host=localhost", oldKey: "same", newKey: "same", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRotateEnv(tc.dsn, tc.oldKey, tc.newKey)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateRotateEnv() error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestRotationMode(t *testing.T) {
	if got := rotationMode(false); got != "applied" {
		t.Fatalf("rotationMode(false)=%q want applied", got)
	}
	if got := rotationMode(true); got != "dry-run" {
		t.Fatalf("rotationMode(true)=%q want dry-run", got)
	}
}
