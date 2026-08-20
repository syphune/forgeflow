CREATE TABLE github_installations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    github_installation_id bigint NOT NULL,
    account_login text NOT NULL,
    encrypted_private_key text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (github_installation_id),
    UNIQUE (organization_id, id)
);

CREATE TABLE repositories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    github_installation_id bigint,
    github_repository_id bigint,
    full_name text NOT NULL,
    default_branch text NOT NULL DEFAULT 'main',
    clone_url text NOT NULL DEFAULT '',
    linked_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, full_name)
);

CREATE TABLE repository_links (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, project_id, repository_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE TABLE branches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    repository_id uuid NOT NULL,
    name text NOT NULL,
    head_sha text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, repository_id, name),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE TABLE commits (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    repository_id uuid NOT NULL,
    sha text NOT NULL,
    message text NOT NULL DEFAULT '',
    author_login text NOT NULL DEFAULT '',
    committed_at timestamptz,
    UNIQUE (organization_id, repository_id, sha),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE TABLE pull_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    repository_id uuid NOT NULL,
    number bigint NOT NULL,
    title text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'open',
    head_sha text NOT NULL DEFAULT '',
    url text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, repository_id, number),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE TABLE ci_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    repository_id uuid NOT NULL,
    external_id text NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    conclusion text NOT NULL DEFAULT '',
    sha text NOT NULL DEFAULT '',
    url text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, repository_id, external_id),
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id)
);

CREATE TABLE webhook_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    github_delivery_id text NOT NULL UNIQUE,
    event_name text NOT NULL,
    payload jsonb NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    processing_error text
);

CREATE INDEX webhook_events_pending_idx ON webhook_events (received_at)
WHERE processed_at IS NULL;
