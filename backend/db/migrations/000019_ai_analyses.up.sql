CREATE TABLE ai_analyses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    root_cause_hypothesis text NOT NULL,
    blast_radius text NOT NULL DEFAULT '',
    implementation_plan text NOT NULL,
    test_plan text NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]',
    confidence numeric(4,3) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    provenance text NOT NULL CHECK (provenance IN ('AI_INFERRED', 'AI_HYPOTHESIS')),
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id)
);

CREATE INDEX ai_analyses_work_item_idx ON ai_analyses (organization_id, project_id, work_item_id, created_at DESC);
