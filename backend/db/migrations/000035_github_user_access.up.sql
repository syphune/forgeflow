CREATE TABLE github_user_authorization_states (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    state_hash text NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES users(id),
    organization_id uuid NOT NULL REFERENCES organizations(id),
    code_verifier text NOT NULL,
    redirect_uri text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX github_user_authorization_states_pending_idx
ON github_user_authorization_states (expires_at)
WHERE used_at IS NULL;

CREATE TABLE github_user_credentials (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    github_user_id bigint NOT NULL,
    encrypted_access_token text NOT NULL,
    encrypted_refresh_token text NOT NULL DEFAULT '',
    access_token_expires_at timestamptz,
    refresh_token_expires_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
