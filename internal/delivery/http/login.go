package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	admin, viewer := loadCreds()
	if admin.user == "" && viewer.user == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if sessionUserFromCookie(r) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.loginTmpl.ExecuteTemplate(w, "login.html", map[string]any{
		"Error": "",
		"Next":  safeNext(r.URL.Query().Get("next")),
	})
}

func (h *Handlers) LoginPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	admin, viewer := loadCreds()
	if admin.user == "" && viewer.user == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	user := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	ip := clientIP(r)
	now := time.Now()

	if ok, retryAfter := authLoginLimiter.check(ip, user, now); !ok {
		msg := "Слишком много попыток входа. Повторите позже."
		if retryAfter > 0 {
			msg = fmt.Sprintf("Слишком много попыток входа. Повторите через %d сек.", int(retryAfter.Seconds())+1)
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = h.loginTmpl.ExecuteTemplate(w, "login.html", map[string]any{
			"Error": msg,
			"Next":  next,
		})
		h.logger.Warn("login throttled", zap.String("ip", ip), zap.String("user", user), zap.Duration("retry_after", retryAfter))
		return
	}

	var rl role
	switch {
	case basicMatch(admin, user, pass):
		rl = roleAdmin
	case basicMatch(viewer, user, pass):
		rl = roleViewer
	default:
		authLoginLimiter.onFailure(ip, user, now)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = h.loginTmpl.ExecuteTemplate(w, "login.html", map[string]any{
			"Error": "Неверный логин или пароль.",
			"Next":  next,
		})
		h.logger.Warn("login failed", zap.String("ip", ip), zap.String("user", user))
		return
	}
	authLoginLimiter.onSuccess(ip, user)

	token, err := signSessionToken(user, rl)
	if err != nil {
		h.logger.Error("signSessionToken", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, r, token)
	http.Redirect(w, r, next, http.StatusFound)
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusFound)
}

func safeNext(raw string) string {
	if raw == "" {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "/"
	}
	if u.IsAbs() || u.Host != "" {
		return "/"
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	out := u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
