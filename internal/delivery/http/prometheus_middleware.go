package http

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// responseWriter captures status code for metrics labels.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack support is required for websocket upgrades.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("prometheus responseWriter: Hijacker not supported by underlying writer")
	}
	return hj.Hijack()
}

func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// PrometheusMetrics records request counters and duration by route/status.
func PrometheusMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrap := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrap, r)

		pattern := r.URL.Path
		if ctx := chi.RouteContext(r.Context()); ctx != nil && ctx.RoutePattern() != "" {
			pattern = ctx.RoutePattern()
		}
		status := strconv.Itoa(wrap.status)
		requestsTotal.WithLabelValues(r.Method, pattern, status).Inc()
		requestDurationSeconds.WithLabelValues(r.Method, pattern, status).Observe(time.Since(start).Seconds())
	})
}
