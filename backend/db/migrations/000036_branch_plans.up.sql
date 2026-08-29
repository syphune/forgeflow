ALTER TABLE pull_requests
    ADD COLUMN base_ref text NOT NULL DEFAULT '';

INSERT INTO permissions (key, display_name)
VALUES ('branch.manage', 'Manage Forgeflow branches and pull requests')
ON CONFLICT (key) DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT role_key, 'branch.manage'
FROM (VALUES ('owner'), ('admin'), ('project_manager'), ('developer')) AS roles(role_key)
ON CONFLICT DO NOTHING;

CREATE TABLE project_branch_policies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    development_branch text NOT NULL DEFAULT 'develop',
    production_branch text NOT NULL DEFAULT 'main',
    release_prefix text NOT NULL DEFAULT 'release/',
    work_branch_template text NOT NULL DEFAULT 'forgeflow/{assignee}/{type}/{task-key}-{slug}',
    rules jsonb NOT NULL DEFAULT '[]',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, project_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE TABLE work_item_branch_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    work_item_key text NOT NULL,
    branch_type text NOT NULL,
    release_branch text,
    base_branch text NOT NULL,
    target_branch text NOT NULL,
    working_branch text NOT NULL,
    sync_branches jsonb NOT NULL DEFAULT '[]',
    create_pull_request boolean NOT NULL DEFAULT true,
    require_sync boolean NOT NULL DEFAULT false,
    base_sha text NOT NULL,
    policy_version bigint NOT NULL,
    work_item_version bigint NOT NULL,
    state text NOT NULL DEFAULT 'PENDING',
    error_message text,
    created_by text NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, project_id, id),
    UNIQUE (organization_id, project_id, work_item_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE INDEX work_item_branch_plans_project_state_idx
    ON work_item_branch_plans (organization_id, project_id, state, updated_at DESC);

CREATE TABLE branch_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    kind text NOT NULL,
    state text NOT NULL DEFAULT 'PENDING',
    idempotency_key text NOT NULL,
    source_branch text,
    target_branch text,
    pull_request_number bigint,
    pull_request_url text,
    error_message text,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id, id),
    UNIQUE (organization_id, project_id, idempotency_key),
    FOREIGN KEY (organization_id, project_id, plan_id) REFERENCES work_item_branch_plans(organization_id, project_id, id),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE INDEX branch_operations_sync_pr_idx
    ON branch_operations (organization_id, repository_id, pull_request_number)
    WHERE kind = 'SYNC_PULL_REQUEST';
