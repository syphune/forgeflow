CREATE TABLE automation_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    event_type text NOT NULL,
    action_type text NOT NULL CHECK (action_type IN ('notify')),
    config jsonb NOT NULL DEFAULT '{}',
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id, name),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE INDEX automation_rules_event_idx ON automation_rules (organization_id, project_id, event_type)
WHERE enabled;

CREATE TABLE automation_executions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    event_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('PROCESSING', 'COMPLETED', 'FAILED')),
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (organization_id, rule_id, event_id),
    FOREIGN KEY (organization_id, rule_id) REFERENCES automation_rules(organization_id, id)
);

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid,
    notification_type text NOT NULL,
    title text NOT NULL,
    body text NOT NULL DEFAULT '',
    resource_type text NOT NULL DEFAULT '',
    resource_id text NOT NULL DEFAULT '',
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships(organization_id, user_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE INDEX notifications_user_feed_idx ON notifications (organization_id, user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (organization_id, user_id, created_at DESC)
WHERE read_at IS NULL;
