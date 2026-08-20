CREATE TABLE autonomous_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    repository_id uuid,
    base_sha text NOT NULL DEFAULT '',
    branch text NOT NULL DEFAULT '',
    objective text NOT NULL,
    agent_provider text NOT NULL,
    agent_name text NOT NULL,
    model text NOT NULL DEFAULT '',
    target_environment text NOT NULL DEFAULT '',
    policy jsonb NOT NULL DEFAULT '{}',
    status text NOT NULL,
    phase text NOT NULL,
    gate text NOT NULL DEFAULT '',
    attempt integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 3,
    current_agent_run_id uuid,
    pull_request_id uuid,
    commit_sha text NOT NULL DEFAULT '',
    unresolved_positions integer[] NOT NULL DEFAULT '{}',
    last_error text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);

CREATE INDEX autonomous_runs_project_created_idx ON autonomous_runs (organization_id, project_id, created_at DESC);
CREATE INDEX autonomous_runs_work_item_idx ON autonomous_runs (organization_id, project_id, work_item_id, created_at DESC);

CREATE TABLE autonomous_feedback (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    autonomous_run_id uuid NOT NULL,
    source text NOT NULL,
    note text NOT NULL,
    severity text NOT NULL DEFAULT '',
    commit_sha text NOT NULL DEFAULT '',
    test_case_positions integer[] NOT NULL DEFAULT '{}',
    evidence_refs text[] NOT NULL DEFAULT '{}',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, autonomous_run_id) REFERENCES autonomous_runs(organization_id, id) ON DELETE CASCADE
);

CREATE INDEX autonomous_feedback_run_created_idx ON autonomous_feedback (organization_id, autonomous_run_id, created_at);
