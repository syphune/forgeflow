package planning

import (
	"context"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type Service struct {
	store       Store
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

func (s *Service) Create(ctx context.Context, actor identity.Actor, projectID, name, goal string, startsAt, endsAt *time.Time) (Sprint, error) {
	if err := authorize(actor); err != nil {
		return Sprint{}, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(name) == "" {
		return Sprint{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and name are required", nil)
	}
	if startsAt != nil && endsAt != nil && endsAt.Before(*startsAt) {
		return Sprint{}, apperr.New(apperr.CodeInvalidArgument, 422, "sprint end date must be on or after the start date", nil)
	}
	var result Sprint
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.Create(txCtx, actor.OrganizationID, projectID, strings.TrimSpace(name), strings.TrimSpace(goal), startsAt, endsAt)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "sprint.created", result.ID, nil, result)
	})
	return result, err
}

func (s *Service) List(ctx context.Context, actor identity.Actor, projectID string) ([]Sprint, error) {
	if err := authorizeRead(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	return s.store.List(ctx, actor.OrganizationID, projectID)
}

func (s *Service) Update(ctx context.Context, actor identity.Actor, projectID, id, name, goal string, startsAt, endsAt *time.Time) (Sprint, error) {
	if err := authorize(actor); err != nil {
		return Sprint{}, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
		return Sprint{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id, id, and name are required", nil)
	}
	if startsAt != nil && endsAt != nil && endsAt.Before(*startsAt) {
		return Sprint{}, apperr.New(apperr.CodeInvalidArgument, 422, "sprint end date must be on or after the start date", nil)
	}
	var result Sprint
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.Update(txCtx, actor.OrganizationID, projectID, id, strings.TrimSpace(name), strings.TrimSpace(goal), startsAt, endsAt)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "sprint.updated", result.ID, nil, result)
	})
	return result, err
}

func (s *Service) Delete(ctx context.Context, actor identity.Actor, projectID, id string) error {
	if err := authorize(actor); err != nil {
		return err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(id) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id and id are required", nil)
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.Delete(txCtx, actor.OrganizationID, projectID, id); err != nil {
			return err
		}
		return s.record(txCtx, actor, "sprint.deleted", id, nil, nil)
	})
}

func (s *Service) Start(ctx context.Context, actor identity.Actor, projectID, id string) (Sprint, error) {
	return s.transition(ctx, actor, projectID, id, Active)
}
func (s *Service) Complete(ctx context.Context, actor identity.Actor, projectID, id string) (Sprint, error) {
	return s.transition(ctx, actor, projectID, id, Completed)
}
func (s *Service) transition(ctx context.Context, actor identity.Actor, projectID, id string, status Status) (Sprint, error) {
	if err := authorize(actor); err != nil {
		return Sprint{}, err
	}
	var result Sprint
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.Transition(txCtx, actor.OrganizationID, projectID, id, status)
		if err != nil {
			return err
		}
		action := "sprint.started"
		if status == Completed {
			action = "sprint.completed"
		}
		return s.record(txCtx, actor, action, result.ID, nil, result)
	})
	return result, err
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "sprint", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: actor.OrganizationID, EventType: action, AggregateType: "sprint", AggregateID: resourceID, IdempotencyKey: action + ":" + resourceID + ":" + id, Payload: map[string]any{"sprint_id": resourceID}, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}
func authorize(actor identity.Actor) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(identity.CapabilitySprintManage) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySprintManage})
	}
	return nil
}
func authorizeRead(actor identity.Actor) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(identity.CapabilityProjectRead) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilityProjectRead})
	}
	return nil
}
