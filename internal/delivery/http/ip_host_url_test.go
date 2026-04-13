package http

import "testing"

func TestIPHostForURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"192.168.0.1", "192.168.0.1"},
		{"2001:db8::1", "[2001:db8::1]"},
		{"::1", "[::1]"},
		{"::ffff:192.0.2.1", "192.0.2.1"},
		{"router.lab", "router.lab"},
		{"fe80::1%eth0", "[fe80::1%25eth0]"},
	}
	for _, tc := range tests {
		got := ipHostForURL(tc.in)
		if got != tc.want {
			t.Errorf("ipHostForURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
