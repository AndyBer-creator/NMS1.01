package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSafeNext(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
		{"/devices", "/devices"},
		{"/devices?tab=all", "/devices?tab=all"},
		{"https://evil.example/path", "/"},
		{"//evil.example/path", "/"},
		{"not-a-path", "/"},
	}

	for _, tc := range cases {
		got := safeNext(tc.in)
		if got != tc.want {
			t.Fatalf("safeNext(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPrefersJSONAPI(t *testing.T) {
	rJSON := httptest.NewRequest(http.MethodGet, "/", nil)
	rJSON.Header.Set("Accept", "application/json")
	if !prefersJSONAPI(rJSON) {
		t.Fatalf("expected JSON API preference for application/json")
	}

	rHTML := httptest.NewRequest(http.MethodGet, "/", nil)
	rHTML.Header.Set("Accept", "application/json,text/html")
	if prefersJSONAPI(rHTML) {
		t.Fatalf("expected HTML mixed accept to disable JSON API preference")
	}
}

func TestIsUnsafeMethod(t *testing.T) {
	if !isUnsafeMethod(http.MethodPost) || !isUnsafeMethod(http.MethodDelete) {
		t.Fatalf("expected mutating methods to be unsafe")
	}
	if isUnsafeMethod(http.MethodGet) || isUnsafeMethod(http.MethodHead) {
		t.Fatalf("expected GET/HEAD to be safe")
	}
}

func TestHTTPSHelpers(t *testing.T) {
	rHTTP := httptest.NewRequest(http.MethodGet, "http://example.local/path", nil)
	if isHTTPSRequest(rHTTP) {
		t.Fatalf("plain HTTP request must not be treated as HTTPS")
	}

	rForwarded := httptest.NewRequest(http.MethodGet, "http://example.local/path", nil)
	rForwarded.Header.Set("X-Forwarded-Proto", "https")
	rForwarded.RemoteAddr = "127.0.0.1:12345"
	if !isHTTPSRequest(rForwarded) {
		t.Fatalf("forwarded https request must be treated as HTTPS")
	}

	if !isHTTPSBypassPath("/health") || !isHTTPSBypassPath("/metrics") || isHTTPSBypassPath("/") {
		t.Fatalf("bypass path matching unexpected")
	}
}

func TestHTTPSOnlyEnabled(t *testing.T) {
	key := "NMS_ENFORCE_HTTPS"
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		_ = os.Unsetenv(key)
	})

	if httpsOnlyEnabled() {
		t.Fatalf("expected disabled by default")
	}
	if err := os.Setenv(key, "true"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if !httpsOnlyEnabled() {
		t.Fatalf("expected enabled for true")
	}
}

func TestEnforceHTTPS_RedirectAndBypass(t *testing.T) {
	key := "NMS_ENFORCE_HTTPS"
	if err := os.Setenv(key, "true"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv(key)
	})

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	h := EnforceHTTPS(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://nms.local/login", nil)
	req.Host = "nms.local"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected 308 redirect, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "https://nms.local/login" {
		t.Fatalf("unexpected redirect location: %q", loc)
	}
	if nextCalled {
		t.Fatalf("next handler should not be called on redirect")
	}

	nextCalled = false
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://nms.local/health", nil)
	req2.Host = "nms.local"
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK || !nextCalled {
		t.Fatalf("expected bypass path to call next with 200")
	}

	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "http://nms.local/login?next=%2Fdevices%3Ftab%3Dall", nil)
	req3.Host = "nms.local"
	h.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected 308 redirect with query, got %d", rr3.Code)
	}
	if loc := rr3.Header().Get("Location"); loc != "https://nms.local/login?next=%2Fdevices%3Ftab%3Dall" {
		t.Fatalf("expected query-preserving redirect location, got %q", loc)
	}
}

