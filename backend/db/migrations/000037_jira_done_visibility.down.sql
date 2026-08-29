DROP INDEX IF EXISTS work_items_project_completed_idx;
ALTER TABLE work_items DROP COLUMN IF EXISTS completed_at;
ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_done_visibility_days_check,
    DROP COLUMN IF EXISTS done_visibility_days;
