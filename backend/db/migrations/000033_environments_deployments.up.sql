CREATE TABLE project_ai_policies (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    policy jsonb NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, project_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE TABLE project_environments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    key text NOT NULL,
    name text NOT NULL,
    kind text NOT NULL,
    repository_id uuid,
    workflow_ref text NOT NULL DEFAULT '',
    dispatch_url text NOT NULL DEFAULT '',
    health_check_url text NOT NULL DEFAULT '',
    auto_deploy boolean NOT NULL DEFAULT false,
    require_approval boolean NOT NULL DEFAULT true,
    secret_refs text[] NOT NULL DEFAULT '{}',
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id, key),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE TABLE deployment_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    autonomous_run_id uuid,
    commit_sha text NOT NULL,
    status text NOT NULL,
    external_id text NOT NULL DEFAULT '',
    url text NOT NULL DEFAULT '',
    approved_by text NOT NULL DEFAULT '',
    approved_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (environment_id) REFERENCES project_environments(id)
);

CREATE INDEX deployment_requests_project_created_idx ON deployment_requests (organization_id, project_id, created_at DESC);
