package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestAccessLog_DoesNotLogAuthorizationHeader(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	prev := zap.L()
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)
	t.Cleanup(func() { zap.ReplaceGlobals(prev) })

	mw := AccessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if logs.Len() == 0 {
		t.Fatal("expected access log entry")
	}
	for _, entry := range logs.All() {
		for _, field := range entry.Context {
			if field.Key == "authorization" || field.Key == "Authorization" {
				t.Fatalf("authorization header leaked into logs: %s", field.String)
			}
		}
	}
}
