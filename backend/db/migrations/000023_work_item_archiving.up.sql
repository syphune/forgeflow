ALTER TABLE work_items
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN archived_by text;

CREATE INDEX work_items_project_archived_updated_idx
    ON work_items (organization_id, project_id, archived_at, updated_at DESC);
