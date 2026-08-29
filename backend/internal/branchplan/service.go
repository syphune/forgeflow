package branchplan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type MutationRecorder struct {
	Audit  audit.Writer
	Outbox outbox.Writer
}

type Options struct {
	Recorder    MutationRecorder
	Transaction TransactionRunner
	Now         func() time.Time
}

type Service struct {
	store       Store
	provider    Provider
	recorder    MutationRecorder
	transaction TransactionRunner
	now         func() time.Time
}

func NewService(store Store, provider Provider, options ...Options) *Service {
	configured := Options{}
	if len(options) > 0 {
		configured = options[0]
	}
	if configured.Transaction == nil {
		configured.Transaction = directTransactionRunner{}
	}
	if configured.Now == nil {
		configured.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, provider: provider, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) Configured() bool { return s.provider != nil }

func (s *Service) GetPolicy(ctx context.Context, actor identity.Actor, projectID string) (Policy, error) {
	if err := authorize(actor, projectID, identity.CapabilityProjectRead); err != nil {
		return Policy{}, err
	}
	policy, err := s.store.GetPolicy(ctx, actor.OrganizationID, strings.TrimSpace(projectID))
	if err != nil {
		return Policy{}, err
	}
	policy = policy.Normalized()
	policy.Rules = SortedRules(policy.Rules)
	return policy, nil
}

func (s *Service) SavePolicy(ctx context.Context, actor identity.Actor, projectID string, input PolicyInput) (Policy, error) {
	if err := authorize(actor, projectID, identity.CapabilityProjectManage); err != nil {
		return Policy{}, err
	}
	policy := Policy{OrganizationID: actor.OrganizationID, ProjectID: strings.TrimSpace(projectID), DevelopmentBranch: strings.TrimSpace(input.DevelopmentBranch), ProductionBranch: strings.TrimSpace(input.ProductionBranch), ReleasePrefix: strings.TrimSpace(input.ReleasePrefix), WorkBranchTemplate: strings.TrimSpace(input.WorkBranchTemplate), Rules: append([]Rule(nil), input.Rules...)}.Normalized()
	if err := policy.Validate(); err != nil {
		return Policy{}, apperr.New(apperr.CodeInvalidArgument, 422, err.Error(), nil)
	}
	var result Policy
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, loadErr := s.store.GetPolicy(txCtx, actor.OrganizationID, policy.ProjectID)
		if loadErr != nil {
			return loadErr
		}
		var saveErr error
		result, saveErr = s.store.SavePolicy(txCtx, policy, input.ExpectedVersion)
		if saveErr != nil {
			return saveErr
		}
		return s.record(txCtx, actor, "branch_policy.updated", "branch_policy", result.ID, before, result, map[string]any{"project_id": result.ProjectID})
	})
	if err != nil {
		return Policy{}, err
	}
	result.Rules = SortedRules(result.Rules)
	return result, nil
}

func (s *Service) Preview(ctx context.Context, actor identity.Actor, item WorkItemInput, input PreviewInput) (Plan, error) {
	if err := authorize(actor, item.ProjectID, identity.CapabilityProjectRead); err != nil {
		return Plan{}, err
	}
	policy, err := s.GetPolicy(ctx, actor, item.ProjectID)
	if err != nil {
		return Plan{}, err
	}
	plan, err := BuildPreview(policy, item, input)
	if err != nil {
		return Plan{}, apperr.New(apperr.CodeInvalidArgument, 422, err.Error(), nil)
	}
	plan.OrganizationID = actor.OrganizationID
	return plan, nil
}

func (s *Service) GetPlan(ctx context.Context, actor identity.Actor, projectID, workItemID string) (Plan, error) {
	if err := authorize(actor, projectID, identity.CapabilityProjectRead); err != nil {
		return Plan{}, err
	}
	return s.store.GetPlan(ctx, actor.OrganizationID, strings.TrimSpace(projectID), strings.TrimSpace(workItemID))
}

func (s *Service) HasPlan(ctx context.Context, organizationID, projectID, workItemID string) (bool, error) {
	_, err := s.store.GetPlan(ctx, organizationID, projectID, workItemID)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) Prepare(ctx context.Context, actor identity.Actor, item WorkItemInput, input PreviewInput) (Plan, error) {
	if err := authorize(actor, item.ProjectID, identity.CapabilityBranchManage); err != nil {
		return Plan{}, err
	}
	if item.Version < 1 {
		return Plan{}, apperr.New(apperr.CodeInvalidArgument, 422, "work item version is required", nil)
	}
	if existing, err := s.store.GetPlan(ctx, actor.OrganizationID, item.ProjectID, item.ID); err == nil {
		switch existing.State {
		case PlanReady:
			return existing, nil
		case PlanFailed:
			if strings.Contains(strings.ToLower(existing.Error), "already exists") {
				return Plan{}, apperr.New(apperr.CodeConflict, 409, "working branch already exists; refusing to reuse it", map[string]any{"plan_id": existing.ID, "branch": existing.WorkingBranch})
			}
			reset, resetErr := s.resetPlan(ctx, actor, existing)
			if resetErr != nil {
				return Plan{}, resetErr
			}
			existing = reset
		}
		return s.prepareExistingPlan(ctx, actor, existing)
	} else if !isNotFound(err) {
		return Plan{}, err
	}
	preview, err := s.Preview(ctx, actor, item, input)
	if err != nil {
		return Plan{}, err
	}
	baseSHA := "HEAD"
	var repository Repository
	if s.provider != nil {
		repository, err = s.provider.Repository(ctx, actor, item.ProjectID, item.RepositoryID)
		if err != nil {
			return Plan{}, err
		}
		baseSHA, err = s.ensurePlanBranches(ctx, actor, repository, preview, input.ReleaseBranch)
		if err != nil {
			return Plan{}, err
		}
	}
	preview.BaseSHA = baseSHA
	preview.CreatedBy = actor.ID
	preview.State = PlanPending
	created, err := s.createPlan(ctx, actor, preview)
	if err != nil {
		if isConflict(err) {
			return s.store.GetPlan(ctx, actor.OrganizationID, item.ProjectID, item.ID)
		}
		return Plan{}, err
	}
	return s.prepareExistingPlan(ctx, actor, created)
}

// ensurePlanBranches makes the configured development/production branches
// available before a work-item plan is persisted. Missing configured branches
// are created from the linked repository's default branch. Explicit release
// branches remain managed branches and must already exist.
func (s *Service) ensurePlanBranches(ctx context.Context, actor identity.Actor, repository Repository, plan Plan, releaseBranch string) (string, error) {
	defaultBranch := strings.TrimSpace(repository.DefaultBranch)
	if defaultBranch == "" {
		return "", apperr.New(apperr.CodeConflict, 409, "repository default branch is not configured", nil)
	}

	branches := make([]string, 0, 2+len(plan.SyncBranches))
	for _, branch := range append([]string{plan.BaseBranch, plan.TargetBranch}, plan.SyncBranches...) {
		branch = strings.TrimSpace(branch)
		if branch != "" && !contains(branches, branch) {
			branches = append(branches, branch)
		}
	}

	releaseBranch = strings.TrimSpace(releaseBranch)
	defaultSHA := ""
	baseSHA := ""
	for _, branch := range branches {
		head, headErr := s.provider.Head(ctx, actor, repository, branch)
		if headErr == nil && strings.TrimSpace(head) != "" {
			if branch == plan.BaseBranch {
				baseSHA = strings.TrimSpace(head)
			}
			continue
		}

		if releaseBranch != "" && branch == releaseBranch {
			return "", apperr.New(apperr.CodeConflict, 409, "managed release branch does not exist", map[string]any{"branch": branch})
		}
		if headErr == nil || !isNotFound(headErr) {
			return "", apperr.New(apperr.CodeConflict, 409, "could not resolve configured branch", map[string]any{"branch": branch})
		}
		if branch == defaultBranch {
			return "", apperr.New(apperr.CodeConflict, 409, "could not resolve repository default branch", map[string]any{"branch": defaultBranch})
		}

		if defaultSHA == "" {
			defaultSHA, headErr = s.provider.Head(ctx, actor, repository, defaultBranch)
			if headErr != nil || strings.TrimSpace(defaultSHA) == "" {
				return "", apperr.New(apperr.CodeConflict, 409, "could not resolve repository default branch", map[string]any{"branch": defaultBranch})
			}
		}
		if createErr := s.provider.CreateBranch(ctx, actor, repository, branch, defaultSHA); createErr != nil {
			// A concurrent request may have created the configured branch. Re-read
			// it before reporting a provisioning failure.
			head, headErr = s.provider.Head(ctx, actor, repository, branch)
			if headErr != nil || strings.TrimSpace(head) == "" {
				return "", createErr
			}
		} else {
			head = defaultSHA
		}
		if branch == plan.BaseBranch {
			baseSHA = strings.TrimSpace(head)
		}
	}

	if baseSHA == "" {
		return "", apperr.New(apperr.CodeConflict, 409, "could not resolve the configured base branch", map[string]any{"branch": plan.BaseBranch})
	}
	return baseSHA, nil
}

func (s *Service) prepareExistingPlan(ctx context.Context, actor identity.Actor, plan Plan) (Plan, error) {
	if s.provider == nil {
		return s.markPlanReady(ctx, actor, plan)
	}
	repository, err := s.provider.Repository(ctx, actor, plan.ProjectID, plan.RepositoryID)
	if err != nil {
		return Plan{}, err
	}
	if plan.BranchType == Release {
		if _, err := s.provider.Head(ctx, actor, repository, plan.WorkingBranch); err != nil {
			_ = s.markPlanFailed(ctx, actor, plan, "managed release branch does not exist")
			return Plan{}, apperr.New(apperr.CodeConflict, 409, "managed release branch does not exist", map[string]any{"branch": plan.WorkingBranch})
		}
		return s.markPlanReady(ctx, actor, plan)
	}

	key := "branchplan.branch:" + plan.ID
	operationInput := Operation{PlanID: plan.ID, OrganizationID: actor.OrganizationID, ProjectID: plan.ProjectID, RepositoryID: plan.RepositoryID, Kind: OperationBranchCreate, State: OperationPending, IdempotencyKey: key, SourceBranch: plan.BaseBranch, TargetBranch: plan.WorkingBranch}
	operation, operationErr := s.store.GetOperation(ctx, actor.OrganizationID, plan.ProjectID, key)
	if isNotFound(operationErr) {
		if _, headErr := s.provider.Head(ctx, actor, repository, plan.WorkingBranch); headErr == nil {
			_ = s.markPlanFailed(ctx, actor, plan, "working branch already exists")
			return Plan{}, apperr.New(apperr.CodeConflict, 409, "working branch already exists; refusing to reuse it", map[string]any{"branch": plan.WorkingBranch})
		} else if !isNotFound(headErr) {
			_ = s.markPlanFailed(ctx, actor, plan, "could not inspect working branch")
			return Plan{}, headErr
		}
		operation, operationErr = s.createOperation(ctx, actor, operationInput)
	} else if operationErr == nil && operation.State == OperationFailed {
		operation, operationErr = s.resetOperation(ctx, actor, operation)
	}
	if operationErr != nil {
		return Plan{}, operationErr
	}
	if operation.State == OperationCompleted {
		return s.markPlanReady(ctx, actor, plan)
	}

	branchHead, headErr := s.provider.Head(ctx, actor, repository, plan.WorkingBranch)
	if headErr == nil && strings.TrimSpace(branchHead) != "" {
		_, completeErr := s.completeOperation(ctx, actor, operation, PullRequest{})
		if completeErr != nil {
			return Plan{}, completeErr
		}
		return s.markPlanReady(ctx, actor, plan)
	}
	if headErr != nil && !isNotFound(headErr) {
		_ = s.failOperation(ctx, actor, operation, "could not inspect working branch")
		_ = s.markPlanFailed(ctx, actor, plan, "could not inspect working branch")
		return Plan{}, headErr
	}
	if err := s.provider.CreateBranch(ctx, actor, repository, plan.WorkingBranch, plan.BaseSHA); err != nil {
		// A concurrent request may have created the branch. Re-read it before
		// marking the immutable plan failed.
		if _, headErr := s.provider.Head(ctx, actor, repository, plan.WorkingBranch); headErr == nil {
			completed, completeErr := s.completeOperation(ctx, actor, operation, PullRequest{})
			if completeErr != nil {
				return Plan{}, completeErr
			}
			_ = completed
			return s.markPlanReady(ctx, actor, plan)
		}
		_ = s.failOperation(ctx, actor, operation, "working branch creation failed")
		_ = s.markPlanFailed(ctx, actor, plan, "working branch creation failed")
		return Plan{}, err
	}
	completed, err := s.completeOperation(ctx, actor, operation, PullRequest{})
	if err != nil {
		return Plan{}, err
	}
	_ = completed
	return s.markPlanReady(ctx, actor, plan)
}

func (s *Service) CreatePullRequest(ctx context.Context, actor identity.Actor, projectID, workItemID string) (Operation, error) {
	if err := authorize(actor, projectID, identity.CapabilityBranchManage); err != nil {
		return Operation{}, err
	}
	plan, err := s.GetPlan(ctx, actor, projectID, workItemID)
	if err != nil {
		return Operation{}, err
	}
	if plan.State != PlanReady {
		return Operation{}, apperr.New(apperr.CodeConflict, 409, "branch plan is not ready", map[string]any{"state": plan.State})
	}
	if !plan.CreatePullRequest {
		return Operation{}, apperr.New(apperr.CodeConflict, 409, "pull request creation is disabled by the branch policy", nil)
	}
	if s.provider == nil {
		return Operation{}, apperr.New(apperr.CodeConflict, 409, "GitHub branch provider is not configured", nil)
	}
	key := "branchplan.pr:" + plan.ID + ":" + plan.TargetBranch
	if existing, getErr := s.store.GetOperation(ctx, actor.OrganizationID, projectID, key); getErr == nil && existing.State != OperationFailed {
		return existing, nil
	}
	operation, err := s.ensureOperation(ctx, actor, Operation{PlanID: plan.ID, OrganizationID: actor.OrganizationID, ProjectID: projectID, RepositoryID: plan.RepositoryID, Kind: OperationPullRequest, State: OperationPending, IdempotencyKey: key, SourceBranch: plan.WorkingBranch, TargetBranch: plan.TargetBranch})
	if err != nil {
		return Operation{}, err
	}
	_, err = s.provider.Repository(ctx, actor, projectID, plan.RepositoryID)
	if err != nil {
		return Operation{}, err
	}
	if remote, found, findErr := s.provider.FindPullRequest(ctx, actor, projectID, plan.RepositoryID, plan.WorkingBranch, plan.TargetBranch); findErr != nil {
		return Operation{}, findErr
	} else if found {
		return s.completeOperation(ctx, actor, operation, remote)
	}
	title := "[" + plan.WorkItemKey + "] Implement " + plan.WorkItemKey
	body := fmt.Sprintf("Forgeflow Work Item: %s\n\nBranch plan: `%s` → `%s`\n\nThis draft was created from the approved branch plan.", plan.WorkItemKey, plan.WorkingBranch, plan.TargetBranch)
	remote, err := s.provider.CreateDraftPullRequest(ctx, actor, projectID, plan.RepositoryID, title, body, plan.WorkingBranch, plan.TargetBranch)
	if err != nil {
		_ = s.failOperation(ctx, actor, operation, "pull request creation failed")
		return Operation{}, err
	}
	return s.completeOperation(ctx, actor, operation, remote)
}

func (s *Service) Sync(ctx context.Context, actor identity.Actor, projectID, workItemID string) ([]Operation, error) {
	if err := authorize(actor, projectID, identity.CapabilityBranchManage); err != nil {
		return nil, err
	}
	plan, err := s.GetPlan(ctx, actor, projectID, workItemID)
	if err != nil {
		return nil, err
	}
	if plan.State != PlanReady {
		return nil, apperr.New(apperr.CodeConflict, 409, "branch plan is not ready", nil)
	}
	if len(plan.SyncBranches) == 0 {
		return []Operation{}, nil
	}
	if s.provider == nil {
		return nil, apperr.New(apperr.CodeConflict, 409, "GitHub branch provider is not configured", nil)
	}
	result := make([]Operation, 0, len(plan.SyncBranches))
	for _, target := range plan.SyncBranches {
		key := "branchplan.sync:" + plan.ID + ":" + target
		if existing, getErr := s.store.GetOperation(ctx, actor.OrganizationID, projectID, key); getErr == nil && existing.State != OperationFailed {
			result = append(result, existing)
			continue
		}
		operation, opErr := s.ensureOperation(ctx, actor, Operation{PlanID: plan.ID, OrganizationID: actor.OrganizationID, ProjectID: projectID, RepositoryID: plan.RepositoryID, Kind: OperationSyncPull, State: OperationPending, IdempotencyKey: key, SourceBranch: plan.TargetBranch, TargetBranch: target})
		if opErr != nil {
			return nil, opErr
		}
		if remote, found, findErr := s.provider.FindPullRequest(ctx, actor, projectID, plan.RepositoryID, plan.TargetBranch, target); findErr != nil {
			return nil, findErr
		} else if found {
			completed, completeErr := s.completeOperation(ctx, actor, operation, remote)
			if completeErr != nil {
				return nil, completeErr
			}
			result = append(result, completed)
			continue
		}
		title := fmt.Sprintf("[%s] Sync %s into %s", plan.WorkItemKey, plan.TargetBranch, target)
		body := fmt.Sprintf("Forgeflow sync-backport for Work Item %s. Merge `%s` into `%s` without rewriting history.", plan.WorkItemKey, plan.TargetBranch, target)
		remote, createErr := s.provider.CreateDraftPullRequest(ctx, actor, projectID, plan.RepositoryID, title, body, plan.TargetBranch, target)
		if createErr != nil {
			_ = s.failOperation(ctx, actor, operation, "sync pull request creation failed")
			return nil, createErr
		}
		completed, completeErr := s.completeOperation(ctx, actor, operation, remote)
		if completeErr != nil {
			return nil, completeErr
		}
		result = append(result, completed)
	}
	return result, nil
}

func (s *Service) ListOperations(ctx context.Context, actor identity.Actor, projectID, workItemID string) ([]Operation, error) {
	if err := authorize(actor, projectID, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	plan, err := s.GetPlan(ctx, actor, projectID, workItemID)
	if err != nil {
		return nil, err
	}
	return s.store.ListOperations(ctx, actor.OrganizationID, projectID, plan.ID)
}

func (s *Service) HasSync(ctx context.Context, organizationID, projectID, workItemID string) (bool, error) {
	plan, err := s.store.GetPlan(ctx, organizationID, projectID, workItemID)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if !plan.RequireSync || len(plan.SyncBranches) == 0 {
		return true, nil
	}
	operations, err := s.store.ListOperations(ctx, organizationID, projectID, plan.ID)
	if err != nil {
		return false, err
	}
	for _, target := range plan.SyncBranches {
		merged := false
		for _, operation := range operations {
			if operation.Kind == OperationSyncPull && operation.TargetBranch == target && operation.State == OperationMerged {
				merged = true
				break
			}
		}
		if !merged {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) MarkPullRequestMerged(ctx context.Context, organizationID, repositoryID string, number int64) error {
	return s.store.MarkPullRequestMerged(ctx, organizationID, repositoryID, number)
}

func (s *Service) createPlan(ctx context.Context, actor identity.Actor, plan Plan) (Plan, error) {
	var result Plan
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.CreatePlan(txCtx, plan)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_plan.created", "branch_plan", result.ID, nil, result, map[string]any{"project_id": result.ProjectID, "work_item_id": result.WorkItemID})
	})
	return result, err
}

func (s *Service) createOperation(ctx context.Context, actor identity.Actor, operation Operation) (Operation, error) {
	if existing, err := s.store.GetOperation(ctx, operation.OrganizationID, operation.ProjectID, operation.IdempotencyKey); err == nil {
		return existing, nil
	} else if !isNotFound(err) {
		return Operation{}, err
	}
	var result Operation
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.CreateOperation(txCtx, operation)
		if err != nil {
			if isConflict(err) {
				result, err = s.store.GetOperation(txCtx, operation.OrganizationID, operation.ProjectID, operation.IdempotencyKey)
				return err
			}
			return err
		}
		return s.record(txCtx, actor, "branch_operation.created", "branch_operation", result.ID, nil, result, map[string]any{"project_id": result.ProjectID, "plan_id": result.PlanID})
	})
	if err != nil && result.ID != "" && isConflict(err) {
		return result, nil
	}
	return result, err
}

func (s *Service) ensureOperation(ctx context.Context, actor identity.Actor, operation Operation) (Operation, error) {
	existing, err := s.store.GetOperation(ctx, operation.OrganizationID, operation.ProjectID, operation.IdempotencyKey)
	if err == nil {
		if existing.State != OperationFailed {
			return existing, nil
		}
		return s.resetOperation(ctx, actor, existing)
	}
	if !isNotFound(err) {
		return Operation{}, err
	}
	return s.createOperation(ctx, actor, operation)
}

func (s *Service) resetOperation(ctx context.Context, actor identity.Actor, operation Operation) (Operation, error) {
	var updated Operation
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.store.UpdateOperation(txCtx, operation.OrganizationID, operation.ProjectID, operation.ID, operation.Version, OperationPending, "", 0, "")
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_operation.retried", "branch_operation", updated.ID, operation, updated, map[string]any{"project_id": updated.ProjectID, "plan_id": updated.PlanID})
	})
	return updated, err
}

func (s *Service) completeOperation(ctx context.Context, actor identity.Actor, operation Operation, remote PullRequest) (Operation, error) {
	if operation.State == OperationCompleted || operation.State == OperationMerged {
		return operation, nil
	}
	var updated Operation
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.store.UpdateOperation(txCtx, actor.OrganizationID, operation.ProjectID, operation.ID, operation.Version, OperationCompleted, "", remote.Number, remote.URL)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_operation.completed", "branch_operation", updated.ID, operation, updated, map[string]any{"project_id": updated.ProjectID, "pull_request_number": updated.PullRequestNumber})
	})
	if err != nil {
		return Operation{}, err
	}
	return updated, nil
}

func (s *Service) markPlanReady(ctx context.Context, actor identity.Actor, plan Plan) (Plan, error) {
	if plan.State == PlanReady {
		return plan, nil
	}
	var updated Plan
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.store.UpdatePlan(txCtx, actor.OrganizationID, plan.ProjectID, plan.ID, plan.Version, PlanReady, "")
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_plan.ready", "branch_plan", updated.ID, plan, updated, map[string]any{"project_id": updated.ProjectID, "work_item_id": updated.WorkItemID})
	})
	if err != nil {
		if isConflict(err) {
			current, getErr := s.store.GetPlan(ctx, actor.OrganizationID, plan.ProjectID, plan.WorkItemID)
			if getErr == nil && current.State == PlanReady {
				return current, nil
			}
		}
		return Plan{}, err
	}
	return updated, nil
}

func (s *Service) markPlanFailed(ctx context.Context, actor identity.Actor, plan Plan, message string) error {
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.store.UpdatePlan(txCtx, actor.OrganizationID, plan.ProjectID, plan.ID, plan.Version, PlanFailed, message)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_plan.failed", "branch_plan", updated.ID, plan, updated, map[string]any{"project_id": updated.ProjectID, "work_item_id": updated.WorkItemID})
	})
}

func (s *Service) resetPlan(ctx context.Context, actor identity.Actor, plan Plan) (Plan, error) {
	var updated Plan
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.store.UpdatePlan(txCtx, actor.OrganizationID, plan.ProjectID, plan.ID, plan.Version, PlanPending, "")
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_plan.retried", "branch_plan", updated.ID, plan, updated, map[string]any{"project_id": updated.ProjectID, "work_item_id": updated.WorkItemID})
	})
	return updated, err
}

func (s *Service) failOperation(ctx context.Context, actor identity.Actor, operation Operation, message string) error {
	if operation.State == OperationFailed {
		return nil
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.store.UpdateOperation(txCtx, actor.OrganizationID, operation.ProjectID, operation.ID, operation.Version, OperationFailed, message, 0, "")
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "branch_operation.failed", "branch_operation", updated.ID, operation, updated, map[string]any{"project_id": updated.ProjectID, "plan_id": updated.PlanID})
	})
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceType, resourceID string, before, after any, payload map[string]any) error {
	if s.recorder.Audit != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if payload == nil {
			payload = map[string]any{}
		}
		payload["resource_id"] = resourceID
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: actor.OrganizationID, EventType: action, AggregateType: resourceType, AggregateID: resourceID, IdempotencyKey: action + ":" + resourceID, Payload: payload, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func authorize(actor identity.Actor, projectID, capability string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if strings.TrimSpace(projectID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}

func isNotFound(err error) bool { return err != nil && apperr.From(err).Code == apperr.CodeNotFound }
func isConflict(err error) bool { return err != nil && apperr.From(err).Code == apperr.CodeConflict }
