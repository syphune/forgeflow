CREATE TABLE specifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    repository_id uuid,
    summary text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (work_item_id),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);

CREATE TABLE specification_fields (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    specification_id uuid NOT NULL REFERENCES specifications(id),
    field_key text NOT NULL,
    value_text text NOT NULL DEFAULT '',
    provenance text NOT NULL,
    verification_status text NOT NULL DEFAULT 'UNVERIFIED',
    source_proposal_id uuid,
    verified_by uuid REFERENCES users(id),
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (specification_id, field_key)
);

CREATE TABLE specification_reproduction_steps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    specification_id uuid NOT NULL REFERENCES specifications(id),
    position integer NOT NULL,
    action text NOT NULL DEFAULT '',
    expected_result text NOT NULL DEFAULT '',
    observed_result text NOT NULL DEFAULT '',
    evidence_refs jsonb NOT NULL DEFAULT '[]',
    provenance text NOT NULL,
    verification_status text NOT NULL DEFAULT 'UNVERIFIED',
    UNIQUE (specification_id, position)
);

CREATE TABLE specification_acceptance_criteria (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    specification_id uuid NOT NULL REFERENCES specifications(id),
    position integer NOT NULL,
    statement text NOT NULL DEFAULT '',
    provenance text NOT NULL,
    verification_status text NOT NULL DEFAULT 'UNVERIFIED',
    UNIQUE (specification_id, position)
);

CREATE TABLE ai_proposals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    field_key text NOT NULL,
    proposed_value text NOT NULL,
    provenance text NOT NULL CHECK (provenance IN ('AI_INFERRED', 'AI_HYPOTHESIS')),
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'ACCEPTED', 'REJECTED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);

CREATE TABLE specification_verifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    work_item_id uuid NOT NULL,
    field_key text NOT NULL,
    verified_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);
