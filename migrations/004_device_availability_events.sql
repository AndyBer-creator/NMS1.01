-- +goose Up
-- История недоступности/восстановления по результатам SNMP-опроса worker

CREATE TABLE IF NOT EXISTS device_availability_events (
    id BIGSERIAL PRIMARY KEY,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('unavailable', 'available')),
    detail TEXT
);

CREATE INDEX IF NOT EXISTS idx_device_availability_events_occurred
    ON device_availability_events(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_device_availability_events_device
    ON device_availability_events(device_id);

-- +goose Down
DROP TABLE IF EXISTS device_availability_events;
