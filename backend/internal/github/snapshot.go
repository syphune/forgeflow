package github

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/intelligence"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

const (
	maxSnapshotFiles     = 2000
	maxSnapshotFileBytes = 256 << 10
	maxSnapshotTotal     = 64 << 20
)

type SnapshotRecord struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	ProjectID      string     `json:"project_id"`
	RepositoryID   string     `json:"repository_id"`
	CommitSHA      string     `json:"commit_sha"`
	RefName        string     `json:"ref_name"`
	Status         string     `json:"status"`
	FileCount      int        `json:"file_count"`
	SymbolCount    int        `json:"symbol_count"`
	SkippedCount   int        `json:"skipped_count"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type SnapshotFile struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content,omitempty"`
}

type SnapshotSymbol struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Qualified  string `json:"qualified_name"`
	Kind       string `json:"kind"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Confidence string `json:"confidence"`
	Provenance string `json:"provenance"`
}

type SnapshotEdge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	Provenance string `json:"provenance"`
}

type SnapshotStore interface {
	SaveSnapshot(context.Context, SnapshotRecord, *intelligence.Snapshot) (SnapshotRecord, error)
	ListSnapshots(context.Context, string, string, string, int) ([]SnapshotRecord, error)
	GetSnapshot(context.Context, string, string, string, string) (SnapshotRecord, error)
	GetSnapshotFile(context.Context, string, string, string, string, string) (SnapshotFile, error)
	SearchSnapshot(context.Context, string, string, string, string, string, int) ([]SnapshotFile, error)
	ListSnapshotSymbols(context.Context, string, string, string, string, string, int) ([]SnapshotSymbol, error)
	ListSnapshotEdges(context.Context, string, string, string, string, string, int) ([]SnapshotEdge, error)
}

type SnapshotService struct {
	parent    *Service
	store     SnapshotStore
	indexer   *intelligence.Indexer
	knowledge *KnowledgeService
}

func NewSnapshotService(parent *Service, store SnapshotStore) *SnapshotService {
	return &SnapshotService{parent: parent, store: store, indexer: intelligence.NewIndexer(intelligence.Config{MaxFiles: maxSnapshotFiles, MaxFileBytes: maxSnapshotFileBytes, MaxTotalBytes: maxSnapshotTotal})}
}

func (s *SnapshotService) SetKnowledgeService(knowledge *KnowledgeService) {
	s.knowledge = knowledge
}

func (s *SnapshotService) KnowledgeService() *KnowledgeService {
	return s.knowledge
}

func (s *SnapshotService) Refresh(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (SnapshotRecord, error) {
	if err := s.authorize(actor); err != nil {
		return SnapshotRecord{}, err
	}
	if s.parent == nil || s.store == nil {
		return SnapshotRecord{}, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	started := time.Now().UTC()
	repository, client, err := s.parent.contentClient(ctx, actor, projectID, repositoryID)
	if err != nil {
		return SnapshotRecord{}, err
	}
	refClient, ok := client.(RefClient)
	if !ok {
		return SnapshotRecord{}, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "GitHub fixed-ref API is not configured", nil)
	}
	commitSHA, err := refClient.RepositoryHead(ctx, repository.InstallationID, repository.FullName, repository.DefaultBranch)
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("resolve repository commit: %w", err)
	}
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return SnapshotRecord{}, fmt.Errorf("GitHub returned an empty repository commit")
	}
	tree, err := client.RepositoryTree(ctx, repository.InstallationID, repository.FullName, commitSHA)
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("load repository tree at %s: %w", commitSHA, err)
	}
	sort.Slice(tree, func(i, j int) bool { return tree[i].Path < tree[j].Path })
	contents := make(map[string][]byte)
	var total int64
	partialErrors := make([]string, 0)
	for _, entry := range tree {
		if len(contents) >= maxSnapshotFiles || (entry.Type != "blob" && entry.Type != "file") || entry.Size > maxSnapshotFileBytes {
			continue
		}
		file, fileErr := client.RepositoryFile(ctx, repository.InstallationID, repository.FullName, entry.Path, commitSHA)
		if fileErr != nil {
			if len(partialErrors) < 10 {
				partialErrors = append(partialErrors, entry.Path)
			}
			continue
		}
		if len(file.Content) > maxSnapshotFileBytes || total+int64(len(file.Content)) > maxSnapshotTotal {
			continue
		}
		contents[file.Path] = []byte(file.Content)
		total += int64(len(file.Content))
	}
	indexed, err := s.indexer.IndexFiles(ctx, commitSHA, contents)
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("index repository snapshot: %w", err)
	}
	finished := time.Now().UTC()
	recordID, err := ids.New()
	if err != nil {
		return SnapshotRecord{}, err
	}
	record := SnapshotRecord{
		ID: recordID, OrganizationID: actor.OrganizationID, ProjectID: projectID, RepositoryID: repositoryID,
		CommitSHA: commitSHA, RefName: repository.DefaultBranch, Status: "READY", FileCount: len(indexed.Files),
		SymbolCount: len(indexed.Symbols), SkippedCount: len(tree) - len(contents) + len(indexed.Skipped),
		StartedAt: started, FinishedAt: &finished, CreatedAt: finished,
	}
	if len(partialErrors) > 0 {
		record.ErrorMessage = "some repository files could not be fetched: " + strings.Join(partialErrors, ", ")
	}
	record, err = s.store.SaveSnapshot(ctx, record, indexed)
	if err != nil {
		return SnapshotRecord{}, err
	}
	if err := s.parent.record(ctx, actor, "repository.snapshot.created", record.ID, nil, record); err != nil {
		return SnapshotRecord{}, err
	}
	if err := s.parent.emit(ctx, actor.OrganizationID, record.ID, "repository.snapshot.ready", map[string]any{"project_id": projectID, "repository_id": repositoryID, "commit_sha": commitSHA}); err != nil {
		return SnapshotRecord{}, err
	}
	return record, nil
}

func (s *SnapshotService) List(ctx context.Context, actor identity.Actor, projectID, repositoryID string, limit int) ([]SnapshotRecord, error) {
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return nil, err
		}
	}
	if s.store == nil {
		return nil, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	return s.store.ListSnapshots(ctx, actor.OrganizationID, projectID, repositoryID, limit)
}

func (s *SnapshotService) Get(ctx context.Context, actor identity.Actor, projectID, repositoryID, snapshotID string) (SnapshotRecord, error) {
	if err := s.authorize(actor); err != nil {
		return SnapshotRecord{}, err
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return SnapshotRecord{}, err
		}
	}
	if s.store == nil {
		return SnapshotRecord{}, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	return s.store.GetSnapshot(ctx, actor.OrganizationID, projectID, repositoryID, snapshotID)
}

func (s *SnapshotService) File(ctx context.Context, actor identity.Actor, projectID, repositoryID, snapshotID, path string) (SnapshotFile, error) {
	if err := s.authorize(actor); err != nil {
		return SnapshotFile{}, err
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return SnapshotFile{}, err
		}
	}
	if s.store == nil {
		return SnapshotFile{}, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	return s.store.GetSnapshotFile(ctx, actor.OrganizationID, projectID, repositoryID, snapshotID, path)
}

func (s *SnapshotService) Search(ctx context.Context, actor identity.Actor, projectID, repositoryID, snapshotID, query string, limit int) ([]SnapshotFile, error) {
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return nil, err
		}
	}
	if s.store == nil {
		return nil, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	return s.store.SearchSnapshot(ctx, actor.OrganizationID, projectID, repositoryID, snapshotID, query, limit)
}

func (s *SnapshotService) Symbols(ctx context.Context, actor identity.Actor, projectID, repositoryID, snapshotID, name string, limit int) ([]SnapshotSymbol, error) {
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return nil, err
		}
	}
	if s.store == nil {
		return nil, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	return s.store.ListSnapshotSymbols(ctx, actor.OrganizationID, projectID, repositoryID, snapshotID, name, limit)
}

func (s *SnapshotService) Edges(ctx context.Context, actor identity.Actor, projectID, repositoryID, snapshotID, from string, limit int) ([]SnapshotEdge, error) {
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return nil, err
		}
	}
	if s.store == nil {
		return nil, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
	}
	return s.store.ListSnapshotEdges(ctx, actor.OrganizationID, projectID, repositoryID, snapshotID, from, limit)
}

func (s *SnapshotService) authorize(actor identity.Actor) error {
	if !actor.Has(identity.CapabilityRepositoryRead) {
		return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "permission denied", map[string]any{"capability": identity.CapabilityRepositoryRead})
	}
	if strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "organization scope is required", nil)
	}
	return nil
}
