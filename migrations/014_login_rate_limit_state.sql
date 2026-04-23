-- +goose Up
CREATE TABLE IF NOT EXISTS login_attempt_limits (
    scope TEXT NOT NULL,
    subject TEXT NOT NULL,
    fail_count INTEGER NOT NULL DEFAULT 0,
    first_fail_at TIMESTAMPTZ,
    locked_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scope, subject)
);

CREATE INDEX IF NOT EXISTS idx_login_attempt_limits_updated_at
    ON login_attempt_limits(updated_at);

CREATE INDEX IF NOT EXISTS idx_login_attempt_limits_locked_until
    ON login_attempt_limits(locked_until);

-- +goose Down
DROP TABLE IF EXISTS login_attempt_limits;
