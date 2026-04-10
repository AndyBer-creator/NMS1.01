package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestPrometheusMetrics_IncrementsCountersForChiRoute(t *testing.T) {
	const path = "/__prom_metrics_probe"
	r := chi.NewRouter()
	r.Use(PrometheusMetrics)
	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	ctr := requestsTotal.WithLabelValues(http.MethodGet, path, "204")
	before := testutil.ToFloat64(ctr)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("handler status: got %d", rr.Code)
	}

	after := testutil.ToFloat64(ctr)
	if after != before+1 {
		t.Fatalf("nms_requests_total: before=%v after=%v", before, after)
	}
}
