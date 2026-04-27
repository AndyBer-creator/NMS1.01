package http

import (
	"NMS1/internal/domain"
	"context"
	"encoding/json"
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

func TestDecodeAndResolveITSMInbound_EmptyStatusUsesMappedStatusForJira(t *testing.T) {
	body := `{"incident_id":42,"provider":"jira","status":"","priority":"","owner":""}`
	req := httptest.NewRequest(http.MethodPost, "/itsm/inbound/dry-run", strings.NewReader(body))
	rr := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer test-token")
	t.Setenv("NMS_ITSM_INBOUND_TOKEN", "test-token")

	resolved, code, msg := decodeAndResolveITSMInbound(rr, req, func(_ context.Context, provider, status, priority, owner string) (*domain.ITSMInboundMapping, error) {
		if provider != "jira" {
			t.Fatalf("expected jira provider, got %q", provider)
		}
		if status != "" || priority != "" || owner != "" {
			t.Fatalf("expected empty mapping keys, got status=%q priority=%q owner=%q", status, priority, owner)
		}
		return &domain.ITSMInboundMapping{
			ID:           101,
			MappedStatus: "in-progress",
		}, nil
	})
	if code != http.StatusOK {
		t.Fatalf("code=%d msg=%s", code, msg)
	}
	if resolved.Status != "in_progress" {
		t.Fatalf("expected mapped normalized status, got %q", resolved.Status)
	}
}

func TestDecodeAndResolveITSMInbound_EmptyStatusUsesMappedStatusForSNOW(t *testing.T) {
	body := `{"incident_id":42,"provider":"snow","status":"","priority":"","owner":"NOC"}`
	req := httptest.NewRequest(http.MethodPost, "/itsm/inbound/dry-run", strings.NewReader(body))
	rr := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer test-token")
	t.Setenv("NMS_ITSM_INBOUND_TOKEN", "test-token")

	resolved, code, msg := decodeAndResolveITSMInbound(rr, req, func(_ context.Context, provider, status, priority, owner string) (*domain.ITSMInboundMapping, error) {
		if provider != "snow" {
			t.Fatalf("expected snow provider, got %q", provider)
		}
		if owner != "NOC" {
			t.Fatalf("expected owner passthrough, got %q", owner)
		}
		return &domain.ITSMInboundMapping{
			ID:           202,
			MappedStatus: "ack",
		}, nil
	})
	if code != http.StatusOK {
		t.Fatalf("code=%d msg=%s", code, msg)
	}
	if resolved.Status != "acknowledged" {
		t.Fatalf("expected mapped normalized status, got %q", resolved.Status)
	}
}

func TestDecodeAndResolveITSMInbound_EmptyStatusAndAssigneeRejectsWhenMappingEmpty(t *testing.T) {
	body := `{"incident_id":42,"provider":"jira","status":"","priority":"","owner":""}`
	req := httptest.NewRequest(http.MethodPost, "/itsm/inbound/dry-run", strings.NewReader(body))
	rr := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer test-token")
	t.Setenv("NMS_ITSM_INBOUND_TOKEN", "test-token")

	_, code, msg := decodeAndResolveITSMInbound(rr, req, func(_ context.Context, provider, status, priority, owner string) (*domain.ITSMInboundMapping, error) {
		return &domain.ITSMInboundMapping{
			ID:             303,
			MappedStatus:   "",
			MappedAssignee: "",
		}, nil
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d (%s)", code, msg)
	}
	if !strings.Contains(msg, "at least one of status or assignee") {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func TestDecodeJSONBodyRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"incident_id":42,"extra":"x"}`))
		rr := httptest.NewRecorder()
		var input itsmInboundRequest
		if err := decodeJSONBody(rr, req, &input); err == nil {
			t.Fatal("expected error for unknown JSON field")
		}
	})

	t.Run("trailing json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"incident_id":42}{"another":1}`))
		rr := httptest.NewRecorder()
		var input itsmInboundRequest
		if err := decodeJSONBody(rr, req, &input); err == nil {
			t.Fatal("expected error for trailing JSON document")
		}
	})
}

func TestITSMInboundDryRunUsesMappedAssignee(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/itsm/inbound/dry-run", strings.NewReader(`{"incident_id":42,"status":"open"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()
	t.Setenv("NMS_ITSM_INBOUND_TOKEN", "test-token")

	resolved, code, msg := decodeAndResolveITSMInbound(rr, req, func(_ context.Context, provider, status, priority, owner string) (*domain.ITSMInboundMapping, error) {
		return &domain.ITSMInboundMapping{ID: 11, MappedAssignee: "mapped-owner"}, nil
	})
	if code != http.StatusOK {
		t.Fatalf("code=%d msg=%s", code, msg)
	}
	body, err := json.Marshal(map[string]any{
		"effective_assignee": resolved.Assignee,
		"applied_mapping_id": resolved.AppliedMappingID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "mapped-owner") {
		t.Fatalf("expected mapped assignee in dry-run output, got %s", body)
	}
}
