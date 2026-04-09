package http

import (
	"NMS1/internal/config"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookieName = "nms_session"
	sessionTTL        = 7 * 24 * time.Hour
)

type sessionClaims struct {
	User string `json:"u"`
	Role string `json:"r"`
	Exp  int64  `json:"exp"`
}

// sessionSigningKey — 32 байта; при NMS_SESSION_SECRET задаётся явно, иначе стабильный вывод из кредов (без хранения пароля в коде).
func sessionSigningKey() [32]byte {
	secret := strings.TrimSpace(config.EnvOrFile("NMS_SESSION_SECRET"))
	if secret != "" {
		return sha256.Sum256([]byte(secret))
	}
	admin, viewer := loadCreds()
	raw := admin.user + "\x00" + admin.pass + "\x00" + viewer.user + "\x00" + viewer.pass + "\x00nms1-session-v1"
	return sha256.Sum256([]byte(raw))
}

func signSessionToken(user string, rl role) (string, error) {
	c := sessionClaims{
		User: user,
		Role: string(rl),
		Exp:  time.Now().Add(sessionTTL).Unix(),
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	key := sessionSigningKey()
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifySessionToken(token string) *authUser {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	wantSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(wantSig) != 32 {
		return nil
	}
	key := sessionSigningKey()
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(payload)
	got := mac.Sum(nil)
	if subtle.ConstantTimeCompare(got, wantSig) != 1 {
		return nil
	}
	var c sessionClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil
	}
	if time.Now().Unix() > c.Exp {
		return nil
	}
	var rl role
	switch c.Role {
	case string(roleAdmin):
		rl = roleAdmin
	case string(roleViewer):
		rl = roleViewer
	default:
		return nil
	}
	return &authUser{username: c.User, role: rl}
}

func sessionUserFromCookie(r *http.Request) *authUser {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	return verifySessionToken(c.Value)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}
