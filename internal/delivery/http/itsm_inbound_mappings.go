package http

import (
	"NMS1/internal/domain"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handlers) ITSMInboundMappingsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.itsmInboundMappingsPageTmpl.Execute(w, map[string]any{"CSPNonce": cspNonceFromContext(r)})
}

func (h *Handlers) ListITSMInboundMappings(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	var enabled *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("enabled")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			http.Error(w, "bad enabled filter", http.StatusBadRequest)
			return
		}
		enabled = &v
	}
	items, err := h.repo.ListITSMInboundMappings(r.Context(), provider, enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *Handlers) CreateITSMInboundMapping(w http.ResponseWriter, r *http.Request) {
	var input domain.ITSMInboundMapping
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	created, err := h.repo.CreateITSMInboundMapping(r.Context(), &input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(created)
}

func (h *Handlers) UpdateITSMInboundMapping(w http.ResponseWriter, r *http.Request) {
	id, err := itsmInboundMappingIDFromQuery(r)
	if err != nil {
		http.Error(w, "bad mapping id", http.StatusBadRequest)
		return
	}
	var input domain.ITSMInboundMapping
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	updated, err := h.repo.UpdateITSMInboundMapping(r.Context(), id, &input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.Error(w, "mapping not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *Handlers) DeleteITSMInboundMapping(w http.ResponseWriter, r *http.Request) {
	id, err := itsmInboundMappingIDFromQuery(r)
	if err != nil {
		http.Error(w, "bad mapping id", http.StatusBadRequest)
		return
	}
	deleted, err := h.repo.DeleteITSMInboundMapping(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !deleted {
		http.Error(w, "mapping not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"deleted":    true,
		"mapping_id": id,
	})
}

func itsmInboundMappingIDFromQuery(r *http.Request) (int64, error) {
	s := strings.TrimSpace(r.URL.Query().Get("id"))
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, strconv.ErrSyntax
	}
	return v, nil
}
