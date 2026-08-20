ALTER TABLE specifications
    ADD COLUMN version integer NOT NULL DEFAULT 1,
    ADD COLUMN reviewed_version integer,
    ADD COLUMN reviewed_by uuid REFERENCES users(id),
    ADD COLUMN reviewed_at timestamptz;

-- Compatibility window: a work item that was already READY must not be
-- downgraded by the new review-version gate during the additive rollout.
UPDATE specifications s
SET reviewed_version = s.version
FROM work_items wi
JOIN workflow_statuses ws ON ws.id = wi.status_id
JOIN workflows w ON w.id = ws.workflow_id
WHERE s.organization_id = wi.organization_id
  AND s.project_id = wi.project_id
  AND s.work_item_id = wi.id
  AND ws.key = 'READY'
  AND w.organization_id = wi.organization_id
  AND w.project_id = wi.project_id;

ALTER TABLE agent_runs
    ADD COLUMN approval_fingerprint_version integer NOT NULL DEFAULT 0,
    ADD COLUMN approval_fingerprint text NOT NULL DEFAULT '',
    ADD COLUMN execution_policy jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN last_heartbeat_at timestamptz,
    ADD COLUMN interruption_reason text NOT NULL DEFAULT '';

CREATE TABLE work_item_column_orderings (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    project_id uuid NOT NULL,
    status_key text NOT NULL,
    sprint_id text NOT NULL DEFAULT '',
    ordering_version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, project_id, status_key, sprint_id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id)
);

CREATE INDEX work_item_column_orderings_project_idx
    ON work_item_column_orderings (organization_id, project_id, status_key);
