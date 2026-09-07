DROP TABLE IF EXISTS agent_run_impact_approvals;
DROP TABLE IF EXISTS agent_run_impact_reports;
DROP TABLE IF EXISTS repository_project_scopes;
ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_organization_id_id_key;
