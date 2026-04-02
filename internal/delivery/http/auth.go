package http

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
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
		user: strings.TrimSpace(os.Getenv("NMS_ADMIN_USER")),
		pass: os.Getenv("NMS_ADMIN_PASS"),
		role: roleAdmin,
	}
	viewer = basicCred{
		user: strings.TrimSpace(os.Getenv("NMS_VIEWER_USER")),
		pass: os.Getenv("NMS_VIEWER_PASS"),
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

func challenge(w http.ResponseWriter, realm string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`", charset="UTF-8"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

// RequireAuth принимает либо admin, либо viewer. Если не настроены креды — доступ открыт (dev-friendly).
func RequireAuth(next http.Handler) http.Handler {
	admin, viewer := loadCreds()
	if admin.user == "" && viewer.user == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			challenge(w, "NMS1")
			return
		}

		switch {
		case basicMatch(admin, user, pass):
			next.ServeHTTP(w, withUser(r, &authUser{username: user, role: roleAdmin}))
		case basicMatch(viewer, user, pass):
			next.ServeHTTP(w, withUser(r, &authUser{username: user, role: roleViewer}))
		default:
			challenge(w, "NMS1")
			return
		}
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

