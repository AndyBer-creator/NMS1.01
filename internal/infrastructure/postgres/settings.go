package postgres

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

const (
	SettingKeyWorkerPollIntervalSec = "worker_poll_interval_sec"
	SettingKeyAlertEmailTo          = "alert_email_to"
	SettingKeySNMPTimeoutSec        = "snmp_timeout_sec"
	SettingKeySNMPRetries           = "snmp_retries"
	SettingKeySMTPHost              = "smtp_host"
	SettingKeySMTPPort              = "smtp_port"
	SettingKeySMTPFrom              = "smtp_from"
	SettingKeyGrafanaBaseURL        = "grafana_base_url"
	SettingKeyPrometheusBaseURL     = "prometheus_base_url"
	SettingKeySMTPPassSecret        = "smtp_pass_secret"
	SettingKeyTelegramTokenSecret   = "telegram_token_secret"
	SettingKeyTelegramChatIDSecret  = "telegram_chat_id_secret"
	SettingKeyAlertWebhookSecret    = "alert_webhook_token_secret"
	SettingKeyGRPCAuthTokenSecret   = "grpc_auth_token_secret"

	DefaultWorkerPollIntervalSeconds = 60
	MinWorkerPollIntervalSeconds     = 10
	MaxWorkerPollIntervalSeconds     = 86400 // 24 ч

	DefaultSNMPTimeoutSeconds = 3
	MinSNMPTimeoutSeconds     = 1
	MaxSNMPTimeoutSeconds     = 30

	DefaultSNMPRetries = 1
	MinSNMPRetries     = 0
	MaxSNMPRetries     = 5
)

func clampWorkerPollIntervalSec(n int) int {
	if n < MinWorkerPollIntervalSeconds {
		return MinWorkerPollIntervalSeconds
	}
	if n > MaxWorkerPollIntervalSeconds {
		return MaxWorkerPollIntervalSeconds
	}
	return n
}

func clampSNMPTimeoutSeconds(n int) int {
	if n < MinSNMPTimeoutSeconds {
		return MinSNMPTimeoutSeconds
	}
	if n > MaxSNMPTimeoutSeconds {
		return MaxSNMPTimeoutSeconds
	}
	return n
}

func clampSNMPRetries(n int) int {
	if n < MinSNMPRetries {
		return MinSNMPRetries
	}
	if n > MaxSNMPRetries {
		return MaxSNMPRetries
	}
	return n
}

func (r *Repo) getIntSetting(ctx context.Context, key string, fallback int, clamp func(int) int) int {
	var raw string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM nms_settings WHERE key = $1`, key).Scan(&raw)
	if err != nil {
		return clamp(fallback)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return clamp(fallback)
	}
	return clamp(n)
}

func (r *Repo) setIntSetting(ctx context.Context, key string, value int) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, value_enc, updated_at)
		VALUES ($1, $2, NULL, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, value_enc = NULL, updated_at = NOW()`,
		key, strconv.Itoa(value))
	return err
}

// GetWorkerPollIntervalSeconds возвращает интервал между циклами SNMP-опроса в worker (секунды).
func (r *Repo) GetWorkerPollIntervalSeconds(ctx context.Context) int {
	return r.getIntSetting(ctx, SettingKeyWorkerPollIntervalSec, DefaultWorkerPollIntervalSeconds, clampWorkerPollIntervalSec)
}

// SetWorkerPollIntervalSeconds сохраняет интервал (секунды), значение приводится к допустимому диапазону.
func (r *Repo) SetWorkerPollIntervalSeconds(ctx context.Context, sec int) error {
	sec = clampWorkerPollIntervalSec(sec)
	return r.setIntSetting(ctx, SettingKeyWorkerPollIntervalSec, sec)
}

func (r *Repo) GetSNMPTimeoutSeconds(ctx context.Context, fallback int) int {
	return r.getIntSetting(ctx, SettingKeySNMPTimeoutSec, fallback, clampSNMPTimeoutSeconds)
}

func (r *Repo) SetSNMPTimeoutSeconds(ctx context.Context, sec int) error {
	sec = clampSNMPTimeoutSeconds(sec)
	return r.setIntSetting(ctx, SettingKeySNMPTimeoutSec, sec)
}

func (r *Repo) GetSNMPRetries(ctx context.Context, fallback int) int {
	return r.getIntSetting(ctx, SettingKeySNMPRetries, fallback, clampSNMPRetries)
}

func (r *Repo) SetSNMPRetries(ctx context.Context, retries int) error {
	retries = clampSNMPRetries(retries)
	return r.setIntSetting(ctx, SettingKeySNMPRetries, retries)
}

func (r *Repo) GetAlertEmailTo(ctx context.Context) string {
	return r.GetStringSetting(ctx, SettingKeyAlertEmailTo)
}

func (r *Repo) SetAlertEmailTo(ctx context.Context, email string) error {
	return r.SetStringSetting(ctx, SettingKeyAlertEmailTo, email)
}

func (r *Repo) GetStringSetting(ctx context.Context, key string) string {
	var raw string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM nms_settings WHERE key = $1`, key).Scan(&raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

func (r *Repo) SetStringSetting(ctx context.Context, key, value string) error {
	value = strings.TrimSpace(value)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, value_enc, updated_at)
		VALUES ($1, $2, NULL, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, value_enc = NULL, updated_at = NOW()`,
		key, value)
	return err
}

// GetSecretSetting returns decrypted setting value when encrypted storage is used.
func (r *Repo) GetSecretSetting(ctx context.Context, key string) (string, error) {
	var plain string
	var enc sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT value, value_enc FROM nms_settings WHERE key = $1`, key).Scan(&plain, &enc)
	if err != nil {
		return "", nil
	}
	val, err := r.protector.mergeSecretFromStorage(plain, enc)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(val), nil
}

// HasSecretSetting reports whether a secret has any persisted value.
func (r *Repo) HasSecretSetting(ctx context.Context, key string) bool {
	var plain string
	var enc sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT value, value_enc FROM nms_settings WHERE key = $1`, key).Scan(&plain, &enc)
	if err != nil {
		return false
	}
	if strings.TrimSpace(plain) != "" {
		return true
	}
	return enc.Valid && strings.TrimSpace(enc.String) != ""
}

// SetSecretSetting stores secret as encrypted payload when DB encryption is enabled.
func (r *Repo) SetSecretSetting(ctx context.Context, key, value string) error {
	value = strings.TrimSpace(value)
	plain := ""
	var enc sql.NullString
	if value != "" {
		if r.protector.enabled {
			ciphertext, err := r.protector.encrypt(value)
			if err != nil {
				return err
			}
			enc = sql.NullString{String: ciphertext, Valid: true}
		} else {
			plain = value
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, value_enc, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, value_enc = EXCLUDED.value_enc, updated_at = NOW()`,
		key, plain, enc)
	return err
}
