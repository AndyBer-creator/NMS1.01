package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func signRawSessionPayload(payload []byte) string {
	key := sessionSigningKey()
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestVerifySessionToken_InvalidFormats(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")
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
	ok, err := signSessionToken("admin", roleAdmin)
	if err != nil {
		t.Fatalf("signSessionToken: %v", err)
	}
	if verifySessionToken(ok) == nil {
		t.Fatal("expected baseline signed token to verify")
	}

	parts := []byte(ok)
	if len(parts) == 0 {
		t.Fatal("unexpected empty token")
	}
	parts[len(parts)-1] = 'A'
	if got := verifySessionToken(string(parts)); got != nil {
		t.Fatalf("expected tampered token to fail verification, got %+v", got)
	}
}

func TestVerifySessionToken_RejectsExpiredAndUnknownRole(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "itest-session-secret")

	expiredPayload, err := json.Marshal(sessionClaims{
		User: "admin",
		Role: string(roleAdmin),
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

