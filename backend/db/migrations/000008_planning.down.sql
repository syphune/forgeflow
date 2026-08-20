DROP TABLE IF EXISTS board_column_statuses;
DROP TABLE IF EXISTS board_columns;
DROP TABLE IF EXISTS boards;
DROP INDEX IF EXISTS work_items_project_sprint_status_idx;
DROP INDEX IF EXISTS sprints_project_status_idx;
DROP INDEX IF EXISTS sprints_one_active_idx;
ALTER TABLE work_items DROP CONSTRAINT IF EXISTS work_items_sprint_fk;
DROP TABLE IF EXISTS sprints;
ALTER TABLE work_items DROP COLUMN IF EXISTS sprint_id;
