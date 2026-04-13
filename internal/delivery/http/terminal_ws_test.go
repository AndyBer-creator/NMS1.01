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
