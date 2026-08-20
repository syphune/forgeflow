package github

import (
	"context"
	"sort"
	"sync"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
)

type memoryKnowledgeDocument struct {
	document  KnowledgeDocument
	revisions []KnowledgeRevision
}

type MemoryKnowledgeStore struct {
	mu        sync.Mutex
	documents map[string]memoryKnowledgeDocument
}

func NewMemoryKnowledgeStore() *MemoryKnowledgeStore {
	return &MemoryKnowledgeStore{documents: make(map[string]memoryKnowledgeDocument)}
}

func (s *MemoryKnowledgeStore) Create(_ context.Context, document KnowledgeDocument, revision KnowledgeRevision) (KnowledgeDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document.LatestRevision = &revision
	s.documents[document.ID] = memoryKnowledgeDocument{document: document, revisions: []KnowledgeRevision{revision}}
	return document, nil
}

func (s *MemoryKnowledgeStore) List(_ context.Context, organizationID, projectID, repositoryID string, limit int) ([]KnowledgeDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]KnowledgeDocument, 0)
	for _, item := range s.documents {
		if item.document.OrganizationID != organizationID || item.document.ProjectID != projectID || (repositoryID != "" && item.document.RepositoryID != "" && item.document.RepositoryID != repositoryID) {
			continue
		}
		result = append(result, item.document)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	limit = knowledgeLimit(limit)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *MemoryKnowledgeStore) Get(_ context.Context, organizationID, projectID, repositoryID, documentID string) (KnowledgeDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.documents[documentID]
	if !ok || item.document.OrganizationID != organizationID || item.document.ProjectID != projectID || (repositoryID != "" && item.document.RepositoryID != "" && item.document.RepositoryID != repositoryID) {
		return KnowledgeDocument{}, knowledgeNotFound()
	}
	return item.document, nil
}

func (s *MemoryKnowledgeStore) ListRevisions(_ context.Context, organizationID, projectID, repositoryID, documentID string, limit int) ([]KnowledgeRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.documents[documentID]
	if !ok || item.document.OrganizationID != organizationID || item.document.ProjectID != projectID || (repositoryID != "" && item.document.RepositoryID != "" && item.document.RepositoryID != repositoryID) {
		return nil, knowledgeNotFound()
	}
	result := append([]KnowledgeRevision(nil), item.revisions...)
	sort.Slice(result, func(i, j int) bool { return result[i].RevisionNumber > result[j].RevisionNumber })
	limit = knowledgeLimit(limit)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *MemoryKnowledgeStore) AppendRevision(_ context.Context, organizationID, projectID, repositoryID, documentID string, revision KnowledgeRevision) (KnowledgeRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.documents[documentID]
	if !ok || item.document.OrganizationID != organizationID || item.document.ProjectID != projectID || (repositoryID != "" && item.document.RepositoryID != "" && item.document.RepositoryID != repositoryID) {
		return KnowledgeRevision{}, knowledgeNotFound()
	}
	if item.document.CurrentProvenance == "HUMAN_VERIFIED" && revision.Provenance != "HUMAN_VERIFIED" {
		return KnowledgeRevision{}, apperr.New(apperr.CodeConflict, 409, "verified knowledge cannot be overwritten by an unverified revision", nil)
	}
	revision.RevisionNumber = len(item.revisions) + 1
	item.revisions = append(item.revisions, revision)
	item.document.CurrentProvenance = revision.Provenance
	item.document.UpdatedAt = revision.CreatedAt
	item.document.LatestRevision = &revision
	s.documents[documentID] = item
	return revision, nil
}

var _ KnowledgeStore = (*MemoryKnowledgeStore)(nil)
