package github

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type RepositoryTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
	SHA  string `json:"sha"`
}

type RepositoryFile struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Size    int    `json:"size"`
	SHA     string `json:"sha"`
	Ref     string `json:"ref"`
	Content string `json:"content"`
}

type RepositorySearchMatch struct {
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

func (s *Service) RepositoryTree(ctx context.Context, actor identity.Actor, projectID, repositoryID string) ([]RepositoryTreeEntry, error) {
	repository, client, err := s.contentClient(ctx, actor, projectID, repositoryID)
	if err != nil {
		return nil, err
	}
	entries, err := client.RepositoryTree(ctx, repository.InstallationID, repository.FullName, repository.DefaultBranch)
	if err != nil {
		return nil, fmt.Errorf("load repository tree: %w", err)
	}
	result := make([]RepositoryTreeEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Path) == "" {
			continue
		}
		result = append(result, RepositoryTreeEntry{Path: entry.Path, Type: entry.Type, Size: entry.Size, SHA: entry.SHA})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func (s *Service) RepositoryFile(ctx context.Context, actor identity.Actor, projectID, repositoryID, path string) (RepositoryFile, error) {
	repository, client, err := s.contentClient(ctx, actor, projectID, repositoryID)
	if err != nil {
		return RepositoryFile{}, err
	}
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") || hasParentSegment(path) {
		return RepositoryFile{}, apperr.New(apperr.CodeInvalidArgument, 422, "repository path is invalid", nil)
	}
	file, err := client.RepositoryFile(ctx, repository.InstallationID, repository.FullName, path, repository.DefaultBranch)
	if err != nil {
		return RepositoryFile{}, fmt.Errorf("load repository file: %w", err)
	}
	if file.Type != "blob" && file.Type != "file" {
		return RepositoryFile{}, apperr.New(apperr.CodeInvalidArgument, 422, "repository path is not a file", nil)
	}
	if len(file.Content) > 256<<10 {
		return RepositoryFile{}, apperr.New(apperr.CodeInvalidArgument, http.StatusRequestEntityTooLarge, "repository file exceeds the context limit", nil)
	}
	return RepositoryFile{Path: file.Path, Type: file.Type, Size: file.Size, SHA: file.SHA, Ref: repository.DefaultBranch, Content: file.Content}, nil
}

func (s *Service) SearchRepository(ctx context.Context, actor identity.Actor, projectID, repositoryID, query string, limit int) ([]RepositorySearchMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "repository search query must be 1-256 characters", nil)
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	tree, err := s.RepositoryTree(ctx, actor, projectID, repositoryID)
	if err != nil {
		return nil, err
	}
	result := make([]RepositorySearchMatch, 0, limit)
	seen := make(map[string]bool, limit)
	needle := strings.ToLower(query)
	for _, entry := range tree {
		if entry.Type != "blob" || entry.Size > 256<<10 {
			continue
		}
		if strings.Contains(strings.ToLower(entry.Path), needle) {
			result = append(result, RepositorySearchMatch{Path: entry.Path, SHA: entry.SHA, Line: 1, Snippet: "path match"})
			seen[entry.Path] = true
			if len(result) >= limit {
				break
			}
		}
		// ponytail: bounded sequential reads keep the first version simple; move
		// search to a persisted snapshot/index when GitHub rate limits or latency
		// become visible in repository search metrics.
		file, fileErr := s.RepositoryFile(ctx, actor, projectID, repositoryID, entry.Path)
		if fileErr != nil {
			continue
		}
		for lineNumber, line := range strings.Split(file.Content, "\n") {
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			if seen[entry.Path] {
				continue
			}
			seen[entry.Path] = true
			result = append(result, RepositorySearchMatch{Path: entry.Path, SHA: entry.SHA, Line: lineNumber + 1, Snippet: strings.TrimSpace(line)})
			if len(result) >= limit {
				return result, nil
			}
		}
	}
	return result, nil
}

func hasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func (s *Service) RelatedRepositoryFiles(ctx context.Context, actor identity.Actor, projectID, repositoryID, path string, limit int) ([]RepositoryTreeEntry, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") || hasParentSegment(path) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "repository path is required", nil)
	}
	directory := path
	if index := strings.LastIndex(directory, "/"); index >= 0 {
		directory = directory[:index]
	} else {
		directory = ""
	}
	tree, err := s.RepositoryTree(ctx, actor, projectID, repositoryID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	result := make([]RepositoryTreeEntry, 0, limit)
	for _, entry := range tree {
		if entry.Type != "blob" || entry.Path == path || (directory != "" && !strings.HasPrefix(entry.Path, directory+"/")) {
			continue
		}
		result = append(result, entry)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *Service) contentClient(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (Repository, ContentClient, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return Repository{}, nil, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(repositoryID) == "" {
		return Repository{}, nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	client, ok := s.client.(ContentClient)
	if !ok || client == nil {
		return Repository{}, nil, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "GitHub repository content is not configured", nil)
	}
	repositories, err := s.store.ListProjectRepositories(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return Repository{}, nil, err
	}
	for _, repository := range repositories {
		if repository.ID == repositoryID {
			return repository, client, nil
		}
	}
	return Repository{}, nil, apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
}

func (s *Service) requireLinkedRepository(ctx context.Context, organizationID, projectID, repositoryID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(repositoryID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	repositories, err := s.store.ListProjectRepositories(ctx, organizationID, projectID)
	if err != nil {
		return err
	}
	for _, repository := range repositories {
		if repository.ID == repositoryID {
			return nil
		}
	}
	return apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
}
