package http

import (
	"NMS1/internal/config"
	"NMS1/internal/repository"
	"NMS1/internal/services"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type TrapHandler struct {
	repo   *repository.TrapsRepo
	logger *zap.Logger
}

func NewTrapHandler(repo *repository.TrapsRepo, logger *zap.Logger) *TrapHandler {
	return &TrapHandler{
		repo:   repo,
		logger: logger, // ← Передаём logger
	}
}

func (h *TrapHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	traps, err := h.repo.List(ctx, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traps)
}

func (h *TrapHandler) ByDevice(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		http.Error(w, "IP required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
		if limit > 1000 {
			limit = 1000
		}
	}

	traps, err := h.repo.ByDevice(ctx, ip, limit)
	if err != nil {
		h.logger.Error("Failed to get traps by device",
			zap.String("ip", ip), zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traps)
}

func (h *Handlers) ListTraps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
		if limit > 1000 {
			limit = 1000
		}
	}

	traps, err := h.TrapsRepo.List(ctx, limit)
	if err != nil {
		h.logger.Error("Failed to list traps", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traps)
}

func (h *Handlers) TrapsByDevice(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		http.Error(w, "IP required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
		if limit > 1000 {
			limit = 1000
		}
	}

	traps, err := h.TrapsRepo.ByDevice(ctx, ip, limit)
	if err != nil {
		h.logger.Error("Failed to get traps by device",
			zap.String("ip", ip), zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traps)
}

func (h *Handlers) TrapsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, "templates/traps_page.html")
}

func (h *Handlers) testAlert(w http.ResponseWriter, r *http.Request) {
	type testAlertRequest struct {
		DeviceIP string `json:"device_ip"`
		OID      string `json:"oid"`
		TrapVars string `json:"trap_vars,omitempty"`
		Message  string `json:"message,omitempty"`
	}

	var input testAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if input.DeviceIP == "" || input.OID == "" {
		http.Error(w, "device_ip and oid are required", http.StatusBadRequest)
		return
	}
	trapVars := input.TrapVars
	if trapVars == "" {
		trapVars = input.Message
	}

	botToken := config.EnvOrFile("TELEGRAM_TOKEN")
	chatID := config.EnvOrFile("TELEGRAM_CHAT_ID")
	if botToken == "" || chatID == "" {
		http.Error(w, "TELEGRAM_TOKEN and TELEGRAM_CHAT_ID must be set", http.StatusInternalServerError)
		return
	}

	telegram := services.NewTelegramAlert(
		botToken,
		chatID,
	)

	err := telegram.SendCriticalTrap(input.DeviceIP, input.OID, trapVars)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"device_ip": input.DeviceIP,
		"oid":       input.OID,
	})
}
