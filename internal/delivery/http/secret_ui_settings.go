package http

import (
	"net/http"
	"strings"

	"NMS1/internal/infrastructure/postgres"

	"go.uber.org/zap"
)

type secretSettingsPanelVM struct {
	Admin                   bool
	SMTPPassConfigured      bool
	TelegramTokenConfigured bool
	TelegramChatConfigured  bool
	WebhookTokenConfigured  bool
	GRPCTokenConfigured     bool
	Saved                   bool
	Err                     string
}

func (h *Handlers) SecretSettingsPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := userFromContext(r.Context())
	admin := u != nil && u.role == roleAdmin
	vm := secretSettingsPanelVM{
		Admin:                   admin,
		SMTPPassConfigured:      h.repo.HasSecretSetting(r.Context(), postgres.SettingKeySMTPPassSecret),
		TelegramTokenConfigured: h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyTelegramTokenSecret),
		TelegramChatConfigured:  h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyTelegramChatIDSecret),
		WebhookTokenConfigured:  h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyAlertWebhookSecret),
		GRPCTokenConfigured:     h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyGRPCAuthTokenSecret),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "secretSettingsPanel", vm)
}

func (h *Handlers) SetSecretSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}
	changed := []string{}
	if err := h.applySecretSettingUpdate(
		r,
		"smtp_pass",
		"clear_smtp_pass",
		postgres.SettingKeySMTPPassSecret,
		"SMTP pass",
		&changed,
	); err != nil {
		h.renderSecretSettingsError(w, r, err.Error())
		return
	}
	if err := h.applySecretSettingUpdate(
		r,
		"telegram_token",
		"clear_telegram_token",
		postgres.SettingKeyTelegramTokenSecret,
		"Telegram token",
		&changed,
	); err != nil {
		h.renderSecretSettingsError(w, r, err.Error())
		return
	}
	if err := h.applySecretSettingUpdate(
		r,
		"telegram_chat_id",
		"clear_telegram_chat_id",
		postgres.SettingKeyTelegramChatIDSecret,
		"Telegram chat ID",
		&changed,
	); err != nil {
		h.renderSecretSettingsError(w, r, err.Error())
		return
	}
	if err := h.applySecretSettingUpdate(
		r,
		"alert_webhook_token",
		"clear_alert_webhook_token",
		postgres.SettingKeyAlertWebhookSecret,
		"Alert webhook token",
		&changed,
	); err != nil {
		h.renderSecretSettingsError(w, r, err.Error())
		return
	}
	if err := h.applySecretSettingUpdate(
		r,
		"grpc_auth_token",
		"clear_grpc_auth_token",
		postgres.SettingKeyGRPCAuthTokenSecret,
		"gRPC auth token",
		&changed,
	); err != nil {
		h.renderSecretSettingsError(w, r, err.Error())
		return
	}
	vm := secretSettingsPanelVM{
		Admin:                   true,
		SMTPPassConfigured:      h.repo.HasSecretSetting(r.Context(), postgres.SettingKeySMTPPassSecret),
		TelegramTokenConfigured: h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyTelegramTokenSecret),
		TelegramChatConfigured:  h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyTelegramChatIDSecret),
		WebhookTokenConfigured:  h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyAlertWebhookSecret),
		GRPCTokenConfigured:     h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyGRPCAuthTokenSecret),
	}
	if len(changed) > 0 {
		vm.Saved = true
		changedBy := "unknown"
		if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
			changedBy = strings.TrimSpace(u.username)
		}
		h.logger.Info("secret settings updated", zap.String("changed_by", changedBy), zap.Strings("keys", changed))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "secretSettingsPanel", vm)
}

func (h *Handlers) applySecretSettingUpdate(
	r *http.Request,
	valueField, clearField, settingKey, label string,
	changed *[]string,
) error {
	value := strings.TrimSpace(r.FormValue(valueField))
	clear := strings.TrimSpace(r.FormValue(clearField)) == "1"
	if clear {
		if err := h.repo.SetSecretSetting(r.Context(), settingKey, ""); err != nil {
			h.logger.Error("SetSecretSettings clear failed", zap.String("key", settingKey), zap.Error(err))
			return err
		}
		*changed = append(*changed, label+":cleared")
		return nil
	}
	if value == "" {
		return nil
	}
	if err := h.repo.SetSecretSetting(r.Context(), settingKey, value); err != nil {
		h.logger.Error("SetSecretSettings update failed", zap.String("key", settingKey), zap.Error(err))
		return err
	}
	*changed = append(*changed, label+":updated")
	return nil
}

func (h *Handlers) renderSecretSettingsError(w http.ResponseWriter, r *http.Request, msg string) {
	vm := secretSettingsPanelVM{
		Admin:                   true,
		SMTPPassConfigured:      h.repo.HasSecretSetting(r.Context(), postgres.SettingKeySMTPPassSecret),
		TelegramTokenConfigured: h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyTelegramTokenSecret),
		TelegramChatConfigured:  h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyTelegramChatIDSecret),
		WebhookTokenConfigured:  h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyAlertWebhookSecret),
		GRPCTokenConfigured:     h.repo.HasSecretSetting(r.Context(), postgres.SettingKeyGRPCAuthTokenSecret),
		Err:                     "Failed to save secrets: " + strings.TrimSpace(msg),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "secretSettingsPanel", vm)
}
