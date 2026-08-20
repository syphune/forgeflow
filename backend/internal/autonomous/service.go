package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/forgeflow/forgeflow/backend/internal/runner"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type Service struct {
	store       Store
	workItems   *workitem.Service
	spec        *specification.Service
	agentRuns   *agentrun.Service
	policy      PolicyProvider
	recorder    MutationRecorder
	transaction TransactionRunner
	now         func() time.Time
}

type PolicyProvider interface {
	Policy(context.Context, string, string) (Policy, error)
}

type MutationRecorder struct {
	Audit  audit.Writer
	Outbox outbox.Writer
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type Options struct {
	Recorder    MutationRecorder
	Transaction TransactionRunner
	Now         func() time.Time
	Policy      PolicyProvider
}

func NewService(store Store, workItems *workitem.Service, spec *specification.Service, agentRuns *agentrun.Service, options ...Options) *Service {
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
	return &Service{store: store, workItems: workItems, spec: spec, agentRuns: agentRuns, policy: configured.Policy, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) Start(ctx context.Context, actor identity.Actor, input StartInput) (Run, error) {
	if err := require(actor, identity.CapabilityAutonomousStart); err != nil {
		return Run{}, err
	}
	input, err := normalizeStartInput(input)
	if err != nil {
		return Run{}, err
	}
	policy := input.Policy
	if s.policy != nil {
		policy, err = s.policy.Policy(ctx, actor.OrganizationID, input.ProjectID)
		if err != nil {
			return Run{}, err
		}
	}
	policy = policy.Normalize()
	if !policy.Enabled {
		return Run{}, apperr.New(apperr.CodeForbidden, 403, "autonomous execution is disabled by project policy", nil)
	}
	if !providerAllowed(policy, input.AgentProvider) {
		return Run{}, apperr.New(apperr.CodeForbidden, 403, "agent provider is not allowed by autonomous policy", map[string]any{"provider": input.AgentProvider})
	}
	if input.WorkItemID == "" {
		if !actor.Has(identity.CapabilityWorkItemCreate) {
			return Run{}, apperr.New(apperr.CodeForbidden, 403, "creating a work item requires work_item.create", nil)
		}
		item, createErr := s.workItems.Create(ctx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: input.ProjectID}, actor, workitem.CreateInput{
			Type:         workitem.Type(strings.ToUpper(input.WorkItemType)),
			Title:        input.Title,
			Description:  input.Objective,
			RepositoryID: input.RepositoryID,
			AssigneeID:   actor.ID,
		})
		if createErr != nil {
			return Run{}, createErr
		}
		input.WorkItemID = item.ID
	}
	item, err := s.workItems.Get(ctx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: input.ProjectID}, actor, input.WorkItemID)
	if err != nil {
		return Run{}, err
	}
	if input.RepositoryID == "" {
		input.RepositoryID = item.RepositoryID
	}
	if input.RepositoryID == "" {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "repository_id is required for autonomous code execution", nil)
	}
	if input.AgentName == "" {
		input.AgentName = input.AgentProvider
	}
	if input.BaseSHA == "" {
		input.BaseSHA = "HEAD"
	}
	if input.Branch == "" {
		input.Branch = "forgeflow/" + strings.ToLower(item.Key)
	}
	var run Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var createErr error
		run, createErr = s.store.Create(txCtx, actor.OrganizationID, input, policy)
		if createErr != nil {
			return createErr
		}
		if createErr = s.record(txCtx, actor, "create", run.ID, nil, run); createErr != nil {
			return createErr
		}
		return s.emit(txCtx, actor.OrganizationID, run.ID, "autonomous_run.created", map[string]any{"project_id": run.ProjectID, "work_item_id": run.WorkItemID, "phase": run.Phase})
	})
	if err != nil {
		return Run{}, err
	}
	readiness, err := s.workItems.Readiness(ctx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: input.ProjectID}, actor, item)
	if err != nil {
		return Run{}, err
	}
	if !readiness.Ready {
		return s.updateState(ctx, actor, run, StateUpdate{Status: StatusWaitingSpecReview, Phase: PhaseSpecification, Gate: GateSpecification, Attempt: run.Attempt, UnresolvedPositions: input.TestCasePositions, LastError: strings.Join(readiness.Missing, ", ")})
	}
	return s.startAttempt(ctx, actor, run, false)
}

func (s *Service) Get(ctx context.Context, actor identity.Actor, projectID, id string) (Run, []Feedback, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return Run{}, nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return Run{}, nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	run, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, nil, err
	}
	feedback, err := s.store.ListFeedback(ctx, actor.OrganizationID, projectID, id)
	return run, feedback, err
}

func (s *Service) List(ctx context.Context, actor identity.Actor, projectID, workItemID string) ([]Run, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.store.List(ctx, actor.OrganizationID, projectID, strings.TrimSpace(workItemID))
}

func (s *Service) Resume(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAutonomousRetry); err != nil {
		return Run{}, err
	}
	run, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != StatusWaitingSpecReview && run.Status != StatusPaused && run.Status != StatusFailed && run.Status != StatusWaitingTestFeedback {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "autonomous run is not waiting for a resumable action", map[string]any{"status": run.Status})
	}
	item, err := s.workItems.Get(ctx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}, actor, run.WorkItemID)
	if err != nil {
		return Run{}, err
	}
	readiness, err := s.workItems.Readiness(ctx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}, actor, item)
	if err != nil {
		return Run{}, err
	}
	if !readiness.Ready {
		return Run{}, apperr.New(apperr.CodeSpecificationNotReady, 422, "specification review is still required", map[string]any{"missing": readiness.Missing})
	}
	return s.startAttempt(ctx, actor, run, true)
}

func (s *Service) Retry(ctx context.Context, actor identity.Actor, projectID, id string, input RetryInput) (Run, error) {
	if err := require(actor, identity.CapabilityAutonomousRetry); err != nil {
		return Run{}, err
	}
	run, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if run.Attempt >= run.MaxAttempts {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "autonomous retry limit has been reached", map[string]any{"max_attempts": run.MaxAttempts})
	}
	if strings.TrimSpace(input.Feedback) != "" {
		if _, err := s.AddFeedback(ctx, actor, projectID, id, FeedbackInput{Source: actor.Type, Note: input.Feedback, TestCasePositions: input.TestCasePositions}); err != nil {
			return Run{}, err
		}
	}
	if len(input.TestCasePositions) > 0 {
		run.UnresolvedPositions = intersection(input.TestCasePositions, run.UnresolvedPositions)
	}
	return s.startAttempt(ctx, actor, run, true)
}

func (s *Service) Cancel(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAutonomousCancel); err != nil {
		return Run{}, err
	}
	run, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if isTerminal(run.Status) {
		return run, nil
	}
	var updated Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if run.CurrentAgentRunID != "" && s.agentRuns != nil {
			if _, cancelErr := s.agentRuns.Cancel(txCtx, systemActor(actor.OrganizationID), projectID, run.CurrentAgentRunID); cancelErr != nil {
				return cancelErr
			}
		}
		var updateErr error
		updated, updateErr = s.updateState(txCtx, actor, run, StateUpdate{Status: StatusCancelled, Phase: run.Phase, Gate: GateNone, Attempt: run.Attempt, UnresolvedPositions: run.UnresolvedPositions, LastError: "cancelled by user", Finished: true})
		return updateErr
	})
	return updated, err
}

func (s *Service) AddFeedback(ctx context.Context, actor identity.Actor, projectID, id string, input FeedbackInput) (Feedback, error) {
	if !actor.Has(identity.CapabilityAutonomousRetry) && !actor.Has(identity.CapabilityWorkItemEdit) && !actor.Has(identity.CapabilityAgentExecute) {
		return Feedback{}, apperr.New(apperr.CodeForbidden, 403, "permission denied", nil)
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(input.Note) == "" || len(input.Note) > 4000 {
		return Feedback{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and a bounded feedback note are required", nil)
	}
	if input.Source == "" {
		input.Source = actor.Type
	}
	if input.Source != "human" && input.Source != "agent" && input.Source != "ci" && input.Source != "system" {
		return Feedback{}, apperr.New(apperr.CodeInvalidArgument, 422, "feedback source is invalid", nil)
	}
	if len(input.TestCasePositions) > 200 || len(input.EvidenceRefs) > 30 {
		return Feedback{}, apperr.New(apperr.CodeInvalidArgument, 422, "feedback references exceed the limit", nil)
	}
	if _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id); err != nil {
		return Feedback{}, err
	}
	feedbackID, err := ids.New()
	if err != nil {
		return Feedback{}, err
	}
	var feedback Feedback
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var addErr error
		feedback, addErr = s.store.AddFeedback(txCtx, actor.OrganizationID, projectID, id, Feedback{ID: feedbackID, Source: input.Source, Note: strings.TrimSpace(input.Note), Severity: strings.TrimSpace(input.Severity), CommitSHA: strings.TrimSpace(input.CommitSHA), TestCasePositions: uniquePositions(input.TestCasePositions), EvidenceRefs: cleanStrings(input.EvidenceRefs), CreatedBy: actor.ID})
		if addErr != nil {
			return addErr
		}
		if addErr = s.record(txCtx, actor, "feedback", id, nil, feedback); addErr != nil {
			return addErr
		}
		return s.emit(txCtx, actor.OrganizationID, id, "autonomous_run.feedback_added", map[string]any{"source": feedback.Source, "test_case_positions": feedback.TestCasePositions})
	})
	if err != nil {
		return Feedback{}, err
	}
	return feedback, nil
}

func (s *Service) RecordTestResults(ctx context.Context, actor identity.Actor, projectID, id string, input agentrun.TestResultsInput) (Run, error) {
	if s.agentRuns == nil {
		return Run{}, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
	}
	autonomousRun, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if autonomousRun.CurrentAgentRunID == "" {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "autonomous run has no active AgentRun", nil)
	}
	var updated Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		child, recordErr := s.agentRuns.RecordTestResults(txCtx, actor, projectID, autonomousRun.CurrentAgentRunID, input)
		if recordErr != nil {
			return recordErr
		}
		unresolved := unresolvedTestPositions(child)
		status, phase, gate := StatusWaitingPRReview, PhasePullRequest, GatePullRequest
		lastError := ""
		if len(unresolved) > 0 {
			status, phase, gate = StatusWaitingTestFeedback, PhaseTesting, GateTestFeedback
			lastError = strings.TrimSpace(input.ReviewNote)
		}
		var updateErr error
		updated, updateErr = s.updateState(txCtx, actor, autonomousRun, StateUpdate{Status: status, Phase: phase, Gate: gate, CurrentAgentRunID: autonomousRun.CurrentAgentRunID, Attempt: autonomousRun.Attempt, UnresolvedPositions: unresolved, LastError: lastError})
		if updateErr != nil {
			return updateErr
		}
		if len(unresolved) > 0 && strings.TrimSpace(input.ReviewNote) != "" {
			_, updateErr = s.AddFeedback(txCtx, actor, projectID, id, FeedbackInput{Source: actor.Type, Note: input.ReviewNote, TestCasePositions: unresolved})
		}
		return updateErr
	})
	return updated, err
}

func (s *Service) HandleAgentRunResult(ctx context.Context, actor identity.Actor, projectID, id string, result runner.Result) (Run, error) {
	run, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	var updated Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if strings.TrimSpace(result.Error) != "" {
			if _, feedbackErr := s.AddFeedback(txCtx, actor, projectID, id, FeedbackInput{Source: "agent", Note: result.Error, Severity: "error"}); feedbackErr != nil {
				return feedbackErr
			}
			var updateErr error
			updated, updateErr = s.updateState(txCtx, actor, run, StateUpdate{Status: StatusWaitingTestFeedback, Phase: PhaseTesting, Gate: GateTestFeedback, CurrentAgentRunID: run.CurrentAgentRunID, Attempt: run.Attempt, UnresolvedPositions: run.UnresolvedPositions, LastError: result.Error})
			return updateErr
		}
		var updateErr error
		updated, updateErr = s.updateState(txCtx, actor, run, StateUpdate{Status: StatusWaitingPRReview, Phase: PhasePullRequest, Gate: GatePullRequest, CurrentAgentRunID: run.CurrentAgentRunID, Attempt: run.Attempt, UnresolvedPositions: run.UnresolvedPositions, LastError: ""})
		return updateErr
	})
	return updated, err
}

func (s *Service) startAttempt(ctx context.Context, actor identity.Actor, run Run, retry bool) (Run, error) {
	if s.agentRuns == nil {
		return Run{}, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
	}
	var updated Run
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, txErr := s.workItems.Get(txCtx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: run.ProjectID}, actor, run.WorkItemID)
		if txErr != nil {
			return txErr
		}
		if item.Status == workflow.Ready {
			if _, txErr = s.workItems.Transition(txCtx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: run.ProjectID}, systemActor(actor.OrganizationID), item.ID, workitem.TransitionInput{TransitionKey: "start_work", ExpectedVersion: item.Version}); txErr != nil {
				return txErr
			}
		}
		positions := append([]int(nil), run.UnresolvedPositions...)
		policy := run.Policy.Normalize()
		baseSHA := run.BaseSHA
		if baseSHA == "" {
			baseSHA = "HEAD"
		}
		branch := run.Branch
		if branch == "" {
			branch = "forgeflow/" + strings.ToLower(strings.ReplaceAll(run.ID, "-", ""))[:12]
		}
		child, createErr := s.agentRuns.Create(txCtx, systemActor(actor.OrganizationID), agentrun.CreateInput{
			ProjectID: run.ProjectID, WorkItemID: run.WorkItemID, RepositoryID: run.RepositoryID,
			AgentProvider: run.AgentProvider, AgentName: run.AgentName, Model: run.Model,
			BaseSHA: baseSHA, Branch: branch,
			ExecutionInputs: agentrun.ExecutionInputs{Prompt: run.Objective + "\n\nAutonomous workflow attempt " + fmt.Sprint(run.Attempt+1) + ". Preserve passing test cases and fix only unresolved feedback.", TestCasePositions: positions, MCPPermissions: policy.MCPPermissions, ExecutionProfile: policy.ExecutionProfile, NetworkPolicy: policy.NetworkPolicy},
			ExecutionPolicy: map[string]any{"autonomous": true, "workflow_id": run.ID, "runtime": policy.Runtime, "max_attempts": policy.MaxAttempts},
		})
		if createErr != nil {
			return createErr
		}
		started, startErr := s.agentRuns.StartAutonomousForOrganization(txCtx, actor.OrganizationID, run.ProjectID, child.ID)
		if startErr != nil {
			return startErr
		}
		status := StatusExecuting
		if retry {
			status = StatusFixing
		}
		var updateErr error
		updated, updateErr = s.updateState(txCtx, actor, run, StateUpdate{Status: status, Phase: PhaseImplementation, Gate: GateRunner, CurrentAgentRunID: started.ID, Attempt: run.Attempt + 1, UnresolvedPositions: positions, LastError: ""})
		return updateErr
	})
	if err != nil {
		return Run{}, err
	}
	return updated, nil
}

func (s *Service) updateState(ctx context.Context, actor identity.Actor, run Run, input StateUpdate) (Run, error) {
	var updated Run
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var updateErr error
		updated, updateErr = s.store.Update(txCtx, actor.OrganizationID, run.ProjectID, run.ID, StateUpdate{ExpectedVersion: run.Version, Status: input.Status, Phase: input.Phase, Gate: input.Gate, CurrentAgentRunID: input.CurrentAgentRunID, PullRequestID: input.PullRequestID, CommitSHA: input.CommitSHA, Attempt: input.Attempt, UnresolvedPositions: input.UnresolvedPositions, LastError: input.LastError, Finished: input.Finished})
		if updateErr != nil {
			return updateErr
		}
		if updateErr = s.record(txCtx, actor, "state", run.ID, run, updated); updateErr != nil {
			return updateErr
		}
		return s.emit(txCtx, actor.OrganizationID, run.ID, "autonomous_run.phase_changed", map[string]any{"status": updated.Status, "phase": updated.Phase, "gate": updated.Gate})
	})
	if err != nil {
		return Run{}, err
	}
	return updated, nil
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: "autonomous_run." + action, ResourceType: "autonomous_run", ResourceID: resourceID, Before: before, After: after, RequestID: httpapi.RequestID(ctx), CorrelationID: httpapi.CorrelationID(ctx), CreatedAt: s.now().UTC()})
}

func (s *Service) emit(ctx context.Context, organizationID, runID, eventType string, payload map[string]any) error {
	if s.recorder.Outbox == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: organizationID, EventType: eventType, AggregateType: "autonomous_run", AggregateID: runID, IdempotencyKey: eventType + ":" + runID + ":" + id, Payload: payload, OccurredAt: s.now().UTC()})
}

func normalizeStartInput(input StartInput) (StartInput, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.WorkItemID = strings.TrimSpace(input.WorkItemID)
	input.WorkItemType = strings.ToUpper(strings.TrimSpace(input.WorkItemType))
	if input.WorkItemType == "" {
		input.WorkItemType = "TASK"
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Objective = strings.TrimSpace(input.Objective)
	input.RepositoryID = strings.TrimSpace(input.RepositoryID)
	input.AgentProvider = strings.ToLower(strings.TrimSpace(input.AgentProvider))
	input.AgentName = strings.TrimSpace(input.AgentName)
	input.Model = strings.TrimSpace(input.Model)
	input.TargetEnvironment = strings.TrimSpace(input.TargetEnvironment)
	if input.ProjectID == "" || input.Objective == "" {
		return StartInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and objective are required", nil)
	}
	if len(input.Objective) > 131072 || len(input.Title) > 160 {
		return StartInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "autonomous objective or title is too long", nil)
	}
	if input.AgentProvider == "" {
		input.AgentProvider = "codex"
	}
	if input.AgentProvider != "codex" && input.AgentProvider != "claude" {
		return StartInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "only codex and claude providers are supported", nil)
	}
	if input.Title == "" {
		input.Title = strings.TrimSpace(strings.SplitN(input.Objective, "\n", 2)[0])
		if len(input.Title) > 120 {
			input.Title = input.Title[:120]
		}
	}
	input.TestCasePositions = uniquePositions(input.TestCasePositions)
	return input, nil
}

func providerAllowed(policy Policy, provider string) bool {
	for _, value := range policy.Providers {
		if strings.EqualFold(value, provider) {
			return true
		}
	}
	return false
}

func systemActor(organizationID string) identity.Actor {
	return identity.Actor{Type: "system", ID: "autonomous-orchestrator", OrganizationID: organizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
}

func require(actor identity.Actor, capability string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}

func uniquePositions(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 1 || value > 10000 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func intersection(wanted, allowed []int) []int {
	set := make(map[int]bool, len(allowed))
	for _, value := range allowed {
		set[value] = true
	}
	result := make([]int, 0, len(wanted))
	for _, value := range uniquePositions(wanted) {
		if set[value] {
			result = append(result, value)
		}
	}
	return result
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && len(value) <= 512 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func unresolvedTestPositions(run agentrun.Run) []int {
	raw, err := json.Marshal(run.Result)
	if err != nil {
		return nil
	}
	var result struct {
		Cases []struct {
			Position int    `json:"position"`
			Status   string `json:"status"`
		} `json:"test_cases"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	positions := make([]int, 0)
	for _, item := range result.Cases {
		if item.Status != string(agentrun.TestPassed) {
			positions = append(positions, item.Position)
		}
	}
	return uniquePositions(positions)
}

func isTerminal(status Status) bool {
	return status == StatusCompleted || status == StatusCancelled
}

var _ = json.Valid
