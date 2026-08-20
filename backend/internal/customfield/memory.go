package customfield

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type MemoryStore struct {
	mu          sync.Mutex
	definitions map[string]Definition
	values      map[string]Value
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{definitions: make(map[string]Definition), values: make(map[string]Value)}
}

func (s *MemoryStore) ListDefinitions(_ context.Context, organizationID, projectID string) ([]Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Definition, 0)
	for _, item := range s.definitions {
		if item.OrganizationID == organizationID && item.ProjectID == projectID {
			result = append(result, cloneDefinition(item))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DisplayName < result[j].DisplayName })
	return result, nil
}

func (s *MemoryStore) GetDefinition(_ context.Context, organizationID, projectID, id string) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.definitions[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return Definition{}, apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	return cloneDefinition(item), nil
}

func (s *MemoryStore) CreateDefinition(_ context.Context, organizationID string, input CreateInput) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.definitions {
		if item.OrganizationID == organizationID && item.ProjectID == input.ProjectID && item.Key == input.Key {
			return Definition{}, apperr.New(apperr.CodeConflict, 409, "custom field key is already used", nil)
		}
	}
	id, err := ids.New()
	if err != nil {
		return Definition{}, err
	}
	now := time.Now().UTC()
	item := Definition{ID: id, OrganizationID: organizationID, ProjectID: input.ProjectID, Key: input.Key, DisplayName: input.DisplayName, ValueType: input.ValueType, Options: append([]string(nil), input.Options...), Required: input.Required, CreatedAt: now, UpdatedAt: now}
	s.definitions[id] = item
	return cloneDefinition(item), nil
}

func (s *MemoryStore) UpdateDefinition(_ context.Context, organizationID, projectID, id string, input UpdateInput) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.definitions[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return Definition{}, apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	if input.DisplayName != nil {
		item.DisplayName = *input.DisplayName
	}
	if input.Options != nil {
		item.Options = append([]string(nil), (*input.Options)...)
	}
	if input.Required != nil {
		item.Required = *input.Required
	}
	item.UpdatedAt = time.Now().UTC()
	s.definitions[id] = item
	return cloneDefinition(item), nil
}

func (s *MemoryStore) DeleteDefinition(_ context.Context, organizationID, projectID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.definitions[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	delete(s.definitions, id)
	for key, value := range s.values {
		if value.DefinitionID == id {
			delete(s.values, key)
		}
	}
	return nil
}

func (s *MemoryStore) ListValues(_ context.Context, organizationID, projectID, workItemID string) ([]Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Value, 0)
	for _, value := range s.values {
		if value.WorkItemID == workItemID {
			definition := s.definitions[value.DefinitionID]
			if definition.OrganizationID == organizationID && definition.ProjectID == projectID {
				result = append(result, cloneValue(value))
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DisplayName < result[j].DisplayName })
	return result, nil
}

func (s *MemoryStore) SetValue(_ context.Context, organizationID, projectID, workItemID, fieldID string, typed TypedValue) (Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	definition, ok := s.definitions[fieldID]
	if !ok || definition.OrganizationID != organizationID || definition.ProjectID != projectID {
		return Value{}, apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	key := workItemID + ":" + fieldID
	item := Value{DefinitionID: fieldID, WorkItemID: workItemID, Key: definition.Key, DisplayName: definition.DisplayName, ValueType: definition.ValueType, Options: append([]string(nil), definition.Options...), UpdatedAt: time.Now().UTC()}
	item.Value = typedString(typed)
	s.values[key] = item
	return cloneValue(item), nil
}

func (s *MemoryStore) ClearValue(_ context.Context, _, _, workItemID, fieldID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, workItemID+":"+fieldID)
	return nil
}

func typedString(value TypedValue) string {
	if value.Text != nil {
		return *value.Text
	}
	if value.Number != nil {
		return strconvFormat(*value.Number)
	}
	if value.Boolean != nil {
		if *value.Boolean {
			return "true"
		}
		return "false"
	}
	if value.Date != nil {
		return value.Date.Format("2006-01-02")
	}
	if value.Option != nil {
		return *value.Option
	}
	return ""
}

func strconvFormat(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func cloneDefinition(item Definition) Definition {
	item.Options = append([]string(nil), item.Options...)
	return item
}
func cloneValue(item Value) Value { item.Options = append([]string(nil), item.Options...); return item }

var _ Store = (*MemoryStore)(nil)
