package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseWriterCapturesStatusCode(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &responseWriter{ResponseWriter: rr, status: http.StatusOK}
	w.WriteHeader(http.StatusTeapot)
	if w.status != http.StatusTeapot {
		t.Fatalf("status: got %d", w.status)
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("underlying recorder: got %d", rr.Code)
	}
}
