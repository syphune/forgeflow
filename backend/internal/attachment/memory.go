package attachment

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type MemoryStore struct {
	mu          sync.Mutex
	attachments map[string]Attachment
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{attachments: make(map[string]Attachment)} }

func (s *MemoryStore) List(_ context.Context, organizationID, projectID, workItemID string) ([]Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Attachment, 0)
	for _, item := range s.attachments {
		if item.OrganizationID == organizationID && item.ProjectID == projectID && item.WorkItemID == workItemID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryStore) Create(_ context.Context, item Attachment) (Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Attachment{}, err
		}
		item.ID = id
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	s.attachments[item.ID] = item
	return item, nil
}

func (s *MemoryStore) Get(_ context.Context, organizationID, projectID, workItemID, id string) (Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.attachments[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID || item.WorkItemID != workItemID {
		return Attachment{}, apperr.New(apperr.CodeNotFound, 404, "attachment not found", nil)
	}
	return item, nil
}

func (s *MemoryStore) Delete(_ context.Context, organizationID, projectID, workItemID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.attachments[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID || item.WorkItemID != workItemID {
		return apperr.New(apperr.CodeNotFound, 404, "attachment not found", nil)
	}
	delete(s.attachments, id)
	return nil
}

var _ Store = (*MemoryStore)(nil)
