package http

import (
	"NMS1/internal/config"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "nms_session"
	sessionTTL        = 7 * 24 * time.Hour
	sessionStoreIOTTL = 2 * time.Second
)

type sessionClaims struct {
	User string `json:"u"`
	Role string `json:"r"`
	JTI  string `json:"jti"`
	Exp  int64  `json:"exp"`
}

var (
	fallbackSessionKey     [32]byte
	fallbackSessionKeyOnce sync.Once
	sessionRevokeMu        sync.Mutex
	sessionRevokedByJTI    = map[string]int64{}
	sessionRevokeLastGC    time.Time
	sessionRevocationStore sharedSessionRevocationStore
)

type sharedSessionRevocationStore interface {
	RevokeSessionJTI(ctx context.Context, jti string, expUnix int64) error
	IsSessionJTIRevoked(ctx context.Context, jti string, nowUnix int64) (bool, error)
}

func initFallbackSessionKey() {
	if _, err := rand.Read(fallbackSessionKey[:]); err != nil {
		// Крайне маловероятный fallback: сохраняем работоспособность токенов в рамках процесса.
		fallbackSessionKey = sha256.Sum256([]byte(time.Now().UTC().String() + "-nms-session-fallback"))
	}
}

// sessionSigningKey — 32 байта; рекомендуется всегда задавать NMS_SESSION_SECRET.
// Если секрет не задан, используется случайный ключ процесса (без детерминированного вывода из паролей).
func sessionSigningKey() [32]byte {
	secret := strings.TrimSpace(config.EnvOrFile("NMS_SESSION_SECRET"))
	if secret != "" {
		return sha256.Sum256([]byte(secret))
	}
	fallbackSessionKeyOnce.Do(initFallbackSessionKey)
	return fallbackSessionKey
}

func signSessionToken(user string, rl role) (string, error) {
	jti, err := generateSessionJTI()
	if err != nil {
		return "", err
	}
	c := sessionClaims{
		User: user,
		Role: string(rl),
		JTI:  jti,
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
	if strings.TrimSpace(c.JTI) == "" {
		return nil
	}
	if time.Now().Unix() > c.Exp {
		return nil
	}
	if isSessionRevoked(c.JTI, time.Now().Unix()) {
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

func generateSessionJTI() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func revokeSessionToken(token string) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return
	}
	var c sessionClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return
	}
	if strings.TrimSpace(c.JTI) == "" || c.Exp <= 0 {
		return
	}
	storeLocalSessionRevocation(c.JTI, c.Exp, time.Now().Unix())
	if sessionRevocationStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), sessionStoreIOTTL)
		defer cancel()
		_ = sessionRevocationStore.RevokeSessionJTI(ctx, c.JTI, c.Exp)
	}
}

func isSessionRevoked(jti string, nowUnix int64) bool {
	if isSessionRevokedLocally(jti, nowUnix) {
		return true
	}
	if sessionRevocationStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), sessionStoreIOTTL)
		revoked, err := sessionRevocationStore.IsSessionJTIRevoked(ctx, jti, nowUnix)
		cancel()
		if err == nil && revoked {
			return true
		}
	}
	return false
}

func isSessionRevokedLocally(jti string, nowUnix int64) bool {
	sessionRevokeMu.Lock()
	defer sessionRevokeMu.Unlock()
	revokeGC(nowUnix)
	exp, ok := sessionRevokedByJTI[jti]
	return ok && exp >= nowUnix
}

func storeLocalSessionRevocation(jti string, expUnix, nowUnix int64) {
	sessionRevokeMu.Lock()
	defer sessionRevokeMu.Unlock()
	revokeGC(nowUnix)
	sessionRevokedByJTI[jti] = expUnix
}

func revokeGC(nowUnix int64) {
	if !sessionRevokeLastGC.IsZero() && time.Since(sessionRevokeLastGC) < time.Minute {
		return
	}
	for jti, exp := range sessionRevokedByJTI {
		if exp < nowUnix {
			delete(sessionRevokedByJTI, jti)
		}
	}
	sessionRevokeLastGC = time.Now()
}

func sessionUserFromCookie(r *http.Request) *authUser {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	return verifySessionToken(c.Value)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := isHTTPSRequest(r)
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
	secure := isHTTPSRequest(r)
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

func setSessionRevocationStore(store sharedSessionRevocationStore) {
	sessionRevokeMu.Lock()
	defer sessionRevokeMu.Unlock()
	sessionRevocationStore = store
}
