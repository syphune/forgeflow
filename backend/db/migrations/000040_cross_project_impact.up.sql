ALTER TABLE agent_runs
    ADD CONSTRAINT agent_runs_organization_id_id_key UNIQUE (organization_id, id);

CREATE TABLE repository_project_scopes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    path_prefixes text[] NOT NULL DEFAULT '{}',
    modules text[] NOT NULL DEFAULT '{}',
    impact_summary text NOT NULL DEFAULT '',
    provenance text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_by text NOT NULL DEFAULT '',
    verified_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_project_scopes_provenance_check CHECK (provenance IN ('HUMAN_VERIFIED', 'AI_INFERRED', 'SYSTEM_DERIVED')),
    CONSTRAINT repository_project_scopes_status_check CHECK (status IN ('PROPOSED', 'ACTIVE', 'REJECTED')),
    FOREIGN KEY (organization_id, project_id, repository_id)
        REFERENCES repository_links(organization_id, project_id, repository_id)
);

CREATE INDEX repository_project_scopes_lookup_idx
    ON repository_project_scopes (organization_id, project_id, repository_id, status);

CREATE TABLE agent_run_impact_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    agent_run_id uuid NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    report_version bigint NOT NULL,
    input_hash text NOT NULL,
    base_sha text NOT NULL DEFAULT '',
    diff_hash text NOT NULL DEFAULT '',
    changed_paths text[] NOT NULL DEFAULT '{}',
    affected_projects jsonb NOT NULL DEFAULT '[]',
    unknown_impact boolean NOT NULL DEFAULT false,
    truncated boolean NOT NULL DEFAULT false,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_run_impact_reports_status_check CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'STALE')),
    CONSTRAINT agent_run_impact_reports_version_key UNIQUE (organization_id, agent_run_id, report_version),
    CONSTRAINT agent_run_impact_reports_id_key UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, agent_run_id) REFERENCES agent_runs(organization_id, id),
    FOREIGN KEY (organization_id, project_id, repository_id)
        REFERENCES repository_links(organization_id, project_id, repository_id)
);

CREATE INDEX agent_run_impact_reports_latest_idx
    ON agent_run_impact_reports (organization_id, agent_run_id, report_version DESC);

CREATE TABLE agent_run_impact_approvals (
    organization_id uuid NOT NULL,
    report_id uuid NOT NULL,
    affected_project_id uuid NOT NULL,
    decision text NOT NULL,
    approver_id text NOT NULL,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (report_id, affected_project_id),
    CONSTRAINT agent_run_impact_approvals_decision_check CHECK (decision IN ('APPROVED', 'REJECTED')),
    FOREIGN KEY (organization_id, report_id) REFERENCES agent_run_impact_reports(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, affected_project_id) REFERENCES projects(organization_id, id)
);

CREATE INDEX agent_run_impact_approvals_project_idx
    ON agent_run_impact_approvals (organization_id, affected_project_id, created_at DESC);
