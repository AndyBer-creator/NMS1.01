package http

import (
	"NMS1/internal/domain"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *Handlers) ListIncidents(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	var deviceID *int
	if v := strings.TrimSpace(r.URL.Query().Get("device_id")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			deviceID = &n
		}
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	severity := strings.TrimSpace(r.URL.Query().Get("severity"))
	items, err := h.repo.ListIncidents(limit, deviceID, status, severity)
	if err != nil {
		h.logger.Error("ListIncidents failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *Handlers) GetIncident(w http.ResponseWriter, r *http.Request) {
	id, err := incidentIDFromChi(r)
	if err != nil {
		http.Error(w, "bad incident id", http.StatusBadRequest)
		return
	}
	item, err := h.repo.GetIncidentByID(id)
	if err != nil {
		h.logger.Error("GetIncidentByID failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if item == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	transitions, err := h.repo.ListIncidentTransitions(id, 100)
	if err != nil {
		h.logger.Error("ListIncidentTransitions failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"incident":    item,
		"transitions": transitions,
	})
}

func (h *Handlers) CreateIncident(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DeviceID *int             `json:"device_id"`
		Title    string           `json:"title"`
		Severity string           `json:"severity"`
		Source   string           `json:"source"`
		Details  *json.RawMessage `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item := &domain.Incident{
		DeviceID: input.DeviceID,
		Title:    input.Title,
		Severity: input.Severity,
		Source:   input.Source,
	}
	if input.Details != nil {
		item.Details = *input.Details
	}
	out, err := h.repo.CreateIncident(item)
	if err != nil {
		h.logger.Error("CreateIncident failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) TransitionIncident(w http.ResponseWriter, r *http.Request) {
	id, err := incidentIDFromChi(r)
	if err != nil {
		http.Error(w, "bad incident id", http.StatusBadRequest)
		return
	}
	var input struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	changedBy := "system"
	if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
		changedBy = strings.TrimSpace(u.username)
	}
	item, err := h.repo.TransitionIncidentStatus(id, input.Status, changedBy, input.Comment)
	if err != nil {
		h.logger.Error("TransitionIncidentStatus failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if item == nil {
		http.Error(w, "incident not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

func incidentIDFromChi(r *http.Request) (int64, error) {
	s := strings.TrimSpace(chi.URLParam(r, "incidentID"))
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}
