package workitem

import (
	"context"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/branchplan"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
)

type Service struct {
	repository  Repository
	spec        *specification.Service
	workflow    *workflow.Service
	evidence    EngineeringEvidence
	policy      DeliveryPolicyProvider
	qa          QAEvidence
	deployment  DeploymentEvidence
	audit       audit.Writer
	outbox      outbox.Writer
	branchPlan  BranchPlanPreparer
	now         func() time.Time
	transaction TransactionRunner
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

// EngineeringEvidence is deliberately owned by the work-item module: the
// workflow gate needs facts, but must not depend on a GitHub SDK or adapter.
type EngineeringEvidence interface {
	HasPullRequest(context.Context, string, string, string, string) (bool, error)
	HasSuccessfulCI(context.Context, string, string, string, string) (bool, error)
}

type PullRequestEvidence struct {
	Found            bool
	AuthorLogin      string
	HeadSHA          string
	State            string
	ApprovedByLogins []string
	ReviewerRoles    map[string][]string
	ChangedPaths     []string
	ChangesRequested bool
	Merged           bool
}

type PullRequestEvidenceProvider interface {
	PullRequestEvidence(context.Context, string, string, string, string, string) (PullRequestEvidence, error)
}

type RequiredCIEvidence interface {
	HasRequiredCI(context.Context, string, string, string, string, string, []string) (bool, error)
}

type QAEvidence interface {
	HasPassingQA(context.Context, string, string, string, string) (bool, error)
}

type DeploymentEvidence interface {
	HasSuccessfulDeployment(context.Context, string, string, string) (bool, error)
	HasProductionVerification(context.Context, string, string, string) (bool, error)
}

type PathReviewRule struct {
	Pattern       string
	MinApprovals  int
	RequiredRoles []string
}

type ReviewPolicy struct {
	MinApprovals       int
	ForbidSelfApproval bool
	RequiredRoles      []string
	PathRules          []PathReviewRule
}

type RiskRule struct {
	MinApprovals                  int
	RequiredRoles                 []string
	RequireQA                     bool
	RequireRollbackPlan           bool
	RequireProductionVerification bool
}

type DeliveryPolicy struct {
	ReleaseMode    string
	RequiredChecks []string
	Review         ReviewPolicy
	RiskRules      map[RiskLevel]RiskRule
}

type DeliveryPolicyProvider interface {
	DeliveryPolicy(context.Context, string, string) (DeliveryPolicy, error)
}

type BranchPlanPreparer interface {
	Prepare(context.Context, identity.Actor, branchplan.WorkItemInput, branchplan.PreviewInput) (branchplan.Plan, error)
	HasPlan(context.Context, string, string, string) (bool, error)
	HasSync(context.Context, string, string, string) (bool, error)
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func NewService(repository Repository, spec *specification.Service, workflowService *workflow.Service, auditWriter audit.Writer, outboxWriter outbox.Writer, now func() time.Time, runners ...TransactionRunner) *Service {
	transaction := TransactionRunner(directTransactionRunner{})
	if len(runners) > 0 && runners[0] != nil {
		transaction = runners[0]
	}
	return &Service{repository: repository, spec: spec, workflow: workflowService, audit: auditWriter, outbox: outboxWriter, now: now, transaction: transaction}
}

func (s *Service) SetEngineeringEvidence(evidence EngineeringEvidence) {
	s.evidence = evidence
}

func (s *Service) SetDeliveryPolicyProvider(provider DeliveryPolicyProvider) {
	s.policy = provider
}

func (s *Service) SetQAEvidence(evidence QAEvidence) {
	s.qa = evidence
}

func (s *Service) SetDeploymentEvidence(evidence DeploymentEvidence) {
	s.deployment = evidence
}

func (s *Service) SetBranchPlanPreparer(preparer BranchPlanPreparer) {
	s.branchPlan = preparer
}

func (s *Service) BranchPlanInput(ctx context.Context, projectID string, actor identity.Actor, id string) (branchplan.WorkItemInput, error) {
	scope := Scope{OrganizationID: actor.OrganizationID, ProjectID: strings.TrimSpace(projectID)}
	item, err := s.Get(ctx, scope, actor, id)
	if err != nil {
		return branchplan.WorkItemInput{}, err
	}
	assignee := ""
	if item.AssigneeID == actor.ID {
		assignee = actor.Login
	}
	return branchplan.WorkItemInput{ID: item.ID, Key: item.Key, Title: item.Title, Type: string(item.Type), ProjectID: item.ProjectID, RepositoryID: s.repositoryIDFor(ctx, scope, item), Assignee: assignee, AssigneeID: item.AssigneeID, Version: item.Version}, nil
}

func (s *Service) Create(ctx context.Context, scope Scope, actor identity.Actor, input CreateInput) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemCreate); err != nil {
		return nil, err
	}
	if !validType(input.Type) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported work item type", map[string]any{"type": input.Type})
	}
	if strings.TrimSpace(input.Title) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "title is required", nil)
	}
	if input.Priority == "" {
		input.Priority = PriorityMedium
	}
	if !validPriority(input.Priority) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported priority", map[string]any{"priority": input.Priority})
	}
	if input.EstimatePoints != nil && *input.EstimatePoints < 0 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "estimate_points cannot be negative", nil)
	}
	input.RiskLevel = normalizedRiskLevel(input.RiskLevel)
	if !validRiskLevel(input.RiskLevel) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported risk level", map[string]any{"risk_level": input.RiskLevel})
	}
	if input.ReporterID == "" {
		input.ReporterID = actor.ID
	}
	input.AssigneeID = strings.TrimSpace(input.AssigneeID)
	var result *WorkItem
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.createMutation(txCtx, scope, actor, input)
		return err
	})
	return result, err
}

func (s *Service) createMutation(ctx context.Context, scope Scope, actor identity.Actor, input CreateInput) (*WorkItem, error) {
	item, err := s.repository.Create(ctx, scope, input)
	if err != nil {
		return nil, err
	}
	if _, err := s.spec.Ensure(ctx, scope.OrganizationID, scope.ProjectID, item.ID); err != nil {
		return nil, err
	}
	if err := s.record(ctx, actor, scope, "create", nil, item); err != nil {
		return nil, err
	}
	return item, s.emit(ctx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.created", AggregateType: "work_item", AggregateID: item.ID, IdempotencyKey: "work_item.created:" + item.ID, Payload: map[string]any{"project_id": scope.ProjectID, "key": item.Key}, OccurredAt: s.now().UTC()})
}

func (s *Service) Get(ctx context.Context, scope Scope, actor identity.Actor, id string) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.repository.Get(ctx, scope, id)
}

func (s *Service) WorkItemState(ctx context.Context, organizationID, projectID, id string) (specification.WorkItemState, error) {
	item, err := s.repository.Get(ctx, Scope{OrganizationID: organizationID, ProjectID: projectID}, id)
	if err != nil {
		return specification.WorkItemState{}, err
	}
	state := specification.WorkItemState{Status: item.Status, SpecificationPhase: specification.SpecificationPhaseLocked}
	if item.Status == workflow.Raw {
		state.SpecificationPhase = specification.SpecificationPhaseRaw
		return state, nil
	}
	if s.workflow == nil {
		if item.Status == workflow.ReviewRequired {
			state.SpecificationPhase = specification.SpecificationPhaseReview
		} else if item.Status == workflow.Refining {
			state.SpecificationPhase = specification.SpecificationPhaseRefining
		}
		return state, nil
	}
	projectWorkflow, err := s.workflow.WorkflowFor(ctx, organizationID, projectID)
	if err != nil {
		return specification.WorkItemState{}, err
	}
	status, exists := projectWorkflow.Statuses[item.Status]
	if !exists {
		return state, nil
	}
	reviewStatus := item.Status == workflow.ReviewRequired
	lockedStatus := item.Status == workflow.Ready
	for _, transition := range projectWorkflow.Transitions {
		requiresSpecification := containsWorkflowRule(transition.Required, workflow.RequireSpecificationReady)
		if requiresSpecification && transition.From == item.Status {
			reviewStatus = true
		}
		if (requiresSpecification || transition.To == workflow.Ready) && transition.To == item.Status {
			lockedStatus = true
		}
	}
	if lockedStatus {
		return state, nil
	}
	if reviewStatus {
		state.SpecificationPhase = specification.SpecificationPhaseReview
		return state, nil
	}
	if status.Category == "TODO" && !status.IsTerminal {
		state.SpecificationPhase = specification.SpecificationPhaseRefining
	}
	return state, nil
}

func (s *Service) RefinementTransitionKey(ctx context.Context, organizationID, projectID string) (string, error) {
	if s.workflow == nil {
		return "start_refining", nil
	}
	projectWorkflow, err := s.workflow.WorkflowFor(ctx, organizationID, projectID)
	if err != nil {
		return "", err
	}
	best := ""
	for key, transition := range projectWorkflow.Transitions {
		if transition.From != workflow.Raw || transition.To == workflow.Ready || containsWorkflowRule(transition.Required, workflow.RequireSpecificationReady) {
			continue
		}
		target, exists := projectWorkflow.Statuses[transition.To]
		if !exists || target.Category != "TODO" || target.IsTerminal {
			continue
		}
		if key == "start_refining" {
			return key, nil
		}
		if best == "" || key < best {
			best = key
		}
	}
	if best == "" {
		return "", apperr.New(apperr.CodeConflict, 409, "workflow has no refinement transition from RAW", nil)
	}
	return best, nil
}

func (s *Service) List(ctx context.Context, scope Scope, actor identity.Actor, filter ListFilter) ([]*WorkItem, error) {
	page, err := s.ListPage(ctx, scope, actor, filter)
	return page.Items, err
}

func (s *Service) ListPage(ctx context.Context, scope Scope, actor identity.Actor, filter ListFilter) (ListResult, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return ListResult{}, err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Type = strings.TrimSpace(filter.Type)
	filter.Priority = strings.TrimSpace(filter.Priority)
	filter.AssigneeID = strings.TrimSpace(filter.AssigneeID)
	filter.SprintID = strings.TrimSpace(filter.SprintID)
	filter.RepositoryID = strings.TrimSpace(filter.RepositoryID)
	filter.Sort = strings.ToLower(strings.TrimSpace(filter.Sort))
	if filter.Sort == "" {
		filter.Sort = "updated"
	}
	if filter.Sort != "updated" && filter.Sort != "backlog" {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported work item sort", nil)
	}
	if len(filter.Query) > 256 {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "search query is too long", nil)
	}
	if len(filter.Status) > 64 {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "status filter is too long", nil)
	}
	if filter.Type != "" && !validType(Type(strings.ToUpper(filter.Type))) {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported work item type filter", nil)
	}
	if filter.Priority != "" && !validPriority(filter.Priority) {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported priority filter", nil)
	}
	if _, err := decodeWorkItemCursor(filter.Cursor); err != nil {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "cursor is invalid", nil)
	}
	if cursor, err := decodeWorkItemCursor(filter.Cursor); err == nil && cursor.Sort != "" && cursor.Sort != filter.Sort {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "cursor does not match work item sort", nil)
	}
	if (filter.ExcludeTerminal || filter.ExcludeTerminalCompletedBefore != nil) && filter.Status == "" {
		projectWorkflow, err := s.workflow.WorkflowFor(ctx, scope.OrganizationID, scope.ProjectID)
		if err != nil {
			return ListResult{}, err
		}
		filter.ExcludedStatuses = make([]string, 0, len(projectWorkflow.Statuses))
		for key, status := range projectWorkflow.Statuses {
			if status.IsTerminal {
				filter.ExcludedStatuses = append(filter.ExcludedStatuses, key)
			}
		}
		sort.Strings(filter.ExcludedStatuses)
	}
	return s.repository.ListPage(ctx, scope, filter)
}

func (s *Service) completionTimestamp(ctx context.Context, scope Scope, before *WorkItem, targetStatus string) (*time.Time, error) {
	projectWorkflow, err := s.workflow.WorkflowFor(ctx, scope.OrganizationID, scope.ProjectID)
	if err != nil {
		return nil, err
	}
	status, ok := projectWorkflow.Statuses[targetStatus]
	if !ok {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "target status is not configured for this project", map[string]any{"status": targetStatus})
	}
	if !status.IsTerminal {
		return nil, nil
	}
	if before != nil && before.CompletedAt != nil {
		return before.CompletedAt, nil
	}
	completedAt := s.now().UTC()
	return &completedAt, nil
}

func (s *Service) ColumnOrderingVersions(ctx context.Context, scope Scope, actor identity.Actor, sprintID string) (map[string]int64, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	versions, err := s.repository.ColumnOrderingVersions(ctx, scope, strings.TrimSpace(sprintID))
	if err != nil {
		return nil, err
	}
	if versions == nil {
		versions = make(map[string]int64)
	}
	return versions, nil
}

func (s *Service) Reorder(ctx context.Context, scope Scope, actor identity.Actor, id, direction string, expectedVersion int64) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return nil, err
	}
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction != "up" && direction != "down" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "rank direction must be up or down", nil)
	}
	if expectedVersion < 1 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "expected_version is required", nil)
	}
	var result *WorkItem
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, err := s.repository.Get(txCtx, scope, id)
		if err != nil {
			return err
		}
		result, err = s.repository.MoveRank(txCtx, scope, id, direction, expectedVersion)
		if err != nil {
			return err
		}
		if before.BacklogRank == result.BacklogRank {
			return nil
		}
		if err := s.record(txCtx, actor, scope, "rank", before, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.reordered", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.reordered:" + id + ":" + itoa(result.Version), Payload: map[string]any{"project_id": scope.ProjectID, "direction": direction, "backlog_rank": result.BacklogRank}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) Move(ctx context.Context, scope Scope, actor identity.Actor, id string, input MoveInput) (MoveResult, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return MoveResult{}, err
	}
	if input.ExpectedVersion < 1 || input.ExpectedSourceOrderingVersion < 1 || input.ExpectedDestinationOrderingVersion < 1 {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "item and column ordering versions are required", nil)
	}
	input.ItemID = id
	input.TargetStatus = strings.ToUpper(strings.TrimSpace(input.TargetStatus))
	if input.TargetStatus == "" {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "target_status is required", nil)
	}
	var result MoveResult
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, err := s.repository.Get(txCtx, scope, id)
		if err != nil {
			return err
		}
		if before.Status != input.TargetStatus {
			if err := authorize(scope, actor, identity.CapabilityWorkItemTransition); err != nil {
				return err
			}
			if strings.TrimSpace(input.TransitionKey) == "" {
				return apperr.New(apperr.CodeInvalidArgument, 422, "transition_key is required for a cross-column move", nil)
			}
			transition, transitionErr := s.workflow.TransitionForProject(txCtx, scope.OrganizationID, scope.ProjectID, before.Status, input.TransitionKey)
			if transitionErr != nil {
				return transitionErr
			}
			if transition.To != input.TargetStatus {
				return apperr.New(apperr.CodeConflict, 409, "transition target does not match destination column", map[string]any{"expected_status": transition.To, "target_status": input.TargetStatus})
			}
			if err := s.validateTransition(txCtx, scope, actor, before, transition, false); err != nil {
				return err
			}
			input.CompletedAt, err = s.completionTimestamp(txCtx, scope, before, input.TargetStatus)
			if err != nil {
				return err
			}
		}
		result, err = s.repository.Move(txCtx, scope, input)
		if err != nil {
			return err
		}
		if before.Status != result.Item.Status {
			if err := s.record(txCtx, actor, scope, "transition", before, result.Item); err != nil {
				return err
			}
			if err := s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.transitioned", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.transitioned:" + id + ":" + itoa(result.Item.Version), Payload: map[string]any{"project_id": scope.ProjectID, "from": before.Status, "to": result.Item.Status}, OccurredAt: s.now().UTC()}); err != nil {
				return err
			}
		}
		if result.Reordered {
			if err := s.record(txCtx, actor, scope, "rank", before, result.Item); err != nil {
				return err
			}
			if err := s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.reordered", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.reordered:" + id + ":" + itoa(result.Item.Version), Payload: map[string]any{"project_id": scope.ProjectID, "backlog_rank": result.Item.BacklogRank, "source_ordering_version": result.SourceOrderingVersion, "destination_ordering_version": result.DestinationOrderingVersion}, OccurredAt: s.now().UTC()}); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func (s *Service) Update(ctx context.Context, scope Scope, actor identity.Actor, id string, input UpdateInput) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return nil, err
	}
	if input.ExpectedVersion < 1 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "expected_version is required", nil)
	}
	if input.Title == nil && input.Description == nil {
		if input.Priority == nil && !input.RiskLevelSet && !input.DueAtSet && !input.EstimatePointsSet && !input.SprintIDSet && !input.ParentIDSet && !input.RepositoryIDSet {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "at least one editable field is required", nil)
		}
	}
	if input.Priority != nil && !validPriority(*input.Priority) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported priority", map[string]any{"priority": *input.Priority})
	}
	if input.RiskLevelSet && (input.RiskLevel == nil || !validRiskLevel(normalizedRiskLevel(*input.RiskLevel))) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported risk level", nil)
	}
	if input.EstimatePoints != nil && *input.EstimatePoints < 0 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "estimate_points cannot be negative", nil)
	}
	var result *WorkItem
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.updateMutation(txCtx, scope, actor, id, input)
		return err
	})
	return result, err
}

func (s *Service) updateMutation(ctx context.Context, scope Scope, actor identity.Actor, id string, input UpdateInput) (*WorkItem, error) {
	if input.ParentIDSet && input.ParentID != nil && strings.TrimSpace(*input.ParentID) == id {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "a work item cannot be its own parent", nil)
	}
	if input.ParentIDSet && input.ParentID != nil {
		parentID := strings.TrimSpace(*input.ParentID)
		if parentID != "" {
			if _, err := s.repository.Get(ctx, scope, parentID); err != nil {
				return nil, err
			}
		}
	}
	before, err := s.repository.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	updated, err := s.repository.Update(ctx, scope, id, input.ExpectedVersion, func(item *WorkItem) error {
		if input.Title != nil {
			if strings.TrimSpace(*input.Title) == "" {
				return apperr.New(apperr.CodeInvalidArgument, 422, "title cannot be empty", nil)
			}
			item.Title = strings.TrimSpace(*input.Title)
		}
		if input.Description != nil {
			item.Description = *input.Description
		}
		if input.ParentIDSet {
			if input.ParentID == nil {
				item.ParentID = ""
			} else {
				item.ParentID = strings.TrimSpace(*input.ParentID)
			}
		}
		if input.RepositoryIDSet {
			if input.RepositoryID == nil {
				item.RepositoryID = ""
			} else {
				item.RepositoryID = strings.TrimSpace(*input.RepositoryID)
			}
		}
		if input.Priority != nil {
			item.Priority = strings.ToUpper(strings.TrimSpace(*input.Priority))
		}
		if input.RiskLevelSet && input.RiskLevel != nil {
			item.RiskLevel = normalizedRiskLevel(*input.RiskLevel)
		}
		if input.DueAtSet {
			item.DueAt = input.DueAt
		}
		if input.EstimatePointsSet {
			item.EstimatePoints = input.EstimatePoints
		}
		if input.SprintIDSet && input.SprintID != nil {
			item.SprintID = strings.TrimSpace(*input.SprintID)
		} else if input.SprintIDSet {
			item.SprintID = ""
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, actor, scope, "update", before, updated); err != nil {
		return nil, err
	}
	return updated, s.emit(ctx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.updated", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.updated:" + id + ":" + itoa(updated.Version), Payload: map[string]any{"project_id": scope.ProjectID, "version": updated.Version}, OccurredAt: s.now().UTC()})
}

func (s *Service) Archive(ctx context.Context, scope Scope, actor identity.Actor, id string, expectedVersion int64) error {
	if err := authorize(scope, actor, identity.CapabilityWorkItemDelete); err != nil {
		return err
	}
	if expectedVersion < 1 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "expected_version is required", nil)
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, err := s.repository.Get(txCtx, scope, id)
		if err != nil {
			return err
		}
		updated, err := s.repository.Archive(txCtx, scope, id, expectedVersion, actor.ID)
		if err != nil {
			return err
		}
		if before.ArchivedAt != nil {
			return nil
		}
		if err := s.record(txCtx, actor, scope, "archive", before, updated); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.archived", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.archived:" + id + ":" + itoa(updated.Version), Payload: map[string]any{"project_id": scope.ProjectID, "version": updated.Version}, OccurredAt: s.now().UTC()})
	})
}

func (s *Service) Restore(ctx context.Context, scope Scope, actor identity.Actor, id string, expectedVersion int64) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return nil, err
	}
	if expectedVersion < 1 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "expected_version is required", nil)
	}
	var result *WorkItem
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, err := s.repository.Get(txCtx, scope, id)
		if err != nil {
			return err
		}
		result, err = s.repository.Restore(txCtx, scope, id, expectedVersion)
		if err != nil {
			return err
		}
		if before.ArchivedAt == nil {
			return nil
		}
		if err := s.record(txCtx, actor, scope, "restore", before, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.restored", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.restored:" + id + ":" + itoa(result.Version), Payload: map[string]any{"project_id": scope.ProjectID, "version": result.Version}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) Assign(ctx context.Context, scope Scope, actor identity.Actor, id, assigneeID string, expectedVersion int64) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemAssign); err != nil {
		return nil, err
	}
	if expectedVersion < 1 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "expected_version is required", nil)
	}
	var result *WorkItem
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.assignMutation(txCtx, scope, actor, id, assigneeID, expectedVersion)
		return err
	})
	return result, err
}

func (s *Service) assignMutation(ctx context.Context, scope Scope, actor identity.Actor, id, assigneeID string, expectedVersion int64) (*WorkItem, error) {
	before, err := s.repository.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	updated, err := s.repository.Update(ctx, scope, id, expectedVersion, func(item *WorkItem) error {
		item.AssigneeID = strings.TrimSpace(assigneeID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, actor, scope, "assignment", before, updated); err != nil {
		return nil, err
	}
	return updated, s.emit(ctx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.assigned", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.assigned:" + id + ":" + itoa(updated.Version), Payload: map[string]any{"project_id": scope.ProjectID, "assignee_id": updated.AssigneeID}, OccurredAt: s.now().UTC()})
}

func (s *Service) Transition(ctx context.Context, scope Scope, actor identity.Actor, id string, input TransitionInput) (*WorkItem, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemTransition); err != nil {
		return nil, err
	}
	if input.ExpectedVersion < 1 || strings.TrimSpace(input.TransitionKey) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "transition_key and expected_version are required", nil)
	}
	if s.branchPlan != nil {
		before, loadErr := s.repository.Get(ctx, scope, id)
		if loadErr != nil {
			return nil, loadErr
		}
		transition, transitionErr := s.workflow.TransitionForProject(ctx, scope.OrganizationID, scope.ProjectID, before.Status, input.TransitionKey)
		if transitionErr != nil {
			return nil, transitionErr
		}
		if before.Version != input.ExpectedVersion {
			return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": input.ExpectedVersion, "current_version": before.Version})
		}
		if transition.To == workflow.InProgress || containsWorkflowRule(transition.Required, workflow.RequireBranchPlan) {
			if validateErr := s.validateTransition(ctx, scope, actor, before, transition, true); validateErr != nil {
				return nil, validateErr
			}
			planInput := branchplan.WorkItemInput{ID: before.ID, Key: before.Key, Title: before.Title, Type: string(before.Type), ProjectID: before.ProjectID, RepositoryID: s.repositoryIDFor(ctx, scope, before), AssigneeID: before.AssigneeID, Version: before.Version}
			if before.AssigneeID == actor.ID {
				planInput.Assignee = actor.Login
			}
			if _, prepareErr := s.branchPlan.Prepare(ctx, actor, planInput, branchplan.PreviewInput{}); prepareErr != nil {
				return nil, prepareErr
			}
		}
	}
	var result *WorkItem
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.transitionMutation(txCtx, scope, actor, id, input)
		return err
	})
	return result, err
}

func (s *Service) transitionMutation(ctx context.Context, scope Scope, actor identity.Actor, id string, input TransitionInput) (*WorkItem, error) {
	before, err := s.repository.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	transition, err := s.workflow.TransitionForProject(ctx, scope.OrganizationID, scope.ProjectID, before.Status, input.TransitionKey)
	if err != nil {
		return nil, err
	}
	if err := s.validateTransition(ctx, scope, actor, before, transition, false); err != nil {
		return nil, err
	}
	completedAt, err := s.completionTimestamp(ctx, scope, before, transition.To)
	if err != nil {
		return nil, err
	}
	updated, err := s.repository.Update(ctx, scope, id, input.ExpectedVersion, func(item *WorkItem) error {
		item.Status = transition.To
		item.CompletedAt = completedAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, actor, scope, "transition", before, updated); err != nil {
		return nil, err
	}
	return updated, s.emit(ctx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.transitioned", AggregateType: "work_item", AggregateID: id, IdempotencyKey: "work_item.transitioned:" + id + ":" + itoa(updated.Version), Payload: map[string]any{"project_id": scope.ProjectID, "from": before.Status, "to": updated.Status}, OccurredAt: s.now().UTC()})
}

func (s *Service) validateTransition(ctx context.Context, scope Scope, actor identity.Actor, before *WorkItem, transition workflow.Transition, skipBranchRules bool) error {
	repositoryID := s.repositoryIDFor(ctx, scope, before)
	policy := DeliveryPolicy{}
	if s.policy != nil {
		var err error
		policy, err = s.policy.DeliveryPolicy(ctx, scope.OrganizationID, scope.ProjectID)
		if err != nil {
			return err
		}
	}
	var targetStatus workflow.Status
	targetExists := false
	if s.workflow != nil {
		projectWorkflow, err := s.workflow.WorkflowFor(ctx, scope.OrganizationID, scope.ProjectID)
		if err != nil {
			return err
		}
		targetStatus, targetExists = projectWorkflow.Statuses[transition.To]
	}
	releaseTransition := policy.ReleaseMode == "release_gated" && isReleaseTransition(transition.To)
	var pullRequest PullRequestEvidence
	pullRequestLoaded := false
	loadPullRequest := func(required bool) error {
		if pullRequestLoaded {
			return nil
		}
		provider, ok := s.evidence.(PullRequestEvidenceProvider)
		if !ok {
			if required {
				return apperr.New(apperr.CodeInvalidArgument, 422, "pull request review evidence is unavailable", nil)
			}
			return nil
		}
		var err error
		pullRequest, err = provider.PullRequestEvidence(ctx, scope.OrganizationID, scope.ProjectID, repositoryID, before.Key, "")
		if err != nil {
			return err
		}
		pullRequestLoaded = true
		if !pullRequest.Found || strings.TrimSpace(pullRequest.HeadSHA) == "" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "current pull request head is required", map[string]any{"work_item_key": before.Key})
		}
		return nil
	}
	// READY is a platform invariant, even when a project customizes its
	// workflow. A saved workflow cannot weaken the specification quality gate.
	if transition.To == workflow.Ready {
		readiness, evaluateErr := s.spec.Readiness(ctx, scope.OrganizationID, scope.ProjectID, before.ID, string(before.Type), before.Title, repositoryID)
		if evaluateErr != nil {
			return evaluateErr
		}
		if !readiness.Ready {
			return apperr.New(apperr.CodeSpecificationNotReady, 422, "work item cannot transition to READY", map[string]any{"missing": readiness.Missing})
		}
	}
	for _, rule := range transition.Required {
		switch rule {
		case workflow.RequireSpecificationReady:
			if transition.To == workflow.Ready {
				continue
			}
			readiness, evaluateErr := s.spec.Readiness(ctx, scope.OrganizationID, scope.ProjectID, before.ID, string(before.Type), before.Title, repositoryID)
			if evaluateErr != nil {
				return evaluateErr
			}
			if !readiness.Ready {
				return apperr.New(apperr.CodeSpecificationNotReady, 422, "work item cannot transition to READY", map[string]any{"missing": readiness.Missing})
			}
		case workflow.RequireAssignee:
			if strings.TrimSpace(before.AssigneeID) == "" {
				return apperr.New(apperr.CodeInvalidArgument, 422, "work item requires an assignee", nil)
			}
		case workflow.RequireRepository:
			if repositoryID == "" {
				return apperr.New(apperr.CodeInvalidArgument, 422, "work item requires a repository", nil)
			}
		case workflow.RequirePullRequest:
			if repositoryID == "" || s.evidence == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "pull request is required before code review", nil)
			}
			hasPR, evidenceErr := s.evidence.HasPullRequest(ctx, scope.OrganizationID, scope.ProjectID, repositoryID, before.Key)
			if evidenceErr != nil {
				return evidenceErr
			}
			if !hasPR {
				return apperr.New(apperr.CodeInvalidArgument, 422, "pull request is required before code review", map[string]any{"work_item_key": before.Key})
			}
		case workflow.RequireCISuccess:
			if repositoryID == "" || s.evidence == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "successful CI is required before QA", nil)
			}
			hasCI, evidenceErr := s.evidence.HasSuccessfulCI(ctx, scope.OrganizationID, scope.ProjectID, repositoryID, before.Key)
			if evidenceErr != nil {
				return evidenceErr
			}
			if !hasCI {
				return apperr.New(apperr.CodeInvalidArgument, 422, "successful CI is required before QA", map[string]any{"work_item_key": before.Key})
			}
		case workflow.RequireCurrentCIChecks:
			if repositoryID == "" || s.evidence == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "current-head CI checks are required", nil)
			}
			if err := loadPullRequest(true); err != nil {
				return err
			}
			ci, ok := s.evidence.(RequiredCIEvidence)
			if !ok {
				return apperr.New(apperr.CodeInvalidArgument, 422, "current-head CI evidence is unavailable", nil)
			}
			passed, err := ci.HasRequiredCI(ctx, scope.OrganizationID, scope.ProjectID, repositoryID, before.Key, pullRequest.HeadSHA, policy.RequiredChecks)
			if err != nil {
				return err
			}
			if !passed {
				return apperr.New(apperr.CodeInvalidArgument, 422, "required CI checks have not passed on the current pull request head", map[string]any{"head_sha": pullRequest.HeadSHA, "required_checks": policy.RequiredChecks})
			}
		case workflow.RequirePullRequestReview:
			if repositoryID == "" || s.evidence == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "pull request review evidence is required", nil)
			}
			if err := loadPullRequest(true); err != nil {
				return err
			}
			reviewPolicy := policy.Review
			if releaseTransition {
				riskRule := deliveryRiskRule(policy, before.RiskLevel)
				if riskRule.MinApprovals > reviewPolicy.MinApprovals {
					reviewPolicy.MinApprovals = riskRule.MinApprovals
				}
				reviewPolicy.RequiredRoles = append(reviewPolicy.RequiredRoles, riskRule.RequiredRoles...)
			}
			if err := validatePullRequestReview(pullRequest, reviewPolicy); err != nil {
				return err
			}
		case workflow.RequireQAPassing:
			if s.qa == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "QA evidence is unavailable", nil)
			}
			if err := loadPullRequest(true); err != nil {
				return err
			}
			if releaseTransition && !pullRequest.Merged {
				return apperr.New(apperr.CodeInvalidArgument, 422, "pull request must be approved and merged before release", nil)
			}
			passed, err := s.qa.HasPassingQA(ctx, scope.OrganizationID, scope.ProjectID, before.ID, pullRequest.HeadSHA)
			if err != nil {
				return err
			}
			if !passed {
				return apperr.New(apperr.CodeInvalidArgument, 422, "all required QA tests must pass on the current commit", nil)
			}
		case workflow.RequireReleaseApproval:
			if actor.Type != "human" || !actor.Has(identity.CapabilityReleaseApprove) {
				return apperr.New(apperr.CodeForbidden, 403, "a human release approver is required", nil)
			}
			if policy.ReleaseMode == "release_gated" {
				if err := loadPullRequest(true); err != nil {
					return err
				}
				if sameIdentity(actor, pullRequest.AuthorLogin) || containsIdentity(pullRequest.ApprovedByLogins, actor) {
					return apperr.New(apperr.CodeForbidden, 403, "production approver must be separate from the PR author and reviewers", nil)
				}
			}
		case workflow.RequireDeploymentSuccess:
			if s.deployment == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "successful deployment evidence is unavailable", nil)
			}
			ok, err := s.deployment.HasSuccessfulDeployment(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
			if err != nil {
				return err
			}
			if !ok {
				return apperr.New(apperr.CodeInvalidArgument, 422, "a successful deployment is required before production verification", nil)
			}
		case workflow.RequireProductionVerification:
			if s.deployment == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "production verification evidence is unavailable", nil)
			}
			ok, err := s.deployment.HasProductionVerification(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
			if err != nil {
				return err
			}
			if !ok {
				return apperr.New(apperr.CodeInvalidArgument, 422, "production verification is required before completion", nil)
			}
		case workflow.RequireRollbackPlan:
			if err := s.requireRollbackPlan(ctx, scope, before.ID); err != nil {
				return err
			}
		case workflow.RequireHumanVerification:
			if actor.Type != "human" {
				return apperr.New(apperr.CodeForbidden, 403, "only a human can complete this transition", nil)
			}
			readiness, evaluateErr := s.spec.Readiness(ctx, scope.OrganizationID, scope.ProjectID, before.ID, string(before.Type), before.Title, repositoryID)
			if evaluateErr != nil {
				return evaluateErr
			}
			if !readiness.Ready {
				return apperr.New(apperr.CodeSpecificationNotReady, 422, "human verification is required", map[string]any{"missing": readiness.Missing})
			}
		case workflow.RequirePermission:
			if len(transition.RequiredPermissions) == 0 {
				return apperr.New(apperr.CodeInternal, 500, "workflow permission rule is missing its configuration", map[string]any{"transition": transition.Key})
			}
			for _, permission := range transition.RequiredPermissions {
				if !actor.Has(permission) {
					return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": permission, "transition": transition.Key})
				}
			}
		case workflow.RequireBranchPlan:
			if skipBranchRules || s.branchPlan == nil {
				continue
			}
			hasPlan, planErr := s.branchPlan.HasPlan(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
			if planErr != nil {
				return planErr
			}
			if !hasPlan {
				return apperr.New(apperr.CodeInvalidArgument, 422, "a branch plan is required before starting work", nil)
			}
		case workflow.RequireBranchSync:
			if skipBranchRules || s.branchPlan == nil {
				continue
			}
			synced, syncErr := s.branchPlan.HasSync(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
			if syncErr != nil {
				return syncErr
			}
			if !synced {
				return apperr.New(apperr.CodeInvalidArgument, 422, "branch sync-backport must be merged before completing the work item", nil)
			}
		}
	}
	if releaseTransition {
		riskRule := deliveryRiskRule(policy, before.RiskLevel)
		if riskRule.RequireQA && !containsWorkflowRule(transition.Required, workflow.RequireQAPassing) {
			if s.qa == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "QA evidence is unavailable", nil)
			}
			passed, err := s.qa.HasPassingQA(ctx, scope.OrganizationID, scope.ProjectID, before.ID, pullRequestHead(pullRequest, pullRequestLoaded))
			if err != nil {
				return err
			}
			if !passed {
				return apperr.New(apperr.CodeInvalidArgument, 422, "all required QA tests must pass before release", nil)
			}
		}
		if riskRule.RequireRollbackPlan && !containsWorkflowRule(transition.Required, workflow.RequireRollbackPlan) {
			if err := s.requireRollbackPlan(ctx, scope, before.ID); err != nil {
				return err
			}
		}
		if riskRule.RequireProductionVerification && transition.To == workflow.Done && !containsWorkflowRule(transition.Required, workflow.RequireProductionVerification) {
			if s.deployment == nil {
				return apperr.New(apperr.CodeInvalidArgument, 422, "production verification evidence is unavailable", nil)
			}
			verified, err := s.deployment.HasProductionVerification(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
			if err != nil {
				return err
			}
			if !verified {
				return apperr.New(apperr.CodeInvalidArgument, 422, "production verification is required before completion", nil)
			}
		}
	}
	if before.RiskLevel == RiskHigh || before.RiskLevel == RiskCritical {
		if targetExists && targetStatus.IsTerminal && targetStatus.Category == "DONE" && !containsWorkflowRule(transition.Required, workflow.RequireRollbackPlan) {
			if err := s.requireRollbackPlan(ctx, scope, before.ID); err != nil {
				return err
			}
		}
	}
	if targetExists && targetStatus.IsTerminal && targetStatus.Category == "DONE" && s.branchPlan != nil {
		hasPlan, err := s.branchPlan.HasPlan(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
		if err != nil {
			return err
		}
		if hasPlan {
			synced, err := s.branchPlan.HasSync(ctx, scope.OrganizationID, scope.ProjectID, before.ID)
			if err != nil {
				return err
			}
			if !synced {
				return apperr.New(apperr.CodeInvalidArgument, 422, "hotfix or release branch sync-backport must be merged before completion", nil)
			}
		}
	}
	return nil
}

func (s *Service) requireRollbackPlan(ctx context.Context, scope Scope, workItemID string) error {
	spec, err := s.spec.Get(ctx, scope.OrganizationID, scope.ProjectID, workItemID)
	if err != nil {
		return err
	}
	field, ok := spec.Fields[specification.RollbackPlan]
	if !ok || strings.TrimSpace(field.Value) == "" || field.VerificationStatus != specification.VerifiedByHuman || field.Provenance == specification.AIHypothesis || field.Provenance == specification.AIInferred {
		return apperr.New(apperr.CodeSpecificationNotReady, 422, "a human-verified rollback plan is required", nil)
	}
	return nil
}

func isReleaseTransition(status string) bool {
	switch status {
	case workflow.ReadyForRelease, workflow.Deploying, workflow.ProductionVerify, workflow.Done:
		return true
	default:
		return false
	}
}

func deliveryRiskRule(policy DeliveryPolicy, level RiskLevel) RiskRule {
	level = normalizedRiskLevel(level)
	if policy.RiskRules == nil {
		return RiskRule{}
	}
	if rule, ok := policy.RiskRules[level]; ok {
		return rule
	}
	return policy.RiskRules[RiskNormal]
}

func pullRequestHead(evidence PullRequestEvidence, loaded bool) string {
	if loaded {
		return evidence.HeadSHA
	}
	return ""
}

func validatePullRequestReview(evidence PullRequestEvidence, policy ReviewPolicy) error {
	if evidence.ChangesRequested {
		return apperr.New(apperr.CodeInvalidArgument, 422, "pull request has requested changes", nil)
	}
	approvers := make(map[string]struct{}, len(evidence.ApprovedByLogins))
	for _, login := range evidence.ApprovedByLogins {
		login = strings.ToLower(strings.TrimSpace(login))
		if login == "" || sameIdentityValue(login, evidence.AuthorLogin) {
			continue
		}
		approvers[login] = struct{}{}
	}
	minApprovals := policy.MinApprovals
	for _, rule := range policy.PathRules {
		matched := false
		for _, changed := range evidence.ChangedPaths {
			if pathPatternMatch(rule.Pattern, changed) {
				matched = true
				break
			}
		}
		if matched {
			if rule.MinApprovals > minApprovals {
				minApprovals = rule.MinApprovals
			}
			if err := requireReviewerRoles(evidence, rule.RequiredRoles, approvers); err != nil {
				return err
			}
		}
	}
	if len(approvers) < minApprovals {
		return apperr.New(apperr.CodeInvalidArgument, 422, "pull request does not have the required independent approvals", map[string]any{"required": minApprovals, "approved": len(approvers)})
	}
	return requireReviewerRoles(evidence, policy.RequiredRoles, approvers)
}

func pathPatternMatch(pattern, value string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	valueParts := strings.Split(strings.Trim(value, "/"), "/")
	var match func(int, int) bool
	match = func(patternIndex, valueIndex int) bool {
		if patternIndex == len(patternParts) {
			return valueIndex == len(valueParts)
		}
		if patternParts[patternIndex] == "**" {
			return match(patternIndex+1, valueIndex) || (valueIndex < len(valueParts) && match(patternIndex, valueIndex+1))
		}
		if valueIndex == len(valueParts) {
			return false
		}
		ok, err := path.Match(patternParts[patternIndex], valueParts[valueIndex])
		return err == nil && ok && match(patternIndex+1, valueIndex+1)
	}
	return match(0, 0)
}

func requireReviewerRoles(evidence PullRequestEvidence, roles []string, approvers map[string]struct{}) error {
	for _, required := range roles {
		required = strings.ToLower(strings.TrimSpace(required))
		found := false
		for login := range approvers {
			for _, role := range evidence.ReviewerRoles[login] {
				if strings.EqualFold(strings.TrimSpace(role), required) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return apperr.New(apperr.CodeInvalidArgument, 422, "required reviewer role is missing", map[string]any{"role": required})
		}
	}
	return nil
}

func sameIdentity(actor identity.Actor, value string) bool {
	return sameIdentityValue(actor.Login, value) || sameIdentityValue(actor.ID, value)
}

func sameIdentityValue(left, right string) bool {
	left, right = strings.ToLower(strings.TrimSpace(left)), strings.ToLower(strings.TrimSpace(right))
	return left != "" && right != "" && left == right
}

func containsIdentity(values []string, actor identity.Actor) bool {
	for _, value := range values {
		if sameIdentity(actor, value) {
			return true
		}
	}
	return false
}

func containsWorkflowRule(rules []workflow.RuleType, wanted workflow.RuleType) bool {
	for _, rule := range rules {
		if rule == wanted {
			return true
		}
	}
	return false
}

func validRiskLevel(level RiskLevel) bool {
	switch normalizedRiskLevel(level) {
	case RiskLow, RiskNormal, RiskHigh, RiskCritical:
		return true
	default:
		return false
	}
}

func normalizedRiskLevel(level RiskLevel) RiskLevel {
	level = RiskLevel(strings.ToUpper(strings.TrimSpace(string(level))))
	if level == "" {
		return RiskNormal
	}
	return level
}

func (s *Service) Readiness(ctx context.Context, scope Scope, actor identity.Actor, item *WorkItem) (specification.Readiness, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return specification.Readiness{}, err
	}
	return s.spec.Readiness(ctx, scope.OrganizationID, scope.ProjectID, item.ID, string(item.Type), item.Title, s.repositoryIDFor(ctx, scope, item))
}

// RequiredRegressionTestPositions exposes only the specification data needed
// by the QA evidence gate; optional and informational cases never block a
// release.
func (s *Service) RequiredRegressionTestPositions(ctx context.Context, organizationID, projectID, workItemID string) ([]int, error) {
	spec, err := s.spec.Get(ctx, organizationID, projectID, workItemID)
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, nil
	}
	positions := make([]int, 0, len(spec.RegressionCases))
	for index, testCase := range spec.RegressionCases {
		if testCase.GateClass == "" || testCase.GateClass == specification.GateRequired {
			positions = append(positions, index+1)
		}
	}
	return positions, nil
}

func (s *Service) repositoryIDFor(ctx context.Context, scope Scope, item *WorkItem) string {
	repositoryID := strings.TrimSpace(item.RepositoryID)
	if repositoryID != "" {
		return repositoryID
	}
	if currentSpec, err := s.spec.Get(ctx, scope.OrganizationID, scope.ProjectID, item.ID); err == nil && currentSpec != nil {
		return strings.TrimSpace(currentSpec.RepositoryID)
	}
	return ""
}

// RepositoryIDFor returns the repository bound to an item or its specification.
// Agent execution uses this shared lookup so a run cannot point at unrelated code.
func (s *Service) RepositoryIDFor(ctx context.Context, scope Scope, item *WorkItem) string {
	return s.repositoryIDFor(ctx, scope, item)
}

func (s *Service) record(ctx context.Context, actor identity.Actor, scope Scope, action string, before, after any) error {
	id, err := ids.New()
	if err != nil {
		return err
	}
	resourceID := ""
	switch item := after.(type) {
	case *WorkItem:
		resourceID = item.ID
	case Comment:
		resourceID = item.ID
	case Link:
		resourceID = item.ID
	case Label:
		resourceID = item.ID
	case map[string]string:
		resourceID = item["label_id"]
	}
	return s.recordResource(ctx, id, actor, scope, action, "work_item", resourceID, before, after)
}

func (s *Service) recordWorkItem(ctx context.Context, actor identity.Actor, scope Scope, workItemID, action string, before, after any) error {
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recordResource(ctx, id, actor, scope, action, "work_item", workItemID, before, after)
}

func (s *Service) recordResource(ctx context.Context, id string, actor identity.Actor, scope Scope, action, resourceType, resourceID string, before, after any) error {
	return s.audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: scope.OrganizationID, Source: actor.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, Before: before, After: after, RequestID: httpapi.RequestID(ctx), CorrelationID: httpapi.CorrelationID(ctx), CreatedAt: s.now().UTC()})
}

func (s *Service) emit(ctx context.Context, event outbox.Event) error {
	if event.ID == "" {
		id, err := ids.New()
		if err != nil {
			return err
		}
		event.ID = id
	}
	return s.outbox.Append(ctx, event)
}

func authorize(scope Scope, actor identity.Actor, capability string) error {
	if strings.TrimSpace(scope.OrganizationID) == "" || strings.TrimSpace(scope.ProjectID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "organization and project scope are required", nil)
	}
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if actor.OrganizationID != scope.OrganizationID {
		return apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}

func (s *Service) CreateComment(ctx context.Context, scope Scope, actor identity.Actor, workItemID, body string) (Comment, error) {
	if err := authorize(scope, actor, identity.CapabilityCommentCreate); err != nil {
		return Comment{}, err
	}
	var result Comment
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.repository.AddComment(txCtx, scope, workItemID, actor.ID, body)
		if err != nil {
			return err
		}
		if err := s.recordWorkItem(txCtx, actor, scope, workItemID, "comment.create", nil, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.comment.created", AggregateType: "work_item", AggregateID: workItemID, IdempotencyKey: "work_item.comment.created:" + result.ID, Payload: map[string]any{"project_id": scope.ProjectID, "comment_id": result.ID}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) Comments(ctx context.Context, scope Scope, actor identity.Actor, workItemID string) ([]Comment, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.repository.ListComments(ctx, scope, workItemID)
}

func (s *Service) UpdateComment(ctx context.Context, scope Scope, actor identity.Actor, workItemID, commentID, body string) (Comment, error) {
	if err := authorize(scope, actor, identity.CapabilityCommentCreate); err != nil {
		return Comment{}, err
	}
	var result Comment
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		before, err := s.commentBefore(txCtx, scope, workItemID, commentID)
		if err != nil {
			return err
		}
		result, err = s.repository.UpdateComment(txCtx, scope, commentID, actor.ID, body)
		if err != nil {
			return err
		}
		if err := s.recordWorkItem(txCtx, actor, scope, workItemID, "comment.update", before, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.comment.updated", AggregateType: "work_item", AggregateID: workItemID, IdempotencyKey: "work_item.comment.updated:" + result.ID + ":" + result.UpdatedAt.UTC().Format(time.RFC3339Nano), Payload: map[string]any{"project_id": scope.ProjectID, "comment_id": result.ID}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) DeleteComment(ctx context.Context, scope Scope, actor identity.Actor, workItemID, commentID string) (Comment, error) {
	if err := authorize(scope, actor, identity.CapabilityCommentCreate); err != nil {
		return Comment{}, err
	}
	var result Comment
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		before, err := s.commentBefore(txCtx, scope, workItemID, commentID)
		if err != nil {
			return err
		}
		result, err = s.repository.DeleteComment(txCtx, scope, commentID, actor.ID)
		if err != nil {
			return err
		}
		if err := s.recordWorkItem(txCtx, actor, scope, workItemID, "comment.delete", before, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.comment.deleted", AggregateType: "work_item", AggregateID: workItemID, IdempotencyKey: "work_item.comment.deleted:" + result.ID + ":" + result.UpdatedAt.UTC().Format(time.RFC3339Nano), Payload: map[string]any{"project_id": scope.ProjectID, "comment_id": result.ID}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) commentBefore(ctx context.Context, scope Scope, workItemID, commentID string) (Comment, error) {
	comments, err := s.repository.ListComments(ctx, scope, workItemID)
	if err != nil {
		return Comment{}, err
	}
	for _, comment := range comments {
		if comment.ID == commentID {
			return comment, nil
		}
	}
	return Comment{}, apperr.New(apperr.CodeNotFound, 404, "comment not found", nil)
}

func (s *Service) CreateLink(ctx context.Context, scope Scope, actor identity.Actor, sourceID, targetID, relationType string) (Link, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return Link{}, err
	}
	var result Link
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.repository.AddLink(txCtx, scope, sourceID, targetID, relationType)
		if err != nil {
			return err
		}
		if err := s.recordWorkItem(txCtx, actor, scope, sourceID, "link.create", nil, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.link.created", AggregateType: "work_item", AggregateID: sourceID, IdempotencyKey: "work_item.link.created:" + result.ID, Payload: map[string]any{"project_id": scope.ProjectID, "target_id": targetID, "relation_type": result.RelationType}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) Links(ctx context.Context, scope Scope, actor identity.Actor, workItemID string) ([]Link, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.repository.ListLinks(ctx, scope, workItemID)
}

func (s *Service) RemoveLink(ctx context.Context, scope Scope, actor identity.Actor, workItemID, linkID string) error {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return err
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repository.RemoveLink(txCtx, scope, workItemID, linkID); err != nil {
			return err
		}
		if err := s.recordWorkItem(txCtx, actor, scope, workItemID, "link.delete", map[string]any{"link_id": linkID}, nil); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.link.deleted", AggregateType: "work_item", AggregateID: workItemID, IdempotencyKey: "work_item.link.deleted:" + linkID, Payload: map[string]any{"project_id": scope.ProjectID, "link_id": linkID}, OccurredAt: s.now().UTC()})
	})
}

func (s *Service) AddLabel(ctx context.Context, scope Scope, actor identity.Actor, workItemID, name, color string) (Label, error) {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return Label{}, err
	}
	if strings.TrimSpace(name) == "" || len(strings.TrimSpace(name)) > 50 {
		return Label{}, apperr.New(apperr.CodeInvalidArgument, 422, "label name must be 1-50 characters", nil)
	}
	var result Label
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.repository.AddLabel(txCtx, scope, workItemID, name, color)
		if err != nil {
			return err
		}
		if err := s.recordWorkItem(txCtx, actor, scope, workItemID, "label.create", nil, result); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{OrganizationID: scope.OrganizationID, EventType: "work_item.label.created", AggregateType: "work_item", AggregateID: workItemID, IdempotencyKey: "work_item.label.created:" + result.ID, Payload: map[string]any{"project_id": scope.ProjectID, "label_id": result.ID}, OccurredAt: s.now().UTC()})
	})
	return result, err
}

func (s *Service) RemoveLabel(ctx context.Context, scope Scope, actor identity.Actor, workItemID, labelID string) error {
	if err := authorize(scope, actor, identity.CapabilityWorkItemEdit); err != nil {
		return err
	}
	eventID, err := ids.New()
	if err != nil {
		return err
	}
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repository.RemoveLabel(txCtx, scope, workItemID, labelID); err != nil {
			return err
		}
		beforeAfter := map[string]string{"work_item_id": workItemID, "label_id": labelID}
		if err := s.recordWorkItem(txCtx, actor, scope, workItemID, "label.remove", beforeAfter, beforeAfter); err != nil {
			return err
		}
		return s.emit(txCtx, outbox.Event{ID: eventID, OrganizationID: scope.OrganizationID, EventType: "work_item.label.removed", AggregateType: "work_item", AggregateID: workItemID, IdempotencyKey: "work_item.label.removed:" + eventID, Payload: map[string]any{"project_id": scope.ProjectID, "label_id": labelID}, OccurredAt: s.now().UTC()})
	})
	return err
}

func (s *Service) Labels(ctx context.Context, scope Scope, actor identity.Actor, workItemID string) ([]Label, error) {
	if err := authorize(scope, actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.repository.ListLabels(ctx, scope, workItemID)
}

const (
	PriorityLowest  = "LOWEST"
	PriorityLow     = "LOW"
	PriorityMedium  = "MEDIUM"
	PriorityHigh    = "HIGH"
	PriorityHighest = "HIGHEST"
)

func validPriority(priority string) bool {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case PriorityLowest, PriorityLow, PriorityMedium, PriorityHigh, PriorityHighest:
		return true
	default:
		return false
	}
}
