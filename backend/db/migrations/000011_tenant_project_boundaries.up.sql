ALTER TABLE work_items
    ADD CONSTRAINT work_items_organization_project_id_key UNIQUE (organization_id, project_id, id);

ALTER TABLE specifications
    ADD CONSTRAINT specifications_project_work_item_fk
    FOREIGN KEY (organization_id, project_id, work_item_id)
    REFERENCES work_items (organization_id, project_id, id);

ALTER TABLE ai_proposals
    ADD CONSTRAINT ai_proposals_project_work_item_fk
    FOREIGN KEY (organization_id, project_id, work_item_id)
    REFERENCES work_items (organization_id, project_id, id);

ALTER TABLE agent_runs
    ADD CONSTRAINT agent_runs_project_work_item_fk
    FOREIGN KEY (organization_id, project_id, work_item_id)
    REFERENCES work_items (organization_id, project_id, id);
