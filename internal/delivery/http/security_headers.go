package http

import (
	"net/http"
	"strings"
)

func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// SecurityHeaders adds baseline browser-side protections.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME sniffing.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Forbid clickjacking.
		w.Header().Set("X-Frame-Options", "DENY")
		// Limit referrer leakage.
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Disable dangerous browser features by default.
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// CSP tuned for current templates (inline scripts + CDN assets).
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://unpkg.com https://cdn.tailwindcss.com; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'",
		)

		// HSTS only when request is actually HTTPS (or forwarded as HTTPS).
		if isHTTPSRequest(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}
