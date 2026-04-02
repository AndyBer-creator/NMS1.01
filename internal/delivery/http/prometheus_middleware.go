package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// responseWriter перехватывает status code для метрик.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// PrometheusMetrics увеличивает nms_requests_total по каждому запросу (method, endpoint, status).
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
