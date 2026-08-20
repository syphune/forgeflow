package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthenticationRequest struct {
	BearerToken                string
	SessionToken               string
	OrganizationID             string
	ProjectID                  string
	AllowOrganizationSelection bool
}

type Authenticator interface {
	Authenticate(context.Context, AuthenticationRequest) (Actor, error)
}

type CreatedToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Token     string    `json:"token,omitempty"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type TokenStore interface {
	CreatePAT(context.Context, string, string, string, []string, time.Time) (CreatedToken, error)
	ListPAT(context.Context, string, string) ([]CreatedToken, error)
	RevokePAT(context.Context, string, string, string) error
}

type SessionStore interface {
	CreateSession(context.Context, string, time.Time) (string, string, error)
	RevokeSession(context.Context, string) error
}

type OAuthStore interface {
	BeginOAuth(context.Context, string, string, string, time.Time) error
	ConsumeOAuth(context.Context, string) (string, string, error)
	UpsertGitHubUser(context.Context, int64, string, string) (string, error)
	EnsureDefaultOrganization(context.Context, string, int64, string) (string, error)
}

type PostgresAuthenticator struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPostgresAuthenticator(pool *pgxpool.Pool, now func() time.Time) *PostgresAuthenticator {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PostgresAuthenticator{pool: pool, now: now}
}

func (a *PostgresAuthenticator) Authenticate(ctx context.Context, request AuthenticationRequest) (Actor, error) {
	if strings.TrimSpace(request.BearerToken) != "" {
		return a.authenticatePAT(ctx, request)
	}
	if strings.TrimSpace(request.SessionToken) != "" {
		return a.authenticateSession(ctx, request)
	}
	return Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "authentication is required", nil)
}

func (a *PostgresAuthenticator) authenticatePAT(ctx context.Context, request AuthenticationRequest) (Actor, error) {
	rows, err := a.pool.Query(ctx, `
SELECT pat.user_id::text, pat.organization_id::text, pat.scopes,
       CASE WHEN pm.user_id IS NULL THEN COALESCE(rp.permission_key, '') ELSE COALESCE(pp.permission_key, '') END
FROM personal_access_tokens pat
JOIN organization_memberships om ON om.organization_id=pat.organization_id AND om.user_id=pat.user_id
JOIN projects p ON p.organization_id=om.organization_id AND ($4='' OR p.id::text=$4)
LEFT JOIN roles r ON r.key=om.role_key
LEFT JOIN role_permissions rp ON rp.role_key=r.key
LEFT JOIN project_memberships pm ON pm.organization_id=om.organization_id AND pm.project_id::text=$4 AND pm.user_id=pat.user_id
LEFT JOIN roles pr ON pr.key=pm.role_key
LEFT JOIN role_permissions pp ON pp.role_key=pr.key
WHERE pat.token_hash=$1 AND pat.revoked_at IS NULL AND pat.expires_at > $2
  AND ($3='' OR pat.organization_id::text=$3)
`, hashToken(request.BearerToken), a.now().UTC(), request.OrganizationID, request.ProjectID)
	if err != nil {
		return Actor{}, fmt.Errorf("authenticate PAT: %w", err)
	}
	defer rows.Close()
	var actor Actor
	var scopes []string
	roleCapabilities := make(map[string]bool)
	for rows.Next() {
		var userID, organizationID, permission string
		var rowScopes []string
		if err := rows.Scan(&userID, &organizationID, &rowScopes, &permission); err != nil {
			return Actor{}, fmt.Errorf("scan PAT identity: %w", err)
		}
		if request.OrganizationID != "" && request.OrganizationID != organizationID {
			continue
		}
		if actor.ID == "" {
			actor = Actor{Type: "human", ID: userID, OrganizationID: organizationID, Source: "pat"}
			scopes = rowScopes
		}
		if permission != "" {
			roleCapabilities[permission] = true
		}
	}
	if err := rows.Err(); err != nil {
		return Actor{}, fmt.Errorf("iterate PAT identity: %w", err)
	}
	if actor.ID == "" {
		return Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "invalid or expired access token", nil)
	}
	actor.Capabilities = make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if roleCapabilities[scope] {
			actor.Capabilities[scope] = true
		}
	}
	return actor, nil
}

func (a *PostgresAuthenticator) authenticateSession(ctx context.Context, request AuthenticationRequest) (Actor, error) {
	rows, err := a.pool.Query(ctx, `
SELECT s.user_id::text, om.organization_id::text,
       CASE WHEN pm.user_id IS NULL THEN COALESCE(rp.permission_key, '') ELSE COALESCE(pp.permission_key, '') END
FROM sessions s
JOIN organization_memberships om ON om.user_id=s.user_id
JOIN projects p ON p.organization_id=om.organization_id AND ($4='' OR p.id::text=$4)
LEFT JOIN roles r ON r.key=om.role_key
LEFT JOIN role_permissions rp ON rp.role_key=r.key
LEFT JOIN project_memberships pm ON pm.organization_id=om.organization_id AND pm.project_id::text=$4 AND pm.user_id=s.user_id
LEFT JOIN roles pr ON pr.key=pm.role_key
LEFT JOIN role_permissions pp ON pp.role_key=pr.key
WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > $2
  AND ($3='' OR om.organization_id::text=$3)
`, hashToken(request.SessionToken), a.now().UTC(), request.OrganizationID, request.ProjectID)
	if err != nil {
		return Actor{}, fmt.Errorf("authenticate session: %w", err)
	}
	defer rows.Close()
	var actor Actor
	organizations := make(map[string]bool)
	for rows.Next() {
		var userID, organizationID, permission string
		if err := rows.Scan(&userID, &organizationID, &permission); err != nil {
			return Actor{}, fmt.Errorf("scan session identity: %w", err)
		}
		organizations[organizationID] = true
		if actor.ID == "" {
			actor = Actor{Type: "human", ID: userID, OrganizationID: organizationID, Source: "session", Capabilities: make(map[string]bool)}
		}
		if organizationID != actor.OrganizationID {
			continue
		}
		if permission != "" {
			actor.Capabilities[permission] = true
		}
	}
	if err := rows.Err(); err != nil {
		return Actor{}, fmt.Errorf("iterate session identity: %w", err)
	}
	if actor.ID == "" || (request.OrganizationID == "" && len(organizations) != 1 && !request.AllowOrganizationSelection) {
		return Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "invalid session or organization selection is required", nil)
	}
	return actor, nil
}

type PostgresTokenStore struct{ pool *pgxpool.Pool }

func NewPostgresTokenStore(pool *pgxpool.Pool) *PostgresTokenStore {
	return &PostgresTokenStore{pool: pool}
}

func (s *PostgresTokenStore) CreatePAT(ctx context.Context, organizationID, userID, name string, scopes []string, expiresAt time.Time) (CreatedToken, error) {
	plain, prefix, hash, err := newToken("ff_pat_")
	if err != nil {
		return CreatedToken{}, err
	}
	id, err := ids.New()
	if err != nil {
		return CreatedToken{}, err
	}
	now := time.Now().UTC()
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO personal_access_tokens (id, organization_id, user_id, name, token_prefix, token_hash, scopes, expires_at, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
`, id, organizationID, userID, strings.TrimSpace(name), prefix, hash, scopes, expiresAt.UTC(), now)
	if err != nil {
		return CreatedToken{}, fmt.Errorf("create personal access token: %w", err)
	}
	return CreatedToken{ID: id, Name: strings.TrimSpace(name), Prefix: prefix, Token: plain, Scopes: append([]string(nil), scopes...), ExpiresAt: expiresAt.UTC(), CreatedAt: now}, nil
}

func (s *PostgresTokenStore) ListPAT(ctx context.Context, organizationID, userID string) ([]CreatedToken, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT id::text, name, token_prefix, scopes, expires_at, created_at
FROM personal_access_tokens
WHERE organization_id=$1 AND user_id=$2 AND revoked_at IS NULL
ORDER BY created_at DESC
`, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("list personal access tokens: %w", err)
	}
	defer rows.Close()
	var result []CreatedToken
	for rows.Next() {
		var token CreatedToken
		if err := rows.Scan(&token.ID, &token.Name, &token.Prefix, &token.Scopes, &token.ExpiresAt, &token.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan personal access token: %w", err)
		}
		result = append(result, token)
	}
	return result, rows.Err()
}

func (s *PostgresTokenStore) RevokePAT(ctx context.Context, organizationID, userID, id string) error {
	commandTag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE personal_access_tokens SET revoked_at=now() WHERE id=$1 AND organization_id=$2 AND user_id=$3 AND revoked_at IS NULL`, id, organizationID, userID)
	if err != nil {
		return fmt.Errorf("revoke personal access token: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return apperr.New(apperr.CodeNotFound, 404, "personal access token not found", nil)
	}
	return nil
}

func (s *PostgresTokenStore) CreateSession(ctx context.Context, userID string, expiresAt time.Time) (string, string, error) {
	plain, _, hash, err := newToken("ff_session_")
	if err != nil {
		return "", "", err
	}
	id, err := ids.New()
	if err != nil {
		return "", "", err
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1,$2,$3,$4)`, id, userID, hash, expiresAt.UTC())
	if err != nil {
		return "", "", fmt.Errorf("create session: %w", err)
	}
	return id, plain, nil
}

func (s *PostgresTokenStore) RevokeSession(ctx context.Context, token string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, hashToken(token))
	return err
}

func newToken(prefix string) (plain, tokenPrefix, hash string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", fmt.Errorf("generate token: %w", err)
	}
	plain = prefix + base64.RawURLEncoding.EncodeToString(bytes)
	tokenPrefix = plain
	if len(tokenPrefix) > 16 {
		tokenPrefix = tokenPrefix[:16]
	}
	return plain, tokenPrefix, hashToken(plain), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

var _ Authenticator = (*PostgresAuthenticator)(nil)
var _ TokenStore = (*PostgresTokenStore)(nil)
var _ SessionStore = (*PostgresTokenStore)(nil)
