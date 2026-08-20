DROP INDEX IF EXISTS work_items_project_archived_updated_idx;
ALTER TABLE work_items
    DROP COLUMN IF EXISTS archived_by,
    DROP COLUMN IF EXISTS archived_at;
