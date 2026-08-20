CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    github_user_id bigint UNIQUE,
    login text NOT NULL,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    key text NOT NULL,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, key)
);

CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    workspace_id uuid NOT NULL,
    key text NOT NULL,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, key),
    FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id, id)
);

CREATE TABLE organization_memberships (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    user_id uuid NOT NULL REFERENCES users(id),
    role_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE workflows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, project_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE TABLE workflow_statuses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id uuid NOT NULL REFERENCES workflows(id),
    key text NOT NULL,
    display_name text NOT NULL,
    category text NOT NULL CHECK (category IN ('TODO', 'IN_PROGRESS', 'DONE', 'CANCELLED')),
    position integer NOT NULL,
    is_terminal boolean NOT NULL DEFAULT false,
    UNIQUE (workflow_id, key)
);

CREATE TABLE workflow_transitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id uuid NOT NULL REFERENCES workflows(id),
    from_status_id uuid NOT NULL REFERENCES workflow_statuses(id),
    to_status_id uuid NOT NULL REFERENCES workflow_statuses(id),
    key text NOT NULL,
    display_name text NOT NULL,
    UNIQUE (workflow_id, key),
    UNIQUE (workflow_id, from_status_id, to_status_id)
);

CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    source text NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    before_json jsonb,
    after_json jsonb,
    request_id text,
    correlation_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETED', 'DEAD')),
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    last_error text,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);

CREATE INDEX outbox_events_claim_idx ON outbox_events (next_attempt_at, lease_until) WHERE status IN ('PENDING', 'PROCESSING');
CREATE INDEX audit_logs_resource_idx ON audit_logs (resource_type, resource_id, created_at);
