package postgres

import (
	"database/sql"
	"testing"
)

func TestNormalizeIncidentSeverity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "warning"},
		{in: "warning", want: "warning"},
		{in: "CRITICAL", want: "critical"},
		{in: "info", want: "info"},
		{in: "bad", wantErr: true},
	}
	for _, tc := range cases {
		got, err := normalizeIncidentSeverity(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeIncidentSeverity(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeIncidentSeverity(%q): unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeIncidentSeverity(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeIncidentSourceAndStatus(t *testing.T) {
	t.Parallel()
	if got, err := normalizeIncidentSource("TRAP"); err != nil || got != "trap" {
		t.Fatalf("normalizeIncidentSource trap: got=%q err=%v", got, err)
	}
	if got, err := normalizeIncidentSource(""); err != nil || got != "manual" {
		t.Fatalf("normalizeIncidentSource empty: got=%q err=%v", got, err)
	}
	if _, err := normalizeIncidentSource("x"); err == nil {
		t.Fatalf("normalizeIncidentSource invalid: expected error")
	}

	if got, err := normalizeIncidentStatus("RESOLVED"); err != nil || got != "resolved" {
		t.Fatalf("normalizeIncidentStatus resolved: got=%q err=%v", got, err)
	}
	if _, err := normalizeIncidentStatus("x"); err == nil {
		t.Fatalf("normalizeIncidentStatus invalid: expected error")
	}
}

func TestIncidentDedupLockKeyStableAndDistinct(t *testing.T) {
	t.Parallel()
	base := incidentDedupLockKey(sql.NullInt64{Valid: true, Int64: 42}, "Down", "critical", "polling")
	same := incidentDedupLockKey(sql.NullInt64{Valid: true, Int64: 42}, " down ", "CRITICAL", "POLLING")
	other := incidentDedupLockKey(sql.NullInt64{Valid: true, Int64: 43}, "Down", "critical", "polling")
	if base != same {
		t.Fatalf("expected normalized inputs to have same key: %d != %d", base, same)
	}
	if base == other {
		t.Fatalf("expected different device id to change key")
	}
}

func TestDefaultIncidentAssigneePriority(t *testing.T) {
	t.Setenv("NMS_INCIDENT_ASSIGNEE_CRITICAL", "crit")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_TRAP", "trap")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_POLLING", "poll")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_MANUAL", "manual")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_DEFAULT", "global")

	if got := defaultIncidentAssignee("trap", "critical"); got == nil || *got != "crit" {
		t.Fatalf("critical priority mismatch: %v", got)
	}
	t.Setenv("NMS_INCIDENT_ASSIGNEE_CRITICAL", "")
	if got := defaultIncidentAssignee("trap", "warning"); got == nil || *got != "trap" {
		t.Fatalf("source trap priority mismatch: %v", got)
	}
	t.Setenv("NMS_INCIDENT_ASSIGNEE_TRAP", "")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_POLLING", "")
	t.Setenv("NMS_INCIDENT_ASSIGNEE_MANUAL", "")
	if got := defaultIncidentAssignee("manual", "warning"); got == nil || *got != "global" {
		t.Fatalf("global fallback mismatch: %v", got)
	}
}

func TestSettingsClampHelpersAndPollStatus(t *testing.T) {
	t.Parallel()
	if got := clampWorkerPollIntervalSec(1); got != MinWorkerPollIntervalSeconds {
		t.Fatalf("clampWorkerPollIntervalSec min: %d", got)
	}
	if got := clampWorkerPollIntervalSec(MaxWorkerPollIntervalSeconds + 1); got != MaxWorkerPollIntervalSeconds {
		t.Fatalf("clampWorkerPollIntervalSec max: %d", got)
	}
	if got := clampSNMPTimeoutSeconds(0); got != MinSNMPTimeoutSeconds {
		t.Fatalf("clampSNMPTimeoutSeconds min: %d", got)
	}
	if got := clampSNMPRetries(MaxSNMPRetries + 1); got != MaxSNMPRetries {
		t.Fatalf("clampSNMPRetries max: %d", got)
	}
	if !pollStatusWasOK("") || !pollStatusWasOK(" active ") {
		t.Fatalf("pollStatusWasOK should accept empty/active")
	}
	if pollStatusWasOK("failed") {
		t.Fatalf("pollStatusWasOK should reject failed")
	}
	if !pollStatusWasFailure(" failed_timeout ") || pollStatusWasFailure("active") {
		t.Fatalf("pollStatusWasFailure mismatch")
	}
}
