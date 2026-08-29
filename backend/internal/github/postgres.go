package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) BeginInstallationState(ctx context.Context, stateHash, userID, organizationID string, expiresAt time.Time) error {
	id, err := ids.New()
	if err != nil {
		return err
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO github_installation_states (id, state_hash, user_id, organization_id, expires_at) VALUES ($1,$2,$3,$4,$5)`, id, stateHash, userID, organizationID, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("store GitHub installation state: %w", err)
	}
	return nil
}

func (s *PostgresStore) ConsumeInstallationState(ctx context.Context, stateHash string) (string, string, error) {
	var userID, organizationID string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE github_installation_states SET used_at=now() WHERE state_hash=$1 AND used_at IS NULL AND expires_at > now() RETURNING user_id::text, organization_id::text`, stateHash).Scan(&userID, &organizationID)
	if err == pgx.ErrNoRows {
		return "", "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub installation state is invalid or expired", nil)
	}
	if err != nil {
		return "", "", fmt.Errorf("consume GitHub installation state: %w", err)
	}
	return userID, organizationID, nil
}

func (s *PostgresStore) InstallationOrganization(ctx context.Context, installationID int64) (string, error) {
	var organizationID string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT organization_id::text FROM github_installations WHERE github_installation_id=$1`, installationID).Scan(&organizationID)
	if err == pgx.ErrNoRows {
		return "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub installation is not linked", nil)
	}
	if err != nil {
		return "", fmt.Errorf("load GitHub installation organization: %w", err)
	}
	return organizationID, nil
}

func (s *PostgresStore) BeginUserAuthorizationState(ctx context.Context, stateHash, userID, organizationID, codeVerifier, redirectURI string, expiresAt time.Time) error {
	id, err := ids.New()
	if err != nil {
		return err
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO github_user_authorization_states (id, state_hash, user_id, organization_id, code_verifier, redirect_uri, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, stateHash, userID, organizationID, codeVerifier, redirectURI, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("store GitHub user authorization state: %w", err)
	}
	return nil
}

func (s *PostgresStore) ConsumeUserAuthorizationState(ctx context.Context, stateHash string) (string, string, string, string, error) {
	var userID, organizationID, verifier, redirectURI string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE github_user_authorization_states SET used_at=now() WHERE state_hash=$1 AND used_at IS NULL AND expires_at > now() RETURNING user_id::text, organization_id::text, code_verifier, redirect_uri`, stateHash).Scan(&userID, &organizationID, &verifier, &redirectURI)
	if err == pgx.ErrNoRows {
		return "", "", "", "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub user authorization state is invalid or expired", nil)
	}
	if err != nil {
		return "", "", "", "", fmt.Errorf("consume GitHub user authorization state: %w", err)
	}
	return userID, organizationID, verifier, redirectURI, nil
}

func (s *PostgresStore) GitHubUserID(ctx context.Context, userID string) (int64, error) {
	var githubUserID int64
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT COALESCE(github_user_id,0) FROM users WHERE id=$1`, userID).Scan(&githubUserID)
	if err == pgx.ErrNoRows || githubUserID <= 0 {
		return 0, apperr.New(apperr.CodeNotFound, 404, "Forgeflow user is not linked to GitHub", nil)
	}
	if err != nil {
		return 0, fmt.Errorf("load Forgeflow GitHub user: %w", err)
	}
	return githubUserID, nil
}

func (s *PostgresStore) UpsertUserCredential(ctx context.Context, userID string, credential StoredUserCredential) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO github_user_credentials (user_id, github_user_id, encrypted_access_token, encrypted_refresh_token, access_token_expires_at, refresh_token_expires_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (user_id) DO UPDATE SET github_user_id=EXCLUDED.github_user_id, encrypted_access_token=EXCLUDED.encrypted_access_token, encrypted_refresh_token=EXCLUDED.encrypted_refresh_token, access_token_expires_at=EXCLUDED.access_token_expires_at, refresh_token_expires_at=EXCLUDED.refresh_token_expires_at, updated_at=now()
`, userID, credential.GitHubUserID, credential.EncryptedAccessToken, credential.EncryptedRefreshToken, nullableTime(credential.AccessTokenExpiresAt), nullableTime(credential.RefreshTokenExpiresAt))
	if err != nil {
		return fmt.Errorf("store GitHub user credential: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetUserCredential(ctx context.Context, userID string) (StoredUserCredential, error) {
	var credential StoredUserCredential
	var accessExpiresAt, refreshExpiresAt *time.Time
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT github_user_id, encrypted_access_token, encrypted_refresh_token, access_token_expires_at, refresh_token_expires_at FROM github_user_credentials WHERE user_id=$1`, userID).Scan(&credential.GitHubUserID, &credential.EncryptedAccessToken, &credential.EncryptedRefreshToken, &accessExpiresAt, &refreshExpiresAt)
	if err == pgx.ErrNoRows {
		return StoredUserCredential{}, apperr.New(apperr.CodeNotFound, 404, "GitHub code authorization is not connected", nil)
	}
	if err != nil {
		return StoredUserCredential{}, fmt.Errorf("load GitHub user credential: %w", err)
	}
	if accessExpiresAt != nil {
		credential.AccessTokenExpiresAt = accessExpiresAt.UTC()
	}
	if refreshExpiresAt != nil {
		credential.RefreshTokenExpiresAt = refreshExpiresAt.UTC()
	}
	return credential, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func (s *PostgresStore) UpsertInstallation(ctx context.Context, organizationID string, installationID int64, accountLogin string) (Installation, error) {
	var result Installation
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO github_installations (organization_id, github_installation_id, account_login)
VALUES ($1,$2,$3)
ON CONFLICT (github_installation_id) DO UPDATE SET account_login=EXCLUDED.account_login
WHERE github_installations.organization_id=EXCLUDED.organization_id
RETURNING id::text, github_installation_id, account_login, created_at
`, organizationID, installationID, strings.TrimSpace(accountLogin)).Scan(&result.ID, &result.GitHubInstallationID, &result.AccountLogin, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return Installation{}, apperr.New(apperr.CodeConflict, 409, "GitHub installation is already linked to another organization", nil)
	}
	if err != nil {
		return Installation{}, fmt.Errorf("upsert GitHub installation: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListInstallations(ctx context.Context, organizationID string) ([]Installation, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, github_installation_id, account_login, created_at FROM github_installations WHERE organization_id=$1 ORDER BY account_login, id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list GitHub installations: %w", err)
	}
	defer rows.Close()
	result := make([]Installation, 0)
	for rows.Next() {
		var item Installation
		if err := rows.Scan(&item.ID, &item.GitHubInstallationID, &item.AccountLogin, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan GitHub installation: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) UpsertRepository(ctx context.Context, organizationID string, installationID, repositoryID int64, fullName, defaultBranch, cloneURL string) (Repository, error) {
	var result Repository
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO repositories (organization_id, github_installation_id, github_repository_id, full_name, default_branch, clone_url, last_seen_at)
VALUES ($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (organization_id, github_repository_id) WHERE github_repository_id IS NOT NULL
DO UPDATE SET github_installation_id=EXCLUDED.github_installation_id, full_name=EXCLUDED.full_name, default_branch=EXCLUDED.default_branch, clone_url=EXCLUDED.clone_url, last_seen_at=now()
RETURNING id::text, github_repository_id, full_name, default_branch, clone_url, github_installation_id
`, organizationID, installationID, repositoryID, strings.TrimSpace(fullName), strings.TrimSpace(defaultBranch), strings.TrimSpace(cloneURL)).Scan(&result.ID, &result.GitHubRepositoryID, &result.FullName, &result.DefaultBranch, &result.CloneURL, &result.InstallationID)
	if err != nil {
		return Repository{}, fmt.Errorf("upsert GitHub repository: %w", err)
	}
	var account string
	if err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT account_login FROM github_installations WHERE organization_id=$1 AND github_installation_id=$2`, organizationID, result.InstallationID).Scan(&account); err != nil && err != pgx.ErrNoRows {
		return Repository{}, fmt.Errorf("load GitHub installation account: %w", err)
	}
	result.InstallationAccount = account
	return result, nil
}

func (s *PostgresStore) ProjectRepositoryIDs(ctx context.Context, organizationID, projectID string) (map[string]bool, error) {
	result := make(map[string]bool)
	if strings.TrimSpace(projectID) == "" {
		return result, nil
	}
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT repository_id::text FROM repository_links WHERE organization_id=$1 AND project_id=$2`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project GitHub repositories: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var repositoryID string
		if err := rows.Scan(&repositoryID); err != nil {
			return nil, fmt.Errorf("scan project GitHub repository: %w", err)
		}
		result[repositoryID] = true
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListProjectRepositories(ctx context.Context, organizationID, projectID string) ([]Repository, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT r.id::text, r.github_repository_id, r.full_name, r.default_branch, r.clone_url, r.github_installation_id, COALESCE(gi.account_login,''), true
FROM repository_links rl
JOIN repositories r ON r.organization_id=rl.organization_id AND r.id=rl.repository_id
LEFT JOIN github_installations gi ON gi.organization_id=r.organization_id AND gi.github_installation_id=r.github_installation_id
WHERE rl.organization_id=$1 AND rl.project_id=$2
ORDER BY r.full_name
`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list linked GitHub repositories: %w", err)
	}
	defer rows.Close()
	result := make([]Repository, 0)
	for rows.Next() {
		var item Repository
		if err := rows.Scan(&item.ID, &item.GitHubRepositoryID, &item.FullName, &item.DefaultBranch, &item.CloneURL, &item.InstallationID, &item.InstallationAccount, &item.Linked); err != nil {
			return nil, fmt.Errorf("scan linked GitHub repository: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) LinkRepository(ctx context.Context, organizationID, projectID, repositoryID string) error {
	var valid bool
	if err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM projects WHERE organization_id=$1 AND id=$2)
   AND EXISTS (SELECT 1 FROM repositories WHERE organization_id=$1 AND id=$3)
`, organizationID, projectID, repositoryID).Scan(&valid); err != nil {
		return fmt.Errorf("validate GitHub repository link: %w", err)
	}
	if !valid {
		return apperr.New(apperr.CodeNotFound, 404, "project or repository not found", nil)
	}
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO repository_links (organization_id, project_id, repository_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, organizationID, projectID, repositoryID)
	if err != nil {
		return fmt.Errorf("link GitHub repository: %w", err)
	}
	return nil
}

func (s *PostgresStore) UnlinkRepository(ctx context.Context, organizationID, projectID, repositoryID string) error {
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM repository_links WHERE organization_id=$1 AND project_id=$2 AND repository_id=$3`, organizationID, projectID, repositoryID)
	if err != nil {
		return fmt.Errorf("unlink GitHub repository: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.CodeNotFound, 404, "repository link not found", nil)
	}
	return nil
}

func (s *PostgresStore) ListBranches(ctx context.Context, organizationID, repositoryID string) ([]Branch, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, repository_id::text, name, head_sha, updated_at FROM branches WHERE organization_id=$1 AND repository_id=$2 ORDER BY name`, organizationID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository branches: %w", err)
	}
	defer rows.Close()
	result := make([]Branch, 0)
	for rows.Next() {
		var item Branch
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.Name, &item.HeadSHA, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan repository branch: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListCommits(ctx context.Context, organizationID, repositoryID string) ([]Commit, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, repository_id::text, sha, message, author_login, committed_at FROM commits WHERE organization_id=$1 AND repository_id=$2 ORDER BY committed_at DESC NULLS LAST, sha DESC LIMIT 100`, organizationID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository commits: %w", err)
	}
	defer rows.Close()
	result := make([]Commit, 0)
	for rows.Next() {
		var item Commit
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.SHA, &item.Message, &item.AuthorLogin, &item.CommittedAt); err != nil {
			return nil, fmt.Errorf("scan repository commit: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListPullRequests(ctx context.Context, organizationID, repositoryID string) ([]PullRequest, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, repository_id::text, COALESCE(work_item_id::text,''), number, title, state, draft, head_sha, head_ref, base_ref, body, url, COALESCE(author_login,''), changed_paths, approved_by_logins, changes_requested, merged, updated_at FROM pull_requests WHERE organization_id=$1 AND repository_id=$2 ORDER BY updated_at DESC, number DESC LIMIT 100`, organizationID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository pull requests: %w", err)
	}
	defer rows.Close()
	result := make([]PullRequest, 0)
	for rows.Next() {
		var item PullRequest
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.WorkItemID, &item.Number, &item.Title, &item.State, &item.Draft, &item.HeadSHA, &item.HeadRef, &item.BaseRef, &item.Body, &item.URL, &item.AuthorLogin, &item.ChangedPaths, &item.ApprovedByLogins, &item.ChangesRequested, &item.Merged, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan repository pull request: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) UpsertPullRequest(ctx context.Context, organizationID, repositoryID string, item PullRequest) (PullRequest, error) {
	if item.Number <= 0 {
		return PullRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "pull request number is required", nil)
	}
	id, err := ids.New()
	if err != nil {
		return PullRequest{}, err
	}
	var result PullRequest
	err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO pull_requests (id, organization_id, repository_id, work_item_id, number, title, state, draft, head_sha, head_ref, base_ref, body, url, author_login, changed_paths, approved_by_logins, changes_requested, merged, updated_at)
VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (organization_id, repository_id, number)
DO UPDATE SET work_item_id=COALESCE(EXCLUDED.work_item_id,pull_requests.work_item_id), title=EXCLUDED.title, state=EXCLUDED.state, draft=EXCLUDED.draft, head_sha=EXCLUDED.head_sha, head_ref=EXCLUDED.head_ref, base_ref=EXCLUDED.base_ref, body=EXCLUDED.body, url=EXCLUDED.url, author_login=EXCLUDED.author_login, changed_paths=EXCLUDED.changed_paths, approved_by_logins=EXCLUDED.approved_by_logins, changes_requested=EXCLUDED.changes_requested, merged=EXCLUDED.merged, updated_at=EXCLUDED.updated_at
RETURNING id::text, repository_id::text, COALESCE(work_item_id::text,''), number, title, state, draft, head_sha, head_ref, base_ref, body, url, COALESCE(author_login,''), changed_paths, approved_by_logins, changes_requested, merged, updated_at
`, id, organizationID, repositoryID, item.WorkItemID, item.Number, strings.TrimSpace(item.Title), strings.TrimSpace(item.State), item.Draft, strings.TrimSpace(item.HeadSHA), strings.TrimSpace(item.HeadRef), strings.TrimSpace(item.BaseRef), item.Body, strings.TrimSpace(item.URL), strings.TrimSpace(item.AuthorLogin), item.ChangedPaths, item.ApprovedByLogins, item.ChangesRequested, item.Merged, item.UpdatedAt.UTC()).Scan(&result.ID, &result.RepositoryID, &result.WorkItemID, &result.Number, &result.Title, &result.State, &result.Draft, &result.HeadSHA, &result.HeadRef, &result.BaseRef, &result.Body, &result.URL, &result.AuthorLogin, &result.ChangedPaths, &result.ApprovedByLogins, &result.ChangesRequested, &result.Merged, &result.UpdatedAt)
	if err != nil {
		return PullRequest{}, fmt.Errorf("upsert pull request: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) HasPullRequest(ctx context.Context, organizationID, projectID, repositoryID, workItemKey string) (bool, error) {
	var exists bool
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pull_requests pr
    JOIN work_items wi ON wi.organization_id=pr.organization_id AND wi.id=pr.work_item_id
    JOIN projects p ON p.organization_id=wi.organization_id AND p.id=wi.project_id
    WHERE pr.organization_id=$1 AND wi.project_id=$2 AND pr.repository_id=$3 AND p.key || '-' || wi.number=$4
)
`, organizationID, projectID, repositoryID, strings.TrimSpace(workItemKey)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check linked pull request: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) HasSuccessfulCI(ctx context.Context, organizationID, projectID, repositoryID, workItemKey string) (bool, error) {
	var exists bool
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM ci_runs ci
    JOIN pull_requests pr
      ON pr.organization_id=ci.organization_id
     AND pr.repository_id=ci.repository_id
     AND pr.head_sha=ci.sha
    JOIN work_items wi ON wi.organization_id=pr.organization_id AND wi.id=pr.work_item_id
    JOIN projects p ON p.organization_id=wi.organization_id AND p.id=wi.project_id
    WHERE ci.organization_id=$1 AND wi.project_id=$2 AND ci.repository_id=$3
      AND p.key || '-' || wi.number=$4 AND lower(ci.conclusion)='success'
)
`, organizationID, projectID, repositoryID, strings.TrimSpace(workItemKey)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check successful CI: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) GetPullRequest(ctx context.Context, organizationID, projectID, repositoryID, workItemKey, currentSHA string) (PullRequest, bool, error) {
	var item PullRequest
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT pr.id::text, pr.repository_id::text, COALESCE(pr.work_item_id::text,''), pr.number, pr.title, pr.state, pr.draft, pr.head_sha, COALESCE(pr.head_ref,''), COALESCE(pr.base_ref,''), COALESCE(pr.body,''), COALESCE(pr.url,''), COALESCE(pr.author_login,''), pr.changed_paths, pr.approved_by_logins, pr.changes_requested, pr.merged, pr.updated_at
FROM pull_requests pr
JOIN work_items wi ON wi.organization_id=pr.organization_id AND wi.id=pr.work_item_id
JOIN projects p ON p.organization_id=wi.organization_id AND p.id=wi.project_id
WHERE pr.organization_id=$1 AND wi.project_id=$2 AND ($3='' OR pr.repository_id=$3) AND (p.key || '-' || wi.number=$4 OR wi.id::text=$4) AND ($5='' OR pr.head_sha=$5)
ORDER BY pr.updated_at DESC, pr.number DESC
LIMIT 1`, organizationID, projectID, repositoryID, strings.TrimSpace(workItemKey), strings.TrimSpace(currentSHA)).Scan(&item.ID, &item.RepositoryID, &item.WorkItemID, &item.Number, &item.Title, &item.State, &item.Draft, &item.HeadSHA, &item.HeadRef, &item.BaseRef, &item.Body, &item.URL, &item.AuthorLogin, &item.ChangedPaths, &item.ApprovedByLogins, &item.ChangesRequested, &item.Merged, &item.UpdatedAt)
	if err == pgx.ErrNoRows {
		return PullRequest{}, false, nil
	}
	if err != nil {
		return PullRequest{}, false, fmt.Errorf("get pull request evidence: %w", err)
	}
	return item, true, nil
}

func (s *PostgresStore) HasRequiredCI(ctx context.Context, organizationID, projectID, repositoryID, workItemKey, currentSHA string, required []string) (bool, error) {
	currentSHA = strings.TrimSpace(currentSHA)
	if currentSHA == "" {
		return false, nil
	}
	var result bool
	if len(required) == 0 {
		err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM ci_runs ci
  JOIN pull_requests pr ON pr.organization_id=ci.organization_id AND pr.repository_id=ci.repository_id AND pr.head_sha=ci.sha
  JOIN work_items wi ON wi.organization_id=pr.organization_id AND wi.id=pr.work_item_id
  JOIN projects p ON p.organization_id=wi.organization_id AND p.id=wi.project_id
  WHERE ci.organization_id=$1 AND wi.project_id=$2 AND ci.repository_id=$3 AND p.key || '-' || wi.number=$4 AND ci.sha=$5 AND lower(ci.conclusion)='success'
)`, organizationID, projectID, repositoryID, strings.TrimSpace(workItemKey), currentSHA).Scan(&result)
		if err != nil {
			return false, fmt.Errorf("check current-head CI: %w", err)
		}
		return result, nil
	}
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pull_requests pr
  JOIN work_items wi ON wi.organization_id=pr.organization_id AND wi.id=pr.work_item_id
  JOIN projects p ON p.organization_id=wi.organization_id AND p.id=wi.project_id
  WHERE pr.organization_id=$1 AND wi.project_id=$2 AND pr.repository_id=$3 AND p.key || '-' || wi.number=$4 AND pr.head_sha=$5
) AND COALESCE((
  SELECT bool_and(EXISTS (
    SELECT 1 FROM ci_runs ci
    WHERE ci.organization_id=$1 AND ci.repository_id=$3 AND ci.sha=$5 AND lower(ci.check_name)=lower(required.check_name) AND lower(ci.conclusion)='success'
  )) FROM unnest($6::text[]) AS required(check_name)
), false)`, organizationID, projectID, repositoryID, strings.TrimSpace(workItemKey), currentSHA, required).Scan(&result)
	if err != nil {
		return false, fmt.Errorf("check required current-head CI: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListCIRuns(ctx context.Context, organizationID, repositoryID string) ([]CIRun, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, repository_id::text, external_id, check_name, status, conclusion, sha, url, updated_at FROM ci_runs WHERE organization_id=$1 AND repository_id=$2 ORDER BY updated_at DESC, external_id DESC LIMIT 100`, organizationID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository CI runs: %w", err)
	}
	defer rows.Close()
	result := make([]CIRun, 0)
	for rows.Next() {
		var item CIRun
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.ExternalID, &item.CheckName, &item.Status, &item.Conclusion, &item.SHA, &item.URL, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan repository CI run: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

var _ Store = (*PostgresStore)(nil)
