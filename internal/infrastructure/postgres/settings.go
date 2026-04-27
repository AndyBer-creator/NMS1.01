package postgres

import (
	"context"
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
