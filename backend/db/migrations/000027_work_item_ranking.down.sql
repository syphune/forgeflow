DROP INDEX IF EXISTS work_items_project_sprint_rank_idx;
ALTER TABLE work_items DROP COLUMN IF EXISTS backlog_rank;
