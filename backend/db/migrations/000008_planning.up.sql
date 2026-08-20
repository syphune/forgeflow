ALTER TABLE work_items ADD COLUMN sprint_id uuid;

CREATE TABLE sprints (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    name text NOT NULL,
    goal text NOT NULL DEFAULT '',
    starts_at timestamptz,
    ends_at timestamptz,
    status text NOT NULL DEFAULT 'PLANNED' CHECK (status IN ('PLANNED', 'ACTIVE', 'COMPLETED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

ALTER TABLE work_items ADD CONSTRAINT work_items_sprint_fk
    FOREIGN KEY (organization_id, sprint_id) REFERENCES sprints(organization_id, id);

CREATE UNIQUE INDEX sprints_one_active_idx ON sprints (project_id)
WHERE status = 'ACTIVE';
CREATE INDEX sprints_project_status_idx ON sprints (project_id, status, created_at DESC);
CREATE INDEX work_items_project_sprint_status_idx ON work_items (project_id, sprint_id, status_id);

CREATE TABLE boards (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE TABLE board_columns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id uuid NOT NULL REFERENCES boards(id),
    name text NOT NULL,
    position integer NOT NULL,
    UNIQUE (board_id, position)
);

CREATE TABLE board_column_statuses (
    column_id uuid NOT NULL REFERENCES board_columns(id),
    status_id uuid NOT NULL REFERENCES workflow_statuses(id),
    PRIMARY KEY (column_id, status_id)
);
