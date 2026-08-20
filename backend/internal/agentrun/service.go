package agentrun

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
	"github.com/google/uuid"
)

type Service struct {
	store       Store
	workItems   *workitem.Service
	recorder    MutationRecorder
	transaction TransactionRunner
	now         func() time.Time
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
}

func NewService(store Store, workItems *workitem.Service, options ...Options) *Service {
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
	return &Service{store: store, workItems: workItems, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) Create(ctx context.Context, actor identity.Actor, input CreateInput) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkItemID) == "" || strings.TrimSpace(input.RepositoryID) == "" || strings.TrimSpace(input.AgentProvider) == "" || strings.TrimSpace(input.AgentName) == "" {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id, work_item_id, repository_id, agent_provider and agent_name are required", nil)
	}
	specificationVersion, err := s.validateReadyWorkItem(ctx, actor, input.ProjectID, input.WorkItemID, input.RepositoryID, len(input.ExecutionInputs.TestCasePositions) > 0 || autonomousExecution(input.ExecutionPolicy))
	if err != nil {
		return Run{}, err
	}
	input.ExecutionInputs = normalizeExecutionInputs(input.ExecutionInputs)
	if specificationVersion > 0 {
		input.ExecutionInputs.SpecificationVersion = specificationVersion
	}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.Create(txCtx, actor.OrganizationID, input)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "create", result.ID, nil, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, result.ID, "agent_run.created", map[string]any{"project_id": result.ProjectID, "work_item_id": result.WorkItemID})
	})
	return result, err
}

func (s *Service) Get(ctx context.Context, actor identity.Actor, projectID, id string) (Run, []Step, []Artifact, error) {
	if err := requireRead(actor); err != nil {
		return Run{}, nil, nil, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, nil, nil, err
	}
	return s.store.Get(ctx, actor.OrganizationID, projectID, id)
}

func (s *Service) List(ctx context.Context, actor identity.Actor, projectID, workItemID string) ([]Run, error) {
	if err := requireRead(actor); err != nil {
		return nil, err
	}
	if err := requireProjectID(projectID); err != nil {
		return nil, err
	}
	return s.store.List(ctx, actor.OrganizationID, projectID, strings.TrimSpace(workItemID))
}

func (s *Service) Approve(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAgentApprove); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	run, _, _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != Queued {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "only queued AgentRuns can be approved", nil)
	}
	if err := s.validateApprovalFingerprint(run); err != nil {
		return Run{}, err
	}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.Approve(txCtx, actor.OrganizationID, id, actor.ID, "approve"); err != nil {
			return err
		}
		run.Approved = true
		result = run
		if err := s.record(txCtx, actor, "approve", id, run, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.approved", nil)
	})
	return result, err
}

func (s *Service) Start(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	run, _, _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if !run.Approved {
		return Run{}, apperr.New(apperr.CodeForbidden, 403, "human approval is required before starting an AgentRun", nil)
	}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		specificationVersion, err := s.validateReadyWorkItem(txCtx, actor, run.ProjectID, run.WorkItemID, run.RepositoryID, len(run.ExecutionInputs.TestCasePositions) > 0)
		if err != nil {
			return err
		}
		if run.ExecutionInputs.SpecificationVersion > 0 && specificationVersion > 0 && run.ExecutionInputs.SpecificationVersion != specificationVersion {
			return apperr.New(apperr.CodeStaleSpecification, 409, "the AgentRun approval is stale because the specification changed", map[string]any{"approved_version": run.ExecutionInputs.SpecificationVersion, "current_version": specificationVersion})
		}
		if err := s.validateApprovalFingerprint(run); err != nil {
			return err
		}
		result, err = s.store.Transition(txCtx, actor.OrganizationID, id, Queued, Preparing)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "start", id, run, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.started", map[string]any{"status": result.Status})
	})
	return result, err
}

// StartAutonomousForOrganization is used by the application orchestrator.
// Keeping the organization argument here prevents an internal worker from
// accidentally crossing tenant boundaries.
func (s *Service) StartAutonomousForOrganization(ctx context.Context, organizationID, projectID, id string) (Run, error) {
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	if strings.TrimSpace(organizationID) == "" {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "organization_id is required", nil)
	}
	run, _, _, err := s.store.Get(ctx, organizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != Queued {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "only queued autonomous AgentRuns can be started", map[string]any{"status": run.Status})
	}
	if autonomous, ok := run.ExecutionPolicy["autonomous"].(bool); !ok || !autonomous {
		return Run{}, apperr.New(apperr.CodeForbidden, 403, "AgentRun is not authorized by an autonomous policy", nil)
	}
	actor := identity.Actor{Type: "system", ID: "autonomous-orchestrator", Source: "worker", OrganizationID: organizationID, Capabilities: map[string]bool{"*": true}}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		specificationVersion, validationErr := s.validateReadyWorkItem(txCtx, actor, run.ProjectID, run.WorkItemID, run.RepositoryID, len(run.ExecutionInputs.TestCasePositions) > 0 || autonomousExecution(run.ExecutionPolicy))
		if validationErr != nil {
			return validationErr
		}
		if run.ExecutionInputs.SpecificationVersion > 0 && specificationVersion > 0 && run.ExecutionInputs.SpecificationVersion != specificationVersion {
			return apperr.New(apperr.CodeStaleSpecification, 409, "the autonomous AgentRun specification is stale", map[string]any{"approved_version": run.ExecutionInputs.SpecificationVersion, "current_version": specificationVersion})
		}
		if err := s.validateApprovalFingerprint(run); err != nil {
			return err
		}
		result, err = s.store.Transition(txCtx, organizationID, id, Queued, Preparing)
		if err != nil {
			return err
		}
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["authorization"] = "autonomous_policy"
		if err := s.record(txCtx, actor, "autonomous_start", id, run, result); err != nil {
			return err
		}
		return s.emit(txCtx, organizationID, id, "agent_run.autonomous_started", map[string]any{"project_id": result.ProjectID, "work_item_id": result.WorkItemID})
	})
	return result, err
}

func (s *Service) Heartbeat(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	if _, _, _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id); err != nil {
		return Run{}, err
	}
	return s.store.Heartbeat(ctx, actor.OrganizationID, id, s.now().UTC())
}

func (s *Service) Resume(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	run, _, _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != Interrupted {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "only interrupted AgentRuns can be resumed", map[string]any{"status": run.Status})
	}
	specificationVersion, err := s.validateReadyWorkItem(ctx, actor, run.ProjectID, run.WorkItemID, run.RepositoryID, len(run.ExecutionInputs.TestCasePositions) > 0)
	if err != nil {
		return Run{}, err
	}
	if run.ExecutionInputs.SpecificationVersion > 0 && specificationVersion > 0 && run.ExecutionInputs.SpecificationVersion != specificationVersion {
		return Run{}, apperr.New(apperr.CodeStaleSpecification, 409, "the AgentRun approval is stale because the specification changed", map[string]any{"approved_version": run.ExecutionInputs.SpecificationVersion, "current_version": specificationVersion})
	}
	if err := s.validateApprovalFingerprint(run); err != nil {
		return Run{}, err
	}
	if !run.Approved {
		return Run{}, apperr.New(apperr.CodeForbidden, 403, "human approval is required before resuming an AgentRun", nil)
	}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		result, err = s.store.Transition(txCtx, actor.OrganizationID, id, Interrupted, Preparing)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "resume", id, run, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.resumed", map[string]any{"status": result.Status})
	})
	return result, err
}

func (s *Service) ReconcileStale(ctx context.Context) ([]Run, error) {
	runs, err := s.store.ReconcileStale(ctx, s.now().UTC())
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		actor := identity.Actor{Type: "system", ID: "agent-run-watchdog", OrganizationID: run.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
		if err := s.record(ctx, actor, "interrupt", run.ID, nil, run); err != nil {
			return nil, err
		}
		if err := s.emit(ctx, run.OrganizationID, run.ID, "agent_run.interrupted", map[string]any{"reason": run.InterruptionReason}); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

func (s *Service) Cancel(ctx context.Context, actor identity.Actor, projectID, id string) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	run, _, _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if !isActive(run.Status) {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "AgentRun is no longer active", nil)
	}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		result, err = s.store.Transition(txCtx, actor.OrganizationID, id, run.Status, Cancelled)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "cancel", id, run, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.cancelled", nil)
	})
	return result, err
}

func (s *Service) Transition(ctx context.Context, actor identity.Actor, projectID, id string, to Status) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	run, _, _, err := s.store.Get(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return Run{}, err
	}
	if !validTransition(run.Status, to) {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "AgentRun transition is invalid", map[string]any{"from": run.Status, "to": to})
	}
	var result Run
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		result, err = s.store.Transition(txCtx, actor.OrganizationID, id, run.Status, to)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "transition", id, run, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.transitioned", map[string]any{"from": run.Status, "to": result.Status})
	})
	return result, err
}

func (s *Service) AttachResult(ctx context.Context, actor identity.Actor, projectID, id string, input ResultInput) (Run, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	if strings.TrimSpace(input.CommitSHA) != "" && len(input.CommitSHA) > 128 {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "commit_sha is too long", nil)
	}
	if len(strings.TrimSpace(input.PullRequestID)) > 128 {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "pull_request_id is too long", nil)
	}
	if strings.TrimSpace(input.PullRequestID) != "" {
		if _, err := uuid.Parse(strings.TrimSpace(input.PullRequestID)); err != nil {
			return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "pull_request_id must be a UUID", nil)
		}
	}
	if input.Error != nil && len(*input.Error) > 4000 {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "error is too long", nil)
	}
	if input.Result != nil {
		encoded, err := json.Marshal(input.Result)
		if err != nil || len(encoded) > 512*1024 {
			return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "agent result is invalid or exceeds the size limit", nil)
		}
	}
	if input.Metadata != nil {
		encoded, err := json.Marshal(input.Metadata)
		if err != nil || len(encoded) > 64*1024 {
			return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "agent metadata is invalid or exceeds the size limit", nil)
		}
	}
	var result Run
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, _, _, err := s.store.Get(txCtx, actor.OrganizationID, projectID, id)
		if err != nil {
			return err
		}
		if !isActive(before.Status) {
			return apperr.New(apperr.CodeConflict, 409, "completed AgentRuns cannot receive more results", map[string]any{"status": before.Status})
		}
		result, err = s.store.UpdateResult(txCtx, actor.OrganizationID, id, input)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "result", id, before, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.result_attached", map[string]any{"commit_sha": result.CommitSHA, "pull_request_id": result.PullRequestID})
	})
	return result, err
}

func (s *Service) RecordTestResults(ctx context.Context, actor identity.Actor, projectID, id string, input TestResultsInput) (Run, error) {
	if actor.Type == "human" {
		if !actor.Has(identity.CapabilityWorkItemEdit) && !actor.Has(identity.CapabilityAgentApprove) {
			return Run{}, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilityWorkItemEdit})
		}
	} else if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Run{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Run{}, err
	}
	if len(input.Cases) > 200 {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "at most 200 test cases can be recorded at once", nil)
	}
	if len(input.ReviewNote) > 4000 {
		return Run{}, apperr.New(apperr.CodeInvalidArgument, 422, "test review note is too long", nil)
	}

	var result Run
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, _, _, err := s.store.Get(txCtx, actor.OrganizationID, projectID, id)
		if err != nil {
			return err
		}
		if before.Status == Cancelled {
			return apperr.New(apperr.CodeConflict, 409, "cancelled AgentRuns cannot receive test results", nil)
		}
		merged, err := mergeTestResults(before, input, actor.ID, s.now().UTC())
		if err != nil {
			return err
		}
		result, err = s.store.UpdateTestResults(txCtx, actor.OrganizationID, id, merged)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "test_results", id, before, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.test_results_recorded", map[string]any{"project_id": projectID, "case_count": len(merged.Cases)})
	})
	return result, err
}

func mergeTestResults(run Run, input TestResultsInput, actorID string, now time.Time) (TestResultSet, error) {
	current := TestResultSet{}
	if raw, err := json.Marshal(run.Result); err == nil {
		var stored struct {
			Cases      []TestCaseResult `json:"test_cases"`
			ReviewNote string           `json:"test_review_note"`
		}
		_ = json.Unmarshal(raw, &stored)
		current.Cases = stored.Cases
		current.ReviewNote = stored.ReviewNote
	}
	byPosition := make(map[int]TestCaseResult, len(current.Cases)+len(input.Cases))
	for _, item := range current.Cases {
		byPosition[item.Position] = item
	}
	seen := make(map[int]struct{}, len(input.Cases))
	for _, item := range input.Cases {
		if item.Position < 1 || item.Position > 10000 {
			return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "test case position is invalid", nil)
		}
		if _, exists := seen[item.Position]; exists {
			return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "test case positions must be unique", nil)
		}
		seen[item.Position] = struct{}{}
		if item.Status != TestNotRun && item.Status != TestPassed && item.Status != TestFailed && item.Status != TestBlocked {
			return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "test case status is invalid", map[string]any{"status": item.Status})
		}
		if len(item.Note) > 4000 {
			return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "test case note is too long", nil)
		}
		if len(item.EvidenceRefs) > 30 {
			return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "at most 30 test evidence references are allowed", nil)
		}
		previous := byPosition[item.Position]
		if (item.Status == TestFailed || item.Status == TestBlocked) && strings.TrimSpace(item.Note) == "" {
			if strings.TrimSpace(previous.Note) == "" {
				return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "failed or blocked test cases require a note", map[string]any{"position": item.Position})
			}
		}
		note := strings.TrimSpace(item.Note)
		if note == "" {
			note = previous.Note
		}
		evidence := make([]string, 0, len(item.EvidenceRefs))
		for _, reference := range item.EvidenceRefs {
			reference = strings.TrimSpace(reference)
			if reference == "" {
				continue
			}
			if len(reference) > 512 {
				return TestResultSet{}, apperr.New(apperr.CodeInvalidArgument, 422, "test evidence reference is too long", nil)
			}
			evidence = append(evidence, reference)
		}
		if item.EvidenceRefs == nil {
			evidence = append([]string(nil), previous.EvidenceRefs...)
		}
		byPosition[item.Position] = TestCaseResult{Position: item.Position, Status: item.Status, Note: note, EvidenceRefs: evidence, UpdatedBy: actorID, UpdatedAt: now}
	}
	current.Cases = current.Cases[:0]
	for _, item := range byPosition {
		current.Cases = append(current.Cases, item)
	}
	sort.Slice(current.Cases, func(i, j int) bool { return current.Cases[i].Position < current.Cases[j].Position })
	if strings.TrimSpace(input.ReviewNote) != "" {
		current.ReviewNote = strings.TrimSpace(input.ReviewNote)
	}
	return current, nil
}

func (s *Service) AttachStep(ctx context.Context, actor identity.Actor, projectID, id string, step Step) (Step, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Step{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Step{}, err
	}
	if step.Sequence < 1 || strings.TrimSpace(step.Phase) == "" || strings.TrimSpace(step.Status) == "" {
		return Step{}, apperr.New(apperr.CodeInvalidArgument, 422, "step sequence, phase and status are required", nil)
	}
	var result Step
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, _, _, err := s.store.Get(txCtx, actor.OrganizationID, projectID, id)
		if err != nil {
			return err
		}
		if !isActive(before.Status) {
			return apperr.New(apperr.CodeConflict, 409, "completed AgentRuns cannot receive more steps", map[string]any{"status": before.Status})
		}
		result, err = s.store.AddStep(txCtx, actor.OrganizationID, id, step)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "step", id, nil, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.step_attached", map[string]any{"sequence": result.Sequence})
	})
	return result, err
}

func (s *Service) AttachArtifact(ctx context.Context, actor identity.Actor, projectID, id string, artifact Artifact) (Artifact, error) {
	if err := require(actor, identity.CapabilityAgentExecute); err != nil {
		return Artifact{}, err
	}
	if err := requireProjectID(projectID); err != nil {
		return Artifact{}, err
	}
	if strings.TrimSpace(artifact.ArtifactType) == "" || strings.TrimSpace(artifact.Name) == "" || artifact.SizeBytes < 0 || artifact.SizeBytes > 50<<20 {
		return Artifact{}, apperr.New(apperr.CodeInvalidArgument, 422, "artifact type, name and a bounded size are required", nil)
	}
	var result Artifact
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, _, _, err := s.store.Get(txCtx, actor.OrganizationID, projectID, id)
		if err != nil {
			return err
		}
		if !isActive(before.Status) {
			return apperr.New(apperr.CodeConflict, 409, "completed AgentRuns cannot receive more artifacts", map[string]any{"status": before.Status})
		}
		result, err = s.store.AddArtifact(txCtx, actor.OrganizationID, id, artifact)
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "artifact", id, nil, result); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, id, "agent_run.artifact_attached", map[string]any{"artifact_type": result.ArtifactType})
	})
	return result, err
}

func (s *Service) validateReadyWorkItem(ctx context.Context, actor identity.Actor, projectID, workItemID, repositoryID string, allowInProgress bool) (int, error) {
	if s.workItems == nil {
		return 0, nil
	}
	scope := workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}
	item, err := s.workItems.Get(ctx, scope, actor, workItemID)
	if err != nil {
		return 0, err
	}
	if item.Status != "READY" && (!allowInProgress || item.Status != "IN_PROGRESS") {
		return 0, apperr.New(apperr.CodeConflict, 409, "an AgentRun can only start from a READY work item or continue unresolved test cases in IN_PROGRESS", map[string]any{"status": item.Status})
	}
	linkedRepositoryID := s.workItems.RepositoryIDFor(ctx, scope, item)
	if linkedRepositoryID == "" || linkedRepositoryID != strings.TrimSpace(repositoryID) {
		return 0, apperr.New(apperr.CodeConflict, 409, "the AgentRun repository must match the READY work item repository", map[string]any{"work_item_repository_id": linkedRepositoryID})
	}
	readiness, err := s.workItems.Readiness(ctx, scope, actor, item)
	if err != nil {
		return 0, err
	}
	if !readiness.Ready {
		return 0, apperr.New(apperr.CodeSpecificationNotReady, 422, "the work item specification is not ready for an AgentRun", map[string]any{"missing": readiness.Missing})
	}
	return readiness.SpecificationVersion, nil
}

func (s *Service) validateApprovalFingerprint(run Run) error {
	if run.ApprovalFingerprintVersion != ApprovalFingerprintVersion || strings.TrimSpace(run.ApprovalFingerprint) == "" {
		return apperr.New(apperr.CodeConflict, 409, "AgentRun approval fingerprint is missing or unsupported", map[string]any{"fingerprint_version": run.ApprovalFingerprintVersion})
	}
	fingerprint, err := fingerprintForRun(run)
	if err != nil {
		return apperr.New(apperr.CodeInternal, 500, "AgentRun approval fingerprint could not be recomputed", nil)
	}
	if fingerprint != run.ApprovalFingerprint {
		return apperr.New(apperr.CodeConflict, 409, "AgentRun approval is stale because execution inputs or policy changed", nil)
	}
	return nil
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "agent_run", ResourceID: resourceID, Before: before, After: after, RequestID: httpapi.RequestID(ctx), CorrelationID: httpapi.CorrelationID(ctx), CreatedAt: s.now().UTC()})
}

func (s *Service) emit(ctx context.Context, organizationID, runID, eventType string, payload map[string]any) error {
	if s.recorder.Outbox == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: organizationID, EventType: eventType, AggregateType: "agent_run", AggregateID: runID, IdempotencyKey: eventType + ":" + runID + ":" + id, Payload: payload, OccurredAt: s.now().UTC()})
}

func validTransition(from, to Status) bool {
	switch from {
	case Queued:
		return to == Preparing
	case Preparing:
		return to == Planning || to == Cancelled
	case Planning:
		return to == Investigating || to == Cancelled
	case Investigating:
		return to == Implementing || to == Cancelled
	case Implementing:
		return to == Testing || to == Cancelled
	case Testing:
		return to == Reviewing || to == Failed || to == Cancelled
	case Reviewing:
		return to == Completed || to == Failed || to == Cancelled
	default:
		return false
	}
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
func requireRead(actor identity.Actor) error { return require(actor, identity.CapabilityProjectRead) }

func requireProjectID(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	return nil
}

func autonomousExecution(policy map[string]any) bool {
	value, ok := policy["autonomous"].(bool)
	return ok && value
}
