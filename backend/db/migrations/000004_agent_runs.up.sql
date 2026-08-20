CREATE TABLE agent_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    repository_id uuid,
    agent_provider text NOT NULL,
    agent_name text NOT NULL,
    model text NOT NULL DEFAULT '',
    base_sha text NOT NULL DEFAULT '',
    branch text NOT NULL DEFAULT '',
    status text NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    commit_sha text,
    pull_request_id uuid,
    result jsonb,
    error text,
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);

CREATE UNIQUE INDEX agent_runs_one_active_code_change_idx ON agent_runs (work_item_id)
WHERE status IN ('QUEUED', 'PREPARING', 'PLANNING', 'INVESTIGATING', 'IMPLEMENTING', 'TESTING', 'REVIEWING');

CREATE TABLE idempotency_keys (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    actor_id text NOT NULL,
    key text NOT NULL,
    response_status integer,
    response_body jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, actor_id, key)
);
