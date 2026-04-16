package http

import (
	"NMS1/internal/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeInboundIncidentStatus(t *testing.T) {
	cases := map[string]string{
		"open":        "new",
		"ACK":         "acknowledged",
		"in-progress": "in_progress",
		"working":     "in_progress",
		"resolve":     "resolved",
		"close":       "closed",
		"closed":      "closed",
		"unknown":     "unknown",
		"":            "",
	}
	for in, want := range cases {
		if got := normalizeInboundIncidentStatus(in); got != want {
			t.Fatalf("normalizeInboundIncidentStatus(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRequestITSMToken(t *testing.T) {
	r := httptest.NewRequest("POST", "/itsm/inbound", nil)
	r.Header.Set("Authorization", "Bearer abc123")
	if got := requestITSMToken(r); got != "abc123" {
		t.Fatalf("Bearer token: got %q", got)
	}
	r2 := httptest.NewRequest("POST", "/itsm/inbound", nil)
	r2.Header.Set("X-ITSM-Token", "xyz")
	if got := requestITSMToken(r2); got != "xyz" {
		t.Fatalf("X-ITSM-Token: got %q", got)
	}
}

func TestApplyITSMInboundMapping(t *testing.T) {
	m := &domain.ITSMInboundMapping{
		MappedStatus:   "ack",
		MappedAssignee: "noc-l2",
	}
	status, assignee := applyITSMInboundMapping("", "", m)
	if status != "acknowledged" {
		t.Fatalf("status=%q", status)
	}
	if assignee != "noc-l2" {
		t.Fatalf("assignee=%q", assignee)
	}
	// direct values should win over mapping values
	status, assignee = applyITSMInboundMapping("resolved", "alice", m)
	if status != "resolved" || assignee != "alice" {
		t.Fatalf("unexpected override status=%q assignee=%q", status, assignee)
	}
}

func TestDecodeAndResolveITSMInbound(t *testing.T) {
	body := `{"incident_id":42,"status":"open","priority":"p1"}`
	req := httptest.NewRequest(http.MethodPost, "/itsm/inbound/dry-run", strings.NewReader(body))
	rr := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer test-token")
	t.Setenv("NMS_ITSM_INBOUND_TOKEN", "test-token")
	resolved, code, msg := decodeAndResolveITSMInbound(rr, req, func(_ context.Context, provider, status, priority, owner string) (*domain.ITSMInboundMapping, error) {
		return &domain.ITSMInboundMapping{
			ID:             7,
			MappedAssignee: "noc-l2",
		}, nil
	})
	if code != http.StatusOK {
		t.Fatalf("code=%d msg=%s", code, msg)
	}
	if resolved.AppliedMappingID != 7 {
		t.Fatalf("mapping id=%d", resolved.AppliedMappingID)
	}
	if resolved.Status != "new" {
		t.Fatalf("status=%q", resolved.Status)
	}
	if resolved.Assignee != "noc-l2" {
		t.Fatalf("assignee=%q", resolved.Assignee)
	}
}
