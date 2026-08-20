CREATE TABLE oauth_states (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    state_hash text NOT NULL UNIQUE,
    code_verifier text NOT NULL,
    redirect_uri text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_states_expiry_idx ON oauth_states (expires_at)
WHERE used_at IS NULL;
