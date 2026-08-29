DROP TABLE IF EXISTS branch_operations;
DROP TABLE IF EXISTS work_item_branch_plans;
DROP TABLE IF EXISTS project_branch_policies;

DELETE FROM role_permissions WHERE permission_key = 'branch.manage';
DELETE FROM permissions WHERE key = 'branch.manage';

ALTER TABLE pull_requests
    DROP COLUMN IF EXISTS base_ref;
