package http

import (
	"NMS1/internal/config"
	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/services"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

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

	var traps []domain.Trap
	var err error
	if ds := strings.TrimSpace(r.URL.Query().Get("device_id")); ds != "" {
		id, convErr := strconv.Atoi(ds)
		if convErr != nil || id <= 0 {
			http.Error(w, "invalid device_id", http.StatusBadRequest)
			return
		}
		dev, derr := h.repo.GetDeviceByID(ctx, id)
		if derr != nil {
			h.logger.Error("GetDeviceByID for traps filter", zap.Int("id", id), zap.Error(derr))
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if dev == nil {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}
		traps, err = h.trapHTTPRepo.ByDevice(ctx, dev.IP, limit)
	} else {
		traps, err = h.trapHTTPRepo.List(ctx, limit)
	}
	if err != nil {
		h.logger.Error("Failed to list traps", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(traps)
}

func (h *Handlers) TrapsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.trapsPageTmpl.Execute(w, map[string]any{"CSPNonce": cspNonceFromContext(r)})
}

func (h *Handlers) testAlert(w http.ResponseWriter, r *http.Request) {
	type testAlertRequest struct {
		DeviceIP string `json:"device_ip"`
		OID      string `json:"oid"`
		TrapVars string `json:"trap_vars,omitempty"`
		Message  string `json:"message,omitempty"`
	}

	var input testAlertRequest
	if err := decodeJSONBody(w, r, &input); err != nil {
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

	botToken, tokErr := h.repo.GetSecretSetting(r.Context(), postgres.SettingKeyTelegramTokenSecret)
	if tokErr != nil || strings.TrimSpace(botToken) == "" {
		botToken = config.EnvOrFile("TELEGRAM_TOKEN")
	}
	chatID, chatErr := h.repo.GetSecretSetting(r.Context(), postgres.SettingKeyTelegramChatIDSecret)
	if chatErr != nil || strings.TrimSpace(chatID) == "" {
		chatID = config.EnvOrFile("TELEGRAM_CHAT_ID")
	}
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
		h.logger.Error("SendCriticalTrap failed", zap.String("device_ip", input.DeviceIP), zap.String("oid", input.OID), zap.Error(err))
		http.Error(w, "Delivery failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"device_ip": input.DeviceIP,
		"oid":       input.OID,
	})
}
