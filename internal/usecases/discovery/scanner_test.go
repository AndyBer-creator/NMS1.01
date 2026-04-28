package discovery

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"NMS1/internal/domain"

	"go.uber.org/zap"
)

func TestNormalizeSNMPVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "v2c"},
		{"V2C", "v2c"},
		{"2c", "v2c"},
		{"v1", "v1"},
		{"v3", "v3"},
		{"weird", "v2c"},
	}
	for _, tc := range cases {
		if got := domain.NormalizeSNMPVersionOrDefault(tc.in); got != tc.want {
			t.Fatalf("NormalizeSNMPVersionOrDefault(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestDeviceNameFromDescr(t *testing.T) {
	if got := deviceNameFromDescr("10.0.0.1", "Cisco IOS\x00Software"); got != "Cisco IOSSoftware" {
		t.Fatalf("sanitize: %q", got)
	}
	if got := deviceNameFromDescr("10.0.0.2", "!!!"); got != "SNMP-10.0.0.2" {
		t.Fatalf("fallback name: %q", got)
	}
}

func TestGenerateIPs_skipsNetworkAndBroadcast(t *testing.T) {
	_, n, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	ips := generateIPs(n)
	if len(ips) != 254 {
		t.Fatalf("want 254 usable hosts in /24, got %d", len(ips))
	}
	if ips[0].String() != "192.0.2.1" || ips[len(ips)-1].String() != "192.0.2.254" {
		t.Fatalf("range: first=%s last=%s", ips[0], ips[len(ips)-1])
	}
}

func TestGenerateIPs_IPv6DoesNotDropBoundaryAddresses(t *testing.T) {
	_, n, err := net.ParseCIDR("2001:db8::/126")
	if err != nil {
		t.Fatal(err)
	}
	ips := generateIPs(n)
	if len(ips) != 4 {
		t.Fatalf("want 4 hosts in /126, got %d", len(ips))
	}
	if ips[0].String() != "2001:db8::" || ips[len(ips)-1].String() != "2001:db8::3" {
		t.Fatalf("range: first=%s last=%s", ips[0], ips[len(ips)-1])
	}
}

func TestIncIP_IPv6Progression(t *testing.T) {
	ip := net.ParseIP("2001:db8::ff")
	if ip == nil {
		t.Fatal("expected IPv6 address")
	}
	incIP(ip)
	if got := ip.String(); got != "2001:db8::100" {
		t.Fatalf("expected IPv6 increment to carry, got %s", got)
	}
}

func TestEmptyScanHints_tcpPrefilter(t *testing.T) {
	h := emptyScanHints(ScanParams{TCPPrefilter: true, SNMPVersion: "v2c"})
	found := false
	for _, s := range h {
		if strings.Contains(s, "tcp_prefilter") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hints: %#v", h)
	}
}

func TestScanNetwork_invalidCIDR(t *testing.T) {
	s := NewScanner(nil, nil, zap.NewNop())
	_, err := s.ScanNetwork(context.Background(), ScanParams{CIDR: "not-a-network"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestScanNetwork_v3requiresAuth(t *testing.T) {
	s := NewScanner(nil, nil, zap.NewNop())
	_, err := s.ScanNetwork(context.Background(), ScanParams{
		CIDR:        "192.0.2.0/32",
		Community:   "user",
		SNMPVersion: "v3",
	})
	var se *ScanError
	if err == nil {
		t.Fatal("expected ScanError")
	}
	if !errors.As(err, &se) {
		t.Fatalf("want *ScanError, got %T %v", err, err)
	}
	if !strings.Contains(se.Msg, "auth") {
		t.Fatalf("msg: %q", se.Msg)
	}
}

func TestTCPPing_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := tcpPing(ctx, "127.0.0.1"); got {
		t.Fatal("tcpPing should return false when context is canceled")
	}
}

func TestScanNetwork_RespectsCanceledContext(t *testing.T) {
	s := NewScanner(nil, nil, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ScanNetwork(ctx, ScanParams{CIDR: "192.0.2.0/24"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
