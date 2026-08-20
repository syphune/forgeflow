package customfield

import (
	"context"
	"strconv"
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

type Options struct {
	Recorder    MutationRecorder
	Transaction TransactionRunner
	Now         func() time.Time
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
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

func (s *Service) ListDefinitions(ctx context.Context, actor identity.Actor, projectID string) ([]Definition, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	return s.store.ListDefinitions(ctx, actor.OrganizationID, projectID)
}

func (s *Service) CreateDefinition(ctx context.Context, actor identity.Actor, input CreateInput) (Definition, error) {
	if err := authorize(actor, identity.CapabilityProjectManage); err != nil {
		return Definition{}, err
	}
	if strings.TrimSpace(input.ProjectID) == "" {
		return Definition{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	input.Key = strings.ToUpper(strings.TrimSpace(input.Key))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.Key == "" || len(input.Key) > 32 || !validKey(input.Key) || input.DisplayName == "" || len(input.DisplayName) > 120 {
		return Definition{}, apperr.New(apperr.CodeInvalidArgument, 422, "field key and display_name are invalid", nil)
	}
	if err := validateTypeOptions(input.ValueType, input.Options); err != nil {
		return Definition{}, err
	}
	input.Options = cleanOptions(input.Options)
	var item Definition
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.CreateDefinition(txCtx, actor.OrganizationID, input)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "custom_field.created", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) UpdateDefinition(ctx context.Context, actor identity.Actor, projectID, id string, input UpdateInput) (Definition, error) {
	if err := authorize(actor, identity.CapabilityProjectManage); err != nil {
		return Definition{}, err
	}
	if input.DisplayName != nil {
		value := strings.TrimSpace(*input.DisplayName)
		if value == "" || len(value) > 120 {
			return Definition{}, apperr.New(apperr.CodeInvalidArgument, 422, "display_name is invalid", nil)
		}
		input.DisplayName = &value
	}
	if input.Options != nil {
		options := cleanOptions(*input.Options)
		if len(options) > 50 {
			return Definition{}, apperr.New(apperr.CodeInvalidArgument, 422, "a field cannot have more than 50 options", nil)
		}
		input.Options = &options
	}
	var item Definition
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.UpdateDefinition(txCtx, actor.OrganizationID, projectID, id, input)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "custom_field.updated", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) DeleteDefinition(ctx context.Context, actor identity.Actor, projectID, id string) error {
	if err := authorize(actor, identity.CapabilityProjectManage); err != nil {
		return err
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.DeleteDefinition(txCtx, actor.OrganizationID, projectID, id); err != nil {
			return err
		}
		return s.record(txCtx, actor, "custom_field.deleted", id, nil, nil)
	})
}

func (s *Service) ListValues(ctx context.Context, actor identity.Actor, projectID, workItemID string) ([]Value, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(workItemID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and work_item_id are required", nil)
	}
	return s.store.ListValues(ctx, actor.OrganizationID, projectID, workItemID)
}

func (s *Service) SetValue(ctx context.Context, actor identity.Actor, projectID, workItemID, fieldID string, value *string) (Value, error) {
	if err := authorize(actor, identity.CapabilityWorkItemEdit); err != nil {
		return Value{}, err
	}
	var result Value
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		definition, err := s.store.GetDefinition(txCtx, actor.OrganizationID, projectID, fieldID)
		if err != nil {
			return err
		}
		if value == nil {
			if err := s.store.ClearValue(txCtx, actor.OrganizationID, projectID, workItemID, fieldID); err != nil {
				return err
			}
			result = Value{DefinitionID: definition.ID, WorkItemID: workItemID, Key: definition.Key, DisplayName: definition.DisplayName, ValueType: definition.ValueType, Options: definition.Options}
			return s.record(txCtx, actor, "custom_field.value.cleared", fieldID, nil, result)
		}
		typed, canonical, err := parseValue(definition, *value)
		if err != nil {
			return err
		}
		result, err = s.store.SetValue(txCtx, actor.OrganizationID, projectID, workItemID, fieldID, typed)
		if err != nil {
			return err
		}
		result.Value = canonical
		return s.record(txCtx, actor, "custom_field.value.set", fieldID, nil, result)
	})
	return result, err
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit != nil {
		if err := s.recorder.Audit.Record(ctx, audit.Record{ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "custom_field", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: actor.OrganizationID, EventType: action, AggregateType: "custom_field", AggregateID: resourceID, IdempotencyKey: action + ":" + resourceID + ":" + id, Payload: map[string]any{"custom_field_id": resourceID}, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func parseValue(definition Definition, raw string) (TypedValue, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "custom field value cannot be empty", nil)
	}
	switch definition.ValueType {
	case Text:
		if len(raw) > 4000 {
			return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "text custom field value is too long", nil)
		}
		return TypedValue{Text: &raw}, raw, nil
	case Number:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "number custom field value is invalid", nil)
		}
		canonical := strconv.FormatFloat(value, 'f', -1, 64)
		return TypedValue{Number: &value}, canonical, nil
	case Boolean:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "boolean custom field value must be true or false", nil)
		}
		return TypedValue{Boolean: &value}, strconv.FormatBool(value), nil
	case Date:
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "date custom field value must be YYYY-MM-DD", nil)
		}
		return TypedValue{Date: &value}, value.Format("2006-01-02"), nil
	case Select:
		for _, option := range definition.Options {
			if option == raw {
				return TypedValue{Option: &raw}, raw, nil
			}
		}
		return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "custom field option is not allowed", nil)
	default:
		return TypedValue{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "custom field type is invalid", nil)
	}
}

func validateTypeOptions(valueType ValueType, options []string) error {
	if valueType != Text && valueType != Number && valueType != Boolean && valueType != Date && valueType != Select {
		return apperr.New(apperr.CodeInvalidArgument, 422, "custom field type is invalid", nil)
	}
	options = cleanOptions(options)
	if valueType == Select && len(options) == 0 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "select custom fields require options", nil)
	}
	if len(options) > 50 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "a field cannot have more than 50 options", nil)
	}
	if valueType != Select && len(options) > 0 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "only select custom fields can have options", nil)
	}
	return nil
}

func cleanOptions(options []string) []string {
	result := make([]string, 0, len(options))
	seen := make(map[string]bool, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || len(option) > 120 || seen[option] {
			continue
		}
		seen[option] = true
		result = append(result, option)
	}
	return result
}

func validKey(value string) bool {
	for index, char := range value {
		if (char < 'A' || char > 'Z') && (index == 0 || (char < '0' || char > '9') && char != '_') {
			return false
		}
	}
	return true
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
