-- +goose Up
CREATE TABLE IF NOT EXISTS session_revocations (
    jti TEXT PRIMARY KEY,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_session_revocations_expires_at
    ON session_revocations(expires_at);

-- +goose Down
DROP TABLE IF EXISTS session_revocations;
