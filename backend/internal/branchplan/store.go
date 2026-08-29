package branchplan

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type MemoryStore struct {
	mu         sync.Mutex
	policies   map[string]Policy
	plans      map[string]Plan
	operations map[string]Operation
	now        func() time.Time
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryStore{policies: make(map[string]Policy), plans: make(map[string]Plan), operations: make(map[string]Operation), now: now}
}

func (s *MemoryStore) GetPolicy(_ context.Context, organizationID, projectID string) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[scopeKey(organizationID, projectID)]
	if !ok {
		return DefaultPolicy(organizationID, projectID), nil
	}
	return clonePolicy(policy), nil
}

func (s *MemoryStore) SavePolicy(_ context.Context, policy Policy, expectedVersion int64) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	key := scopeKey(policy.OrganizationID, policy.ProjectID)
	current, exists := s.policies[key]
	if !exists {
		if expectedVersion != 0 {
			return Policy{}, stalePolicy(expectedVersion, 0)
		}
		policy.ID, _ = ids.New()
		policy.Version = 1
		policy.CreatedAt = now
	} else {
		if expectedVersion != current.Version {
			return Policy{}, stalePolicy(expectedVersion, current.Version)
		}
		policy.ID = current.ID
		policy.Version = current.Version + 1
		policy.CreatedAt = current.CreatedAt
	}
	policy.UpdatedAt = now
	policy.Rules = append([]Rule(nil), policy.Rules...)
	s.policies[key] = clonePolicy(policy)
	return clonePolicy(policy), nil
}

func (s *MemoryStore) GetPlan(_ context.Context, organizationID, projectID, workItemID string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.plans[planKey(organizationID, projectID, workItemID)]
	if !ok {
		return Plan{}, apperr.New(apperr.CodeNotFound, 404, "branch plan not found", nil)
	}
	return clonePlan(plan), nil
}

func (s *MemoryStore) CreatePlan(_ context.Context, plan Plan) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := planKey(plan.OrganizationID, plan.ProjectID, plan.WorkItemID)
	if existing, ok := s.plans[key]; ok {
		return clonePlan(existing), apperr.New(apperr.CodeConflict, 409, "branch plan already exists", map[string]any{"plan_id": existing.ID})
	}
	if plan.ID == "" {
		var err error
		plan.ID, err = ids.New()
		if err != nil {
			return Plan{}, err
		}
	}
	if plan.Version < 1 {
		plan.Version = 1
	}
	if plan.State == "" {
		plan.State = PlanPending
	}
	plan.CreatedAt = s.now().UTC()
	plan.UpdatedAt = plan.CreatedAt
	s.plans[key] = clonePlan(plan)
	return clonePlan(plan), nil
}

func (s *MemoryStore) UpdatePlan(_ context.Context, organizationID, projectID, id string, expectedVersion int64, state PlanState, message string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, plan, ok := s.findPlan(organizationID, projectID, id)
	if !ok {
		return Plan{}, apperr.New(apperr.CodeNotFound, 404, "branch plan not found", nil)
	}
	if plan.Version != expectedVersion {
		return Plan{}, apperr.New(apperr.CodeConflict, 409, "branch plan version is stale", map[string]any{"expected_version": expectedVersion, "current_version": plan.Version})
	}
	plan.State = state
	plan.Error = message
	plan.Version++
	plan.UpdatedAt = s.now().UTC()
	s.plans[key] = clonePlan(plan)
	return clonePlan(plan), nil
}

func (s *MemoryStore) GetOperation(_ context.Context, organizationID, projectID, idempotencyKey string) (Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation, ok := s.operations[operationKey(organizationID, projectID, idempotencyKey)]
	if !ok {
		return Operation{}, apperr.New(apperr.CodeNotFound, 404, "branch operation not found", nil)
	}
	return operation, nil
}

func (s *MemoryStore) CreateOperation(_ context.Context, operation Operation) (Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := operationKey(operation.OrganizationID, operation.ProjectID, operation.IdempotencyKey)
	if existing, ok := s.operations[key]; ok {
		return existing, apperr.New(apperr.CodeConflict, 409, "branch operation already exists", map[string]any{"operation_id": existing.ID})
	}
	if operation.ID == "" {
		var err error
		operation.ID, err = ids.New()
		if err != nil {
			return Operation{}, err
		}
	}
	if operation.State == "" {
		operation.State = OperationPending
	}
	if operation.Version < 1 {
		operation.Version = 1
	}
	operation.CreatedAt = s.now().UTC()
	operation.UpdatedAt = operation.CreatedAt
	s.operations[key] = operation
	return operation, nil
}

func (s *MemoryStore) UpdateOperation(_ context.Context, organizationID, projectID, id string, expectedVersion int64, state OperationState, message string, pullRequestNumber int64, pullRequestURL string) (Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, operation := range s.operations {
		if operation.OrganizationID != organizationID || operation.ProjectID != projectID || operation.ID != id {
			continue
		}
		if expectedVersion > 0 && operation.Version != expectedVersion {
			return Operation{}, apperr.New(apperr.CodeConflict, 409, "branch operation is stale", map[string]any{"expected_version": expectedVersion, "current_version": operation.Version})
		}
		operation.State = state
		operation.Error = message
		operation.Version++
		if pullRequestNumber > 0 {
			operation.PullRequestNumber = pullRequestNumber
		}
		if pullRequestURL != "" {
			operation.PullRequestURL = pullRequestURL
		}
		operation.UpdatedAt = s.now().UTC()
		s.operations[key] = operation
		return operation, nil
	}
	return Operation{}, apperr.New(apperr.CodeNotFound, 404, "branch operation not found", nil)
}

func (s *MemoryStore) ListOperations(_ context.Context, organizationID, projectID, planID string) ([]Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Operation, 0)
	for _, operation := range s.operations {
		if operation.OrganizationID == organizationID && operation.ProjectID == projectID && operation.PlanID == planID {
			result = append(result, operation)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (s *MemoryStore) MarkPullRequestMerged(_ context.Context, organizationID, repositoryID string, number int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, operation := range s.operations {
		if operation.OrganizationID == organizationID && operation.RepositoryID == repositoryID && operation.PullRequestNumber == number && operation.Kind == OperationSyncPull {
			if operation.State == OperationMerged {
				continue
			}
			operation.State = OperationMerged
			operation.Version++
			operation.UpdatedAt = s.now().UTC()
			s.operations[key] = operation
		}
	}
	return nil
}

func (s *MemoryStore) findPlan(organizationID, projectID, id string) (string, Plan, bool) {
	for key, plan := range s.plans {
		if plan.OrganizationID == organizationID && plan.ProjectID == projectID && plan.ID == id {
			return key, plan, true
		}
	}
	return "", Plan{}, false
}

func clonePolicy(input Policy) Policy {
	input.Rules = append([]Rule(nil), input.Rules...)
	for index := range input.Rules {
		input.Rules[index].SyncRoles = append([]BranchRole(nil), input.Rules[index].SyncRoles...)
	}
	return input
}

func clonePlan(input Plan) Plan {
	input.SyncBranches = append([]string(nil), input.SyncBranches...)
	return input
}

func scopeKey(organizationID, projectID string) string { return organizationID + "\x00" + projectID }
func planKey(organizationID, projectID, workItemID string) string {
	return scopeKey(organizationID, projectID) + "\x00" + workItemID
}
func operationKey(organizationID, projectID, idempotencyKey string) string {
	return scopeKey(organizationID, projectID) + "\x00" + idempotencyKey
}

func stalePolicy(expected, current int64) error {
	return apperr.New(apperr.CodeConflict, 409, "branch policy version is stale", map[string]any{"expected_version": expected, "current_version": current})
}

var _ Store = (*MemoryStore)(nil)
