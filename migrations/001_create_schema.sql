-- +goose Up
-- NMS Полная схема v1.0 (Production-ready) ✅ FIXED FOR GOOSE

-- 📱 Таблица устройств SNMP v1/v2c/v3
CREATE TABLE IF NOT EXISTS devices (
    id SERIAL PRIMARY KEY,
    ip INET UNIQUE NOT NULL,
    name VARCHAR(255),
    community VARCHAR(50) DEFAULT 'public',
    snmp_version VARCHAR(10) DEFAULT 'v2c',          
    auth_proto VARCHAR(10) DEFAULT NULL,             
    auth_pass VARCHAR(100),                          
    priv_proto VARCHAR(10) DEFAULT NULL,             
    priv_pass VARCHAR(100),                          
    version VARCHAR(20) DEFAULT 'unknown',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen TIMESTAMPTZ,
    CONSTRAINT chk_snmp_version CHECK (snmp_version IN ('v1', 'v2c', 'v3'))
);

-- 📊 Метрики (Grafana history)
CREATE TABLE IF NOT EXISTS metrics (
    id BIGSERIAL PRIMARY KEY,
    device_id INTEGER REFERENCES devices(id) ON DELETE CASCADE,
    oid VARCHAR(255) NOT NULL,
    value TEXT,
    timestamp TIMESTAMPTZ DEFAULT NOW()
);

-- 📡 SNMP Traps (critical detection)
CREATE TABLE IF NOT EXISTS traps (
    id BIGSERIAL PRIMARY KEY,
    device_ip INET NOT NULL,
    oid TEXT NOT NULL,
    uptime INTEGER,
    trap_vars JSONB,
    received_at TIMESTAMPTZ DEFAULT NOW(),
    is_critical BOOLEAN DEFAULT FALSE
);

-- ✅ ПРОСТЫЕ индексы (работают в транзакции Goose!)
CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip);
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);
CREATE INDEX IF NOT EXISTS idx_metrics_device_oid ON metrics(device_id, oid);
CREATE INDEX IF NOT EXISTS idx_metrics_device_time ON metrics(device_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_traps_device_ip ON traps(device_ip);
CREATE INDEX IF NOT EXISTS idx_traps_oid ON traps(oid);
CREATE INDEX IF NOT EXISTS idx_traps_received_at ON traps(received_at);
CREATE INDEX IF NOT EXISTS idx_traps_critical ON traps(is_critical) WHERE is_critical = true;
CREATE INDEX IF NOT EXISTS idx_traps_device_time ON traps(device_ip, received_at DESC);

-- Vacuum tuning
ALTER TABLE traps SET (autovacuum_vacuum_scale_factor = 0.05);
ALTER TABLE metrics SET (autovacuum_vacuum_scale_factor = 0.1);

-- +goose Down
DROP TABLE IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS traps CASCADE;
DROP TABLE IF EXISTS devices CASCADE;
