-- +goose Up
-- Базовый lifecycle инцидентов (new -> acknowledged -> in_progress -> resolved -> closed)

CREATE TABLE IF NOT EXISTS incidents (
    id BIGSERIAL PRIMARY KEY,
    device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    severity VARCHAR(20) NOT NULL DEFAULT 'warning'
        CHECK (severity IN ('critical', 'warning', 'info')),
    status VARCHAR(20) NOT NULL DEFAULT 'new'
        CHECK (status IN ('new', 'acknowledged', 'in_progress', 'resolved', 'closed')),
    source VARCHAR(20) NOT NULL DEFAULT 'polling'
        CHECK (source IN ('polling', 'trap', 'manual')),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_incidents_status_updated
    ON incidents(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_device
    ON incidents(device_id);
CREATE INDEX IF NOT EXISTS idx_incidents_severity
    ON incidents(severity);

CREATE TABLE IF NOT EXISTS incident_transitions (
    id BIGSERIAL PRIMARY KEY,
    incident_id BIGINT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    from_status VARCHAR(20) NOT NULL,
    to_status VARCHAR(20) NOT NULL,
    changed_by VARCHAR(128) NOT NULL DEFAULT 'system',
    comment TEXT NOT NULL DEFAULT '',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_incident_transitions_incident
    ON incident_transitions(incident_id, changed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS incident_transitions;
DROP TABLE IF EXISTS incidents;
