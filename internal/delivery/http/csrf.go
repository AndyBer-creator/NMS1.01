package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

const csrfCookieName = "nms_csrf"

type csrfCtxKey struct{}

func withCSRFToken(r *http.Request, token string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), csrfCtxKey{}, token))
}

func csrfTokenFromContext(r *http.Request) string {
	if v := r.Context().Value(csrfCtxKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func generateCSRFToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(csrfCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
		return c.Value, nil
	}
	token, err := generateCSRFToken()
	if err != nil {
		return "", err
	}
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS читает для HTMX/fetch заголовка
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	return token, nil
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// RequireCSRF: double-submit cookie (X-CSRF-Token/header or form field csrf_token).
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := ensureCSRFCookie(w, r)
		if err != nil {
			http.Error(w, "CSRF init failed", http.StatusInternalServerError)
			return
		}
		r = withCSRFToken(r, token)

		if !isUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		provided := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		if provided == "" {
			_ = r.ParseForm()
			provided = strings.TrimSpace(r.FormValue("csrf_token"))
		}
		if provided == "" || !equalConstTime(provided, token) {
			http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
