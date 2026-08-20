DROP TABLE IF EXISTS work_item_labels;
DROP TABLE IF EXISTS labels;
DROP INDEX IF EXISTS work_items_project_priority_due_idx;
ALTER TABLE work_items
    DROP COLUMN IF EXISTS estimate_points,
    DROP COLUMN IF EXISTS due_at,
    DROP COLUMN IF EXISTS reporter_id,
    DROP COLUMN IF EXISTS priority;
