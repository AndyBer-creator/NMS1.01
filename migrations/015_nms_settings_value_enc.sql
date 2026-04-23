-- +goose Up
-- +goose StatementBegin
ALTER TABLE nms_settings
    ADD COLUMN IF NOT EXISTS value_enc TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE nms_settings
    DROP COLUMN IF EXISTS value_enc;
-- +goose StatementEnd
