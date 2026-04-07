-- +goose Up
-- Device health fields + SNMP SET audit log

ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS last_error TEXT,
    ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_poll_ok_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS snmp_set_audit (
    id BIGSERIAL PRIMARY KEY,
    ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_name TEXT,
    device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    oid TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    result TEXT NOT NULL,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_snmp_set_audit_ts ON snmp_set_audit(ts DESC);
CREATE INDEX IF NOT EXISTS idx_snmp_set_audit_device_id ON snmp_set_audit(device_id);
CREATE INDEX IF NOT EXISTS idx_devices_last_error_at ON devices(last_error_at DESC);
CREATE INDEX IF NOT EXISTS idx_devices_last_poll_ok_at ON devices(last_poll_ok_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_devices_last_poll_ok_at;
DROP INDEX IF EXISTS idx_devices_last_error_at;
DROP INDEX IF EXISTS idx_snmp_set_audit_device_id;
DROP INDEX IF EXISTS idx_snmp_set_audit_ts;
DROP TABLE IF EXISTS snmp_set_audit;

ALTER TABLE devices
    DROP COLUMN IF EXISTS last_poll_ok_at,
    DROP COLUMN IF EXISTS last_error_at,
    DROP COLUMN IF EXISTS last_error;
