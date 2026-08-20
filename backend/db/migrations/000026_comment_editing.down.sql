DROP INDEX IF EXISTS comments_work_item_updated_idx;
ALTER TABLE comments
    DROP COLUMN IF EXISTS deleted_by,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS updated_at;
