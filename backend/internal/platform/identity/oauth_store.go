package identity

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresOAuthStore struct{ pool *pgxpool.Pool }

func NewPostgresOAuthStore(pool *pgxpool.Pool) *PostgresOAuthStore {
	return &PostgresOAuthStore{pool: pool}
}

func (s *PostgresOAuthStore) BeginOAuth(ctx context.Context, stateHash, codeVerifier, redirectURI string, expiresAt time.Time) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO oauth_states (state_hash, code_verifier, redirect_uri, expires_at) VALUES ($1,$2,$3,$4)`, stateHash, codeVerifier, redirectURI, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("store OAuth state: %w", err)
	}
	return nil
}

func (s *PostgresOAuthStore) ConsumeOAuth(ctx context.Context, stateHash string) (string, string, error) {
	var verifier, redirectURI string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE oauth_states SET used_at=now() WHERE state_hash=$1 AND used_at IS NULL AND expires_at > now() RETURNING code_verifier, redirect_uri`, stateHash).Scan(&verifier, &redirectURI)
	if err == pgx.ErrNoRows {
		return "", "", apperr.New(apperr.CodeUnauthorized, 401, "OAuth state is invalid or expired", nil)
	}
	if err != nil {
		return "", "", fmt.Errorf("consume OAuth state: %w", err)
	}
	return verifier, redirectURI, nil
}

func (s *PostgresOAuthStore) UpsertGitHubUser(ctx context.Context, githubUserID int64, login, displayName string) (string, error) {
	var userID string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO users (github_user_id, login, display_name)
VALUES ($1,$2,$3)
ON CONFLICT (github_user_id) DO UPDATE SET login=EXCLUDED.login, display_name=EXCLUDED.display_name
RETURNING id::text
`, githubUserID, strings.TrimSpace(login), strings.TrimSpace(displayName)).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("upsert GitHub user: %w", err)
	}
	if _, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO auth_identities (user_id, provider, provider_subject, provider_login)
VALUES ($1, 'github', $2, $3)
ON CONFLICT (provider, provider_subject) DO UPDATE SET user_id=EXCLUDED.user_id, provider_login=EXCLUDED.provider_login
`, userID, strconv.FormatInt(githubUserID, 10), strings.TrimSpace(login)); err != nil {
		return "", fmt.Errorf("store GitHub identity: %w", err)
	}
	return userID, nil
}

func (s *PostgresOAuthStore) EnsureDefaultOrganization(ctx context.Context, userID string, githubUserID int64, login string) (string, error) {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	var organizationID string
	if err := exec.QueryRow(ctx, `SELECT organization_id::text FROM organization_memberships WHERE user_id=$1 ORDER BY created_at LIMIT 1`, userID).Scan(&organizationID); err == nil {
		return organizationID, nil
	} else if err != pgx.ErrNoRows {
		return "", fmt.Errorf("find user organization: %w", err)
	}
	slug := "github-" + strconv.FormatInt(githubUserID, 10)
	if _, err := exec.Exec(ctx, `INSERT INTO organizations (slug, display_name) VALUES ($1,$2) ON CONFLICT (slug) DO NOTHING`, slug, strings.TrimSpace(login)+"'s Forgeflow"); err != nil {
		return "", fmt.Errorf("create default organization: %w", err)
	}
	if err := exec.QueryRow(ctx, `SELECT id::text FROM organizations WHERE slug=$1`, slug).Scan(&organizationID); err != nil {
		return "", fmt.Errorf("load default organization: %w", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role_key) VALUES ($1,$2,'owner') ON CONFLICT DO NOTHING`, organizationID, userID); err != nil {
		return "", fmt.Errorf("create default membership: %w", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO workspaces (organization_id, key, display_name) VALUES ($1,'MAIN','Main') ON CONFLICT (organization_id,key) DO NOTHING`, organizationID); err != nil {
		return "", fmt.Errorf("create default workspace: %w", err)
	}
	return organizationID, nil
}

var _ OAuthStore = (*PostgresOAuthStore)(nil)
