package automation

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type MemoryStore struct {
	mu         sync.Mutex
	rules      map[string]Rule
	executions map[string]bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rules: make(map[string]Rule), executions: make(map[string]bool)}
}

func (s *MemoryStore) Create(_ context.Context, organizationID string, input CreateInput) (Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rule := range s.rules {
		if rule.OrganizationID == organizationID && rule.ProjectID == input.ProjectID && rule.Name == input.Name {
			return Rule{}, apperr.New(apperr.CodeConflict, 409, "automation rule name is already used", nil)
		}
	}
	id, err := ids.New()
	if err != nil {
		return Rule{}, err
	}
	now := time.Now().UTC()
	item := Rule{ID: id, OrganizationID: organizationID, ProjectID: input.ProjectID, Name: input.Name, EventType: input.EventType, ActionType: input.ActionType, Config: input.Config, Enabled: true, CreatedAt: now, UpdatedAt: now}
	s.rules[id] = item
	return item, nil
}

func (s *MemoryStore) List(_ context.Context, organizationID, projectID string) ([]Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Rule, 0)
	for _, rule := range s.rules {
		if rule.OrganizationID == organizationID && rule.ProjectID == projectID {
			result = append(result, rule)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryStore) SetEnabled(_ context.Context, organizationID, projectID, id string, enabled bool) (Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.rules[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return Rule{}, apperr.New(apperr.CodeNotFound, 404, "automation rule not found", nil)
	}
	item.Enabled = enabled
	item.UpdatedAt = time.Now().UTC()
	s.rules[id] = item
	return item, nil
}

func (s *MemoryStore) Delete(_ context.Context, organizationID, projectID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.rules[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return apperr.New(apperr.CodeNotFound, 404, "automation rule not found", nil)
	}
	delete(s.rules, id)
	return nil
}

func (s *MemoryStore) Matching(_ context.Context, event outbox.Event) ([]Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectID := eventProjectID(event)
	result := make([]Rule, 0)
	for _, rule := range s.rules {
		projectMatch := projectID != "" && rule.ProjectID == projectID
		globalMatch := event.EventType == "github.installation.connected"
		if rule.OrganizationID == event.OrganizationID && rule.Enabled && rule.EventType == event.EventType && (projectMatch || globalMatch) {
			result = append(result, rule)
		}
	}
	return result, nil
}

func (s *MemoryStore) ClaimExecution(_ context.Context, organizationID, ruleID, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := organizationID + ":" + ruleID + ":" + eventID
	if s.executions[key] {
		return false, nil
	}
	s.executions[key] = true
	return true, nil
}

func (s *MemoryStore) FinishExecution(context.Context, string, string, string, error) error {
	return nil
}

var _ Store = (*MemoryStore)(nil)
