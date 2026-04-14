package http

import (
	"NMS1/internal/config"
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type role string

const (
	roleAdmin  role = "admin"
	roleViewer role = "viewer"
)

type authUser struct {
	username string
	role     role
}

type ctxKey int

const userKey ctxKey = iota

func userFromContext(ctx context.Context) *authUser {
	if v := ctx.Value(userKey); v != nil {
		if u, ok := v.(*authUser); ok {
			return u
		}
	}
	return nil
}

func withUser(r *http.Request, u *authUser) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey, u))
}

type basicCred struct {
	user string
	pass string
	role role
}

func loadCreds() (admin basicCred, viewer basicCred) {
	admin = basicCred{
		user: strings.TrimSpace(config.EnvOrFile("NMS_ADMIN_USER")),
		pass: config.EnvOrFile("NMS_ADMIN_PASS"),
		role: roleAdmin,
	}
	viewer = basicCred{
		user: strings.TrimSpace(config.EnvOrFile("NMS_VIEWER_USER")),
		pass: config.EnvOrFile("NMS_VIEWER_PASS"),
		role: roleViewer,
	}
	return admin, viewer
}

func equalConstTime(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func basicMatch(cred basicCred, user, pass string) bool {
	if cred.user == "" || cred.pass == "" {
		return false
	}
	return equalConstTime(cred.user, user) && equalConstTime(cred.pass, pass)
}

func prefersJSONAPI(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") &&
		!strings.Contains(accept, "text/html")
}

func envBool(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// RequireAuth: cookie-сессия (форма /login) или HTTP Basic.
// По умолчанию отсутствие сконфигурированных кредов считается ошибкой конфигурации (fail-closed).
func RequireAuth(next http.Handler) http.Handler {
	admin, viewer := loadCreds()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if admin.user == "" && viewer.user == "" {
			if envBool("NMS_ALLOW_NO_AUTH") {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "auth is not configured", http.StatusServiceUnavailable)
			return
		}

		if u := sessionUserFromCookie(r); u != nil {
			next.ServeHTTP(w, withUser(r, u))
			return
		}

		user, pass, ok := r.BasicAuth()
		if ok {
			ip := clientIP(r)
			now := time.Now()
			if allowed, retryAfter := authLoginLimiter.check(ip, user, now); !allowed {
				retrySec := int(retryAfter.Seconds()) + 1
				if retrySec < 1 {
					retrySec = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retrySec))
				if prefersJSONAPI(r) {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"too_many_requests","retry_after_seconds":` + fmt.Sprintf("%d", retrySec) + `}`))
				} else {
					http.Error(w, "Too many authentication attempts", http.StatusTooManyRequests)
				}
				return
			}
			switch {
			case basicMatch(admin, user, pass):
				authLoginLimiter.onSuccess(ip, user)
				next.ServeHTTP(w, withUser(r, &authUser{username: user, role: roleAdmin}))
				return
			case basicMatch(viewer, user, pass):
				authLoginLimiter.onSuccess(ip, user)
				next.ServeHTTP(w, withUser(r, &authUser{username: user, role: roleViewer}))
				return
			}
			authLoginLimiter.onFailure(ip, user, now)
		}

		nextURL := "/login?next=" + url.QueryEscape(safeNext(r.URL.RequestURI()))
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", nextURL)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
			return
		}
		if prefersJSONAPI(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","login":"/login"}`))
			return
		}
		http.Redirect(w, r, nextURL, http.StatusFound)
	})
}

// adminUserFromRequest — только admin: cookie-сессия или HTTP Basic (как RequireAuth, но без viewer).
type adminAuthOutcome struct {
	user       *authUser
	retryAfter time.Duration
}

func adminUserFromRequest(r *http.Request) adminAuthOutcome {
	admin, _ := loadCreds()
	if admin.user == "" {
		return adminAuthOutcome{}
	}
	if u := sessionUserFromCookie(r); u != nil && u.role == roleAdmin {
		return adminAuthOutcome{user: u}
	}
	if user, pass, ok := r.BasicAuth(); ok {
		ip := clientIP(r)
		now := time.Now()
		if allowed, retryAfter := authLoginLimiter.check(ip, user, now); !allowed {
			return adminAuthOutcome{retryAfter: retryAfter}
		}
		if basicMatch(admin, user, pass) {
			authLoginLimiter.onSuccess(ip, user)
			return adminAuthOutcome{user: &authUser{username: user, role: roleAdmin}}
		}
		authLoginLimiter.onFailure(ip, user, now)
	}
	return adminAuthOutcome{}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r.Context())
		if u == nil {
			// Запрещаем fail-open на admin-маршрутах; legacy-режим можно включить явно.
			if envBool("NMS_ALLOW_NO_AUTH") {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if u.role != roleAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
