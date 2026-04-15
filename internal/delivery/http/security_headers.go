package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

type cspNonceKey struct{}

func cspNonceFromContext(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := r.Context().Value(cspNonceKey{})
	s, _ := v.(string)
	return s
}

func newCSPNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; empty nonce will fail closed by blocking inline scripts/styles.
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(b[:])
}

func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(forwardedProto(r), "https")
}

// SecurityHeaders adds baseline browser-side protections.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := newCSPNonce()
		r = r.WithContext(context.WithValue(r.Context(), cspNonceKey{}, nonce))

		// Prevent MIME sniffing.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Forbid clickjacking.
		w.Header().Set("X-Frame-Options", "DENY")
		// Limit referrer leakage.
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Disable dangerous browser features by default.
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// CSP with per-request nonce for inline scripts/styles.
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'nonce-"+nonce+"'; "+
				"style-src 'self' 'nonce-"+nonce+"'; "+
				"img-src 'self' data:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"object-src 'none'; "+
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
