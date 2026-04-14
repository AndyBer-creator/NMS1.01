package http

import (
	"net"
	"net/http"
	"testing"
)

func TestTerminalKindFromQuery(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/x?kind=telnet", nil)
	if got := terminalKindFromQuery(r); got != "telnet" {
		t.Fatalf("telnet: got %q", got)
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
	}
	req.Header.Set("Origin", "https://nms.example.com")
	if !terminalCheckOrigin(req) {
		t.Fatal("expected same-origin websocket request to pass")
	}

	req2 := &http.Request{
		Host:   "nms.example.com",
		Header: make(http.Header),
	}
	req2.Header.Set("Origin", "https://evil.example.com")
	if terminalCheckOrigin(req2) {
		t.Fatal("expected cross-origin websocket request to be blocked")
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
