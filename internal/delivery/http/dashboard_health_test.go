package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseURLOrEmpty(t *testing.T) {
	if got := parseURLOrEmpty(""); got != nil {
		t.Fatalf("expected nil for empty URL, got %v", got)
	}
	if got := parseURLOrEmpty("not-a-url"); got != nil {
		t.Fatalf("expected nil for invalid URL, got %v", got)
	}
	if got := parseURLOrEmpty("http://example.com"); got == nil {
		t.Fatal("expected non-nil URL")
	}
}

func TestProbeExternalEndpoint_Statuses(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okSrv.Close()
	degradedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer degradedSrv.Close()

	h := &Handlers{httpClient: &http.Client{Timeout: 500 * time.Millisecond}}
	ctx := context.Background()
	if got := h.probeExternalEndpoint(ctx, ""); got != "not_configured" {
		t.Fatalf("empty URL status=%q", got)
	}
	if got := h.probeExternalEndpoint(ctx, okSrv.URL); got != "up" {
		t.Fatalf("ok URL status=%q", got)
	}
	if got := h.probeExternalEndpoint(ctx, degradedSrv.URL); got != "degraded" {
		t.Fatalf("degraded URL status=%q", got)
	}
	if got := h.probeExternalEndpoint(ctx, "http://127.0.0.1:1"); got != "down" {
		t.Fatalf("down URL status=%q", got)
	}
}
