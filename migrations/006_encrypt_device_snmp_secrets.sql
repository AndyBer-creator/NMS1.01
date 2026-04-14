-- +goose Up
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS community_enc TEXT,
    ADD COLUMN IF NOT EXISTS auth_pass_enc TEXT,
    ADD COLUMN IF NOT EXISTS priv_pass_enc TEXT;

-- +goose Down
ALTER TABLE devices
    DROP COLUMN IF EXISTS priv_pass_enc,
    DROP COLUMN IF EXISTS auth_pass_enc,
    DROP COLUMN IF EXISTS community_enc;
