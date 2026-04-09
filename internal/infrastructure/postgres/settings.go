package postgres

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

const (
	SettingKeyWorkerPollIntervalSec = "worker_poll_interval_sec"

	DefaultWorkerPollIntervalSeconds = 60
	MinWorkerPollIntervalSeconds     = 10
	MaxWorkerPollIntervalSeconds     = 86400 // 24 ч
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

// GetWorkerPollIntervalSeconds возвращает интервал между циклами SNMP-опроса в worker (секунды).
func (r *Repo) GetWorkerPollIntervalSeconds() int {
	var raw string
	err := r.db.QueryRowContext(context.Background(),
		`SELECT value FROM nms_settings WHERE key = $1`, SettingKeyWorkerPollIntervalSec).Scan(&raw)
	if err != nil {
		if err != sql.ErrNoRows {
			// таблица есть, но ошибка — безопасный дефолт
		}
		return DefaultWorkerPollIntervalSeconds
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return DefaultWorkerPollIntervalSeconds
	}
	return clampWorkerPollIntervalSec(n)
}

// SetWorkerPollIntervalSeconds сохраняет интервал (секунды), значение приводится к допустимому диапазону.
func (r *Repo) SetWorkerPollIntervalSeconds(sec int) error {
	sec = clampWorkerPollIntervalSec(sec)
	_, err := r.db.ExecContext(context.Background(), `
		INSERT INTO nms_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		SettingKeyWorkerPollIntervalSec, strconv.Itoa(sec))
	return err
}
