CREATE TABLE project_delivery_policies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    policy jsonb NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

ALTER TABLE work_items
    ADD COLUMN risk_level text NOT NULL DEFAULT 'NORMAL',
    ADD CONSTRAINT work_items_risk_level_check CHECK (risk_level IN ('LOW', 'NORMAL', 'HIGH', 'CRITICAL'));

ALTER TABLE specification_regression_cases
    ADD COLUMN gate_class text NOT NULL DEFAULT 'REQUIRED',
    ADD CONSTRAINT specification_regression_gate_class_check CHECK (gate_class IN ('REQUIRED', 'OPTIONAL', 'INFORMATIONAL'));

ALTER TABLE pull_requests
    ADD COLUMN author_login text NOT NULL DEFAULT '',
    ADD COLUMN changed_paths text[] NOT NULL DEFAULT '{}',
    ADD COLUMN approved_by_logins text[] NOT NULL DEFAULT '{}',
    ADD COLUMN changes_requested boolean NOT NULL DEFAULT false,
    ADD COLUMN merged boolean NOT NULL DEFAULT false;

ALTER TABLE ci_runs
    ADD COLUMN check_name text NOT NULL DEFAULT '';

ALTER TABLE deployment_requests
    ADD COLUMN work_item_id uuid,
    ADD CONSTRAINT deployment_requests_work_item_fk
        FOREIGN KEY (organization_id, work_item_id)
        REFERENCES work_items(organization_id, id),
    ADD CONSTRAINT deployment_requests_status_check
        CHECK (status IN ('PENDING_APPROVAL', 'DISPATCHED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'VERIFIED'));

CREATE INDEX deployment_requests_work_item_idx
    ON deployment_requests (organization_id, project_id, work_item_id, status)
    WHERE work_item_id IS NOT NULL;

INSERT INTO roles (key, display_name) VALUES
    ('security_reviewer', 'Security Reviewer'),
    ('release_approver', 'Release Approver')
ON CONFLICT (key) DO NOTHING;

INSERT INTO permissions (key, display_name) VALUES
    ('security.review', 'Review security impact'),
    ('release.approve', 'Approve a production release'),
    ('production.verify', 'Verify production deployment')
ON CONFLICT (key) DO NOTHING;

INSERT INTO role_permissions (role_key, permission_key)
SELECT role_key, permission_key
FROM (VALUES
    ('owner', 'security.review'), ('owner', 'release.approve'), ('owner', 'production.verify'),
    ('admin', 'security.review'), ('admin', 'release.approve'), ('admin', 'production.verify'),
    ('project_manager', 'release.approve'), ('project_manager', 'production.verify'),
    ('qa', 'production.verify'),
    ('security_reviewer', 'project.read'), ('security_reviewer', 'repository.read'), ('security_reviewer', 'security.review'), ('security_reviewer', 'specification.verify'),
    ('release_approver', 'project.read'), ('release_approver', 'repository.read'), ('release_approver', 'deployment.approve'), ('release_approver', 'release.approve'), ('release_approver', 'production.verify')
) AS permissions(role_key, permission_key)
ON CONFLICT DO NOTHING;
