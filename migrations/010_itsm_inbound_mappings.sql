-- +goose Up
CREATE TABLE IF NOT EXISTS itsm_inbound_mappings (
    id BIGSERIAL PRIMARY KEY,
    provider VARCHAR(64) NOT NULL DEFAULT 'generic',
    external_status VARCHAR(64) NOT NULL DEFAULT '',
    external_priority VARCHAR(64) NOT NULL DEFAULT '',
    external_owner VARCHAR(128) NOT NULL DEFAULT '',
    mapped_status VARCHAR(32) NOT NULL DEFAULT '',
    mapped_assignee VARCHAR(128) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (mapped_status IN ('', 'new', 'acknowledged', 'in_progress', 'resolved', 'closed')),
    CHECK (mapped_status <> '' OR mapped_assignee <> '')
);

CREATE INDEX IF NOT EXISTS idx_itsm_inbound_mappings_lookup
    ON itsm_inbound_mappings (enabled, provider, external_status, external_priority, external_owner, priority, id);

INSERT INTO itsm_inbound_mappings
    (provider, external_status, mapped_status, priority, enabled)
VALUES
    ('generic', 'open', 'new', 10, TRUE),
    ('generic', 'ack', 'acknowledged', 20, TRUE),
    ('generic', 'in_progress', 'in_progress', 30, TRUE),
    ('generic', 'resolved', 'resolved', 40, TRUE),
    ('generic', 'closed', 'closed', 50, TRUE)
ON CONFLICT DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS idx_itsm_inbound_mappings_lookup;
DROP TABLE IF EXISTS itsm_inbound_mappings;
