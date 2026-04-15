package http

import (
	"net"
	"net/http"
	"os"
	"strings"
)

const trustedProxyEnv = "NMS_TRUSTED_PROXIES"

func remoteAddrIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	host, _, err := net.SplitHostPort(addr)
	if err == nil && host != "" {
		return host
	}
	return addr
}

func trustedProxyNetworks() []*net.IPNet {
	raw := strings.TrimSpace(os.Getenv(trustedProxyEnv))
	if raw == "" {
		raw = "127.0.0.0/8,::1/128"
	}
	var out []*net.IPNet
	for _, p := range strings.Split(raw, ",") {
		part := strings.TrimSpace(p)
		if part == "" {
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			mask := net.CIDRMask(bits, bits)
			out = append(out, &net.IPNet{IP: ip.Mask(mask), Mask: mask})
			continue
		}
		if _, n, err := net.ParseCIDR(part); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func fromTrustedProxy(r *http.Request) bool {
	ip := net.ParseIP(remoteAddrIP(r))
	if ip == nil {
		return false
	}
	for _, n := range trustedProxyNetworks() {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func forwardedProto(r *http.Request) string {
	if !fromTrustedProxy(r) {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
}
