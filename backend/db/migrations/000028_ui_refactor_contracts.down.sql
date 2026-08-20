DROP TABLE IF EXISTS work_item_column_orderings;
ALTER TABLE agent_runs
    DROP COLUMN IF EXISTS interruption_reason,
    DROP COLUMN IF EXISTS last_heartbeat_at,
    DROP COLUMN IF EXISTS execution_policy,
    DROP COLUMN IF EXISTS approval_fingerprint,
    DROP COLUMN IF EXISTS approval_fingerprint_version;
ALTER TABLE specifications
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS reviewed_version,
    DROP COLUMN IF EXISTS version;
