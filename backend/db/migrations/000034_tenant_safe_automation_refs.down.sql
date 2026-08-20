ALTER TABLE deployment_requests
    DROP CONSTRAINT deployment_requests_environment_tenant_fkey,
    DROP CONSTRAINT deployment_requests_autonomous_run_tenant_fkey,
    ADD CONSTRAINT deployment_requests_environment_id_fkey
        FOREIGN KEY (environment_id)
        REFERENCES project_environments (id);

ALTER TABLE project_environments
    DROP CONSTRAINT project_environments_organization_id_id_key;
