CREATE TABLE specification_regression_cases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    specification_id uuid NOT NULL REFERENCES specifications(id),
    position integer NOT NULL,
    scenario text NOT NULL DEFAULT '',
    expected_result text NOT NULL DEFAULT '',
    provenance text NOT NULL,
    verification_status text NOT NULL DEFAULT 'UNVERIFIED',
    verified_by uuid REFERENCES users(id),
    verified_at timestamptz,
    UNIQUE (specification_id, position)
);

CREATE TABLE specification_context_refs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    specification_id uuid NOT NULL REFERENCES specifications(id),
    repository_id uuid,
    module text NOT NULL DEFAULT '',
    file text NOT NULL DEFAULT '',
    symbol text NOT NULL DEFAULT '',
    commit_sha text NOT NULL DEFAULT '',
    pull_request text NOT NULL DEFAULT '',
    rationale text NOT NULL DEFAULT '',
    provenance text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX specification_context_refs_spec_idx ON specification_context_refs (specification_id);
