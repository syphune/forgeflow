ALTER TABLE project_environments
    ADD CONSTRAINT project_environments_organization_id_id_key UNIQUE (organization_id, id);

ALTER TABLE deployment_requests
    DROP CONSTRAINT deployment_requests_environment_id_fkey,
    ADD CONSTRAINT deployment_requests_environment_tenant_fkey
        FOREIGN KEY (organization_id, environment_id)
        REFERENCES project_environments (organization_id, id),
    ADD CONSTRAINT deployment_requests_autonomous_run_tenant_fkey
        FOREIGN KEY (organization_id, autonomous_run_id)
        REFERENCES autonomous_runs (organization_id, id);
