package postgres

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

func (r *Repo) getIntSetting(ctx context.Context, key string, fallback int, clamp func(int) int) int {
	return r.getIntSettingWithExec(ctx, r.db, key, fallback, clamp)
}

func (r *Repo) getIntSettingWithExec(ctx context.Context, exec sqlExecutor, key string, fallback int, clamp func(int) int) int {
	var raw string
	err := exec.QueryRowContext(ctx, `SELECT value FROM nms_settings WHERE key = $1`, key).Scan(&raw)
	if err != nil {
		return clamp(fallback)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return clamp(fallback)
	}
	return clamp(n)
}

func (r *Repo) GetStringSetting(ctx context.Context, key string) string {
	return r.getStringSettingWithExec(ctx, r.db, key)
}

func (r *Repo) getStringSettingWithExec(ctx context.Context, exec sqlExecutor, key string) string {
	var raw string
	err := exec.QueryRowContext(ctx, `SELECT value FROM nms_settings WHERE key = $1`, key).Scan(&raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

// GetSecretSetting returns decrypted setting value when encrypted storage is used.
func (r *Repo) GetSecretSetting(ctx context.Context, key string) (string, error) {
	return r.getSecretSettingWithExec(ctx, r.db, key)
}

func (r *Repo) getSecretSettingWithExec(ctx context.Context, exec sqlExecutor, key string) (string, error) {
	var plain string
	var enc sql.NullString
	err := exec.QueryRowContext(ctx, `SELECT value, value_enc FROM nms_settings WHERE key = $1`, key).Scan(&plain, &enc)
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
	return r.hasSecretSettingWithExec(ctx, r.db, key)
}

func (r *Repo) hasSecretSettingWithExec(ctx context.Context, exec sqlExecutor, key string) bool {
	var plain string
	var enc sql.NullString
	err := exec.QueryRowContext(ctx, `SELECT value, value_enc FROM nms_settings WHERE key = $1`, key).Scan(&plain, &enc)
	if err != nil {
		return false
	}
	if strings.TrimSpace(plain) != "" {
		return true
	}
	return enc.Valid && strings.TrimSpace(enc.String) != ""
}
