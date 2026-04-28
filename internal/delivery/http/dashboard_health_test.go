package http

import (
	"context"
	"io"
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
	if got := h.probeExternalEndpoint(ctx, okSrv.URL); got != "not_configured" {
		t.Fatalf("localhost URL should be blocked, got=%q", got)
	}
	if got := h.probeExternalEndpoint(ctx, degradedSrv.URL); got != "not_configured" {
		t.Fatalf("localhost degraded URL should be blocked, got=%q", got)
	}
	if got := h.probeExternalEndpoint(ctx, "http://127.0.0.1:1"); got != "not_configured" {
		t.Fatalf("local IP should be blocked, got=%q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestExternalEndpointAllowed_IPPolicies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want bool
	}{
		{raw: "http://1.1.1.1", want: true},
		{raw: "https://8.8.8.8", want: true},
		{raw: "http://127.0.0.1", want: false},
		{raw: "http://10.1.2.3", want: false},
		{raw: "http://169.254.169.254", want: false},
		{raw: "http://[::1]", want: false},
		{raw: "ftp://1.1.1.1", want: false},
	}
	for _, tc := range cases {
		u := parseURLOrEmpty(tc.raw)
		got := externalEndpointAllowed(u)
		if got != tc.want {
			t.Fatalf("externalEndpointAllowed(%q): got=%t want=%t", tc.raw, got, tc.want)
		}
	}
}

func TestProbeExternalEndpoint_UsesClientAndMapsStatuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	targetURL := "http://1.1.1.1"

	upHandler := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(http.NoBody),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	degradedHandler := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(http.NoBody),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	downHandler := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})

	up := &Handlers{httpClient: &http.Client{Timeout: 500 * time.Millisecond, Transport: upHandler}}
	if got := up.probeExternalEndpoint(ctx, targetURL); got != "up" {
		t.Fatalf("probe up: got=%q", got)
	}

	degraded := &Handlers{httpClient: &http.Client{Timeout: 500 * time.Millisecond, Transport: degradedHandler}}
	if got := degraded.probeExternalEndpoint(ctx, targetURL); got != "degraded" {
		t.Fatalf("probe degraded: got=%q", got)
	}

	down := &Handlers{httpClient: &http.Client{Timeout: 500 * time.Millisecond, Transport: downHandler}}
	if got := down.probeExternalEndpoint(ctx, targetURL); got != "down" {
		t.Fatalf("probe down: got=%q", got)
	}
}
