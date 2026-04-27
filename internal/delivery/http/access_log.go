package http

import (
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// AccessLog writes structured request logs without request headers.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrap := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrap, r)

		route := r.URL.Path
		if ctx := chi.RouteContext(r.Context()); ctx != nil && ctx.RoutePattern() != "" {
			route = ctx.RoutePattern()
		}

		zap.L().Info("http request",
			zap.String("method", r.Method),
			zap.String("route", route),
			zap.Int("status", wrap.status),
			zap.String("request_id", middleware.GetReqID(r.Context())),
			zap.String("remote_ip", remoteIP(r.RemoteAddr)),
			zap.Int64("duration_ms", time.Since(start).Milliseconds()),
			zap.Int("bytes", wrap.bytes),
		)
	})
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
