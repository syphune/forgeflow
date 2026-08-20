package tenant

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{1,31}$`)

func validateKey(value, field string) error {
	if !keyPattern.MatchString(value) {
		return apperr.New(apperr.CodeInvalidArgument, 422, field+" must be 2-32 alphanumeric characters, '_' or '-'", nil)
	}
	return nil
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Organization(ctx context.Context, id string) (*Organization, error) {
	var result Organization
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text, slug, display_name, created_at FROM organizations WHERE id=$1`, id).Scan(&result.ID, &result.Slug, &result.DisplayName, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, apperr.New(apperr.CodeNotFound, 404, "organization not found", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("load organization: %w", err)
	}
	return &result, nil
}

func (s *PostgresStore) Workspace(ctx context.Context, organizationID, id string) (*Workspace, error) {
	var result Workspace
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text, organization_id::text, key, display_name, created_at FROM workspaces WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&result.ID, &result.OrganizationID, &result.Key, &result.DisplayName, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, apperr.New(apperr.CodeNotFound, 404, "workspace not found", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("load workspace: %w", err)
	}
	return &result, nil
}

func (s *PostgresStore) Project(ctx context.Context, organizationID, id string) (*Project, error) {
	var result Project
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text, organization_id::text, workspace_id::text, key, display_name, created_at FROM projects WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&result.ID, &result.OrganizationID, &result.WorkspaceID, &result.Key, &result.DisplayName, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return &result, nil
}

func (s *PostgresStore) ListOrganizations(ctx context.Context, userID string) ([]Organization, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT o.id::text, o.slug, o.display_name, o.created_at
FROM organizations o
JOIN organization_memberships om ON om.organization_id=o.id
WHERE om.user_id=$1
ORDER BY lower(o.display_name), o.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	defer rows.Close()
	result := make([]Organization, 0)
	for rows.Next() {
		var item Organization
		if err := rows.Scan(&item.ID, &item.Slug, &item.DisplayName, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListWorkspaces(ctx context.Context, organizationID string) ([]Workspace, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, organization_id::text, key, display_name, created_at FROM workspaces WHERE organization_id=$1 ORDER BY key`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	var result []Workspace
	for rows.Next() {
		var item Workspace
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Key, &item.DisplayName, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, organizationID, key, displayName string) (Workspace, error) {
	id, err := ids.New()
	if err != nil {
		return Workspace{}, err
	}
	var result Workspace
	err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1,$2,$3,$4) RETURNING id::text, organization_id::text, key, display_name, created_at`, id, organizationID, strings.ToUpper(key), strings.TrimSpace(displayName)).Scan(&result.ID, &result.OrganizationID, &result.Key, &result.DisplayName, &result.CreatedAt)
	if err != nil {
		return Workspace{}, mapStoreError(err, "workspace key is already used")
	}
	return result, nil
}

func (s *PostgresStore) UpdateWorkspace(ctx context.Context, organizationID, id, displayName string) (Workspace, error) {
	var result Workspace
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE workspaces SET display_name=$1 WHERE organization_id=$2 AND id=$3 RETURNING id::text, organization_id::text, key, display_name, created_at`, strings.TrimSpace(displayName), organizationID, id).Scan(&result.ID, &result.OrganizationID, &result.Key, &result.DisplayName, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return Workspace{}, apperr.New(apperr.CodeNotFound, 404, "workspace not found", nil)
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("update workspace: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListProjects(ctx context.Context, organizationID, workspaceID string) ([]Project, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, organization_id::text, workspace_id::text, key, display_name, created_at FROM projects WHERE organization_id=$1 AND ($2='' OR workspace_id=NULLIF($2, '')::uuid) ORDER BY key`, organizationID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var result []Project
	for rows.Next() {
		var item Project
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Key, &item.DisplayName, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) CreateProject(ctx context.Context, organizationID, workspaceID, key, displayName, creatorID string) (Project, error) {
	id, err := ids.New()
	if err != nil {
		return Project{}, err
	}
	if platformdb.InTransaction(ctx) {
		return s.createProject(ctx, platformdb.ExecutorFrom(ctx, s.pool), id, organizationID, workspaceID, key, displayName, creatorID)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("begin project create: %w", err)
	}
	defer tx.Rollback(ctx)
	result, err := s.createProject(ctx, tx, id, organizationID, workspaceID, key, displayName, creatorID)
	if err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("commit project create: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) createProject(ctx context.Context, exec platformdb.Executor, id, organizationID, workspaceID, key, displayName, creatorID string) (Project, error) {
	var result Project
	err := exec.QueryRow(ctx, `INSERT INTO projects (id, organization_id, workspace_id, key, display_name) SELECT $1, $2, $3, $4, $5 WHERE EXISTS (SELECT 1 FROM workspaces WHERE organization_id=$2 AND id=$3) RETURNING id::text, organization_id::text, workspace_id::text, key, display_name, created_at`, id, organizationID, workspaceID, strings.ToUpper(key), strings.TrimSpace(displayName)).Scan(&result.ID, &result.OrganizationID, &result.WorkspaceID, &result.Key, &result.DisplayName, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return Project{}, apperr.New(apperr.CodeNotFound, 404, "workspace not found", nil)
	}
	if err != nil {
		return Project{}, mapStoreError(err, "project key is already used")
	}
	if _, err := exec.Exec(ctx, `INSERT INTO project_memberships (organization_id, project_id, user_id, role_key) SELECT $1, $2, $3, 'owner' WHERE EXISTS (SELECT 1 FROM organization_memberships WHERE organization_id=$1 AND user_id=$3) ON CONFLICT DO NOTHING`, organizationID, result.ID, creatorID); err != nil {
		return Project{}, fmt.Errorf("add project creator: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) UpdateProject(ctx context.Context, organizationID, id, displayName string) (Project, error) {
	var result Project
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE projects SET display_name=$1 WHERE organization_id=$2 AND id=$3 RETURNING id::text, organization_id::text, workspace_id::text, key, display_name, created_at`, strings.TrimSpace(displayName), organizationID, id).Scan(&result.ID, &result.OrganizationID, &result.WorkspaceID, &result.Key, &result.DisplayName, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return Project{}, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListMembers(ctx context.Context, organizationID, projectID string) ([]Member, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT u.id::text, u.login, u.display_name, COALESCE(pm.role_key, om.role_key), pm.user_id IS NOT NULL
FROM organization_memberships om
JOIN users u ON u.id=om.user_id
JOIN projects p ON p.organization_id=om.organization_id AND p.id=$2
LEFT JOIN project_memberships pm ON pm.organization_id=om.organization_id AND pm.project_id=p.id AND pm.user_id=om.user_id
WHERE om.organization_id=$1
ORDER BY lower(u.display_name), u.id`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}
	defer rows.Close()
	result := make([]Member, 0)
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.Login, &member.DisplayName, &member.RoleKey, &member.ProjectRole); err != nil {
			return nil, fmt.Errorf("scan project member: %w", err)
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

func (s *PostgresStore) SetProjectMember(ctx context.Context, organizationID, projectID, userID, roleKey string) (Member, error) {
	var result Member
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO project_memberships (organization_id, project_id, user_id, role_key)
SELECT $1, p.id, om.user_id, $4
FROM projects p
JOIN organization_memberships om ON om.organization_id=p.organization_id AND om.user_id=$3
JOIN roles r ON r.key=$4
WHERE p.organization_id=$1 AND p.id=$2
ON CONFLICT (organization_id, project_id, user_id) DO UPDATE SET role_key=EXCLUDED.role_key
RETURNING user_id::text, (SELECT login FROM users WHERE id=project_memberships.user_id), (SELECT display_name FROM users WHERE id=project_memberships.user_id), role_key, true`, organizationID, projectID, userID, roleKey).Scan(&result.ID, &result.Login, &result.DisplayName, &result.RoleKey, &result.ProjectRole)
	if err == pgx.ErrNoRows {
		return Member{}, apperr.New(apperr.CodeNotFound, 404, "project or organization member not found", nil)
	}
	if err != nil {
		return Member{}, fmt.Errorf("set project member: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) RemoveProjectMember(ctx context.Context, organizationID, projectID, userID string) error {
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM project_memberships WHERE organization_id=$1 AND project_id=$2 AND user_id=$3`, organizationID, projectID, userID)
	if err != nil {
		return fmt.Errorf("remove project member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.CodeNotFound, 404, "project member override not found", nil)
	}
	return nil
}

func (s *PostgresStore) ListOrganizationMembers(ctx context.Context, organizationID string) ([]Member, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT u.id::text, u.login, u.display_name, om.role_key
FROM organization_memberships om
JOIN users u ON u.id=om.user_id
WHERE om.organization_id=$1
ORDER BY lower(u.display_name), u.id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization members: %w", err)
	}
	defer rows.Close()
	result := make([]Member, 0)
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.Login, &member.DisplayName, &member.RoleKey); err != nil {
			return nil, fmt.Errorf("scan organization member: %w", err)
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

func (s *PostgresStore) AddOrganizationMember(ctx context.Context, organizationID, login, roleKey string) (Member, error) {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	var userID string
	err := exec.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN roles r ON r.key=$2 WHERE lower(u.login)=lower($1)`, login, roleKey).Scan(&userID)
	if err == pgx.ErrNoRows {
		return Member{}, apperr.New(apperr.CodeNotFound, 404, "GitHub user is not registered in Forgeflow or role is invalid", nil)
	}
	if err != nil {
		return Member{}, fmt.Errorf("find organization member: %w", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role_key) VALUES ($1,$2,$3) ON CONFLICT (organization_id, user_id) DO NOTHING`, organizationID, userID, roleKey); err != nil {
		return Member{}, fmt.Errorf("add organization member: %w", err)
	}
	return s.SetOrganizationMember(ctx, organizationID, userID, roleKey)
}

func (s *PostgresStore) SetOrganizationMember(ctx context.Context, organizationID, userID, roleKey string) (Member, error) {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	var validRole string
	if err := exec.QueryRow(ctx, `SELECT key FROM roles WHERE key=$1`, roleKey).Scan(&validRole); err == pgx.ErrNoRows {
		return Member{}, apperr.New(apperr.CodeInvalidArgument, 422, "role_key is invalid", nil)
	} else if err != nil {
		return Member{}, fmt.Errorf("validate organization role: %w", err)
	}
	rows, err := exec.Query(ctx, `SELECT user_id::text, role_key FROM organization_memberships WHERE organization_id=$1 FOR UPDATE`, organizationID)
	if err != nil {
		return Member{}, fmt.Errorf("lock organization members: %w", err)
	}
	var currentRole string
	found := false
	owners := 0
	for rows.Next() {
		var memberID, memberRole string
		if err := rows.Scan(&memberID, &memberRole); err != nil {
			rows.Close()
			return Member{}, fmt.Errorf("scan organization member lock: %w", err)
		}
		if memberRole == "owner" {
			owners++
		}
		if memberID == userID {
			currentRole = memberRole
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Member{}, fmt.Errorf("read organization member lock: %w", err)
	}
	rows.Close()
	if !found {
		return Member{}, apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
	}
	if currentRole == "owner" && roleKey != "owner" && owners <= 1 {
		return Member{}, apperr.New(apperr.CodeConflict, 409, "the organization must retain at least one owner", nil)
	}
	var result Member
	err = exec.QueryRow(ctx, `
UPDATE organization_memberships om
SET role_key=$3
FROM users u
WHERE om.organization_id=$1 AND om.user_id=$2 AND u.id=om.user_id
RETURNING om.user_id::text, u.login, u.display_name, om.role_key`, organizationID, userID, roleKey).Scan(&result.ID, &result.Login, &result.DisplayName, &result.RoleKey)
	if err == pgx.ErrNoRows {
		return Member{}, apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
	}
	if err != nil {
		return Member{}, fmt.Errorf("set organization member: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) RemoveOrganizationMember(ctx context.Context, organizationID, userID string) error {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	rows, err := exec.Query(ctx, `SELECT user_id::text, role_key FROM organization_memberships WHERE organization_id=$1 FOR UPDATE`, organizationID)
	if err != nil {
		return fmt.Errorf("lock organization members for removal: %w", err)
	}
	found := false
	isOwner := false
	owners := 0
	for rows.Next() {
		var memberID, memberRole string
		if err := rows.Scan(&memberID, &memberRole); err != nil {
			rows.Close()
			return fmt.Errorf("scan organization member removal lock: %w", err)
		}
		if memberRole == "owner" {
			owners++
		}
		if memberID == userID {
			found = true
			isOwner = memberRole == "owner"
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read organization member removal lock: %w", err)
	}
	rows.Close()
	if !found {
		return apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
	}
	if isOwner && owners <= 1 {
		return apperr.New(apperr.CodeConflict, 409, "the organization must retain at least one owner", nil)
	}
	if _, err := exec.Exec(ctx, `DELETE FROM project_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID); err != nil {
		return fmt.Errorf("remove project role overrides: %w", err)
	}
	if _, err := exec.Exec(ctx, `DELETE FROM notifications WHERE organization_id=$1 AND user_id=$2`, organizationID, userID); err != nil {
		return fmt.Errorf("remove member notifications: %w", err)
	}
	if _, err := exec.Exec(ctx, `DELETE FROM organization_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID); err != nil {
		return fmt.Errorf("remove organization member: %w", err)
	}
	return nil
}

func mapStoreError(err error, conflict string) error {
	if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
		return apperr.New(apperr.CodeConflict, 409, conflict, nil)
	}
	return fmt.Errorf("tenant store: %w", err)
}

type MemoryStore struct {
	mu                  sync.Mutex
	organizations       map[string]Organization
	workspaces          map[string]Workspace
	projects            map[string]Project
	members             map[string][]Member
	organizationMembers map[string][]Member
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{organizations: make(map[string]Organization), workspaces: make(map[string]Workspace), projects: make(map[string]Project), members: make(map[string][]Member), organizationMembers: make(map[string][]Member)}
}

func (s *MemoryStore) Organization(_ context.Context, id string) (*Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.organizations[id]
	if !ok {
		return nil, apperr.New(apperr.CodeNotFound, 404, "organization not found", nil)
	}
	return &item, nil
}

func (s *MemoryStore) Workspace(_ context.Context, organizationID, id string) (*Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.workspaces[id]
	if !ok || item.OrganizationID != organizationID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "workspace not found", nil)
	}
	return &item, nil
}

func (s *MemoryStore) Project(_ context.Context, organizationID, id string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.projects[id]
	if !ok || item.OrganizationID != organizationID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	return &item, nil
}

func (s *MemoryStore) ListOrganizations(_ context.Context, userID string) ([]Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Organization, 0)
	for organizationID, members := range s.organizationMembers {
		for _, member := range members {
			if member.ID != userID {
				continue
			}
			if organization, ok := s.organizations[organizationID]; ok {
				result = append(result, organization)
			}
			break
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if strings.EqualFold(result[i].DisplayName, result[j].DisplayName) {
			return result[i].ID < result[j].ID
		}
		return strings.ToLower(result[i].DisplayName) < strings.ToLower(result[j].DisplayName)
	})
	return result, nil
}

func (s *MemoryStore) ListWorkspaces(_ context.Context, organizationID string) ([]Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Workspace, 0)
	for _, item := range s.workspaces {
		if item.OrganizationID == organizationID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *MemoryStore) CreateWorkspace(_ context.Context, organizationID, key, displayName string) (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.workspaces {
		if item.OrganizationID == organizationID && strings.EqualFold(item.Key, key) {
			return Workspace{}, apperr.New(apperr.CodeConflict, 409, "workspace key is already used", nil)
		}
	}
	id, err := ids.New()
	if err != nil {
		return Workspace{}, err
	}
	item := Workspace{ID: id, OrganizationID: organizationID, Key: strings.ToUpper(key), DisplayName: strings.TrimSpace(displayName), CreatedAt: time.Now().UTC()}
	s.workspaces[id] = item
	return item, nil
}

func (s *MemoryStore) UpdateWorkspace(_ context.Context, organizationID, id, displayName string) (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.workspaces[id]
	if !ok || item.OrganizationID != organizationID {
		return Workspace{}, apperr.New(apperr.CodeNotFound, 404, "workspace not found", nil)
	}
	item.DisplayName = strings.TrimSpace(displayName)
	s.workspaces[id] = item
	return item, nil
}

func (s *MemoryStore) ListProjects(_ context.Context, organizationID, workspaceID string) ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Project, 0)
	for _, item := range s.projects {
		if item.OrganizationID == organizationID && (workspaceID == "" || item.WorkspaceID == workspaceID) {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *MemoryStore) CreateProject(_ context.Context, organizationID, workspaceID, key, displayName, creatorID string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, ok := s.workspaces[workspaceID]
	if !ok || workspace.OrganizationID != organizationID {
		return Project{}, apperr.New(apperr.CodeNotFound, 404, "workspace not found", nil)
	}
	for _, item := range s.projects {
		if item.OrganizationID == organizationID && strings.EqualFold(item.Key, key) {
			return Project{}, apperr.New(apperr.CodeConflict, 409, "project key is already used", nil)
		}
	}
	id, err := ids.New()
	if err != nil {
		return Project{}, err
	}
	item := Project{ID: id, OrganizationID: organizationID, WorkspaceID: workspaceID, Key: strings.ToUpper(key), DisplayName: strings.TrimSpace(displayName), CreatedAt: time.Now().UTC()}
	s.projects[id] = item
	for _, member := range s.organizationMembers[organizationID] {
		if member.ID == creatorID {
			member.RoleKey = "owner"
			member.ProjectRole = true
			s.members[id] = []Member{member}
			break
		}
	}
	return item, nil
}

func (s *MemoryStore) UpdateProject(_ context.Context, organizationID, id, displayName string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.projects[id]
	if !ok || item.OrganizationID != organizationID {
		return Project{}, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	item.DisplayName = strings.TrimSpace(displayName)
	s.projects[id] = item
	return item, nil
}

func (s *MemoryStore) ListMembers(_ context.Context, organizationID, projectID string) ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok || project.OrganizationID != organizationID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	merged := make(map[string]Member, len(s.organizationMembers[organizationID])+len(s.members[projectID]))
	for _, member := range s.organizationMembers[organizationID] {
		member.ProjectRole = false
		merged[member.ID] = member
	}
	for _, member := range s.members[projectID] {
		member.ProjectRole = true
		merged[member.ID] = member
	}
	result := make([]Member, 0, len(merged))
	for _, member := range merged {
		result = append(result, member)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].DisplayName) < strings.ToLower(result[j].DisplayName)
	})
	return result, nil
}

func (s *MemoryStore) SetProjectMember(_ context.Context, organizationID, projectID, userID, roleKey string) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok || project.OrganizationID != organizationID {
		return Member{}, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	var organizationMember Member
	foundOrganizationMember := false
	for _, member := range s.organizationMembers[organizationID] {
		if member.ID == userID {
			organizationMember = member
			foundOrganizationMember = true
			break
		}
	}
	if !foundOrganizationMember {
		return Member{}, apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
	}
	for index, member := range s.members[projectID] {
		if member.ID == userID {
			member.RoleKey = roleKey
			member.ProjectRole = true
			s.members[projectID][index] = member
			return member, nil
		}
	}
	member := organizationMember
	member.RoleKey = roleKey
	member.ProjectRole = true
	s.members[projectID] = append(s.members[projectID], member)
	return member, nil
}

func (s *MemoryStore) RemoveProjectMember(_ context.Context, organizationID, projectID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok || project.OrganizationID != organizationID {
		return apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	members := s.members[projectID]
	for index, member := range members {
		if member.ID != userID {
			continue
		}
		s.members[projectID] = append(members[:index], members[index+1:]...)
		return nil
	}
	return apperr.New(apperr.CodeNotFound, 404, "project member override not found", nil)
}

func (s *MemoryStore) ListOrganizationMembers(_ context.Context, organizationID string) ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Member(nil), s.organizationMembers[organizationID]...), nil
}

func (s *MemoryStore) AddOrganizationMember(_ context.Context, organizationID, login, roleKey string) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateRoleKey(roleKey); err != nil {
		return Member{}, err
	}
	for index, member := range s.organizationMembers[organizationID] {
		if strings.EqualFold(member.Login, login) {
			if member.RoleKey == "owner" && roleKey != "owner" && countOwners(s.organizationMembers[organizationID]) <= 1 {
				return Member{}, apperr.New(apperr.CodeConflict, 409, "the organization must retain at least one owner", nil)
			}
			member.RoleKey = roleKey
			s.organizationMembers[organizationID][index] = member
			return member, nil
		}
	}
	member := Member{ID: login, Login: login, DisplayName: login, RoleKey: roleKey}
	s.organizationMembers[organizationID] = append(s.organizationMembers[organizationID], member)
	return member, nil
}

func (s *MemoryStore) SetOrganizationMember(_ context.Context, organizationID, userID, roleKey string) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateRoleKey(roleKey); err != nil {
		return Member{}, err
	}
	for index, member := range s.organizationMembers[organizationID] {
		if member.ID != userID {
			continue
		}
		if member.RoleKey == "owner" && roleKey != "owner" && countOwners(s.organizationMembers[organizationID]) <= 1 {
			return Member{}, apperr.New(apperr.CodeConflict, 409, "the organization must retain at least one owner", nil)
		}
		member.RoleKey = roleKey
		s.organizationMembers[organizationID][index] = member
		return member, nil
	}
	return Member{}, apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
}

func (s *MemoryStore) RemoveOrganizationMember(_ context.Context, organizationID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	members := s.organizationMembers[organizationID]
	for index, member := range members {
		if member.ID != userID {
			continue
		}
		if member.RoleKey == "owner" && countOwners(members) <= 1 {
			return apperr.New(apperr.CodeConflict, 409, "the organization must retain at least one owner", nil)
		}
		s.organizationMembers[organizationID] = append(members[:index], members[index+1:]...)
		for projectID, projectMembers := range s.members {
			filtered := projectMembers[:0]
			for _, projectMember := range projectMembers {
				if projectMember.ID != userID {
					filtered = append(filtered, projectMember)
				}
			}
			s.members[projectID] = filtered
		}
		return nil
	}
	return apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
}

func countOwners(members []Member) int {
	count := 0
	for _, member := range members {
		if member.RoleKey == "owner" {
			count++
		}
	}
	return count
}

var _ Store = (*PostgresStore)(nil)
var _ Store = (*MemoryStore)(nil)
