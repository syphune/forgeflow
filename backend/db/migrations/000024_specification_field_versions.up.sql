CREATE TABLE specification_field_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    specification_id uuid NOT NULL REFERENCES specifications(id),
    revision integer NOT NULL CHECK (revision > 0),
    field_key text NOT NULL,
    value_text text NOT NULL DEFAULT '',
    provenance text NOT NULL,
    verification_status text NOT NULL DEFAULT 'UNVERIFIED',
    source_proposal_id uuid,
    verified_by uuid REFERENCES users(id),
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    UNIQUE (specification_id, revision, field_key)
);

CREATE INDEX specification_field_versions_history_idx
    ON specification_field_versions (organization_id, project_id, specification_id, revision DESC, field_key);
