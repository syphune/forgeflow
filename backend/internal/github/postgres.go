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
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, repository_id::text, COALESCE(work_item_id::text,''), number, title, state, draft, head_sha, head_ref, body, url, updated_at FROM pull_requests WHERE organization_id=$1 AND repository_id=$2 ORDER BY updated_at DESC, number DESC LIMIT 100`, organizationID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository pull requests: %w", err)
	}
	defer rows.Close()
	result := make([]PullRequest, 0)
	for rows.Next() {
		var item PullRequest
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.WorkItemID, &item.Number, &item.Title, &item.State, &item.Draft, &item.HeadSHA, &item.HeadRef, &item.Body, &item.URL, &item.UpdatedAt); err != nil {
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
INSERT INTO pull_requests (id, organization_id, repository_id, work_item_id, number, title, state, draft, head_sha, head_ref, body, url, updated_at)
VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (organization_id, repository_id, number)
DO UPDATE SET work_item_id=COALESCE(EXCLUDED.work_item_id,pull_requests.work_item_id), title=EXCLUDED.title, state=EXCLUDED.state, draft=EXCLUDED.draft, head_sha=EXCLUDED.head_sha, head_ref=EXCLUDED.head_ref, body=EXCLUDED.body, url=EXCLUDED.url, updated_at=EXCLUDED.updated_at
RETURNING id::text, repository_id::text, COALESCE(work_item_id::text,''), number, title, state, draft, head_sha, head_ref, body, url, updated_at
`, id, organizationID, repositoryID, item.WorkItemID, item.Number, strings.TrimSpace(item.Title), strings.TrimSpace(item.State), item.Draft, strings.TrimSpace(item.HeadSHA), strings.TrimSpace(item.HeadRef), item.Body, strings.TrimSpace(item.URL), item.UpdatedAt.UTC()).Scan(&result.ID, &result.RepositoryID, &result.WorkItemID, &result.Number, &result.Title, &result.State, &result.Draft, &result.HeadSHA, &result.HeadRef, &result.Body, &result.URL, &result.UpdatedAt)
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
    WHERE pr.organization_id=$1 AND wi.project_id=$2 AND pr.repository_id=$3 AND wi.key=$4
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
    WHERE ci.organization_id=$1 AND wi.project_id=$2 AND ci.repository_id=$3
      AND wi.key=$4 AND lower(ci.conclusion)='success'
)
`, organizationID, projectID, repositoryID, strings.TrimSpace(workItemKey)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check successful CI: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) ListCIRuns(ctx context.Context, organizationID, repositoryID string) ([]CIRun, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, repository_id::text, external_id, status, conclusion, sha, url, updated_at FROM ci_runs WHERE organization_id=$1 AND repository_id=$2 ORDER BY updated_at DESC, external_id DESC LIMIT 100`, organizationID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository CI runs: %w", err)
	}
	defer rows.Close()
	result := make([]CIRun, 0)
	for rows.Next() {
		var item CIRun
		if err := rows.Scan(&item.ID, &item.RepositoryID, &item.ExternalID, &item.Status, &item.Conclusion, &item.SHA, &item.URL, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan repository CI run: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

var _ Store = (*PostgresStore)(nil)
