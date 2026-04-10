package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlers_Health(t *testing.T) {
	h := &Handlers{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.Health(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	if got := rr.Body.String(); got != "OK" {
		t.Fatalf("body: got %q want OK", got)
	}
}
