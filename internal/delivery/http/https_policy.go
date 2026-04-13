package http

import (
	"net/http"
	"os"
	"strings"
)

func httpsOnlyEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("NMS_ENFORCE_HTTPS")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func isHTTPSBypassPath(path string) bool {
	switch path {
	case "/health", "/ready", "/metrics", "/.well-known/security.txt":
		return true
	default:
		return false
	}
}

// EnforceHTTPS redirects plain HTTP traffic to HTTPS when enabled.
// Health, readiness, metrics и security.txt исключены для probe и публичного security.txt.
func EnforceHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpsOnlyEnabled() || isHTTPSRequest(r) || isHTTPSBypassPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		target := "https://" + r.Host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

