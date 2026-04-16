-- +goose Up
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS community_enc TEXT,
    ADD COLUMN IF NOT EXISTS auth_pass_enc TEXT,
    ADD COLUMN IF NOT EXISTS priv_pass_enc TEXT;

-- +goose Down
DO $$
BEGIN
    RAISE EXCEPTION 'migration 006 is irreversible: encrypted SNMP secret columns may contain the only remaining copy of device credentials';
END $$;
