package postgres

import (
	"context"
	"strconv"
	"strings"
)

const (
	SettingKeyWorkerPollIntervalSec = "worker_poll_interval_sec"
	SettingKeyAlertEmailTo          = "alert_email_to"
	SettingKeySNMPTimeoutSec        = "snmp_timeout_sec"
	SettingKeySNMPRetries           = "snmp_retries"

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
		INSERT INTO nms_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
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
	var raw string
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM nms_settings WHERE key = $1`, SettingKeyAlertEmailTo).Scan(&raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

func (r *Repo) SetAlertEmailTo(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		SettingKeyAlertEmailTo, email)
	return err
}
