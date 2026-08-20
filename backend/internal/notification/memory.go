package notification

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type MemoryStore struct {
	mu            sync.Mutex
	notifications map[string]Notification
	resolveUsers  ProjectMemberResolver
}

type ProjectMemberResolver func(context.Context, string, string) ([]string, error)

func NewMemoryStore(resolvers ...ProjectMemberResolver) *MemoryStore {
	var resolveUsers ProjectMemberResolver
	if len(resolvers) > 0 {
		resolveUsers = resolvers[0]
	}
	return &MemoryStore{notifications: make(map[string]Notification), resolveUsers: resolveUsers}
}

func (s *MemoryStore) List(_ context.Context, organizationID, userID string, limit int) ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Notification, 0, limit)
	for _, item := range s.notifications {
		if item.OrganizationID == organizationID && item.UserID == userID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *MemoryStore) CountUnread(_ context.Context, organizationID, userID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, item := range s.notifications {
		if item.OrganizationID == organizationID && item.UserID == userID && item.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) MarkRead(_ context.Context, organizationID, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.notifications[id]
	if !ok || item.OrganizationID != organizationID || item.UserID != userID {
		return apperr.New(apperr.CodeNotFound, 404, "notification not found", nil)
	}
	now := time.Now().UTC()
	item.ReadAt = &now
	s.notifications[id] = item
	return nil
}

func (s *MemoryStore) MarkAllRead(_ context.Context, organizationID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, item := range s.notifications {
		if item.OrganizationID == organizationID && item.UserID == userID && item.ReadAt == nil {
			item.ReadAt = &now
			s.notifications[id] = item
		}
	}
	return nil
}

func (s *MemoryStore) CreateForProject(ctx context.Context, organizationID, projectID, notificationType, title, body, resourceType, resourceID string) error {
	return s.createForProject(ctx, organizationID, projectID, notificationType, title, body, resourceType, resourceID)
}

func (s *MemoryStore) createForProject(ctx context.Context, organizationID, projectID, notificationType, title, body, resourceType, resourceID string) error {
	var users []string
	var err error
	if s.resolveUsers != nil {
		users, err = s.resolveUsers(ctx, organizationID, projectID)
		if err != nil {
			return err
		}
	}
	if len(users) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, userID := range users {
		id, err := ids.New()
		if err != nil {
			return err
		}
		s.notifications[id] = Notification{ID: id, OrganizationID: organizationID, UserID: userID, ProjectID: projectID, NotificationType: notificationType, Title: title, Body: body, ResourceType: resourceType, ResourceID: resourceID, CreatedAt: time.Now().UTC()}
	}
	return nil
}

var _ Store = (*MemoryStore)(nil)
