DROP INDEX IF EXISTS repositories_organization_github_id_idx;
ALTER TABLE repositories DROP COLUMN IF EXISTS last_seen_at;
DROP TABLE IF EXISTS github_installation_states;
