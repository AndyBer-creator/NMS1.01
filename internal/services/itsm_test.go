package services

import (
	"NMS1/internal/domain"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestITSMWebhookNotifier_SendIncidentEvent_OK(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization header mismatch: %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	n := &ITSMWebhookNotifier{
		Provider:    "generic",
		WebhookURL:  srv.URL,
		BearerToken: "test-token",
		Timeout:     2 * time.Second,
		MaxRetries:  1,
	}
	err := n.SendIncidentEvent(context.Background(), ITSMIncidentEvent{
		EventType: "incident.created",
		At:        time.Now(),
		Incident: &domain.Incident{
			ID:     42,
			Title:  "port down",
			Status: "new",
		},
	})
	if err != nil {
		t.Fatalf("SendIncidentEvent: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one call, got %d", calls)
	}
}

func TestITSMWebhookNotifier_SendIncidentEvent_ClientErrorNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	n := &ITSMWebhookNotifier{
		Provider:   "generic",
		WebhookURL: srv.URL,
		Timeout:    2 * time.Second,
		MaxRetries: 3,
	}
	err := n.SendIncidentEvent(context.Background(), ITSMIncidentEvent{
		EventType: "incident.updated",
		At:        time.Now(),
		Incident:  &domain.Incident{ID: 1},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if calls != 1 {
		t.Fatalf("expected no retries on 4xx, got calls=%d", calls)
	}
}

func TestITSMWebhookNotifier_JiraPayloadShape(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	n := &ITSMWebhookNotifier{
		Provider:   "jira",
		WebhookURL: srv.URL,
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	}
	err := n.SendIncidentEvent(context.Background(), ITSMIncidentEvent{
		EventType: "incident.created",
		At:        time.Now(),
		Incident: &domain.Incident{
			ID:       99,
			Title:    "uplink down",
			Status:   "new",
			Severity: "critical",
			Source:   "trap",
		},
	})
	if err != nil {
		t.Fatalf("SendIncidentEvent jira: %v", err)
	}
	fields, ok := got["fields"].(map[string]any)
	if !ok {
		t.Fatalf("expected jira fields payload, got %#v", got)
	}
	if fields["summary"] == "" {
		t.Fatalf("expected non-empty jira summary, got %#v", fields)
	}
}

func TestITSMWebhookNotifier_ServiceNowPayloadShape(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	n := &ITSMWebhookNotifier{
		Provider:   "servicenow",
		WebhookURL: srv.URL,
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	}
	err := n.SendIncidentEvent(context.Background(), ITSMIncidentEvent{
		EventType: "incident.status_changed",
		At:        time.Now(),
		Incident: &domain.Incident{
			ID:       100,
			Title:    "device unreachable",
			Status:   "resolved",
			Severity: "warning",
			Source:   "polling",
		},
	})
	if err != nil {
		t.Fatalf("SendIncidentEvent servicenow: %v", err)
	}
	if got["short_description"] == "" {
		t.Fatalf("expected servicenow short_description, got %#v", got)
	}
	if got["u_nms_incident_id"] != "100" {
		t.Fatalf("unexpected u_nms_incident_id: %#v", got["u_nms_incident_id"])
	}
}
