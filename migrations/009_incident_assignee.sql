-- +goose Up
-- Add assignee field for incident ownership workflow.

ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS assignee VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_incidents_assignee
    ON incidents(assignee)
    WHERE assignee IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_incidents_assignee;
ALTER TABLE incidents DROP COLUMN IF EXISTS assignee;
