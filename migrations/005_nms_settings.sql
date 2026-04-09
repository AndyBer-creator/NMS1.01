-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS nms_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO nms_settings (key, value)
VALUES ('worker_poll_interval_sec', '60')
ON CONFLICT (key) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS nms_settings;
-- +goose StatementEnd
