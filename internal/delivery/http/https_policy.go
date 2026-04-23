package http

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// httpsOnlyEnabled checks strict HTTPS policy switch from environment.
func httpsOnlyEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("NMS_ENFORCE_HTTPS")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// isHTTPSBypassPath reports probe/static endpoints exempt from redirects.
func isHTTPSBypassPath(path string) bool {
	switch path {
	case "/health", "/ready", "/metrics", "/.well-known/security.txt":
		return true
	default:
		return false
	}
}

// EnforceHTTPS redirects plain HTTP traffic to HTTPS when enabled.
// Health/readiness/metrics/security.txt are excluded for probes and public metadata.
func EnforceHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpsOnlyEnabled() || isHTTPSRequest(r) || isHTTPSBypassPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		host := canonicalRequestHost(r)
		if host == "" {
			http.Error(w, "invalid host", http.StatusBadRequest)
			return
		}
		target := "https://" + host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

// canonicalRequestHost validates and normalizes request host for redirects.
func canonicalRequestHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Host)
	if host == "" || strings.ContainsAny(host, " \t\r\n/\\") || strings.Contains(host, "@") {
		return ""
	}
	if strings.HasPrefix(host, "[") {
		if _, _, err := net.SplitHostPort(host); err == nil {
			return host
		}
		if strings.HasSuffix(host, "]") {
			return host
		}
		return ""
	}
	if strings.Count(host, ":") == 0 {
		return host
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		if strings.TrimSpace(h) == "" || strings.TrimSpace(p) == "" {
			return ""
		}
		return net.JoinHostPort(h, p)
	}
	return ""
}
