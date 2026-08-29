package environment

import (
	"context"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type Service struct {
	store           Store
	recorder        MutationRecorder
	transaction     TransactionRunner
	now             func() time.Time
	releaseEvidence ReleaseApprovalEvidenceProvider
}

type ReleaseApprovalEvidence struct {
	Found            bool
	AuthorLogin      string
	HeadSHA          string
	ApprovedByLogins []string
}

type ReleaseApprovalEvidenceProvider interface {
	ReleaseApprovalEvidence(context.Context, string, string, string, string) (ReleaseApprovalEvidence, error)
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

func NewService(store Store, options ...Options) *Service {
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
	return &Service{store: store, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) SetReleaseApprovalEvidence(provider ReleaseApprovalEvidenceProvider) {
	s.releaseEvidence = provider
}

// Policy implements the internal autonomous.PolicyProvider contract.
func (s *Service) Policy(ctx context.Context, organizationID, projectID string) (autonomous.Policy, error) {
	return s.store.GetPolicy(ctx, organizationID, projectID)
}

func (s *Service) GetDeliveryPolicy(ctx context.Context, actor identity.Actor, projectID string) (DeliveryPolicy, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return DeliveryPolicy{}, err
	}
	policy, err := s.store.GetDeliveryPolicy(ctx, actor.OrganizationID, strings.TrimSpace(projectID))
	if err != nil {
		return DeliveryPolicy{}, err
	}
	policy.OrganizationID, policy.ProjectID = actor.OrganizationID, strings.TrimSpace(projectID)
	return policy.Normalize(), nil
}

func (s *Service) DeliveryPolicy(ctx context.Context, organizationID, projectID string) (DeliveryPolicy, error) {
	policy, err := s.store.GetDeliveryPolicy(ctx, organizationID, projectID)
	if err != nil {
		return DeliveryPolicy{}, err
	}
	return policy.Normalize(), nil
}

func (s *Service) SetDeliveryPolicy(ctx context.Context, actor identity.Actor, projectID string, policy DeliveryPolicy) (DeliveryPolicy, error) {
	if err := require(actor, identity.CapabilityProjectManage); err != nil {
		return DeliveryPolicy{}, err
	}
	policy.OrganizationID = actor.OrganizationID
	policy.ProjectID = strings.TrimSpace(projectID)
	if policy.ProjectID == "" {
		return DeliveryPolicy{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return DeliveryPolicy{}, apperr.New(apperr.CodeInvalidArgument, 422, err.Error(), nil)
	}
	var result DeliveryPolicy
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, getErr := s.store.GetDeliveryPolicy(txCtx, actor.OrganizationID, policy.ProjectID)
		if getErr != nil {
			return getErr
		}
		var saveErr error
		result, saveErr = s.store.SetDeliveryPolicy(txCtx, actor.OrganizationID, policy, policy.Version)
		if saveErr != nil {
			return saveErr
		}
		if saveErr = s.record(txCtx, actor, "delivery_policy.updated", result.ID, before, result); saveErr != nil {
			return saveErr
		}
		return s.emit(txCtx, actor.OrganizationID, result.ID, "delivery_policy.updated", map[string]any{"project_id": result.ProjectID, "version": result.Version})
	})
	if err != nil {
		return DeliveryPolicy{}, err
	}
	return result, nil
}

func (s *Service) GetPolicy(ctx context.Context, actor identity.Actor, projectID string) (autonomous.Policy, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return autonomous.Policy{}, err
	}
	return s.Policy(ctx, actor.OrganizationID, projectID)
}

func (s *Service) SetPolicy(ctx context.Context, actor identity.Actor, projectID string, policy autonomous.Policy) (autonomous.Policy, error) {
	if err := require(actor, identity.CapabilityAIPolicyManage); err != nil {
		return autonomous.Policy{}, err
	}
	policy = policy.Normalize()
	if policy.Runtime != "server" && policy.Runtime != "desktop" && policy.Runtime != "auto" {
		return autonomous.Policy{}, apperr.New(apperr.CodeInvalidArgument, 422, "runtime must be server, desktop, or auto", nil)
	}
	if policy.TestScope != "unresolved_only" && policy.TestScope != "full_regression" {
		return autonomous.Policy{}, apperr.New(apperr.CodeInvalidArgument, 422, "test_scope is invalid", nil)
	}
	var result autonomous.Policy
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var updateErr error
		result, updateErr = s.store.SetPolicy(txCtx, actor.OrganizationID, projectID, policy)
		if updateErr != nil {
			return updateErr
		}
		if updateErr = s.record(txCtx, actor, "policy.updated", projectID, nil, result); updateErr != nil {
			return updateErr
		}
		return s.emit(txCtx, actor.OrganizationID, projectID, "ai_policy.updated", map[string]any{"project_id": projectID})
	})
	if err != nil {
		return autonomous.Policy{}, err
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, actor identity.Actor, projectID string) ([]Environment, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.store.List(ctx, actor.OrganizationID, projectID)
}

func (s *Service) Create(ctx context.Context, actor identity.Actor, input CreateInput) (Environment, error) {
	if err := require(actor, identity.CapabilityEnvironmentManage); err != nil {
		return Environment{}, err
	}
	input.Key = strings.ToLower(strings.TrimSpace(input.Key))
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = normalizeKind(input.Kind)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	if input.ProjectID == "" || input.Key == "" || input.Name == "" || len(input.Key) > 64 || len(input.Name) > 120 || !validKind(input.Kind) {
		return Environment{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id, key, name and a valid environment kind are required", nil)
	}
	if input.Kind == "production" {
		input.AutoDeploy = false
		input.RequireApproval = true
	}
	if len(input.SecretRefs) > 30 {
		return Environment{}, apperr.New(apperr.CodeInvalidArgument, 422, "at most 30 secret references are allowed", nil)
	}
	if input.SecretRefs == nil {
		input.SecretRefs = []string{}
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	var item Environment
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var createErr error
		item, createErr = s.store.Create(txCtx, actor.OrganizationID, input)
		if createErr != nil {
			return createErr
		}
		if createErr = s.record(txCtx, actor, "environment.created", item.ID, nil, item); createErr != nil {
			return createErr
		}
		return s.emit(txCtx, actor.OrganizationID, item.ID, "environment.created", map[string]any{"project_id": item.ProjectID, "key": item.Key})
	})
	if err != nil {
		return Environment{}, err
	}
	return item, nil
}

func (s *Service) CreateDeployment(ctx context.Context, actor identity.Actor, input DeploymentInput) (DeploymentRequest, error) {
	if err := require(actor, identity.CapabilityAutonomousRetry); err != nil && !actor.Has(identity.CapabilityEnvironmentManage) && !actor.Has(identity.CapabilityDeploymentApprove) {
		return DeploymentRequest{}, err
	}
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.EnvironmentID) == "" || strings.TrimSpace(input.CommitSHA) == "" {
		return DeploymentRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id, environment_id and commit_sha are required", nil)
	}
	environments, err := s.store.List(ctx, actor.OrganizationID, input.ProjectID)
	if err != nil {
		return DeploymentRequest{}, err
	}
	var target *Environment
	for i := range environments {
		if environments[i].ID == input.EnvironmentID {
			target = &environments[i]
			break
		}
	}
	if target == nil {
		return DeploymentRequest{}, apperr.New(apperr.CodeNotFound, 404, "environment is not linked to this project", nil)
	}
	status := DeploymentPending
	if target.AutoDeploy && !target.RequireApproval && target.Kind != "production" {
		status = DeploymentDispatch
	}
	var item DeploymentRequest
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var createErr error
		item, createErr = s.store.CreateDeployment(txCtx, actor.OrganizationID, input, status)
		if createErr != nil {
			return createErr
		}
		if createErr = s.record(txCtx, actor, "deployment.requested", item.ID, nil, item); createErr != nil {
			return createErr
		}
		return s.emit(txCtx, actor.OrganizationID, item.ID, "deployment.requested", map[string]any{"project_id": input.ProjectID, "environment_id": input.EnvironmentID, "commit_sha": input.CommitSHA, "status": item.Status})
	})
	if err != nil {
		return DeploymentRequest{}, err
	}
	return item, nil
}

func (s *Service) GetDeployment(ctx context.Context, actor identity.Actor, projectID, id string) (DeploymentRequest, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return DeploymentRequest{}, err
	}
	return s.store.GetDeployment(ctx, actor.OrganizationID, projectID, id)
}

func (s *Service) ApproveDeployment(ctx context.Context, actor identity.Actor, projectID, id string) (DeploymentRequest, error) {
	if err := require(actor, identity.CapabilityDeploymentApprove); err != nil {
		return DeploymentRequest{}, err
	}
	if actor.Type != "human" {
		return DeploymentRequest{}, apperr.New(apperr.CodeForbidden, 403, "only a human can approve a deployment", nil)
	}
	requested, err := s.store.GetDeployment(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return DeploymentRequest{}, err
	}
	environments, err := s.store.List(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return DeploymentRequest{}, err
	}
	production := false
	for _, environment := range environments {
		if environment.ID == requested.EnvironmentID {
			production = environment.Kind == "production"
			break
		}
	}
	if production {
		if strings.TrimSpace(requested.WorkItemID) == "" || strings.TrimSpace(actor.Login) == "" || s.releaseEvidence == nil {
			return DeploymentRequest{}, apperr.New(apperr.CodeForbidden, 403, "production approval requires a linked work item and independent pull-request evidence", nil)
		}
		evidence, evidenceErr := s.releaseEvidence.ReleaseApprovalEvidence(ctx, actor.OrganizationID, projectID, requested.WorkItemID, requested.CommitSHA)
		if evidenceErr != nil {
			return DeploymentRequest{}, evidenceErr
		}
		if !evidence.Found || !strings.EqualFold(strings.TrimSpace(evidence.HeadSHA), strings.TrimSpace(requested.CommitSHA)) {
			return DeploymentRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "production approval requires a pull request for the deployed commit", nil)
		}
		if strings.EqualFold(strings.TrimSpace(actor.Login), strings.TrimSpace(evidence.AuthorLogin)) || containsLogin(evidence.ApprovedByLogins, actor.Login) {
			return DeploymentRequest{}, apperr.New(apperr.CodeForbidden, 403, "production approver must be separate from the PR author and reviewers", nil)
		}
	}
	var item DeploymentRequest
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var approveErr error
		item, approveErr = s.store.ApproveDeployment(txCtx, actor.OrganizationID, projectID, id, actor.ID)
		if approveErr != nil {
			return approveErr
		}
		if approveErr = s.record(txCtx, actor, "deployment.approved", id, nil, item); approveErr != nil {
			return approveErr
		}
		return s.emit(txCtx, actor.OrganizationID, id, "deployment.dispatched", map[string]any{"project_id": projectID, "status": item.Status})
	})
	if err != nil {
		return DeploymentRequest{}, err
	}
	return item, nil
}

func containsLogin(values []string, login string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(login)) {
			return true
		}
	}
	return false
}

func (s *Service) UpdateDeploymentStatus(ctx context.Context, actor identity.Actor, projectID, id string, input StatusInput) (DeploymentRequest, error) {
	if actor.Type != "system" && !actor.Has(identity.CapabilityDeploymentApprove) {
		return DeploymentRequest{}, apperr.New(apperr.CodeForbidden, 403, "deployment status callback is not authorized", nil)
	}
	if input.Status != DeploymentDispatch && input.Status != DeploymentRunning && input.Status != DeploymentSuccess && input.Status != DeploymentFailed && input.Status != DeploymentCanceled {
		return DeploymentRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "deployment status is invalid", nil)
	}
	var item DeploymentRequest
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var updateErr error
		item, updateErr = s.store.UpdateDeployment(txCtx, actor.OrganizationID, projectID, id, input)
		if updateErr != nil {
			return updateErr
		}
		return s.emit(txCtx, actor.OrganizationID, id, "deployment.status_updated", map[string]any{"project_id": projectID, "status": item.Status})
	})
	if err != nil {
		return DeploymentRequest{}, err
	}
	return item, nil
}

func (s *Service) VerifyDeployment(ctx context.Context, actor identity.Actor, projectID, id string) (DeploymentRequest, error) {
	if err := require(actor, identity.CapabilityProductionVerify); err != nil {
		return DeploymentRequest{}, err
	}
	if actor.Type != "human" {
		return DeploymentRequest{}, apperr.New(apperr.CodeForbidden, 403, "only a human can verify production", nil)
	}
	var item DeploymentRequest
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var verifyErr error
		item, verifyErr = s.store.VerifyDeployment(txCtx, actor.OrganizationID, projectID, id, actor.ID)
		if verifyErr != nil {
			return verifyErr
		}
		if verifyErr = s.record(txCtx, actor, "deployment.verified", id, nil, item); verifyErr != nil {
			return verifyErr
		}
		return s.emit(txCtx, actor.OrganizationID, id, "deployment.verified", map[string]any{"project_id": projectID, "status": item.Status})
	})
	return item, err
}

func (s *Service) HasSuccessfulDeployment(ctx context.Context, organizationID, projectID, workItemID string) (bool, error) {
	return s.store.HasDeploymentStatus(ctx, organizationID, projectID, workItemID, DeploymentSuccess, "")
}

func (s *Service) HasProductionVerification(ctx context.Context, organizationID, projectID, workItemID string) (bool, error) {
	return s.store.HasDeploymentStatus(ctx, organizationID, projectID, workItemID, DeploymentVerified, "production")
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "environment", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()})
}

func (s *Service) emit(ctx context.Context, organizationID, resourceID, eventType string, payload map[string]any) error {
	if s.recorder.Outbox == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: organizationID, EventType: eventType, AggregateType: "environment", AggregateID: resourceID, IdempotencyKey: eventType + ":" + resourceID + ":" + id, Payload: payload, OccurredAt: s.now().UTC()})
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
