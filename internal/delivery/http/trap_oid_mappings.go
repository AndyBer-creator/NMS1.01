package http

import (
	"NMS1/internal/domain"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

func (h *Handlers) TrapOIDMappingsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.trapOIDMappingsPageTmpl.Execute(w, map[string]any{"CSPNonce": cspNonceFromContext(r)})
}

func (h *Handlers) ListTrapOIDMappings(w http.ResponseWriter, r *http.Request) {
	if h.trapHTTPRepo == nil {
		http.Error(w, "traps repository is not configured", http.StatusInternalServerError)
		return
	}
	vendor := strings.TrimSpace(r.URL.Query().Get("vendor"))
	var enabled *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("enabled")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			http.Error(w, "bad enabled filter", http.StatusBadRequest)
			return
		}
		enabled = &v
	}
	items, err := h.trapHTTPRepo.ListOIDMappings(r.Context(), vendor, enabled)
	if err != nil {
		h.logger.Error("ListOIDMappings failed", zap.Error(err))
		http.Error(w, "failed to load trap OID mappings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *Handlers) CreateTrapOIDMapping(w http.ResponseWriter, r *http.Request) {
	if h.trapHTTPRepo == nil {
		http.Error(w, "traps repository is not configured", http.StatusInternalServerError)
		return
	}
	var input domain.TrapOIDMapping
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	created, err := h.trapHTTPRepo.CreateOIDMapping(r.Context(), &input)
	if err != nil {
		h.logger.Warn("CreateOIDMapping failed", zap.Error(err))
		http.Error(w, "invalid trap OID mapping", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(created)
}

func (h *Handlers) UpdateTrapOIDMapping(w http.ResponseWriter, r *http.Request) {
	if h.trapHTTPRepo == nil {
		http.Error(w, "traps repository is not configured", http.StatusInternalServerError)
		return
	}
	id, err := trapOIDMappingIDFromQuery(r)
	if err != nil {
		http.Error(w, "bad mapping id", http.StatusBadRequest)
		return
	}
	var input domain.TrapOIDMapping
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	updated, err := h.trapHTTPRepo.UpdateOIDMapping(r.Context(), id, &input)
	if err != nil {
		h.logger.Warn("UpdateOIDMapping failed", zap.Int64("id", id), zap.Error(err))
		http.Error(w, "invalid trap OID mapping update", http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.Error(w, "mapping not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *Handlers) DeleteTrapOIDMapping(w http.ResponseWriter, r *http.Request) {
	if h.trapHTTPRepo == nil {
		http.Error(w, "traps repository is not configured", http.StatusInternalServerError)
		return
	}
	id, err := trapOIDMappingIDFromQuery(r)
	if err != nil {
		http.Error(w, "bad mapping id", http.StatusBadRequest)
		return
	}
	deleted, err := h.trapHTTPRepo.DeleteOIDMapping(r.Context(), id)
	if err != nil {
		h.logger.Error("DeleteOIDMapping failed", zap.Int64("id", id), zap.Error(err))
		http.Error(w, "failed to delete trap OID mapping", http.StatusInternalServerError)
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

func trapOIDMappingIDFromQuery(r *http.Request) (int64, error) {
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
