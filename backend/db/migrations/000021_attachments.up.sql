CREATE TABLE attachments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    name text NOT NULL,
    content_type text NOT NULL,
    storage_key text NOT NULL UNIQUE,
    sha256 text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, project_id, work_item_id)
        REFERENCES work_items (organization_id, project_id, id)
        ON DELETE CASCADE
);

CREATE INDEX attachments_work_item_created_idx
    ON attachments (organization_id, project_id, work_item_id, created_at DESC);
