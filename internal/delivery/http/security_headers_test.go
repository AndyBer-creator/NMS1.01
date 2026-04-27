package http

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestSecurityHeaders_Baseline(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeaders(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options: got %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options: got %q", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy: got %q", got)
	}
	if got := rr.Header().Get("Permissions-Policy"); got == "" {
		t.Fatal("expected Permissions-Policy")
	}
	if got := rr.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("expected Content-Security-Policy")
	}
	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("plain HTTP must not set HSTS, got %q", got)
	}
}

func TestSecurityHeaders_HSTSWhenForwardedHTTPS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeaders(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "127.0.0.1:12345"
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("expected HSTS when request is forwarded as HTTPS")
	}
}

func TestSecurityHeaders_CSPNonceIsURLSafe(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeaders(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	re := regexp.MustCompile(`nonce-([A-Za-z0-9_-]+)'`)
	m := re.FindStringSubmatch(csp)
	if len(m) < 2 {
		t.Fatalf("nonce not found in CSP: %q", csp)
	}
	if nonce := m[1]; len(nonce) == 0 {
		t.Fatalf("nonce must not be empty: %q", csp)
	}
}
