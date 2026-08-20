CREATE INDEX work_items_project_updated_idx ON work_items (organization_id, project_id, updated_at DESC);
CREATE INDEX ai_proposals_work_item_created_idx ON ai_proposals (organization_id, project_id, work_item_id, created_at);
CREATE INDEX agent_runs_project_created_idx ON agent_runs (organization_id, project_id, created_at DESC);
