ALTER TABLE work_items
    ADD COLUMN priority text NOT NULL DEFAULT 'MEDIUM' CHECK (priority IN ('LOWEST', 'LOW', 'MEDIUM', 'HIGH', 'HIGHEST')),
    ADD COLUMN reporter_id uuid REFERENCES users(id),
    ADD COLUMN due_at timestamptz,
    ADD COLUMN estimate_points integer CHECK (estimate_points IS NULL OR estimate_points >= 0);

CREATE INDEX work_items_project_priority_due_idx ON work_items (project_id, priority, due_at);

CREATE TABLE labels (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    name text NOT NULL CHECK (length(btrim(name)) > 0 AND length(name) <= 50),
    color text NOT NULL DEFAULT '#8FE0C1' CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, name)
);

CREATE TABLE work_item_labels (
    organization_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    label_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, work_item_id, label_id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, label_id) REFERENCES labels(organization_id, id) ON DELETE CASCADE
);

CREATE INDEX work_item_labels_label_idx ON work_item_labels (organization_id, label_id);
