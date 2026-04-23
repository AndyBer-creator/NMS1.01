package http

import (
	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"NMS1/internal/services"

	"go.uber.org/zap"
)

type alertmanagerWebhookPayload struct {
	Status string `json:"status"`
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
	} `json:"alerts"`
}

func (h *Handlers) alertWebhookToken(ctx context.Context) string {
	fromDB, err := h.repo.GetSecretSetting(ctx, postgres.SettingKeyAlertWebhookSecret)
	if err == nil && strings.TrimSpace(fromDB) != "" {
		return strings.TrimSpace(fromDB)
	}
	return strings.TrimSpace(config.EnvOrFile("NMS_ALERT_WEBHOOK_TOKEN"))
}

func (h *Handlers) alertWebhookAuthorized(r *http.Request) bool {
	if r == nil {
		return false
	}
	want := h.alertWebhookToken(r.Context())
	if want == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(got), "bearer ") {
		got = strings.TrimSpace(got[7:])
	}
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// AlertWebhook receives Alertmanager webhooks and forwards to Telegram/Email.
func (h *Handlers) AlertWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.alertWebhookAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var p alertmanagerWebhookPayload
	if err := decodeJSONBody(w, r, &p); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if len(p.Alerts) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	emailTo := h.repo.GetAlertEmailTo(r.Context())
	smtpHost := strings.TrimSpace(h.repo.GetStringSetting(r.Context(), postgres.SettingKeySMTPHost))
	if smtpHost == "" {
		smtpHost = config.EnvOrFile("SMTP_HOST")
	}
	smtpPort := strings.TrimSpace(h.repo.GetStringSetting(r.Context(), postgres.SettingKeySMTPPort))
	if smtpPort == "" {
		smtpPort = defaultIfEmpty(config.EnvOrFile("SMTP_PORT"), "587")
	}
	smtpFrom := strings.TrimSpace(h.repo.GetStringSetting(r.Context(), postgres.SettingKeySMTPFrom))
	if smtpFrom == "" {
		smtpFrom = config.EnvOrFile("SMTP_FROM")
	}
	smtpPass, passErr := h.repo.GetSecretSetting(r.Context(), postgres.SettingKeySMTPPassSecret)
	if passErr != nil || strings.TrimSpace(smtpPass) == "" {
		smtpPass = config.EnvOrFile("SMTP_PASS")
	}
	smtpClient := services.NewSMTPClient(
		smtpHost,
		smtpPort,
		config.EnvOrFile("SMTP_USER"),
		smtpPass,
		smtpFrom,
	)
	telegramToken, tErr := h.repo.GetSecretSetting(r.Context(), postgres.SettingKeyTelegramTokenSecret)
	if tErr != nil || strings.TrimSpace(telegramToken) == "" {
		telegramToken = config.EnvOrFile("TELEGRAM_TOKEN")
	}
	telegramChatID, cErr := h.repo.GetSecretSetting(r.Context(), postgres.SettingKeyTelegramChatIDSecret)
	if cErr != nil || strings.TrimSpace(telegramChatID) == "" {
		telegramChatID = config.EnvOrFile("TELEGRAM_CHAT_ID")
	}

	telegram := services.NewTelegramAlert(
		telegramToken,
		telegramChatID,
	)

	var sent, skipped int
	for _, a := range p.Alerts {
		if strings.ToLower(strings.TrimSpace(a.Status)) != "firing" {
			continue
		}
		name := strings.TrimSpace(a.Labels["alertname"])
		if name == "" {
			name = "NMS Alert"
		}
		summary := strings.TrimSpace(a.Annotations["summary"])
		desc := strings.TrimSpace(a.Annotations["description"])
		if summary == "" {
			summary = name
		}
		body := fmt.Sprintf("%s\n\n%s\n\nstatus=%s\nstartsAt=%s\nlabels=%v",
			summary, desc, a.Status, a.StartsAt.Format(time.RFC3339), a.Labels)

		// Telegram (best-effort).
		if telegram.BotToken != "" && telegram.ChatID != "" {
			sendCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			err := telegram.SendCriticalTrapContext(sendCtx, "alertmanager", name, body)
			cancel()
			if err != nil {
				h.logger.Warn("telegram alert send failed", zap.Error(err), zap.String("alert", name))
			}
		}

		// Email (best-effort).
		if emailTo != "" && smtpClient.Enabled() {
			sendCtx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
			err := smtpClient.SendContext(sendCtx, emailTo, "[NMS] "+summary, body)
			cancel()
			if err != nil {
				h.logger.Warn("email alert send failed", zap.Error(err), zap.String("alert", name), zap.String("to", emailTo))
			} else {
				sent++
			}
		} else {
			skipped++
		}
	}

	h.logger.Info("alert webhook processed",
		zap.Int("alerts_total", len(p.Alerts)),
		zap.Int("email_sent", sent),
		zap.Int("email_skipped", skipped))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"alerts_total":  len(p.Alerts),
		"email_sent":    sent,
		"email_skipped": skipped,
	})
}

func defaultIfEmpty(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}
