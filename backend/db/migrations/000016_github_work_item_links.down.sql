DROP INDEX IF EXISTS pull_requests_work_item_idx;
ALTER TABLE pull_requests
    DROP CONSTRAINT IF EXISTS pull_requests_work_item_fk,
    DROP COLUMN IF EXISTS work_item_id,
    DROP COLUMN IF EXISTS head_ref,
    DROP COLUMN IF EXISTS body;
