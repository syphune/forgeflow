ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_project_work_item_fk;
ALTER TABLE ai_proposals DROP CONSTRAINT IF EXISTS ai_proposals_project_work_item_fk;
ALTER TABLE specifications DROP CONSTRAINT IF EXISTS specifications_project_work_item_fk;
ALTER TABLE work_items DROP CONSTRAINT IF EXISTS work_items_organization_project_id_key;
