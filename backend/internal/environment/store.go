package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetPolicy(context.Context, string, string) (autonomous.Policy, error)
	SetPolicy(context.Context, string, string, autonomous.Policy) (autonomous.Policy, error)
	GetDeliveryPolicy(context.Context, string, string) (DeliveryPolicy, error)
	SetDeliveryPolicy(context.Context, string, DeliveryPolicy, int64) (DeliveryPolicy, error)
	List(context.Context, string, string) ([]Environment, error)
	Create(context.Context, string, CreateInput) (Environment, error)
	CreateDeployment(context.Context, string, DeploymentInput, DeploymentStatus) (DeploymentRequest, error)
	GetDeployment(context.Context, string, string, string) (DeploymentRequest, error)
	ApproveDeployment(context.Context, string, string, string, string) (DeploymentRequest, error)
	UpdateDeployment(context.Context, string, string, string, StatusInput) (DeploymentRequest, error)
	VerifyDeployment(context.Context, string, string, string, string) (DeploymentRequest, error)
	HasDeploymentStatus(context.Context, string, string, string, DeploymentStatus, string) (bool, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) GetPolicy(ctx context.Context, organizationID, projectID string) (autonomous.Policy, error) {
	var raw []byte
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT policy FROM project_ai_policies WHERE organization_id=$1 AND project_id=$2`, organizationID, projectID).Scan(&raw)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return autonomous.DefaultPolicy(), nil
		}
		return autonomous.Policy{}, fmt.Errorf("get project AI policy: %w", err)
	}
	var policy autonomous.Policy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return autonomous.Policy{}, fmt.Errorf("decode project AI policy: %w", err)
	}
	return policy.Normalize(), nil
}

func (s *PostgresStore) SetPolicy(ctx context.Context, organizationID, projectID string, policy autonomous.Policy) (autonomous.Policy, error) {
	policy = policy.Normalize()
	raw, err := json.Marshal(policy)
	if err != nil {
		return autonomous.Policy{}, err
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO project_ai_policies (organization_id,project_id,policy) VALUES ($1,$2,$3) ON CONFLICT (organization_id,project_id) DO UPDATE SET policy=EXCLUDED.policy, updated_at=now()`, organizationID, projectID, raw)
	if err != nil {
		return autonomous.Policy{}, fmt.Errorf("set project AI policy: %w", err)
	}
	return policy, nil
}

func (s *PostgresStore) GetDeliveryPolicy(ctx context.Context, organizationID, projectID string) (DeliveryPolicy, error) {
	var storedID string
	var storedVersion int64
	var updatedAt time.Time
	var raw []byte
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text, version, policy, updated_at FROM project_delivery_policies WHERE organization_id=$1 AND project_id=$2`, organizationID, projectID).Scan(&storedID, &storedVersion, &raw, &updatedAt)
	if err == pgx.ErrNoRows {
		result := DefaultDeliveryPolicy()
		result.OrganizationID, result.ProjectID = organizationID, projectID
		return result, nil
	}
	if err != nil {
		return DeliveryPolicy{}, fmt.Errorf("get project delivery policy: %w", err)
	}
	result := DeliveryPolicy{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return DeliveryPolicy{}, fmt.Errorf("decode project delivery policy: %w", err)
	}
	result.ID, result.OrganizationID, result.ProjectID = storedID, organizationID, projectID
	result.Version, result.UpdatedAt = maxInt64(storedVersion, 1), updatedAt
	return result.Normalize(), nil
}

func (s *PostgresStore) SetDeliveryPolicy(ctx context.Context, organizationID string, policy DeliveryPolicy, expectedVersion int64) (DeliveryPolicy, error) {
	policy = policy.Normalize()
	raw, err := json.Marshal(policy)
	if err != nil {
		return DeliveryPolicy{}, err
	}
	id, err := ids.New()
	if err != nil {
		return DeliveryPolicy{}, err
	}
	var storedID string
	var storedVersion int64
	var updatedAt time.Time
	var stored []byte
	err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO project_delivery_policies (id, organization_id, project_id, policy, version)
VALUES ($1,$2,$3,$4,1)
ON CONFLICT (organization_id, project_id) DO UPDATE
SET policy=EXCLUDED.policy, version=project_delivery_policies.version+1, updated_at=now()
WHERE project_delivery_policies.version=$5
	RETURNING id::text, version, policy, updated_at
	`, id, organizationID, policy.ProjectID, raw, expectedVersion).Scan(&storedID, &storedVersion, &stored, &updatedAt)
	if err == pgx.ErrNoRows {
		return DeliveryPolicy{}, apperr.New(apperr.CodeConflict, 409, "delivery policy version is stale or project does not exist", nil)
	}
	if err != nil {
		return DeliveryPolicy{}, fmt.Errorf("set project delivery policy: %w", err)
	}
	result := DeliveryPolicy{}
	if err := json.Unmarshal(stored, &result); err != nil {
		return DeliveryPolicy{}, fmt.Errorf("decode saved project delivery policy: %w", err)
	}
	result.ID, result.OrganizationID, result.ProjectID = storedID, organizationID, policy.ProjectID
	result.Version, result.UpdatedAt = storedVersion, updatedAt
	return result.Normalize(), nil
}

func (s *PostgresStore) List(ctx context.Context, organizationID, projectID string) ([]Environment, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, organization_id::text, project_id::text, key, name, kind, COALESCE(repository_id::text,''), COALESCE(workflow_ref,''), COALESCE(dispatch_url,''), COALESCE(health_check_url,''), auto_deploy, require_approval, secret_refs, metadata, created_at, updated_at FROM project_environments WHERE organization_id=$1 AND project_id=$2 ORDER BY key`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	defer rows.Close()
	result := make([]Environment, 0)
	for rows.Next() {
		item, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanEnvironment(row interface{ Scan(...any) error }) (Environment, error) {
	var item Environment
	var metadata []byte
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.Key, &item.Name, &item.Kind, &item.RepositoryID, &item.WorkflowRef, &item.DispatchURL, &item.HealthCheckURL, &item.AutoDeploy, &item.RequireApproval, &item.SecretRefs, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Environment{}, fmt.Errorf("scan environment: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, nil
}

func (s *PostgresStore) Create(ctx context.Context, organizationID string, input CreateInput) (Environment, error) {
	id, err := ids.New()
	if err != nil {
		return Environment{}, err
	}
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return Environment{}, err
	}
	row := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO project_environments (id,organization_id,project_id,key,name,kind,repository_id,workflow_ref,dispatch_url,health_check_url,auto_deploy,require_approval,secret_refs,metadata) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$10,$11,$12,$13,$14) RETURNING id::text, organization_id::text, project_id::text, key, name, kind, COALESCE(repository_id::text,''), COALESCE(workflow_ref,''), COALESCE(dispatch_url,''), COALESCE(health_check_url,''), auto_deploy, require_approval, secret_refs, metadata, created_at, updated_at`, id, organizationID, input.ProjectID, input.Key, input.Name, input.Kind, input.RepositoryID, input.WorkflowRef, input.DispatchURL, input.HealthCheckURL, input.AutoDeploy, input.RequireApproval, input.SecretRefs, metadata)
	return scanEnvironment(row)
}

const deploymentColumns = `id::text, organization_id::text, project_id::text, environment_id::text, COALESCE(work_item_id::text,''), COALESCE(autonomous_run_id::text,''), commit_sha, status, COALESCE(external_id,''), COALESCE(url,''), COALESCE(approved_by,''), approved_at, COALESCE(last_error,''), version, created_at, updated_at`

func scanDeployment(row interface{ Scan(...any) error }) (DeploymentRequest, error) {
	var result DeploymentRequest
	if err := row.Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.EnvironmentID, &result.WorkItemID, &result.AutonomousRunID, &result.CommitSHA, &result.Status, &result.ExternalID, &result.URL, &result.ApprovedBy, &result.ApprovedAt, &result.LastError, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return DeploymentRequest{}, fmt.Errorf("scan deployment request: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) CreateDeployment(ctx context.Context, organizationID string, input DeploymentInput, status DeploymentStatus) (DeploymentRequest, error) {
	id, err := ids.New()
	if err != nil {
		return DeploymentRequest{}, err
	}
	return scanDeployment(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO deployment_requests (id,organization_id,project_id,environment_id,work_item_id,autonomous_run_id,commit_sha,status) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8) RETURNING `+deploymentColumns, id, organizationID, input.ProjectID, input.EnvironmentID, input.WorkItemID, input.AutonomousRunID, input.CommitSHA, status))
}

func (s *PostgresStore) GetDeployment(ctx context.Context, organizationID, projectID, id string) (DeploymentRequest, error) {
	return scanDeployment(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT `+deploymentColumns+` FROM deployment_requests WHERE organization_id=$1 AND project_id=$2 AND id=$3`, organizationID, projectID, id))
}

func (s *PostgresStore) ApproveDeployment(ctx context.Context, organizationID, projectID, id, actorID string) (DeploymentRequest, error) {
	return scanDeployment(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE deployment_requests SET status='DISPATCHED', approved_by=$4, approved_at=now(), version=version+1, updated_at=now() WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND status='PENDING_APPROVAL' RETURNING `+deploymentColumns, organizationID, projectID, id, actorID))
}

func (s *PostgresStore) UpdateDeployment(ctx context.Context, organizationID, projectID, id string, input StatusInput) (DeploymentRequest, error) {
	return scanDeployment(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
UPDATE deployment_requests
SET status=$1, external_id=CASE WHEN $2='' THEN external_id ELSE $2 END, url=CASE WHEN $3='' THEN url ELSE $3 END, last_error=$4, version=version+1, updated_at=now()
WHERE organization_id=$5 AND project_id=$6 AND id=$7
  AND (($1='RUNNING' AND status='DISPATCHED') OR ($1='SUCCEEDED' AND status IN ('DISPATCHED','RUNNING')) OR ($1 IN ('FAILED','CANCELLED') AND status IN ('DISPATCHED','RUNNING')))
RETURNING `+deploymentColumns, input.Status, input.ExternalID, input.URL, input.LastError, organizationID, projectID, id))
}

func (s *PostgresStore) VerifyDeployment(ctx context.Context, organizationID, projectID, id, actorID string) (DeploymentRequest, error) {
	return scanDeployment(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
UPDATE deployment_requests d
SET status='VERIFIED', approved_by=CASE WHEN approved_by='' THEN $4 ELSE approved_by END, version=version+1, updated_at=now()
FROM project_environments e
WHERE d.environment_id=e.id AND d.organization_id=$1 AND d.project_id=$2 AND d.id=$3
  AND d.status='SUCCEEDED' AND e.kind='production'
RETURNING `+deploymentColumns, organizationID, projectID, id, actorID))
}

func (s *PostgresStore) HasDeploymentStatus(ctx context.Context, organizationID, projectID, workItemID string, status DeploymentStatus, kind string) (bool, error) {
	var result bool
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM deployment_requests d
  JOIN project_environments e ON e.id=d.environment_id
  WHERE d.organization_id=$1 AND d.project_id=$2 AND d.work_item_id=$3 AND d.status=$4 AND ($5='' OR e.kind=$5)
)`, organizationID, projectID, workItemID, status, kind).Scan(&result)
	if err != nil {
		return false, fmt.Errorf("check deployment status: %w", err)
	}
	return result, nil
}

type MemoryStore struct {
	mu           sync.Mutex
	policies     map[string]autonomous.Policy
	delivery     map[string]DeliveryPolicy
	environments map[string][]Environment
	deployments  map[string]DeploymentRequest
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{policies: make(map[string]autonomous.Policy), delivery: make(map[string]DeliveryPolicy), environments: make(map[string][]Environment), deployments: make(map[string]DeploymentRequest)}
}

func projectKey(org, project string) string { return org + ":" + project }

func (s *MemoryStore) GetPolicy(_ context.Context, organizationID, projectID string) (autonomous.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy, ok := s.policies[projectKey(organizationID, projectID)]; ok {
		return policy, nil
	}
	return autonomous.DefaultPolicy(), nil
}

func (s *MemoryStore) SetPolicy(_ context.Context, organizationID, projectID string, policy autonomous.Policy) (autonomous.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy = policy.Normalize()
	s.policies[projectKey(organizationID, projectID)] = policy
	return policy, nil
}

func (s *MemoryStore) GetDeliveryPolicy(_ context.Context, organizationID, projectID string) (DeliveryPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy, ok := s.delivery[projectKey(organizationID, projectID)]; ok {
		return policy, nil
	}
	result := DefaultDeliveryPolicy()
	result.OrganizationID, result.ProjectID = organizationID, projectID
	return result, nil
}

func (s *MemoryStore) SetDeliveryPolicy(_ context.Context, organizationID string, policy DeliveryPolicy, expectedVersion int64) (DeliveryPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := projectKey(organizationID, policy.ProjectID)
	current, exists := s.delivery[key]
	if exists && current.Version != expectedVersion {
		return DeliveryPolicy{}, apperr.New(apperr.CodeConflict, 409, "delivery policy version is stale", nil)
	}
	policy.Version = 1
	if exists {
		policy.Version = current.Version + 1
	}
	policy.OrganizationID = organizationID
	s.delivery[key] = policy
	return policy, nil
}

func (s *MemoryStore) List(_ context.Context, organizationID, projectID string) ([]Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]Environment(nil), s.environments[projectKey(organizationID, projectID)]...)
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *MemoryStore) Create(_ context.Context, organizationID string, input CreateInput) (Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := ids.New()
	if err != nil {
		return Environment{}, err
	}
	now := time.Now().UTC()
	item := Environment{ID: id, OrganizationID: organizationID, ProjectID: input.ProjectID, Key: input.Key, Name: input.Name, Kind: input.Kind, RepositoryID: input.RepositoryID, WorkflowRef: input.WorkflowRef, DispatchURL: input.DispatchURL, HealthCheckURL: input.HealthCheckURL, AutoDeploy: input.AutoDeploy, RequireApproval: input.RequireApproval, SecretRefs: append([]string(nil), input.SecretRefs...), Metadata: input.Metadata, CreatedAt: now, UpdatedAt: now}
	s.environments[projectKey(organizationID, input.ProjectID)] = append(s.environments[projectKey(organizationID, input.ProjectID)], item)
	return item, nil
}

func (s *MemoryStore) CreateDeployment(_ context.Context, organizationID string, input DeploymentInput, status DeploymentStatus) (DeploymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := ids.New()
	if err != nil {
		return DeploymentRequest{}, err
	}
	now := time.Now().UTC()
	item := DeploymentRequest{ID: id, OrganizationID: organizationID, ProjectID: input.ProjectID, EnvironmentID: input.EnvironmentID, WorkItemID: input.WorkItemID, AutonomousRunID: input.AutonomousRunID, CommitSHA: input.CommitSHA, Status: status, Version: 1, CreatedAt: now, UpdatedAt: now}
	s.deployments[id] = item
	return item, nil
}

func (s *MemoryStore) GetDeployment(_ context.Context, organizationID, projectID, id string) (DeploymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.deployments[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return DeploymentRequest{}, apperr.New(apperr.CodeNotFound, 404, "deployment request not found", nil)
	}
	return item, nil
}

func (s *MemoryStore) ApproveDeployment(_ context.Context, organizationID, projectID, id, actorID string) (DeploymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.deployments[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return DeploymentRequest{}, apperr.New(apperr.CodeNotFound, 404, "deployment request not found", nil)
	}
	if item.Status != DeploymentPending {
		return DeploymentRequest{}, apperr.New(apperr.CodeConflict, 409, "deployment is not waiting for approval", nil)
	}
	now := time.Now().UTC()
	item.Status, item.ApprovedBy, item.ApprovedAt = DeploymentDispatch, actorID, &now
	item.Version++
	item.UpdatedAt = now
	s.deployments[id] = item
	return item, nil
}

func (s *MemoryStore) UpdateDeployment(_ context.Context, organizationID, projectID, id string, input StatusInput) (DeploymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.deployments[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return DeploymentRequest{}, apperr.New(apperr.CodeNotFound, 404, "deployment request not found", nil)
	}
	if !validDeploymentUpdate(item.Status, input.Status) {
		return DeploymentRequest{}, apperr.New(apperr.CodeConflict, 409, "deployment status transition is invalid", map[string]any{"current_status": item.Status, "requested_status": input.Status})
	}
	item.Status = input.Status
	if input.ExternalID != "" {
		item.ExternalID = input.ExternalID
	}
	if input.URL != "" {
		item.URL = input.URL
	}
	item.LastError = input.LastError
	item.Version++
	item.UpdatedAt = time.Now().UTC()
	s.deployments[id] = item
	return item, nil
}

func (s *MemoryStore) VerifyDeployment(_ context.Context, organizationID, projectID, id, actorID string) (DeploymentRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.deployments[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return DeploymentRequest{}, apperr.New(apperr.CodeNotFound, 404, "deployment request not found", nil)
	}
	if item.Status != DeploymentSuccess {
		return DeploymentRequest{}, apperr.New(apperr.CodeConflict, 409, "only a successful production deployment can be verified", nil)
	}
	for _, environment := range s.environments[projectKey(organizationID, projectID)] {
		if environment.ID != item.EnvironmentID || environment.Kind != "production" {
			continue
		}
		now := time.Now().UTC()
		item.Status, item.ApprovedBy, item.UpdatedAt = DeploymentVerified, actorID, now
		item.Version++
		s.deployments[id] = item
		return item, nil
	}
	return DeploymentRequest{}, apperr.New(apperr.CodeConflict, 409, "deployment is not linked to a production environment", nil)
}

func (s *MemoryStore) HasDeploymentStatus(_ context.Context, organizationID, projectID, workItemID string, status DeploymentStatus, kind string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.deployments {
		if item.OrganizationID != organizationID || item.ProjectID != projectID || item.WorkItemID != workItemID || item.Status != status {
			continue
		}
		if kind == "" {
			return true, nil
		}
		for _, environment := range s.environments[projectKey(organizationID, projectID)] {
			if environment.ID == item.EnvironmentID && environment.Kind == kind {
				return true, nil
			}
		}
	}
	return false, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var _ Store = (*MemoryStore)(nil)
var _ Store = (*PostgresStore)(nil)
