package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func httptestRequest(remoteAddr string) *http.Request {
	r := httptest.NewRequest("GET", "http://example.local/", nil)
	r.RemoteAddr = remoteAddr
	return r
}

func TestLoginLimiter_BlocksByUserAfterThreshold(t *testing.T) {
	l := newLoginLimiter()
	ip := "10.0.0.1"
	user := "admin"
	now := time.Now()

	for i := 0; i < loginMaxAttemptsUser; i++ {
		l.onFailure(ip, user, now.Add(time.Duration(i)*time.Second))
	}

	allowed, retry := l.check(ip, user, now.Add(11*time.Second))
	if allowed {
		t.Fatalf("expected user to be blocked after threshold")
	}
	if retry <= 0 {
		t.Fatalf("expected positive retry duration")
	}
}

func TestLoginLimiter_SuccessResetsState(t *testing.T) {
	l := newLoginLimiter()
	ip := "10.0.0.2"
	user := "viewer"
	now := time.Now()

	l.onFailure(ip, user, now)
	l.onFailure(ip, user, now.Add(1*time.Second))
	l.onSuccess(ip, user)

	allowed, retry := l.check(ip, user, now.Add(2*time.Second))
	if !allowed {
		t.Fatalf("expected allowed after success reset, retry=%s", retry)
	}
}

func TestLoginLimiter_WindowExpiryResetsCounters(t *testing.T) {
	l := newLoginLimiter()
	ip := "10.0.0.3"
	user := "ops"
	now := time.Now()

	for i := 0; i < loginMaxAttemptsUser-1; i++ {
		l.onFailure(ip, user, now.Add(time.Duration(i)*time.Second))
	}

	afterWindow := now.Add(loginWindow + time.Minute)
	allowed, _ := l.check(ip, user, afterWindow)
	if !allowed {
		t.Fatalf("expected allowed after window expiration")
	}
}

func TestClientIPPriority(t *testing.T) {
	r := httptestRequest("203.0.113.20:12345")
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 198.51.100.2")
	if got := clientIP(r); got != "198.51.100.1" {
		t.Fatalf("expected first X-Forwarded-For IP, got %q", got)
	}

	r2 := httptestRequest("203.0.113.20:12345")
	r2.Header.Set("X-Real-IP", "198.51.100.9")
	if got := clientIP(r2); got != "198.51.100.9" {
		t.Fatalf("expected X-Real-IP, got %q", got)
	}

	r3 := httptestRequest("203.0.113.20:12345")
	if got := clientIP(r3); got != "203.0.113.20" {
		t.Fatalf("expected RemoteAddr host, got %q", got)
	}
}

