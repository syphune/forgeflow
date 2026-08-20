package github

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/forgeflow/forgeflow/backend/internal/intelligence"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
)

type memorySnapshot struct {
	record  SnapshotRecord
	files   []SnapshotFile
	symbols []SnapshotSymbol
	edges   []SnapshotEdge
}

type MemorySnapshotStore struct {
	mu        sync.Mutex
	snapshots map[string]memorySnapshot
}

func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{snapshots: make(map[string]memorySnapshot)}
}

func (s *MemorySnapshotStore) SaveSnapshot(_ context.Context, record SnapshotRecord, snapshot *intelligence.Snapshot) (SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := make([]SnapshotFile, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		content, err := snapshot.GetFile(file.Path)
		if err != nil {
			return SnapshotRecord{}, err
		}
		files = append(files, SnapshotFile{Path: file.Path, Language: file.Language, Size: file.Size, ContentHash: file.ContentHash, Content: string(content)})
	}
	symbols := make([]SnapshotSymbol, 0, len(snapshot.Symbols))
	for _, symbol := range snapshot.Symbols {
		symbols = append(symbols, SnapshotSymbol{Path: symbol.Path, Name: symbol.Name, Qualified: symbol.Qualified, Kind: symbol.Kind, StartLine: symbol.StartLine, EndLine: symbol.EndLine, Confidence: symbol.Confidence, Provenance: symbol.Provenance})
	}
	edges := make([]SnapshotEdge, 0, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		edges = append(edges, SnapshotEdge{From: edge.From, To: edge.To, Kind: edge.Kind, Confidence: edge.Confidence, Provenance: edge.Provenance})
	}
	s.snapshots[record.ID] = memorySnapshot{record: record, files: files, symbols: symbols, edges: edges}
	return record, nil
}

func (s *MemorySnapshotStore) ListSnapshots(_ context.Context, organizationID, projectID, repositoryID string, limit int) ([]SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]SnapshotRecord, 0)
	for _, snapshot := range s.snapshots {
		if snapshot.record.OrganizationID == organizationID && snapshot.record.ProjectID == projectID && snapshot.record.RepositoryID == repositoryID {
			result = append(result, snapshot.record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	limit = snapshotLimit(limit)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *MemorySnapshotStore) GetSnapshot(_ context.Context, organizationID, projectID, repositoryID, snapshotID string) (SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[snapshotID]
	if !ok || snapshot.record.OrganizationID != organizationID || snapshot.record.ProjectID != projectID || snapshot.record.RepositoryID != repositoryID {
		return SnapshotRecord{}, snapshotNotFound()
	}
	return snapshot.record, nil
}

func (s *MemorySnapshotStore) GetSnapshotFile(_ context.Context, organizationID, projectID, repositoryID, snapshotID, path string) (SnapshotFile, error) {
	if err := validateSnapshotPath(path); err != nil {
		return SnapshotFile{}, err
	}
	snapshot, err := s.GetSnapshot(context.Background(), organizationID, projectID, repositoryID, snapshotID)
	if err != nil {
		return SnapshotFile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.snapshots[snapshot.ID].files {
		if item.Path == path {
			return item, nil
		}
	}
	return SnapshotFile{}, apperr.New(apperr.CodeNotFound, 404, "repository file not found", nil)
}

func (s *MemorySnapshotStore) SearchSnapshot(_ context.Context, organizationID, projectID, repositoryID, snapshotID, query string, limit int) ([]SnapshotFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.snapshots[snapshotID]
	if !ok || item.record.OrganizationID != organizationID || item.record.ProjectID != projectID || item.record.RepositoryID != repositoryID {
		return nil, snapshotNotFound()
	}
	query = strings.ToLower(strings.TrimSpace(query))
	limit = snapshotLimit(limit)
	result := make([]SnapshotFile, 0, limit)
	for _, file := range item.files {
		if strings.Contains(strings.ToLower(file.Path), query) || strings.Contains(strings.ToLower(file.Content), query) {
			file.Content = ""
			result = append(result, file)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (s *MemorySnapshotStore) ListSnapshotSymbols(_ context.Context, organizationID, projectID, repositoryID, snapshotID, name string, limit int) ([]SnapshotSymbol, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.snapshots[snapshotID]
	if !ok || item.record.OrganizationID != organizationID || item.record.ProjectID != projectID || item.record.RepositoryID != repositoryID {
		return nil, snapshotNotFound()
	}
	name = strings.ToLower(strings.TrimSpace(name))
	limit = snapshotLimit(limit)
	result := make([]SnapshotSymbol, 0, limit)
	for _, symbol := range item.symbols {
		if name != "" && strings.ToLower(symbol.Name) != name && strings.ToLower(symbol.Qualified) != name {
			continue
		}
		result = append(result, symbol)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *MemorySnapshotStore) ListSnapshotEdges(_ context.Context, organizationID, projectID, repositoryID, snapshotID, from string, limit int) ([]SnapshotEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.snapshots[snapshotID]
	if !ok || item.record.OrganizationID != organizationID || item.record.ProjectID != projectID || item.record.RepositoryID != repositoryID {
		return nil, snapshotNotFound()
	}
	from = strings.TrimSpace(from)
	limit = snapshotLimit(limit)
	result := make([]SnapshotEdge, 0, limit)
	for _, edge := range item.edges {
		if from != "" && edge.From != from {
			continue
		}
		result = append(result, edge)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

var _ SnapshotStore = (*MemorySnapshotStore)(nil)
