CREATE TABLE agent_run_steps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    run_id uuid NOT NULL REFERENCES agent_runs(id),
    sequence integer NOT NULL,
    phase text NOT NULL,
    status text NOT NULL,
    summary text NOT NULL DEFAULT '',
    files_read integer NOT NULL DEFAULT 0,
    files_modified integer NOT NULL DEFAULT 0,
    commands jsonb NOT NULL DEFAULT '[]',
    tests jsonb NOT NULL DEFAULT '[]',
    metadata jsonb NOT NULL DEFAULT '{}',
    started_at timestamptz,
    finished_at timestamptz,
    UNIQUE (run_id, sequence)
);

CREATE TABLE agent_run_artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    run_id uuid NOT NULL REFERENCES agent_runs(id),
    artifact_type text NOT NULL,
    name text NOT NULL,
    content_hash text NOT NULL DEFAULT '',
    size_bytes bigint NOT NULL DEFAULT 0,
    object_key text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agent_run_approvals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    run_id uuid NOT NULL REFERENCES agent_runs(id),
    approver_id uuid NOT NULL REFERENCES users(id),
    action text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, action)
);

CREATE INDEX agent_run_steps_run_idx ON agent_run_steps (run_id, sequence);
CREATE INDEX agent_run_artifacts_run_idx ON agent_run_artifacts (run_id, created_at);
