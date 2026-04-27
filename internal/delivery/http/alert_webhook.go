package http

import (
	"NMS1/internal/config"
	"NMS1/internal/infrastructure/postgres"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"NMS1/internal/services"

	"go.uber.org/zap"
)

type alertmanagerWebhookPayload struct {
	Status string `json:"status"`
	Alerts []alertmanagerWebhookPayloadAlert `json:"alerts"`
}

type alertmanagerWebhookPayloadAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
}

type webhookRateState struct {
	windowStart time.Time
	count       int
}

var (
	alertWebhookRateMu    sync.Mutex
	alertWebhookRateState = map[string]webhookRateState{}
	alertWebhookDedupeMu  sync.Mutex
	alertWebhookDedupe    = map[string]time.Time{}
)

func alertWebhookRateLimitPerMinute() int {
	v := strings.TrimSpace(config.EnvOrFile("NMS_ALERT_WEBHOOK_RATE_LIMIT_PER_MIN"))
	if v == "" {
		return 60
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 60
	}
	return n
}

func allowAlertWebhookRequest(ip string, now time.Time) (bool, time.Duration) {
	limit := alertWebhookRateLimitPerMinute()
	if limit <= 0 {
		return true, 0
	}
	key := strings.TrimSpace(ip)
	if key == "" {
		key = "unknown"
	}
	alertWebhookRateMu.Lock()
	defer alertWebhookRateMu.Unlock()
	state := alertWebhookRateState[key]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= time.Minute {
		state = webhookRateState{windowStart: now, count: 0}
	}
	if state.count >= limit {
		retry := state.windowStart.Add(time.Minute).Sub(now)
		if retry < 0 {
			retry = 0
		}
		return false, retry
	}
	state.count++
	alertWebhookRateState[key] = state
	return true, 0
}

func alertWebhookIdempotencyTTL() time.Duration {
	v := strings.TrimSpace(config.EnvOrFile("NMS_ALERT_WEBHOOK_IDEMPOTENCY_TTL"))
	if v == "" {
		return 2 * time.Minute
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 2 * time.Minute
	}
	return d
}

func alertFingerprint(a alertmanagerWebhookPayloadAlert) string {
	b, _ := json.Marshal(a)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shouldProcessAlertFingerprint(fp string, now time.Time) bool {
	ttl := alertWebhookIdempotencyTTL()
	if ttl <= 0 || fp == "" {
		return true
	}
	alertWebhookDedupeMu.Lock()
	defer alertWebhookDedupeMu.Unlock()
	for k, exp := range alertWebhookDedupe {
		if now.After(exp) {
			delete(alertWebhookDedupe, k)
		}
	}
	if exp, ok := alertWebhookDedupe[fp]; ok && now.Before(exp) {
		return false
	}
	alertWebhookDedupe[fp] = now.Add(ttl)
	return true
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
	return constantTimeTokenEqual(got, want)
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
	if allowed, retryAfter := allowAlertWebhookRequest(clientIP(r), time.Now()); !allowed {
		secs := int(retryAfter.Seconds()) + 1
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
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

	var sent, skipped, suppressed int
	for _, a := range p.Alerts {
		if strings.ToLower(strings.TrimSpace(a.Status)) != "firing" {
			continue
		}
		if !shouldProcessAlertFingerprint(alertFingerprint(a), time.Now()) {
			suppressed++
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
		zap.Int("email_skipped", skipped),
		zap.Int("suppressed", suppressed))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"alerts_total":  len(p.Alerts),
		"email_sent":    sent,
		"email_skipped": skipped,
		"suppressed":    suppressed,
	})
}

func defaultIfEmpty(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}
