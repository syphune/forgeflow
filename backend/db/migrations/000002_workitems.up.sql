CREATE TABLE work_item_types (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key text NOT NULL UNIQUE,
    display_name text NOT NULL,
    is_sub_task boolean NOT NULL DEFAULT false
);

CREATE TABLE work_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    project_id uuid NOT NULL,
    number bigint NOT NULL,
    work_item_type_id uuid NOT NULL REFERENCES work_item_types(id),
    parent_id uuid,
    title text NOT NULL CHECK (length(btrim(title)) > 0),
    description text NOT NULL DEFAULT '',
    status_id uuid NOT NULL REFERENCES workflow_statuses(id),
    assignee_id uuid REFERENCES users(id),
    repository_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, number),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, workspace_id) REFERENCES workspaces(organization_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, parent_id) REFERENCES work_items(organization_id, id)
);

CREATE INDEX work_items_project_status_updated_idx ON work_items (project_id, status_id, updated_at DESC);
CREATE INDEX work_items_organization_updated_idx ON work_items (organization_id, updated_at DESC);
CREATE INDEX work_items_search_idx ON work_items USING gin (to_tsvector('simple', title || ' ' || description));

CREATE TABLE work_item_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    source_id uuid NOT NULL,
    target_id uuid NOT NULL,
    relation_type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (source_id <> target_id),
    UNIQUE (organization_id, source_id, target_id, relation_type),
    FOREIGN KEY (organization_id, source_id) REFERENCES work_items(organization_id, id),
    FOREIGN KEY (organization_id, target_id) REFERENCES work_items(organization_id, id)
);

CREATE TABLE comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    work_item_id uuid NOT NULL,
    author_id uuid NOT NULL REFERENCES users(id),
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);

CREATE INDEX comments_work_item_created_idx ON comments (work_item_id, created_at);

CREATE TABLE activities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    activity_type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
