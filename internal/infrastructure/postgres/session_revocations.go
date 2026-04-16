package postgres

import (
	"context"
	"time"
)

func (r *Repo) RevokeSessionJTI(ctx context.Context, jti string, expUnix int64) error {
	if r == nil || r.db == nil || jti == "" || expUnix <= 0 {
		return nil
	}
	expiresAt := time.Unix(expUnix, 0).UTC()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM session_revocations WHERE expires_at < NOW()`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO session_revocations (jti, expires_at, revoked_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (jti) DO UPDATE SET expires_at = EXCLUDED.expires_at, revoked_at = NOW()`,
		jti, expiresAt,
	)
	return err
}

func (r *Repo) IsSessionJTIRevoked(ctx context.Context, jti string, nowUnix int64) (bool, error) {
	if r == nil || r.db == nil || jti == "" {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			  FROM session_revocations
			 WHERE jti = $1
			   AND expires_at >= to_timestamp($2)
		)`, jti, nowUnix,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
