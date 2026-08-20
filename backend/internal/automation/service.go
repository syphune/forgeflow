package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type Service struct {
	store         Store
	notifications notification.Store
	recorder      MutationRecorder
	transaction   TransactionRunner
	now           func() time.Time
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

func NewService(store Store, notifications notification.Store, options ...Options) *Service {
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
	return &Service{store: store, notifications: notifications, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) List(ctx context.Context, actor identity.Actor, projectID string) ([]Rule, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	return s.store.List(ctx, actor.OrganizationID, projectID)
}

func (s *Service) Create(ctx context.Context, actor identity.Actor, input CreateInput) (Rule, error) {
	if err := authorize(actor, identity.CapabilityProjectManage); err != nil {
		return Rule{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.EventType = strings.TrimSpace(input.EventType)
	input.ActionType = strings.TrimSpace(input.ActionType)
	if input.Name == "" || len(input.Name) > 120 || strings.TrimSpace(input.ProjectID) == "" {
		return Rule{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and a bounded rule name are required", nil)
	}
	if !allowedEvents[input.EventType] {
		return Rule{}, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported automation event", map[string]any{"event_type": input.EventType})
	}
	if input.ActionType == "" {
		input.ActionType = ActionNotify
	}
	if input.ActionType != ActionNotify {
		return Rule{}, apperr.New(apperr.CodeInvalidArgument, 422, "only notify automation actions are supported", nil)
	}
	if err := validateConfig(input.Config); err != nil {
		return Rule{}, err
	}
	var result Rule
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.Create(txCtx, actor.OrganizationID, input)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "automation_rule.created", result.ID, nil, result)
	})
	return result, err
}

func (s *Service) SetEnabled(ctx context.Context, actor identity.Actor, projectID, id string, enabled bool) (Rule, error) {
	if err := authorize(actor, identity.CapabilityProjectManage); err != nil {
		return Rule{}, err
	}
	var result Rule
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.store.SetEnabled(txCtx, actor.OrganizationID, projectID, id, enabled)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "automation_rule.updated", result.ID, nil, result)
	})
	return result, err
}

func (s *Service) Delete(ctx context.Context, actor identity.Actor, projectID, id string) error {
	if err := authorize(actor, identity.CapabilityProjectManage); err != nil {
		return err
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.Delete(txCtx, actor.OrganizationID, projectID, id); err != nil {
			return err
		}
		return s.record(txCtx, actor, "automation_rule.deleted", id, nil, nil)
	})
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "automation_rule", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: actor.OrganizationID, EventType: action, AggregateType: "automation_rule", AggregateID: resourceID, IdempotencyKey: action + ":" + resourceID + ":" + id, Payload: map[string]any{"automation_rule_id": resourceID}, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Handle(ctx context.Context, event outbox.Event) error {
	if s.store == nil || s.notifications == nil {
		return nil
	}
	rules, err := s.store.Matching(ctx, event)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		claimed, claimErr := s.store.ClaimExecution(ctx, event.OrganizationID, rule.ID, event.ID)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			continue
		}
		title, body := render(rule, event)
		if err := s.notifications.CreateForProject(ctx, event.OrganizationID, rule.ProjectID, "automation", title, body, event.AggregateType, event.AggregateID); err != nil {
			_ = s.store.FinishExecution(ctx, event.OrganizationID, rule.ID, event.ID, err)
			return err
		}
		if err := s.store.FinishExecution(ctx, event.OrganizationID, rule.ID, event.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

func render(rule Rule, event outbox.Event) (string, string) {
	title := fmt.Sprintf("Forgeflow: %s", event.EventType)
	body := fmt.Sprintf("Automation %q received %s for %s %s.", rule.Name, event.EventType, event.AggregateType, event.AggregateID)
	if value, ok := rule.Config["title"].(string); ok && strings.TrimSpace(value) != "" {
		title = value
	}
	if value, ok := rule.Config["body"].(string); ok && strings.TrimSpace(value) != "" {
		body = value
	}
	for _, replacement := range []struct{ from, to string }{{"{event_type}", event.EventType}, {"{aggregate_type}", event.AggregateType}, {"{aggregate_id}", event.AggregateID}} {
		title = strings.ReplaceAll(title, replacement.from, replacement.to)
		body = strings.ReplaceAll(body, replacement.from, replacement.to)
	}
	return title, body
}

func validateConfig(config map[string]any) error {
	if config == nil {
		return nil
	}
	encoded, err := json.Marshal(config)
	if err != nil || len(encoded) > 4096 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "automation config is invalid or too large", nil)
	}
	for key, value := range config {
		if key != "title" && key != "body" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "automation config only supports title and body", map[string]any{"key": key})
		}
		if _, ok := value.(string); !ok {
			return apperr.New(apperr.CodeInvalidArgument, 422, "automation title and body must be strings", nil)
		}
	}
	return nil
}

func authorize(actor identity.Actor, capability string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}
