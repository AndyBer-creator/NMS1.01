CREATE TABLE IF NOT EXISTS devices (
    id SERIAL PRIMARY KEY,
    ip VARCHAR(45) UNIQUE NOT NULL,
    name VARCHAR(255),
    community VARCHAR(50) DEFAULT 'public',
    version VARCHAR(20) DEFAULT 'v2c',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT NOW(),
    last_seen TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS metrics (
    id BIGSERIAL PRIMARY KEY,
    device_id INTEGER REFERENCES devices(id),
    oid VARCHAR(255) NOT NULL,
    value TEXT,
    timestamp TIMESTAMP DEFAULT NOW()
);
