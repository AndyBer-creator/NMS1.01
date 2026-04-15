package http

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetSessionCookie_HTTPNotSecure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://nms.local/login", nil)
	rr := httptest.NewRecorder()

	setSessionCookie(rr, req, "token-value")

	res := rr.Result()
	defer func() { _ = res.Body.Close() }()
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	c := cookies[0]
	if c.Name != sessionCookieName {
		t.Fatalf("cookie name: got %q", c.Name)
	}
	if c.Value != "token-value" {
		t.Fatalf("cookie value: got %q", c.Value)
	}
	if c.Secure {
		t.Fatal("expected Secure=false for plain HTTP request")
	}
	if !c.HttpOnly {
		t.Fatal("expected HttpOnly=true")
	}
}

func TestSetSessionCookie_ForwardedHTTPSIsSecure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://nms.local/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()

	setSessionCookie(rr, req, "token-value")

	res := rr.Result()
	defer func() { _ = res.Body.Close() }()
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	if !cookies[0].Secure {
		t.Fatal("expected Secure=true when forwarded as HTTPS")
	}
}

func TestSetSessionCookie_TLSRequestIsSecure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://nms.local/login", nil)
	req.TLS = &tls.ConnectionState{}
	rr := httptest.NewRecorder()

	setSessionCookie(rr, req, "token-value")

	res := rr.Result()
	defer func() { _ = res.Body.Close() }()
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	if !cookies[0].Secure {
		t.Fatal("expected Secure=true for TLS request")
	}
}

func TestClearSessionCookie_SetsExpiredCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://nms.local/logout", nil)
	rr := httptest.NewRecorder()

	clearSessionCookie(rr, req)

	res := rr.Result()
	defer func() { _ = res.Body.Close() }()
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected clearing cookie")
	}
	c := cookies[0]
	if c.Name != sessionCookieName {
		t.Fatalf("cookie name: got %q", c.Name)
	}
	if c.MaxAge >= 0 {
		t.Fatalf("expected MaxAge<0, got %d", c.MaxAge)
	}
	if c.Value != "" {
		t.Fatalf("expected cleared value, got %q", c.Value)
	}
}

func TestClearSessionCookie_ForwardedHTTPSIsSecure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://nms.local/logout", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()

	clearSessionCookie(rr, req)

	res := rr.Result()
	defer func() { _ = res.Body.Close() }()
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected clearing cookie")
	}
	if !cookies[0].Secure {
		t.Fatal("expected Secure=true for forwarded HTTPS request")
	}
}

