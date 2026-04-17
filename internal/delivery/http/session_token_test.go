package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func resetSessionRevocationsForTest(t *testing.T) {
	t.Helper()
	sessionRevokeMu.Lock()
	prev := sessionRevokedByJTI
	prevGC := sessionRevokeLastGC
	prevStore := sessionRevocationStore
	sessionRevokedByJTI = map[string]int64{}
	sessionRevokeLastGC = time.Time{}
	sessionRevocationStore = nil
	sessionRevokeMu.Unlock()
	t.Cleanup(func() {
		sessionRevokeMu.Lock()
		sessionRevokedByJTI = prev
		sessionRevokeLastGC = prevGC
		sessionRevocationStore = prevStore
		sessionRevokeMu.Unlock()
	})
}

func signRawSessionPayload(payload []byte) string {
	key := sessionSigningKey()
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestVerifySessionToken_InvalidFormats(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	resetSessionRevocationsForTest(t)
	cases := []string{
		"",
		"one-part-only",
		"a.b.c",
		"@@@.@@@",
	}
	for _, tc := range cases {
		if got := verifySessionToken(tc); got != nil {
			t.Fatalf("verifySessionToken(%q) must be nil", tc)
		}
	}
}

func TestVerifySessionToken_RejectsInvalidSignature(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	resetSessionRevocationsForTest(t)
	ok, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	if verifySessionToken(ok) == nil {
		t.Fatal("expected baseline signed token to verify")
	}

	parts := strings.Split(ok, ".")
	if len(parts) != 2 {
		t.Fatalf("unexpected token format: %q", ok)
	}
	sigRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(sigRaw) == 0 {
		t.Fatalf("decode signature: %v", err)
	}
	sigRaw[0] ^= 0x01
	tampered := fmt.Sprintf("%s.%s", parts[0], base64.RawURLEncoding.EncodeToString(sigRaw))
	if got := verifySessionToken(tampered); got != nil {
		t.Fatalf("expected tampered token to fail verification, got %+v", got)
	}
}

func TestVerifySessionToken_RejectsExpiredAndUnknownRole(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	resetSessionRevocationsForTest(t)

	expiredPayload, err := json.Marshal(sessionClaims{
		User: "admin",
		Role: string(roleAdmin),
		JTI:  "expired-jti",
		Exp:  time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal expired payload: %v", err)
	}
	expiredToken := signRawSessionPayload(expiredPayload)
	if got := verifySessionToken(expiredToken); got != nil {
		t.Fatalf("expected expired token to fail, got %+v", got)
	}

	unknownRolePayload, err := json.Marshal(sessionClaims{
		User: "admin",
		Role: "superuser",
		JTI:  "unknown-role-jti",
		Exp:  time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal unknown role payload: %v", err)
	}
	unknownRoleToken := signRawSessionPayload(unknownRolePayload)
	if got := verifySessionToken(unknownRoleToken); got != nil {
		t.Fatalf("expected unknown role token to fail, got %+v", got)
	}
}

func TestSessionUserFromCookie_UsesVerifySessionToken(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	resetSessionRevocationsForTest(t)
	token, err := signSessionToken("viewer", roleViewer)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	u := sessionUserFromCookie(req)
	if u == nil || u.username != "viewer" || u.role != roleViewer {
		t.Fatalf("unexpected user from cookie: %+v", u)
	}
}

func TestVerifySessionToken_RejectsRevokedJTI(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	resetSessionRevocationsForTest(t)
	token, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	if got := verifySessionToken(token); got == nil {
		t.Fatal("expected token valid before revocation")
	}
	revokeSessionToken(token)
	if got := verifySessionToken(token); got != nil {
		t.Fatalf("expected revoked token to fail verification, got %+v", got)
	}
}

type errRevocationStore struct{}

func (e errRevocationStore) RevokeSessionJTI(ctx context.Context, jti string, expUnix int64) error {
	return nil
}

func (e errRevocationStore) IsSessionJTIRevoked(ctx context.Context, jti string, nowUnix int64) (bool, error) {
	return false, errors.New("store unavailable")
}

func TestVerifySessionToken_RevocationStoreError_ProdFailsClosed(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	t.Setenv("NMS_ENV", "production")
	t.Setenv("NMS_SESSION_REVOCATION_FAIL_CLOSED", "")
	resetSessionRevocationsForTest(t)
	setSessionRevocationStore(errRevocationStore{})

	token, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	if got := verifySessionToken(token); got != nil {
		t.Fatalf("expected token rejected when revocation store fails in production, got %+v", got)
	}
}

func TestVerifySessionToken_RevocationStoreError_DevCanFailOpen(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
	t.Setenv("NMS_ENV", "dev")
	t.Setenv("NMS_SESSION_REVOCATION_FAIL_CLOSED", "false")
	resetSessionRevocationsForTest(t)
	setSessionRevocationStore(errRevocationStore{})

	token, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	if got := verifySessionToken(token); got == nil {
		t.Fatal("expected token accepted when fail-closed disabled and local revocation absent")
	}
}
