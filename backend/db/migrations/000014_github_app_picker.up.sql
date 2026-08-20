CREATE TABLE github_installation_states (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    state_hash text NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES users(id),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX github_installation_states_pending_idx ON github_installation_states (expires_at)
WHERE used_at IS NULL;

ALTER TABLE repositories ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now();

CREATE UNIQUE INDEX repositories_organization_github_id_idx
ON repositories (organization_id, github_repository_id)
WHERE github_repository_id IS NOT NULL;
