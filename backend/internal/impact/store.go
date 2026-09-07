package impact

import (
	"context"
	"encoding/json"
	"fmt"
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

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

type rowScanner interface{ Scan(...any) error }

const scopeColumns = `id::text, organization_id::text, project_id::text, repository_id::text,
path_prefixes, modules, impact_summary, provenance, status, version, COALESCE(created_by,''), COALESCE(verified_by,''), created_at, updated_at`

func scanScope(row rowScanner) (RepositoryProjectScope, error) {
	var item RepositoryProjectScope
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.RepositoryID, &item.PathPrefixes, &item.Modules, &item.ImpactSummary, &item.Provenance, &item.Status, &item.Version, &item.CreatedBy, &item.VerifiedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return RepositoryProjectScope{}, err
	}
	return item, nil
}

func (s *PostgresStore) SaveScope(ctx context.Context, scope RepositoryProjectScope) (RepositoryProjectScope, error) {
	if strings.TrimSpace(scope.ID) == "" {
		id, err := ids.New()
		if err != nil {
			return RepositoryProjectScope{}, err
		}
		scope.ID = id
	}
	pathPrefixes := scope.PathPrefixes
	if pathPrefixes == nil {
		pathPrefixes = []string{}
	}
	modules := scope.Modules
	if modules == nil {
		modules = []string{}
	}
	row := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO repository_project_scopes
 (id, organization_id, project_id, repository_id, path_prefixes, modules, impact_summary, provenance, status, version, created_by, verified_by)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,1,$10,$11)
ON CONFLICT (id) DO UPDATE SET
 path_prefixes=EXCLUDED.path_prefixes, modules=EXCLUDED.modules, impact_summary=EXCLUDED.impact_summary,
 provenance=EXCLUDED.provenance, status=EXCLUDED.status, version=repository_project_scopes.version+1,
	verified_by=EXCLUDED.verified_by, updated_at=now()
WHERE repository_project_scopes.organization_id=EXCLUDED.organization_id
  AND repository_project_scopes.project_id=EXCLUDED.project_id
  AND repository_project_scopes.repository_id=EXCLUDED.repository_id
RETURNING `+scopeColumns,
		scope.ID, scope.OrganizationID, scope.ProjectID, scope.RepositoryID, pathPrefixes, modules, scope.ImpactSummary, scope.Provenance, scope.Status, scope.CreatedBy, scope.VerifiedBy)
	result, err := scanScope(row)
	if err == pgx.ErrNoRows {
		return RepositoryProjectScope{}, apperr.New(apperr.CodeForbidden, 403, "repository project scope belongs to another tenant or project", nil)
	}
	if err != nil {
		return RepositoryProjectScope{}, fmt.Errorf("save repository project scope: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) GetScope(ctx context.Context, organizationID, scopeID string) (RepositoryProjectScope, error) {
	result, err := scanScope(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT `+scopeColumns+` FROM repository_project_scopes WHERE organization_id=$1::uuid AND id=$2::uuid`, organizationID, scopeID))
	if err == pgx.ErrNoRows {
		return RepositoryProjectScope{}, apperr.New(apperr.CodeNotFound, 404, "repository project scope not found", nil)
	}
	if err != nil {
		return RepositoryProjectScope{}, fmt.Errorf("get repository project scope: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListScopes(ctx context.Context, organizationID, projectID, repositoryID string) ([]RepositoryProjectScope, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT `+scopeColumns+` FROM repository_project_scopes WHERE organization_id=$1::uuid AND project_id=$2::uuid AND repository_id=$3::uuid ORDER BY updated_at DESC, id`, organizationID, projectID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository project scopes: %w", err)
	}
	defer rows.Close()
	result := make([]RepositoryProjectScope, 0)
	for rows.Next() {
		item, scanErr := scanScope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan repository project scope: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const reportColumns = `id::text, organization_id::text, agent_run_id::text, project_id::text, repository_id::text,
report_version, input_hash, COALESCE(base_sha,''), COALESCE(diff_hash,''), changed_paths, affected_projects,
unknown_impact, truncated, status, created_at`

func scanReport(row rowScanner) (Report, error) {
	var item Report
	var affected []byte
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.AgentRunID, &item.ProjectID, &item.RepositoryID, &item.ReportVersion, &item.InputHash, &item.BaseSHA, &item.DiffHash, &item.ChangedPaths, &affected, &item.UnknownImpact, &item.Truncated, &item.Status, &item.CreatedAt); err != nil {
		return Report{}, err
	}
	if len(affected) > 0 {
		if err := json.Unmarshal(affected, &item.AffectedProjects); err != nil {
			return Report{}, fmt.Errorf("decode impact report: %w", err)
		}
	}
	return item, nil
}

func (s *PostgresStore) CreateReport(ctx context.Context, report Report) (Report, error) {
	if strings.TrimSpace(report.ID) == "" {
		id, err := ids.New()
		if err != nil {
			return Report{}, err
		}
		report.ID = id
	}
	affected, err := json.Marshal(report.AffectedProjects)
	if err != nil {
		return Report{}, fmt.Errorf("encode impact report: %w", err)
	}
	changedPaths := report.ChangedPaths
	if changedPaths == nil {
		changedPaths = []string{}
	}
	executor := platformdb.ExecutorFrom(ctx, s.pool)
	// Serialize versions per AgentRun; reports are immutable evidence and a
	// changed input always creates a new version.
	if _, err := executor.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, report.AgentRunID); err != nil {
		return Report{}, fmt.Errorf("lock impact report version: %w", err)
	}
	if err := executor.QueryRow(ctx, `SELECT COALESCE(MAX(report_version),0)+1 FROM agent_run_impact_reports WHERE organization_id=$1::uuid AND agent_run_id=$2::uuid`, report.OrganizationID, report.AgentRunID).Scan(&report.ReportVersion); err != nil {
		return Report{}, fmt.Errorf("next impact report version: %w", err)
	}
	row := executor.QueryRow(ctx, `
INSERT INTO agent_run_impact_reports
 (id, organization_id, agent_run_id, project_id, repository_id, report_version, input_hash, base_sha, diff_hash, changed_paths, affected_projects, unknown_impact, truncated, status)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING `+reportColumns,
		report.ID, report.OrganizationID, report.AgentRunID, report.ProjectID, report.RepositoryID, report.ReportVersion, report.InputHash, report.BaseSHA, report.DiffHash, changedPaths, affected, report.UnknownImpact, report.Truncated, report.Status)
	result, err := scanReport(row)
	if err != nil {
		return Report{}, fmt.Errorf("create impact report: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) LatestReport(ctx context.Context, organizationID, agentRunID string) (Report, error) {
	result, err := scanReport(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT `+reportColumns+` FROM agent_run_impact_reports WHERE organization_id=$1::uuid AND agent_run_id=$2::uuid ORDER BY report_version DESC LIMIT 1`, organizationID, agentRunID))
	if err == pgx.ErrNoRows {
		return Report{}, apperr.New(apperr.CodeNotFound, 404, "impact report not found", nil)
	}
	if err != nil {
		return Report{}, fmt.Errorf("get impact report: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListApprovals(ctx context.Context, organizationID, reportID string) ([]Approval, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT organization_id::text, report_id::text, affected_project_id::text, decision, approver_id, note, created_at FROM agent_run_impact_approvals WHERE organization_id=$1::uuid AND report_id=$2::uuid ORDER BY created_at, affected_project_id`, organizationID, reportID)
	if err != nil {
		return nil, fmt.Errorf("list impact approvals: %w", err)
	}
	defer rows.Close()
	result := make([]Approval, 0)
	for rows.Next() {
		var item Approval
		if err := rows.Scan(&item.OrganizationID, &item.ReportID, &item.AffectedProjectID, &item.Decision, &item.ApproverID, &item.Note, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan impact approval: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) SaveApproval(ctx context.Context, approval Approval) error {
	executor := platformdb.ExecutorFrom(ctx, s.pool)
	if _, err := executor.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, approval.ReportID); err != nil {
		return fmt.Errorf("lock impact approval: %w", err)
	}
	_, err := executor.Exec(ctx, `
INSERT INTO agent_run_impact_approvals (organization_id, report_id, affected_project_id, decision, approver_id, note)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6)
ON CONFLICT (report_id, affected_project_id) DO UPDATE SET decision=EXCLUDED.decision, approver_id=EXCLUDED.approver_id, note=EXCLUDED.note, created_at=now()`,
		approval.OrganizationID, approval.ReportID, approval.AffectedProjectID, approval.Decision, approval.ApproverID, approval.Note)
	if err != nil {
		return fmt.Errorf("save impact approval: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetReportStatus(ctx context.Context, organizationID, reportID, status string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE agent_run_impact_reports SET status=$3 WHERE organization_id=$1::uuid AND id=$2::uuid`, organizationID, reportID, status)
	if err != nil {
		return fmt.Errorf("update impact report status: %w", err)
	}
	return nil
}

type MemoryStore struct {
	mu        sync.Mutex
	scopes    map[string]RepositoryProjectScope
	reports   map[string][]Report
	approvals map[string]map[string]Approval
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{scopes: make(map[string]RepositoryProjectScope), reports: make(map[string][]Report), approvals: make(map[string]map[string]Approval)}
}

func (s *MemoryStore) SaveScope(_ context.Context, scope RepositoryProjectScope) (RepositoryProjectScope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if scope.ID == "" {
		id, err := ids.New()
		if err != nil {
			return RepositoryProjectScope{}, err
		}
		scope.ID = id
		scope.Version = 1
		scope.CreatedAt = now
	} else if previous, ok := s.scopes[scope.ID]; ok {
		if previous.OrganizationID != scope.OrganizationID || previous.ProjectID != scope.ProjectID || previous.RepositoryID != scope.RepositoryID {
			return RepositoryProjectScope{}, apperr.New(apperr.CodeForbidden, 403, "repository project scope belongs to another tenant or project", nil)
		}
		scope.OrganizationID = previous.OrganizationID
		scope.ProjectID = previous.ProjectID
		scope.RepositoryID = previous.RepositoryID
		scope.Version = previous.Version + 1
		scope.CreatedAt = previous.CreatedAt
		scope.CreatedBy = previous.CreatedBy
	}
	scope.UpdatedAt = now
	scope.PathPrefixes = append([]string(nil), scope.PathPrefixes...)
	scope.Modules = append([]string(nil), scope.Modules...)
	s.scopes[scope.ID] = scope
	return scope, nil
}

func (s *MemoryStore) GetScope(_ context.Context, organizationID, scopeID string) (RepositoryProjectScope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, ok := s.scopes[scopeID]
	if !ok || scope.OrganizationID != organizationID {
		return RepositoryProjectScope{}, apperr.New(apperr.CodeNotFound, 404, "repository project scope not found", nil)
	}
	return scope, nil
}

func (s *MemoryStore) ListScopes(_ context.Context, organizationID, projectID, repositoryID string) ([]RepositoryProjectScope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]RepositoryProjectScope, 0)
	for _, scope := range s.scopes {
		if scope.OrganizationID == organizationID && scope.ProjectID == projectID && scope.RepositoryID == repositoryID {
			result = append(result, scope)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

func (s *MemoryStore) CreateReport(_ context.Context, report Report) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if report.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Report{}, err
		}
		report.ID = id
	}
	report.ReportVersion = int64(len(s.reports[report.AgentRunID]) + 1)
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	report.AffectedProjects = append([]AffectedProject(nil), report.AffectedProjects...)
	s.reports[report.AgentRunID] = append(s.reports[report.AgentRunID], report)
	return report, nil
}

func (s *MemoryStore) LatestReport(_ context.Context, organizationID, agentRunID string) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.reports[agentRunID]
	if len(items) == 0 || items[len(items)-1].OrganizationID != organizationID {
		return Report{}, apperr.New(apperr.CodeNotFound, 404, "impact report not found", nil)
	}
	return items[len(items)-1], nil
}

func (s *MemoryStore) ListApprovals(_ context.Context, organizationID, reportID string) ([]Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.approvals[reportID]
	result := make([]Approval, 0, len(items))
	for _, item := range items {
		if item.OrganizationID == organizationID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryStore) SaveApproval(_ context.Context, approval Approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approvals[approval.ReportID] == nil {
		s.approvals[approval.ReportID] = make(map[string]Approval)
	}
	approval.CreatedAt = time.Now().UTC()
	s.approvals[approval.ReportID][approval.AffectedProjectID] = approval
	return nil
}

func (s *MemoryStore) SetReportStatus(_ context.Context, organizationID, reportID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for runID, items := range s.reports {
		for index := range items {
			if items[index].OrganizationID == organizationID && items[index].ID == reportID {
				items[index].Status = status
				s.reports[runID] = items
				return nil
			}
		}
	}
	return apperr.New(apperr.CodeNotFound, 404, "impact report not found", nil)
}

var _ Store = (*PostgresStore)(nil)
var _ Store = (*MemoryStore)(nil)
