CREATE TABLE auth_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    provider text NOT NULL,
    provider_subject text NOT NULL,
    provider_login text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subject)
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_active_idx ON sessions (token_hash, expires_at)
WHERE revoked_at IS NULL;

CREATE TABLE personal_access_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    user_id uuid NOT NULL REFERENCES users(id),
    name text NOT NULL,
    token_prefix text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    scopes text[] NOT NULL DEFAULT '{}',
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE INDEX personal_access_tokens_active_idx ON personal_access_tokens (token_hash, expires_at)
WHERE revoked_at IS NULL;

CREATE TABLE roles (
    key text PRIMARY KEY,
    display_name text NOT NULL
);

CREATE TABLE permissions (
    key text PRIMARY KEY,
    display_name text NOT NULL
);

CREATE TABLE role_permissions (
    role_key text NOT NULL REFERENCES roles(key),
    permission_key text NOT NULL REFERENCES permissions(key),
    PRIMARY KEY (role_key, permission_key)
);

CREATE TABLE project_memberships (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    user_id uuid NOT NULL REFERENCES users(id),
    role_key text NOT NULL REFERENCES roles(key),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, project_id, user_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships(organization_id, user_id)
);

INSERT INTO roles (key, display_name) VALUES
    ('owner', 'Owner'),
    ('admin', 'Admin'),
    ('project_manager', 'Project Manager'),
    ('developer', 'Developer'),
    ('qa', 'QA'),
    ('viewer', 'Viewer')
ON CONFLICT (key) DO NOTHING;

INSERT INTO permissions (key, display_name) VALUES
    ('organization.read', 'Read organization'),
    ('organization.manage', 'Manage organization'),
    ('workspace.read', 'Read workspace'),
    ('workspace.manage', 'Manage workspace'),
    ('project.read', 'Read project'),
    ('project.manage', 'Manage project'),
    ('work_item.create', 'Create work item'),
    ('work_item.edit', 'Edit work item'),
    ('work_item.assign', 'Assign work item'),
    ('work_item.transition', 'Transition work item'),
    ('work_item.delete', 'Delete work item'),
    ('comment.create', 'Create comment'),
    ('sprint.manage', 'Manage sprint'),
    ('repository.read', 'Read repository'),
    ('repository.manage', 'Manage repository'),
    ('specification.propose', 'Propose specification'),
    ('specification.verify', 'Verify specification'),
    ('agent.execute', 'Execute agent'),
    ('agent.approve', 'Approve agent'),
    ('audit.read', 'Read audit')
ON CONFLICT (key) DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'owner', key FROM permissions
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'admin', key FROM permissions WHERE key <> 'organization.manage'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'project_manager', key FROM permissions
WHERE key IN ('organization.read', 'workspace.read', 'project.read', 'project.manage', 'work_item.create', 'work_item.edit', 'work_item.assign', 'work_item.transition', 'comment.create', 'sprint.manage', 'repository.read', 'repository.manage', 'specification.propose', 'specification.verify', 'agent.execute', 'agent.approve')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'developer', key FROM permissions
WHERE key IN ('organization.read', 'workspace.read', 'project.read', 'work_item.create', 'work_item.edit', 'work_item.assign', 'work_item.transition', 'comment.create', 'repository.read', 'specification.propose', 'agent.execute')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'qa', key FROM permissions
WHERE key IN ('organization.read', 'workspace.read', 'project.read', 'work_item.edit', 'work_item.transition', 'comment.create', 'repository.read', 'specification.propose', 'specification.verify')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'viewer', key FROM permissions
WHERE key IN ('organization.read', 'workspace.read', 'project.read', 'repository.read')
ON CONFLICT DO NOTHING;
