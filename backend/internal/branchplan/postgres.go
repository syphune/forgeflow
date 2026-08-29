package branchplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) GetPolicy(ctx context.Context, organizationID, projectID string) (Policy, error) {
	var policy Policy
	var raw []byte
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT id::text, organization_id::text, project_id::text, development_branch, production_branch, release_prefix, work_branch_template, rules, version, created_at, updated_at
FROM project_branch_policies
WHERE organization_id=$1 AND project_id=$2`, organizationID, projectID).Scan(&policy.ID, &policy.OrganizationID, &policy.ProjectID, &policy.DevelopmentBranch, &policy.ProductionBranch, &policy.ReleasePrefix, &policy.WorkBranchTemplate, &raw, &policy.Version, &policy.CreatedAt, &policy.UpdatedAt)
	if err == pgx.ErrNoRows {
		return DefaultPolicy(organizationID, projectID), nil
	}
	if err != nil {
		return Policy{}, fmt.Errorf("load branch policy: %w", err)
	}
	if err := json.Unmarshal(raw, &policy.Rules); err != nil {
		return Policy{}, fmt.Errorf("decode branch policy rules: %w", err)
	}
	return policy, nil
}

func (s *PostgresStore) SavePolicy(ctx context.Context, policy Policy, expectedVersion int64) (Policy, error) {
	rules, err := json.Marshal(policy.Rules)
	if err != nil {
		return Policy{}, fmt.Errorf("encode branch policy rules: %w", err)
	}
	id := strings.TrimSpace(policy.ID)
	if id == "" {
		id, err = ids.New()
		if err != nil {
			return Policy{}, err
		}
	}
	var result Policy
	var raw []byte
	if expectedVersion == 0 {
		err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO project_branch_policies (id, organization_id, project_id, development_branch, production_branch, release_prefix, work_branch_template, rules, version)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1)
ON CONFLICT (organization_id, project_id) DO NOTHING
RETURNING id::text, organization_id::text, project_id::text, development_branch, production_branch, release_prefix, work_branch_template, rules, version, created_at, updated_at`, id, policy.OrganizationID, policy.ProjectID, policy.DevelopmentBranch, policy.ProductionBranch, policy.ReleasePrefix, policy.WorkBranchTemplate, rules).Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.DevelopmentBranch, &result.ProductionBranch, &result.ReleasePrefix, &result.WorkBranchTemplate, &raw, &result.Version, &result.CreatedAt, &result.UpdatedAt)
		if err == pgx.ErrNoRows {
			current, loadErr := s.GetPolicy(ctx, policy.OrganizationID, policy.ProjectID)
			if loadErr != nil {
				return Policy{}, loadErr
			}
			return Policy{}, stalePolicy(0, current.Version)
		}
	} else {
		err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
UPDATE project_branch_policies
SET development_branch=$4, production_branch=$5, release_prefix=$6, work_branch_template=$7, rules=$8, version=version+1, updated_at=now()
WHERE organization_id=$1 AND project_id=$2 AND version=$3
RETURNING id::text, organization_id::text, project_id::text, development_branch, production_branch, release_prefix, work_branch_template, rules, version, created_at, updated_at`, policy.OrganizationID, policy.ProjectID, expectedVersion, policy.DevelopmentBranch, policy.ProductionBranch, policy.ReleasePrefix, policy.WorkBranchTemplate, rules).Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.DevelopmentBranch, &result.ProductionBranch, &result.ReleasePrefix, &result.WorkBranchTemplate, &raw, &result.Version, &result.CreatedAt, &result.UpdatedAt)
		if err == pgx.ErrNoRows {
			current, loadErr := s.GetPolicy(ctx, policy.OrganizationID, policy.ProjectID)
			if loadErr != nil {
				return Policy{}, loadErr
			}
			return Policy{}, stalePolicy(expectedVersion, current.Version)
		}
	}
	if err != nil {
		return Policy{}, mapStoreError(err, "branch policy could not be saved")
	}
	if err := json.Unmarshal(raw, &result.Rules); err != nil {
		return Policy{}, fmt.Errorf("decode saved branch policy rules: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) GetPlan(ctx context.Context, organizationID, projectID, workItemID string) (Plan, error) {
	var plan Plan
	var raw []byte
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, planSelect+` WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3`, organizationID, projectID, workItemID).Scan(planArgs(&plan, &raw)...)
	if err == pgx.ErrNoRows {
		return Plan{}, apperr.New(apperr.CodeNotFound, 404, "branch plan not found", nil)
	}
	if err != nil {
		return Plan{}, fmt.Errorf("load branch plan: %w", err)
	}
	if err := json.Unmarshal(raw, &plan.SyncBranches); err != nil {
		return Plan{}, fmt.Errorf("decode branch plan sync branches: %w", err)
	}
	return plan, nil
}

func (s *PostgresStore) CreatePlan(ctx context.Context, plan Plan) (Plan, error) {
	raw, err := json.Marshal(plan.SyncBranches)
	if err != nil {
		return Plan{}, fmt.Errorf("encode branch plan sync branches: %w", err)
	}
	id := strings.TrimSpace(plan.ID)
	if id == "" {
		id, err = ids.New()
		if err != nil {
			return Plan{}, err
		}
	}
	var result Plan
	var savedRaw []byte
	err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO work_item_branch_plans (id, organization_id, project_id, work_item_id, repository_id, work_item_key, branch_type, release_branch, base_branch, target_branch, working_branch, sync_branches, create_pull_request, require_sync, base_sha, policy_version, work_item_version, state, error_message, created_by, version)
VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,NULLIF($19,''),$20,1)
	RETURNING `+planColumns, id, plan.OrganizationID, plan.ProjectID, plan.WorkItemID, plan.RepositoryID, plan.WorkItemKey, plan.BranchType, plan.ReleaseBranch, plan.BaseBranch, plan.TargetBranch, plan.WorkingBranch, raw, plan.CreatePullRequest, plan.RequireSync, plan.BaseSHA, plan.PolicyVersion, plan.WorkItemVersion, plan.State, plan.Error, plan.CreatedBy).Scan(planArgs(&result, &savedRaw)...)
	if err != nil {
		return Plan{}, mapStoreError(err, "branch plan already exists")
	}
	if err := json.Unmarshal(savedRaw, &result.SyncBranches); err != nil {
		return Plan{}, fmt.Errorf("decode saved branch plan sync branches: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) UpdatePlan(ctx context.Context, organizationID, projectID, id string, expectedVersion int64, state PlanState, message string) (Plan, error) {
	var result Plan
	var raw []byte
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
UPDATE work_item_branch_plans SET state=$5, error_message=NULLIF($6,''), version=version+1, updated_at=now()
WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND version=$4
	RETURNING `+planColumns, organizationID, projectID, id, expectedVersion, state, message).Scan(planArgs(&result, &raw)...)
	if err == pgx.ErrNoRows {
		return Plan{}, apperr.New(apperr.CodeConflict, 409, "branch plan version is stale or not found", map[string]any{"expected_version": expectedVersion})
	}
	if err != nil {
		return Plan{}, fmt.Errorf("update branch plan: %w", err)
	}
	if err := json.Unmarshal(raw, &result.SyncBranches); err != nil {
		return Plan{}, fmt.Errorf("decode updated branch plan sync branches: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) GetOperation(ctx context.Context, organizationID, projectID, idempotencyKey string) (Operation, error) {
	var operation Operation
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, operationSelect+` WHERE organization_id=$1 AND project_id=$2 AND idempotency_key=$3`, organizationID, projectID, idempotencyKey).Scan(operationArgs(&operation)...)
	if err == pgx.ErrNoRows {
		return Operation{}, apperr.New(apperr.CodeNotFound, 404, "branch operation not found", nil)
	}
	if err != nil {
		return Operation{}, fmt.Errorf("load branch operation: %w", err)
	}
	return operation, nil
}

func (s *PostgresStore) CreateOperation(ctx context.Context, operation Operation) (Operation, error) {
	id := strings.TrimSpace(operation.ID)
	if id == "" {
		var err error
		id, err = ids.New()
		if err != nil {
			return Operation{}, err
		}
	}
	var result Operation
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO branch_operations (id, plan_id, organization_id, project_id, repository_id, kind, state, idempotency_key, source_branch, target_branch, pull_request_number, pull_request_url, error_message, version)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,0),NULLIF($12,''),NULLIF($13,''),1)
	RETURNING `+operationColumns, id, operation.PlanID, operation.OrganizationID, operation.ProjectID, operation.RepositoryID, operation.Kind, operation.State, operation.IdempotencyKey, operation.SourceBranch, operation.TargetBranch, operation.PullRequestNumber, operation.PullRequestURL, operation.Error).Scan(operationArgs(&result)...)
	if err != nil {
		return Operation{}, mapStoreError(err, "branch operation already exists")
	}
	return result, nil
}

func (s *PostgresStore) UpdateOperation(ctx context.Context, organizationID, projectID, id string, expectedVersion int64, state OperationState, message string, number int64, url string) (Operation, error) {
	var result Operation
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
UPDATE branch_operations SET state=$5, error_message=NULLIF($6,''), pull_request_number=COALESCE(NULLIF($7,0),pull_request_number), pull_request_url=COALESCE(NULLIF($8,''),pull_request_url), version=version+1, updated_at=now()
WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND version=$4
	RETURNING `+operationColumns, organizationID, projectID, id, expectedVersion, state, message, number, url).Scan(operationArgs(&result)...)
	if err == pgx.ErrNoRows {
		return Operation{}, apperr.New(apperr.CodeConflict, 409, "branch operation version is stale or not found", nil)
	}
	if err != nil {
		return Operation{}, fmt.Errorf("update branch operation: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListOperations(ctx context.Context, organizationID, projectID, planID string) ([]Operation, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, operationSelect+` WHERE organization_id=$1 AND project_id=$2 AND plan_id=$3 ORDER BY created_at, id`, organizationID, projectID, planID)
	if err != nil {
		return nil, fmt.Errorf("list branch operations: %w", err)
	}
	defer rows.Close()
	result := make([]Operation, 0)
	for rows.Next() {
		var item Operation
		if err := rows.Scan(operationArgs(&item)...); err != nil {
			return nil, fmt.Errorf("scan branch operation: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) MarkPullRequestMerged(ctx context.Context, organizationID, repositoryID string, number int64) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE branch_operations SET state='MERGED', version=version+1, updated_at=now() WHERE organization_id=$1 AND repository_id=$2 AND pull_request_number=$3 AND kind=$4 AND state<>'MERGED'`, organizationID, repositoryID, number, OperationSyncPull)
	if err != nil {
		return fmt.Errorf("mark branch sync merged: %w", err)
	}
	return nil
}

const planColumns = `id::text, organization_id::text, project_id::text, work_item_id::text, repository_id::text, work_item_key, branch_type, COALESCE(release_branch,''), base_branch, target_branch, working_branch, sync_branches, create_pull_request, require_sync, base_sha, policy_version, work_item_version, state, COALESCE(error_message,''), created_by::text, version, created_at, updated_at`
const planSelect = `SELECT ` + planColumns + ` FROM work_item_branch_plans`
const operationColumns = `id::text, plan_id::text, organization_id::text, project_id::text, repository_id::text, kind, state, idempotency_key, COALESCE(source_branch,''), COALESCE(target_branch,''), COALESCE(pull_request_number,0), COALESCE(pull_request_url,''), COALESCE(error_message,''), version, created_at, updated_at`
const operationSelect = `SELECT ` + operationColumns + ` FROM branch_operations`

func planArgs(plan *Plan, raw *[]byte) []any {
	return []any{&plan.ID, &plan.OrganizationID, &plan.ProjectID, &plan.WorkItemID, &plan.RepositoryID, &plan.WorkItemKey, &plan.BranchType, &plan.ReleaseBranch, &plan.BaseBranch, &plan.TargetBranch, &plan.WorkingBranch, raw, &plan.CreatePullRequest, &plan.RequireSync, &plan.BaseSHA, &plan.PolicyVersion, &plan.WorkItemVersion, &plan.State, &plan.Error, &plan.CreatedBy, &plan.Version, &plan.CreatedAt, &plan.UpdatedAt}
}

func operationArgs(operation *Operation) []any {
	return []any{&operation.ID, &operation.PlanID, &operation.OrganizationID, &operation.ProjectID, &operation.RepositoryID, &operation.Kind, &operation.State, &operation.IdempotencyKey, &operation.SourceBranch, &operation.TargetBranch, &operation.PullRequestNumber, &operation.PullRequestURL, &operation.Error, &operation.Version, &operation.CreatedAt, &operation.UpdatedAt}
}

func mapStoreError(err error, conflictMessage string) error {
	var pgErr *pgconn.PgError
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || (errors.As(err, &pgErr) && pgErr.Code == "23505")) {
		return apperr.New(apperr.CodeConflict, 409, conflictMessage, nil)
	}
	return err
}

var _ Store = (*PostgresStore)(nil)
