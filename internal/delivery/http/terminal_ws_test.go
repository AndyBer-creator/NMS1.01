package http

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func TestTerminalKindFromQuery(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/x?kind=anything", nil)
	if got := terminalKindFromQuery(r); got != "ssh" {
		t.Fatalf("kind query must resolve to ssh, got %q", got)
	}
	r2, _ := http.NewRequest(http.MethodGet, "/x", nil)
	if got := terminalKindFromQuery(r2); got != "ssh" {
		t.Fatalf("default: got %q", got)
	}
}

func TestDeviceDialAddr(t *testing.T) {
	cases := []struct {
		host, wantHost string
		port           int
	}{
		{"192.0.2.1", "192.0.2.1", 22},
		{"2001:db8::1", "2001:db8::1", 22},
		{"[2001:db8::1]", "2001:db8::1", 22},
	}
	for _, tc := range cases {
		addr := deviceDialAddr(tc.host, tc.port)
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("SplitHostPort(%q): %v", addr, err)
		}
		if host != tc.wantHost {
			t.Fatalf("host %q: got %q want %q", tc.host, host, tc.wantHost)
		}
		if port != "22" {
			t.Fatalf("port for %q: got %q", tc.host, port)
		}
	}
}

func TestTerminalCheckOrigin_DefaultStrict(t *testing.T) {
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	req := &http.Request{
		Host:   "nms.example.com",
		Header: make(http.Header),
		TLS:    &tls.ConnectionState{},
	}
	req.Header.Set("Origin", "https://nms.example.com")
	if !terminalCheckOrigin(req) {
		t.Fatal("expected same-origin websocket request to pass")
	}

	req2 := &http.Request{
		Host:   "nms.example.com",
		Header: make(http.Header),
		TLS:    &tls.ConnectionState{},
	}
	req2.Header.Set("Origin", "https://evil.example.com")
	if terminalCheckOrigin(req2) {
		t.Fatal("expected cross-origin websocket request to be blocked")
	}
}

func TestTerminalCheckOrigin_RejectsSchemeAndPortMismatch(t *testing.T) {
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")

	reqHTTP := &http.Request{
		Host:   "nms.example.com",
		Header: make(http.Header),
	}
	reqHTTP.Header.Set("Origin", "https://nms.example.com")
	if terminalCheckOrigin(reqHTTP) {
		t.Fatal("expected scheme mismatch to be blocked")
	}

	reqHTTPS := &http.Request{
		Host:   "nms.example.com:8443",
		Header: make(http.Header),
		TLS:    &tls.ConnectionState{},
	}
	reqHTTPS.Header.Set("Origin", "https://nms.example.com")
	if terminalCheckOrigin(reqHTTPS) {
		t.Fatal("expected port mismatch to be blocked")
	}
}

func TestTerminalCheckOrigin_EmptyOriginBlockedByDefault(t *testing.T) {
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "")
	req := &http.Request{
		Host:   "nms.example.com",
		Header: make(http.Header),
	}
	if terminalCheckOrigin(req) {
		t.Fatal("expected missing origin to be blocked by default")
	}
}

func TestTerminalCheckOrigin_AllowInsecureOverride(t *testing.T) {
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN", "true")
	req := &http.Request{
		Host:   "nms.example.com",
		Header: make(http.Header),
	}
	if !terminalCheckOrigin(req) {
		t.Fatal("expected insecure origin override to allow request")
	}
}

func TestTerminalSSHHostKeyCallback_RequiresPolicy(t *testing.T) {
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "")
	if _, err := terminalSSHHostKeyCallback(); err == nil {
		t.Fatal("expected error when host key verification policy is not configured")
	}
}

func TestTerminalSSHHostKeyCallback_AllowsExplicitInsecure(t *testing.T) {
	t.Setenv("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY", "true")
	t.Setenv("NMS_TERMINAL_SSH_KNOWN_HOSTS", "")
	cb, err := terminalSSHHostKeyCallback()
	if err != nil {
		t.Fatalf("expected insecure callback without error, got %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestTerminalTimeoutEnvHelpers(t *testing.T) {
	t.Setenv("NMS_TERMINAL_DIAL_TIMEOUT", "")
	t.Setenv("NMS_TERMINAL_SESSION_MAX", "")
	t.Setenv("NMS_TERMINAL_WS_READ_IDLE", "")
	t.Setenv("NMS_TERMINAL_WS_READ_LIMIT_BYTES", "")
	if got := terminalDialTimeout(); got != 20*time.Second {
		t.Fatalf("terminalDialTimeout default: got %v", got)
	}
	if got := terminalWSReadIdle(); got != 30*time.Minute {
		t.Fatalf("terminalWSReadIdle default: got %v", got)
	}
	if got := terminalWSReadLimit(); got != defaultTerminalWSReadLimit {
		t.Fatalf("terminalWSReadLimit default: got %d", got)
	}
	deadline := terminalSessionDeadline()
	if diff := time.Until(deadline); diff < 7*time.Hour || diff > 9*time.Hour {
		t.Fatalf("terminalSessionDeadline default out of range: %v", diff)
	}

	t.Setenv("NMS_TERMINAL_DIAL_TIMEOUT", "7s")
	t.Setenv("NMS_TERMINAL_SESSION_MAX", "1h")
	t.Setenv("NMS_TERMINAL_WS_READ_IDLE", "45s")
	t.Setenv("NMS_TERMINAL_WS_READ_LIMIT_BYTES", "131072")
	if got := terminalDialTimeout(); got != 7*time.Second {
		t.Fatalf("terminalDialTimeout override: got %v", got)
	}
	if got := terminalWSReadIdle(); got != 45*time.Second {
		t.Fatalf("terminalWSReadIdle override: got %v", got)
	}
	if got := terminalWSReadLimit(); got != 131072 {
		t.Fatalf("terminalWSReadLimit override: got %d", got)
	}
	deadline = terminalSessionDeadline()
	if diff := time.Until(deadline); diff < 59*time.Minute || diff > 61*time.Minute {
		t.Fatalf("terminalSessionDeadline override out of range: %v", diff)
	}
}

func TestTerminalWSReadLimit_InvalidOrOutOfRangeFallsBack(t *testing.T) {
	t.Setenv("NMS_TERMINAL_WS_READ_LIMIT_BYTES", "abc")
	if got := terminalWSReadLimit(); got != defaultTerminalWSReadLimit {
		t.Fatalf("invalid value must fallback, got %d", got)
	}
	t.Setenv("NMS_TERMINAL_WS_READ_LIMIT_BYTES", "512")
	if got := terminalWSReadLimit(); got != defaultTerminalWSReadLimit {
		t.Fatalf("too small value must fallback, got %d", got)
	}
	t.Setenv("NMS_TERMINAL_WS_READ_LIMIT_BYTES", "2097152")
	if got := terminalWSReadLimit(); got != defaultTerminalWSReadLimit {
		t.Fatalf("too large value must fallback, got %d", got)
	}
}

func TestValidateTerminalInitMsg(t *testing.T) {
	t.Run("valid ssh", func(t *testing.T) {
		err := validateTerminalInitMsg(terminalInitMsg{
			Type:     "init",
			Username: "admin",
			Password: "secret",
			Port:     22,
		}, "ssh")
		if err != nil {
			t.Fatalf("expected valid init, got %v", err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		err := validateTerminalInitMsg(terminalInitMsg{Type: "resize", Username: "u"}, "ssh")
		if err == nil {
			t.Fatal("expected error for non-init type")
		}
	})
	t.Run("ssh username required", func(t *testing.T) {
		err := validateTerminalInitMsg(terminalInitMsg{Type: "init", Username: "   "}, "ssh")
		if err == nil {
			t.Fatal("expected error for empty ssh username")
		}
	})
	t.Run("invalid port", func(t *testing.T) {
		err := validateTerminalInitMsg(terminalInitMsg{Type: "init", Username: "u", Port: 70000}, "ssh")
		if err == nil {
			t.Fatal("expected error for invalid port")
		}
	})
	t.Run("too long credentials", func(t *testing.T) {
		long := strings.Repeat("a", maxTerminalAuthFieldBytes+1)
		err := validateTerminalInitMsg(terminalInitMsg{Type: "init", Username: long}, "ssh")
		if err == nil {
			t.Fatal("expected error for long username")
		}
		err = validateTerminalInitMsg(terminalInitMsg{Type: "init", Username: "u", Password: long}, "ssh")
		if err == nil {
			t.Fatal("expected error for long password")
		}
	})
}

func TestTerminalWSTokenSignVerify(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "terminal-token-test-secret")
	t.Setenv("NMS_SESSION_SECRET_FILE", "")

	token, err := signTerminalWSToken("admin", roleAdmin, 42)
	if err != nil {
		t.Fatalf("signTerminalWSToken: %v", err)
	}
	u := verifyTerminalWSToken(token, 42)
	if u == nil {
		t.Fatal("verifyTerminalWSToken returned nil for valid token")
	}
	if u.username != "admin" || u.role != roleAdmin {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestTerminalWSTokenRejectsInvalidCases(t *testing.T) {
	t.Setenv("NMS_SESSION_SECRET", "terminal-token-test-secret")
	t.Setenv("NMS_SESSION_SECRET_FILE", "")

	token, err := signTerminalWSToken("viewer", roleViewer, 7)
	if err != nil {
		t.Fatalf("signTerminalWSToken viewer: %v", err)
	}
	if got := verifyTerminalWSToken(token, 7); got != nil {
		t.Fatal("viewer role token must be rejected")
	}

	adminToken, err := signTerminalWSToken("admin", roleAdmin, 7)
	if err != nil {
		t.Fatalf("signTerminalWSToken admin: %v", err)
	}
	if got := verifyTerminalWSToken(adminToken, 8); got != nil {
		t.Fatal("token for another device must be rejected")
	}

	parts := strings.Split(adminToken, ".")
	if len(parts) != 2 {
		t.Fatalf("unexpected token format: %q", adminToken)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(sig) == 0 {
		t.Fatalf("decode signature: %v", err)
	}
	sig[0] ^= 0xFF
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(sig)
	if got := verifyTerminalWSToken(tampered, 7); got != nil {
		t.Fatal("tampered signature token must be rejected")
	}
}

func TestTerminalTokenFromSubprotocol(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/terminal/7?kind=ssh", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "chat, nms-term-auth.abc.def, other")
	if got := terminalTokenFromSubprotocol(r); got != "abc.def" {
		t.Fatalf("expected token from subprotocol, got %q", got)
	}
}

func TestTerminalWS_ThrottledBasicAuthReturns429(t *testing.T) {
	resetAuthLimiterForTest(t)
	t.Setenv("NMS_ADMIN_USER", "admin")
	t.Setenv("NMS_ADMIN_PASS", "secret")
	t.Setenv("NMS_ADMIN_USER_FILE", "")
	t.Setenv("NMS_ADMIN_PASS_FILE", "")

	ip := "203.0.113.77"
	user := "admin"
	now := time.Now()
	for i := 0; i < loginMaxAttemptsUser; i++ {
		authLoginLimiter.onFailure(ip, user, now.Add(time.Duration(i)*time.Second))
	}

	req := httptest.NewRequest(http.MethodGet, "/terminal/1?kind=ssh", nil)
	req.RemoteAddr = ip + ":44321"
	req.SetBasicAuth("admin", "secret")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	rr := httptest.NewRecorder()
	h := &Handlers{logger: zap.NewNop()}
	h.TerminalWS(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}
