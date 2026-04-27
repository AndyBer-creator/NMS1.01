package postgres

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

func (r *Repo) setIntSetting(ctx context.Context, key string, value int) error {
	return r.setIntSettingWithExec(ctx, r.db, key, value)
}

func (r *Repo) setIntSettingWithExec(ctx context.Context, exec sqlExecutor, key string, value int) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, value_enc, updated_at)
		VALUES ($1, $2, NULL, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, value_enc = NULL, updated_at = NOW()`,
		key, strconv.Itoa(value))
	return err
}

func (r *Repo) SetStringSetting(ctx context.Context, key, value string) error {
	return r.setStringSettingWithExec(ctx, r.db, key, value)
}

func (r *Repo) setStringSettingWithExec(ctx context.Context, exec sqlExecutor, key, value string) error {
	value = strings.TrimSpace(value)
	_, err := exec.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, value_enc, updated_at)
		VALUES ($1, $2, NULL, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, value_enc = NULL, updated_at = NOW()`,
		key, value)
	return err
}

// SetSecretSetting stores secret as encrypted payload when DB encryption is enabled.
func (r *Repo) SetSecretSetting(ctx context.Context, key, value string) error {
	return r.setSecretSettingWithExec(ctx, r.db, key, value)
}

func (r *Repo) setSecretSettingWithExec(ctx context.Context, exec sqlExecutor, key, value string) error {
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
	_, err := exec.ExecContext(ctx, `
		INSERT INTO nms_settings (key, value, value_enc, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, value_enc = EXCLUDED.value_enc, updated_at = NOW()`,
		key, plain, enc)
	return err
}
