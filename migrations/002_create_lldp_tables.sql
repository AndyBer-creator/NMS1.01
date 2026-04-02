-- +goose Up
-- LLDP topology snapshots + links

CREATE TABLE IF NOT EXISTS lldp_topology_scans (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS lldp_links (
    id BIGSERIAL PRIMARY KEY,
    scan_id BIGINT NOT NULL REFERENCES lldp_topology_scans(id) ON DELETE CASCADE,

    local_device_ip INET NOT NULL,
    local_port_num INTEGER,
    local_port_desc TEXT,

    remote_device_ip INET NULL,
    remote_sys_name TEXT,
    remote_sys_desc TEXT,

    remote_port_id TEXT,
    remote_port_desc TEXT,

    -- Same LLDP entry can be seen multiple times depending on vendor.
    -- Best-effort uniqueness for the UI.
    UNIQUE (scan_id, local_device_ip, local_port_num, remote_device_ip, remote_sys_name, remote_port_id)
);

CREATE INDEX IF NOT EXISTS idx_lldp_links_scan_id ON lldp_links(scan_id);
CREATE INDEX IF NOT EXISTS idx_lldp_links_local_device_ip ON lldp_links(local_device_ip);
CREATE INDEX IF NOT EXISTS idx_lldp_links_remote_device_ip ON lldp_links(remote_device_ip);

-- +goose Down
DROP TABLE IF EXISTS lldp_links;
DROP TABLE IF EXISTS lldp_topology_scans;
