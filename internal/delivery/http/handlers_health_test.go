package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlers_Health(t *testing.T) {
	h := &Handlers{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.Health(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	if got := rr.Body.String(); got != "OK" {
		t.Fatalf("body: got %q want OK", got)
	}
}

func TestHandlers_Ready_UnconfiguredRepo(t *testing.T) {
	h := &Handlers{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h.Ready(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("status field: %v", body["status"])
	}
}
