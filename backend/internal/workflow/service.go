package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type ProjectStore interface {
	LoadWorkflow(context.Context, string, string) (Workflow, error)
}

type MutableProjectStore interface {
	ProjectStore
	SaveWorkflow(context.Context, string, string, SaveInput) (Workflow, error)
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type Service struct {
	workflow    Workflow
	store       ProjectStore
	mutable     MutableProjectStore
	recorder    MutationRecorder
	transaction TransactionRunner
	now         func() time.Time
	mu          sync.RWMutex
	projects    map[string]Workflow
}

type MutationRecorder struct {
	Audit  audit.Writer
	Outbox outbox.Writer
}

func NewService(workflow Workflow, stores ...ProjectStore) *Service {
	service := &Service{workflow: workflow, transaction: directTransactionRunner{}, now: func() time.Time { return time.Now().UTC() }, projects: make(map[string]Workflow)}
	if len(stores) > 0 {
		service.store = stores[0]
		if mutable, ok := stores[0].(MutableProjectStore); ok {
			service.mutable = mutable
		}
	}
	return service
}

func (s *Service) SetRecorder(recorder MutationRecorder, now func() time.Time) {
	s.recorder = recorder
	if now != nil {
		s.now = now
	}
}

func (s *Service) SetTransaction(transaction TransactionRunner) {
	if transaction != nil {
		s.transaction = transaction
	}
}

func (s *Service) WorkflowFor(ctx context.Context, organizationID, projectID string) (Workflow, error) {
	s.mu.RLock()
	projectWorkflow, exists := s.projects[organizationID+"\x00"+projectID]
	s.mu.RUnlock()
	if exists {
		return cloneWorkflow(projectWorkflow), nil
	}
	if s.store == nil {
		return cloneWorkflow(s.workflow), nil
	}
	result, err := s.store.LoadWorkflow(ctx, organizationID, projectID)
	if err != nil {
		return Workflow{}, err
	}
	return cloneWorkflow(result), nil
}

func (s *Service) SaveForProject(ctx context.Context, actor identity.Actor, projectID string, input SaveInput) (Workflow, error) {
	if actor.ID == "" || actor.OrganizationID == "" {
		return Workflow{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(identity.CapabilityProjectManage) {
		return Workflow{}, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilityProjectManage})
	}
	organizationID := actor.OrganizationID
	if projectID == "" {
		return Workflow{}, apperr.New(apperr.CodeInvalidArgument, 422, "organization and project are required", nil)
	}
	normalized, err := validateSaveInput(input)
	if err != nil {
		return Workflow{}, err
	}
	var result Workflow
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, loadErr := s.WorkflowFor(txCtx, organizationID, projectID)
		if loadErr != nil {
			return loadErr
		}
		if s.mutable != nil {
			result, err = s.mutable.SaveWorkflow(txCtx, organizationID, projectID, normalized)
		} else {
			result = workflowFromInput(normalized)
			s.mu.Lock()
			s.projects[organizationID+"\x00"+projectID] = cloneWorkflow(result)
			s.mu.Unlock()
		}
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, projectID, before, result)
	})
	if err != nil {
		return Workflow{}, err
	}
	return cloneWorkflow(result), nil
}

func (s *Service) Transition(current, key string) (Transition, error) {
	return transition(s.workflow, current, key)
}

func (s *Service) TransitionForProject(ctx context.Context, organizationID, projectID, current, key string) (Transition, error) {
	wf, err := s.WorkflowFor(ctx, organizationID, projectID)
	if err != nil {
		return Transition{}, err
	}
	return transition(wf, current, key)
}

func transition(wf Workflow, current, key string) (Transition, error) {
	item, ok := wf.Transitions[key]
	if !ok || item.From != current {
		return Transition{}, apperr.New(apperr.CodeConflict, 409, fmt.Sprintf("transition %q is not allowed from %s", key, current), map[string]any{"current_status": current})
	}
	return item, nil
}

func (s *Service) Status(key string) (Status, bool) {
	status, ok := s.workflow.Statuses[key]
	return status, ok
}

func (s *Service) Workflow() Workflow { return cloneWorkflow(s.workflow) }

func (s *Service) record(ctx context.Context, actor identity.Actor, projectID string, before, after Workflow) error {
	resourceID := after.ID
	if resourceID == "" {
		resourceID = projectID
	}
	if s.recorder.Audit != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: "workflow.updated", ResourceType: "workflow", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: actor.OrganizationID, EventType: "workflow.updated", AggregateType: "workflow", AggregateID: resourceID, IdempotencyKey: "workflow.updated:" + resourceID + ":" + id, Payload: map[string]any{"workflow_id": resourceID, "project_id": projectID}, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func workflowFromInput(input SaveInput) Workflow {
	result := Workflow{Name: input.Name, Statuses: make(map[string]Status, len(input.Statuses)), Transitions: make(map[string]Transition, len(input.Transitions))}
	for _, status := range input.Statuses {
		result.Statuses[status.Key] = status
	}
	for _, transition := range input.Transitions {
		result.Transitions[transition.Key] = transition
	}
	return result
}

func cloneWorkflow(input Workflow) Workflow {
	result := Workflow{ID: input.ID, Name: input.Name, Statuses: make(map[string]Status, len(input.Statuses)), Transitions: make(map[string]Transition, len(input.Transitions))}
	for key, status := range input.Statuses {
		result.Statuses[key] = status
	}
	for key, transition := range input.Transitions {
		transition.Required = append([]RuleType(nil), transition.Required...)
		transition.RequiredPermissions = append([]string(nil), transition.RequiredPermissions...)
		result.Transitions[key] = transition
	}
	return result
}
