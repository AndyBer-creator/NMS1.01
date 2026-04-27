-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'metrics'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_partitioned_table pt
        JOIN pg_class c ON c.oid = pt.partrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relname = 'metrics'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'metrics_legacy'
    ) THEN
        ALTER TABLE metrics RENAME TO metrics_legacy;
    END IF;
END $$;
-- +goose StatementEnd

CREATE SEQUENCE IF NOT EXISTS metrics_id_seq;

CREATE TABLE IF NOT EXISTS metrics (
    id BIGINT NOT NULL DEFAULT nextval('metrics_id_seq'),
    device_id INTEGER REFERENCES devices(id) ON DELETE CASCADE,
    oid VARCHAR(255) NOT NULL,
    value TEXT,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE ("timestamp");

CREATE OR REPLACE FUNCTION ensure_metrics_partition_for(ts timestamptz)
RETURNS void
LANGUAGE plpgsql
-- +goose StatementBegin
AS $$
DECLARE
    part_start timestamptz := date_trunc('month', ts);
    part_end   timestamptz := part_start + interval '1 month';
    part_name  text := format('metrics_y%sm%s', to_char(part_start, 'YYYY'), to_char(part_start, 'MM'));
BEGIN
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF metrics FOR VALUES FROM (%L) TO (%L)',
        part_name,
        part_start,
        part_end
    );
END;
$$;
-- +goose StatementEnd

SELECT ensure_metrics_partition_for(NOW() - interval '1 month');
SELECT ensure_metrics_partition_for(NOW());
SELECT ensure_metrics_partition_for(NOW() + interval '1 month');
SELECT ensure_metrics_partition_for(NOW() + interval '2 month');

CREATE TABLE IF NOT EXISTS metrics_default PARTITION OF metrics DEFAULT;

-- IMPORTANT:
-- Data backfill from metrics_legacy is intentionally NOT executed inside this migration.
-- Use controlled offline job cmd/backfill-metrics-legacy to migrate large datasets in batches
-- and finalize drop of metrics_legacy after verification.

CREATE SEQUENCE IF NOT EXISTS metrics_id_seq;
ALTER TABLE metrics ALTER COLUMN id SET DEFAULT nextval('metrics_id_seq');
-- Keep sequence ahead of both partitioned and legacy tables during transition period.
-- +goose StatementBegin
DO $$
DECLARE
    metrics_max bigint := 0;
    legacy_max bigint := 0;
BEGIN
    SELECT COALESCE(MAX(id), 0) INTO metrics_max FROM metrics;
    IF EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'metrics_legacy'
    ) THEN
        EXECUTE 'SELECT COALESCE(MAX(id), 0) FROM metrics_legacy' INTO legacy_max;
    END IF;
    PERFORM setval('metrics_id_seq', GREATEST(metrics_max, legacy_max) + 1, false);
END $$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_metrics_device_oid ON metrics(device_id, oid);
CREATE INDEX IF NOT EXISTS idx_metrics_device_time ON metrics(device_id, "timestamp" DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics("timestamp" DESC);

-- +goose Down

CREATE TABLE IF NOT EXISTS metrics_unpartitioned (
    id BIGINT NOT NULL DEFAULT nextval('metrics_id_seq') PRIMARY KEY,
    device_id INTEGER REFERENCES devices(id) ON DELETE CASCADE,
    oid VARCHAR(255) NOT NULL,
    value TEXT,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO metrics_unpartitioned (id, device_id, oid, value, timestamp)
SELECT id, device_id, oid, value, timestamp
FROM metrics;

DROP TABLE IF EXISTS metrics CASCADE;
DROP FUNCTION IF EXISTS ensure_metrics_partition_for(timestamptz);
ALTER TABLE metrics_unpartitioned RENAME TO metrics;
SELECT setval('metrics_id_seq', COALESCE((SELECT MAX(id) FROM metrics), 0) + 1, false);

CREATE INDEX IF NOT EXISTS idx_metrics_device_oid ON metrics(device_id, oid);
CREATE INDEX IF NOT EXISTS idx_metrics_device_time ON metrics(device_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics(timestamp DESC);
