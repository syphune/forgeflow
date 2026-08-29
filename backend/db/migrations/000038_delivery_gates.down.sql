DROP INDEX IF EXISTS deployment_requests_work_item_idx;
ALTER TABLE deployment_requests
    DROP CONSTRAINT IF EXISTS deployment_requests_status_check,
    DROP CONSTRAINT IF EXISTS deployment_requests_work_item_fk,
    DROP COLUMN IF EXISTS work_item_id;
ALTER TABLE ci_runs DROP COLUMN IF EXISTS check_name;
ALTER TABLE pull_requests
    DROP COLUMN IF EXISTS author_login,
    DROP COLUMN IF EXISTS changed_paths,
    DROP COLUMN IF EXISTS approved_by_logins,
    DROP COLUMN IF EXISTS changes_requested,
    DROP COLUMN IF EXISTS merged;
ALTER TABLE specification_regression_cases
    DROP CONSTRAINT IF EXISTS specification_regression_gate_class_check,
    DROP COLUMN IF EXISTS gate_class;
ALTER TABLE work_items
    DROP CONSTRAINT IF EXISTS work_items_risk_level_check,
    DROP COLUMN IF EXISTS risk_level;
DROP TABLE IF EXISTS project_delivery_policies;
