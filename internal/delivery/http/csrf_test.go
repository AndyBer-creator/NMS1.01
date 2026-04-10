package http

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFTokenContext(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if csrfTokenFromContext(r) != "" {
		t.Fatal("empty context should yield no token")
	}
	tok := "ctx-token"
	r2 := withCSRFToken(r, tok)
	if csrfTokenFromContext(r2) != tok {
		t.Fatalf("context token mismatch")
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	a, err := generateCSRFToken()
	if err != nil || a == "" {
		t.Fatalf("token a: err=%v len=%d", err, len(a))
	}
	b, err := generateCSRFToken()
	if err != nil || b == "" || a == b {
		t.Fatalf("expected distinct tokens")
	}
}

func TestEnsureCSRFCookie_SetsWhenMissing(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	tok, err := ensureCSRFCookie(w, r)
	if err != nil || tok == "" {
		t.Fatalf("ensureCSRFCookie: %v token=%q", err, tok)
	}
	resp := w.Result()
	_ = resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != csrfCookieName || cookies[0].Value != tok {
		t.Fatalf("unexpected Set-Cookie: %#v", cookies)
	}
}

func TestEnsureCSRFCookie_ReusesExisting(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "existing"})
	tok, err := ensureCSRFCookie(w, r)
	if err != nil || tok != "existing" {
		t.Fatalf("got %q err=%v", tok, err)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("should not set a new cookie when one is present")
	}
}

func TestEnsureCSRFCookie_SecureWhenForwardedHTTPS(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	_, err := ensureCSRFCookie(w, r)
	if err != nil {
		t.Fatalf("ensureCSRFCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("expected Secure cookie, got %#v", cookies)
	}
}

func TestEnsureCSRFCookie_SecureWhenTLSRequest(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://nms.local/", nil)
	r.TLS = &tls.ConnectionState{}
	_, err := ensureCSRFCookie(w, r)
	if err != nil {
		t.Fatalf("ensureCSRFCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("expected Secure cookie for TLS request, got %#v", cookies)
	}
}

func TestRequireCSRF_GetAlwaysPasses(t *testing.T) {
	nextOK := false
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextOK = true
		if csrfTokenFromContext(r) == "" {
			t.Fatal("token should be injected for GET")
		}
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !nextOK {
		t.Fatalf("GET: code=%d next=%v", w.Code, nextOK)
	}
}

func TestRequireCSRF_PostWithoutTokenForbidden(t *testing.T) {
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not run")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%q", w.Code, strings.TrimSpace(w.Body.String()))
	}
}

func TestRequireCSRF_PostWithMatchingHeader(t *testing.T) {
	token := "fixed-test-token"
	nextOK := false
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextOK = true
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	r.Header.Set("X-CSRF-Token", token)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !nextOK {
		t.Fatalf("POST with token: code=%d next=%v", w.Code, nextOK)
	}
}

func TestRequireCSRF_PostWithFormFieldToken(t *testing.T) {
	token := "form-field-token"
	nextOK := false
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextOK = true
	}))
	w := httptest.NewRecorder()
	body := "csrf_token=" + token
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !nextOK {
		t.Fatalf("POST form csrf_token: code=%d next=%v", w.Code, nextOK)
	}
}

func TestRequireCSRF_PostWrongHeaderTokenForbidden(t *testing.T) {
	token := "good-token"
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not run")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	r.Header.Set("X-CSRF-Token", "wrong-token")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}
