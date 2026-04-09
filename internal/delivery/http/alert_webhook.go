package http

import (
	"NMS1/internal/config"
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

// AlertWebhook receives Alertmanager webhooks and forwards to Telegram/Email.
func (h *Handlers) AlertWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p alertmanagerWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if len(p.Alerts) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	emailTo := h.repo.GetAlertEmailTo()
	smtpClient := services.NewSMTPClient(
		config.EnvOrFile("SMTP_HOST"),
		defaultIfEmpty(config.EnvOrFile("SMTP_PORT"), "587"),
		config.EnvOrFile("SMTP_USER"),
		config.EnvOrFile("SMTP_PASS"),
		config.EnvOrFile("SMTP_FROM"),
	)

	telegram := services.NewTelegramAlert(
		config.EnvOrFile("TELEGRAM_TOKEN"),
		config.EnvOrFile("TELEGRAM_CHAT_ID"),
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
			if err := runWithTimeout(5*time.Second, func() error {
				return telegram.SendCriticalTrap("alertmanager", name, body)
			}); err != nil {
				h.logger.Warn("telegram alert send failed", zap.Error(err), zap.String("alert", name))
			}
		}

		// Email (best-effort).
		if emailTo != "" && smtpClient.Enabled() {
			if err := runWithTimeout(6*time.Second, func() error {
				return smtpClient.Send(emailTo, "[NMS] "+summary, body)
			}); err != nil {
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

func runWithTimeout(timeout time.Duration, fn func() error) error {
	ch := make(chan error, 1)
	go func() {
		ch <- fn()
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout after %s", timeout)
	}
}

func defaultIfEmpty(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}
