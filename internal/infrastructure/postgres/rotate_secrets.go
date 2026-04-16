package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type RotateDeviceSecretsStats struct {
	Scanned int
	Updated int
	Skipped int
}

func RotateDeviceSNMPSecrets(ctx context.Context, db *sql.DB, oldKey, newKey string, dryRun bool) (RotateDeviceSecretsStats, error) {
	var stats RotateDeviceSecretsStats
	oldProtector, err := newSecretProtector(oldKey)
	if err != nil {
		return stats, err
	}
	newProtector, err := newSecretProtector(newKey)
	if err != nil {
		return stats, err
	}
	if !oldProtector.enabled {
		return stats, fmt.Errorf("rotation requires NMS_DB_ENCRYPTION_OLD_KEY")
	}
	if !newProtector.enabled {
		return stats, fmt.Errorf("rotation requires NMS_DB_ENCRYPTION_KEY")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id,
		       COALESCE(community, ''),
		       community_enc,
		       COALESCE(auth_pass, ''),
		       auth_pass_enc,
		       COALESCE(priv_pass, ''),
		       priv_pass_enc
		FROM devices
		ORDER BY id`)
	if err != nil {
		return stats, err
	}
	defer func() { _ = rows.Close() }()

	type updateRow struct {
		id           int
		community    sql.NullString
		communityEnc sql.NullString
		authPass     sql.NullString
		authPassEnc  sql.NullString
		privPass     sql.NullString
		privPassEnc  sql.NullString
	}
	var updates []updateRow

	for rows.Next() {
		stats.Scanned++
		var (
			id                         int
			communityPlain             string
			communityEnc               sql.NullString
			authPassPlain              string
			authPassEnc                sql.NullString
			privPassPlain              string
			privPassEnc                sql.NullString
			changedCommunity, changedA bool
			changedP                   bool
		)
		if err := rows.Scan(
			&id,
			&communityPlain,
			&communityEnc,
			&authPassPlain,
			&authPassEnc,
			&privPassPlain,
			&privPassEnc,
		); err != nil {
			return stats, err
		}

		communityPlainOut, communityEncOut, changedCommunity, err := rotateSecretValue(oldProtector, newProtector, communityPlain, communityEnc)
		if err != nil {
			return stats, fmt.Errorf("device id=%d community rotate: %w", id, err)
		}
		authPassPlainOut, authPassEncOut, changedA, err := rotateSecretValue(oldProtector, newProtector, authPassPlain, authPassEnc)
		if err != nil {
			return stats, fmt.Errorf("device id=%d auth_pass rotate: %w", id, err)
		}
		privPassPlainOut, privPassEncOut, changedP, err := rotateSecretValue(oldProtector, newProtector, privPassPlain, privPassEnc)
		if err != nil {
			return stats, fmt.Errorf("device id=%d priv_pass rotate: %w", id, err)
		}

		if !changedCommunity && !changedA && !changedP {
			stats.Skipped++
			continue
		}
		stats.Updated++
		updates = append(updates, updateRow{
			id:           id,
			community:    communityPlainOut,
			communityEnc: communityEncOut,
			authPass:     authPassPlainOut,
			authPassEnc:  authPassEncOut,
			privPass:     privPassPlainOut,
			privPassEnc:  privPassEncOut,
		})
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	if dryRun {
		return stats, nil
	}

	for _, u := range updates {
		_, err := tx.ExecContext(ctx, `
			UPDATE devices
			SET community = $1,
			    community_enc = $2,
			    auth_pass = $3,
			    auth_pass_enc = $4,
			    priv_pass = $5,
			    priv_pass_enc = $6
			WHERE id = $7`,
			u.community,
			u.communityEnc,
			u.authPass,
			u.authPassEnc,
			u.privPass,
			u.privPassEnc,
			u.id,
		)
		if err != nil {
			return stats, fmt.Errorf("device id=%d update: %w", u.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

func rotateSecretValue(oldProtector, newProtector *secretProtector, plain string, enc sql.NullString) (sql.NullString, sql.NullString, bool, error) {
	plain = strings.TrimSpace(plain)
	hasEnc := enc.Valid && strings.TrimSpace(enc.String) != ""
	if plain == "" && !hasEnc {
		return sql.NullString{}, sql.NullString{}, false, nil
	}

	source := plain
	if hasEnc {
		decrypted, err := oldProtector.decrypt(enc.String)
		if err != nil {
			return sql.NullString{}, sql.NullString{}, false, err
		}
		source = strings.TrimSpace(decrypted)
	}

	nextPlain, nextEnc, err := newProtector.splitSecretForStorage(source)
	if err != nil {
		return sql.NullString{}, sql.NullString{}, false, err
	}
	changed := plain != normalizeNullString(nextPlain) || normalizeNullString(enc) != normalizeNullString(nextEnc)
	return nextPlain, nextEnc, changed, nil
}

func normalizeNullString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}
