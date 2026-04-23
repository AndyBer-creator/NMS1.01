package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (r *Repo) LoginRateLimitCheck(ctx context.Context, scope, subject string, now time.Time, window, lockout time.Duration, maxAttempts int) (bool, time.Duration, error) {
	if r == nil || r.db == nil || strings.TrimSpace(scope) == "" || strings.TrimSpace(subject) == "" || maxAttempts <= 0 {
		return true, 0, nil
	}
	scope = strings.TrimSpace(scope)
	subject = strings.TrimSpace(subject)
	now = now.UTC()
	lockUntil := now.Add(lockout)
	windowSecs := int(window.Seconds())
	if windowSecs <= 0 {
		windowSecs = 1
	}
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM login_attempt_limits
		WHERE (locked_until IS NULL OR locked_until < NOW())
		  AND updated_at < NOW() - make_interval(secs => $1)
	`, 2*windowSecs); err != nil {
		return true, 0, fmt.Errorf("login limiter cleanup: %w", err)
	}

	var (
		failCount  int
		firstFail  sql.NullTime
		lockedTill sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT fail_count, first_fail_at, locked_until
		FROM login_attempt_limits
		WHERE scope = $1 AND subject = $2
	`, scope, subject).Scan(&failCount, &firstFail, &lockedTill)
	if err == sql.ErrNoRows {
		return true, 0, nil
	}
	if err != nil {
		return true, 0, fmt.Errorf("login limiter query: %w", err)
	}
	if firstFail.Valid && now.Sub(firstFail.Time.UTC()) > window {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE login_attempt_limits
			SET fail_count = 0, first_fail_at = NULL, locked_until = NULL, updated_at = $3
			WHERE scope = $1 AND subject = $2
		`, scope, subject, now); err != nil {
			return true, 0, fmt.Errorf("login limiter reset window: %w", err)
		}
		return true, 0, nil
	}
	if lockedTill.Valid && now.Before(lockedTill.Time.UTC()) {
		return false, lockedTill.Time.UTC().Sub(now), nil
	}
	if failCount >= maxAttempts {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE login_attempt_limits
			SET locked_until = $3, updated_at = $4
			WHERE scope = $1 AND subject = $2
		`, scope, subject, lockUntil, now); err != nil {
			return true, 0, fmt.Errorf("login limiter lock: %w", err)
		}
		return false, lockout, nil
	}
	return true, 0, nil
}

func (r *Repo) LoginRateLimitOnFailure(ctx context.Context, scope, subject string, now time.Time, window, lockout time.Duration, maxAttempts int) error {
	if r == nil || r.db == nil || strings.TrimSpace(scope) == "" || strings.TrimSpace(subject) == "" || maxAttempts <= 0 {
		return nil
	}
	now = now.UTC()
	windowSecs := int(window.Seconds())
	if windowSecs <= 0 {
		windowSecs = 1
	}
	lockoutSecs := int(lockout.Seconds())
	if lockoutSecs <= 0 {
		lockoutSecs = 1
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO login_attempt_limits (scope, subject, fail_count, first_fail_at, locked_until, updated_at)
		VALUES ($1, $2, 1, $3, NULL, $3)
		ON CONFLICT (scope, subject) DO UPDATE
		SET first_fail_at = CASE
				WHEN login_attempt_limits.first_fail_at IS NULL
				  OR $3 - login_attempt_limits.first_fail_at > make_interval(secs => $4)
				THEN $3
				ELSE login_attempt_limits.first_fail_at
			END,
			fail_count = CASE
				WHEN login_attempt_limits.first_fail_at IS NULL
				  OR $3 - login_attempt_limits.first_fail_at > make_interval(secs => $4)
				THEN 1
				ELSE login_attempt_limits.fail_count + 1
			END,
			locked_until = CASE
				WHEN login_attempt_limits.first_fail_at IS NULL
				  OR $3 - login_attempt_limits.first_fail_at > make_interval(secs => $4)
				THEN NULL
				WHEN login_attempt_limits.fail_count + 1 >= $5
				THEN $3 + make_interval(secs => $6)
				ELSE login_attempt_limits.locked_until
			END,
			updated_at = $3
	`, strings.TrimSpace(scope), strings.TrimSpace(subject), now, windowSecs, maxAttempts, lockoutSecs)
	if err != nil {
		return fmt.Errorf("login limiter failure upsert: %w", err)
	}
	return nil
}

func (r *Repo) LoginRateLimitOnSuccess(ctx context.Context, ip, user string) error {
	if r == nil || r.db == nil {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM login_attempt_limits
		WHERE (scope = 'ip' AND subject = $1)
		   OR (scope = 'user' AND subject = $2)
	`, strings.TrimSpace(ip), strings.TrimSpace(user)); err != nil {
		return fmt.Errorf("login limiter success delete: %w", err)
	}
	return nil
}
