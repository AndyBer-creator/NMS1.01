-- +goose Up
-- Vendor/profile-driven mapping for trap OID classification.

CREATE TABLE IF NOT EXISTS trap_oid_mappings (
    id BIGSERIAL PRIMARY KEY,
    vendor VARCHAR(64) NOT NULL DEFAULT 'generic',
    oid_pattern TEXT NOT NULL,
    signal_kind VARCHAR(32) NOT NULL
        CHECK (signal_kind IN ('generic', 'link_down', 'link_up', 'bfd_down', 'bfd_up')),
    title TEXT NOT NULL,
    severity VARCHAR(20) NOT NULL DEFAULT 'warning'
        CHECK (severity IN ('critical', 'warning', 'info')),
    is_recovery BOOLEAN NOT NULL DEFAULT FALSE,
    priority INTEGER NOT NULL DEFAULT 100,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (vendor, oid_pattern, signal_kind)
);

CREATE INDEX IF NOT EXISTS idx_trap_oid_mappings_enabled_priority
    ON trap_oid_mappings(enabled, priority DESC, id ASC);

INSERT INTO trap_oid_mappings (vendor, oid_pattern, signal_kind, title, severity, is_recovery, priority, enabled)
VALUES
    ('generic', '%linkdown%', 'link_down', 'Link loss detected', 'critical', FALSE, 100, TRUE),
    ('generic', '%bfddown%', 'bfd_down', 'Link loss detected', 'critical', FALSE, 100, TRUE),
    ('generic', '%bfd.down%', 'bfd_down', 'Link loss detected', 'critical', FALSE, 100, TRUE),
    ('generic', '%bfd_down%', 'bfd_down', 'Link loss detected', 'critical', FALSE, 100, TRUE),
    ('generic', '%linkup%', 'link_up', 'Link recovery detected', 'info', TRUE, 100, TRUE),
    ('generic', '%bfdup%', 'bfd_up', 'Link recovery detected', 'info', TRUE, 100, TRUE),
    ('generic', '%bfd.up%', 'bfd_up', 'Link recovery detected', 'info', TRUE, 100, TRUE),
    ('generic', '%bfd_up%', 'bfd_up', 'Link recovery detected', 'info', TRUE, 100, TRUE),
    ('generic', '%coldstart%', 'generic', 'SNMP coldStart detected', 'critical', FALSE, 50, TRUE)
ON CONFLICT (vendor, oid_pattern, signal_kind) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS trap_oid_mappings;
