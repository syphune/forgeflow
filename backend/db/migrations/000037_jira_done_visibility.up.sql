ALTER TABLE projects
    ADD COLUMN done_visibility_days integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT projects_done_visibility_days_check
        CHECK (done_visibility_days IN (0, 7, 14, 30, 60));

ALTER TABLE work_items
    ADD COLUMN completed_at timestamptz;

UPDATE work_items wi
SET completed_at = wi.updated_at
FROM workflow_statuses ws
JOIN workflows w ON w.id = ws.workflow_id
WHERE wi.status_id = ws.id
  AND w.organization_id = wi.organization_id
  AND w.project_id = wi.project_id
  AND ws.is_terminal
  AND wi.completed_at IS NULL;

CREATE INDEX work_items_project_completed_idx
    ON work_items (organization_id, project_id, completed_at)
    WHERE completed_at IS NOT NULL;
