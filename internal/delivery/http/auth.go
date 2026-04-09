package http

import (
	"NMS1/internal/config"
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
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

// RequireAuth: cookie-сессия (форма /login) или HTTP Basic. Если креды не заданы — доступ открыт.
func RequireAuth(next http.Handler) http.Handler {
	admin, viewer := loadCreds()
	if admin.user == "" && viewer.user == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := sessionUserFromCookie(r); u != nil {
			next.ServeHTTP(w, withUser(r, u))
			return
		}

		user, pass, ok := r.BasicAuth()
		if ok {
			switch {
			case basicMatch(admin, user, pass):
				next.ServeHTTP(w, withUser(r, &authUser{username: user, role: roleAdmin}))
				return
			case basicMatch(viewer, user, pass):
				next.ServeHTTP(w, withUser(r, &authUser{username: user, role: roleViewer}))
				return
			}
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

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r.Context())
		if u == nil {
			// если авторизация отключена, user nil — разрешаем (dev)
			next.ServeHTTP(w, r)
			return
		}
		if u.role != roleAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
