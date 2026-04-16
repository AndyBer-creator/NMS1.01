package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go.uber.org/zap"
)

func (h *Handlers) ListAvailabilityEvents(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	var deviceID *int
	if v := r.URL.Query().Get("device_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			deviceID = &n
		}
	}

	events, err := h.repo.ListAvailabilityEvents(r.Context(), limit, deviceID)
	if err != nil {
		h.logger.Error("ListAvailabilityEvents failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

func (h *Handlers) AvailabilityEventsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.eventsPageTmpl.Execute(w, map[string]any{"CSPNonce": cspNonceFromContext(r)})
}
