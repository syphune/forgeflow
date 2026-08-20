package github

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type memoryState struct {
	userID, organizationID string
	expiresAt              time.Time
	used                   bool
}

type repositoryLink struct {
	organizationID string
	projectID      string
	repositoryID   string
}

type MemoryStore struct {
	mu            sync.Mutex
	states        map[string]memoryState
	installations map[string]Installation
	repositories  map[string]Repository
	links         map[repositoryLink]struct{}
	branches      map[string][]Branch
	commits       map[string][]Commit
	pullRequests  map[string][]PullRequest
	ciRuns        map[string][]CIRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{states: make(map[string]memoryState), installations: make(map[string]Installation), repositories: make(map[string]Repository), links: make(map[repositoryLink]struct{}), branches: make(map[string][]Branch), commits: make(map[string][]Commit), pullRequests: make(map[string][]PullRequest), ciRuns: make(map[string][]CIRun)}
}

func (s *MemoryStore) BeginInstallationState(_ context.Context, stateHash, userID, organizationID string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[stateHash] = memoryState{userID: userID, organizationID: organizationID, expiresAt: expiresAt}
	return nil
}

func (s *MemoryStore) ConsumeInstallationState(_ context.Context, stateHash string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[stateHash]
	if !ok || state.used || !state.expiresAt.After(time.Now().UTC()) {
		return "", "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub installation state is invalid or expired", nil)
	}
	state.used = true
	s.states[stateHash] = state
	return state.userID, state.organizationID, nil
}

func (s *MemoryStore) UpsertInstallation(_ context.Context, organizationID string, installationID int64, accountLogin string) (Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.installations {
		if item.GitHubInstallationID == installationID && !strings.HasPrefix(item.ID, organizationID+":") {
			return Installation{}, apperr.New(apperr.CodeConflict, 409, "GitHub installation is already linked to another organization", nil)
		}
	}
	key := organizationID + ":" + formatInt(installationID)
	item, ok := s.installations[key]
	if !ok {
		item = Installation{ID: key, GitHubInstallationID: installationID, CreatedAt: time.Now().UTC()}
	}
	item.AccountLogin = strings.TrimSpace(accountLogin)
	s.installations[key] = item
	return item, nil
}

func (s *MemoryStore) ListInstallations(_ context.Context, organizationID string) ([]Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Installation, 0)
	for key, item := range s.installations {
		if strings.HasPrefix(key, organizationID+":") {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AccountLogin < result[j].AccountLogin })
	return result, nil
}

func (s *MemoryStore) UpsertRepository(_ context.Context, organizationID string, installationID, repositoryID int64, fullName, defaultBranch, cloneURL string) (Repository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := organizationID + ":" + formatInt(repositoryID)
	item, ok := s.repositories[key]
	if !ok {
		id, err := ids.New()
		if err != nil {
			return Repository{}, err
		}
		item.ID = id
	}
	item.GitHubRepositoryID = repositoryID
	item.FullName = strings.TrimSpace(fullName)
	item.DefaultBranch = strings.TrimSpace(defaultBranch)
	item.CloneURL = strings.TrimSpace(cloneURL)
	item.InstallationID = installationID
	for _, installation := range s.installations {
		if installation.GitHubInstallationID == installationID && strings.HasPrefix(installation.ID, organizationID+":") {
			item.InstallationAccount = installation.AccountLogin
		}
	}
	s.repositories[key] = item
	return item, nil
}

func (s *MemoryStore) ProjectRepositoryIDs(_ context.Context, organizationID, projectID string) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]bool)
	for link := range s.links {
		if link.organizationID == organizationID && link.projectID == projectID {
			for key, repository := range s.repositories {
				if strings.HasPrefix(key, organizationID+":") && repository.ID == link.repositoryID {
					result[repository.ID] = true
				}
			}
		}
	}
	return result, nil
}

func (s *MemoryStore) ListProjectRepositories(_ context.Context, organizationID, projectID string) ([]Repository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Repository, 0)
	for link := range s.links {
		if link.organizationID != organizationID || link.projectID != projectID {
			continue
		}
		for _, repository := range s.repositories {
			if repository.ID == link.repositoryID {
				repository.Linked = true
				result = append(result, repository)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FullName < result[j].FullName })
	return result, nil
}

func (s *MemoryStore) LinkRepository(_ context.Context, organizationID, projectID, repositoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for key, repository := range s.repositories {
		if strings.HasPrefix(key, organizationID+":") && repository.ID == repositoryID {
			found = true
			break
		}
	}
	if !found {
		return apperr.New(apperr.CodeNotFound, 404, "project or repository not found", nil)
	}
	s.links[repositoryLink{organizationID: organizationID, projectID: projectID, repositoryID: repositoryID}] = struct{}{}
	return nil
}

func (s *MemoryStore) UnlinkRepository(_ context.Context, organizationID, projectID, repositoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := repositoryLink{organizationID: organizationID, projectID: projectID, repositoryID: repositoryID}
	if _, ok := s.links[key]; !ok {
		return apperr.New(apperr.CodeNotFound, 404, "repository link not found", nil)
	}
	delete(s.links, key)
	return nil
}

func (s *MemoryStore) ListBranches(_ context.Context, organizationID, repositoryID string) ([]Branch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Branch(nil), s.branches[organizationID+":"+repositoryID]...), nil
}

func (s *MemoryStore) ListCommits(_ context.Context, organizationID, repositoryID string) ([]Commit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Commit{}, s.commits[organizationID+":"+repositoryID]...), nil
}

func (s *MemoryStore) ListPullRequests(_ context.Context, organizationID, repositoryID string) ([]PullRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]PullRequest(nil), s.pullRequests[organizationID+":"+repositoryID]...), nil
}

func (s *MemoryStore) UpsertPullRequest(_ context.Context, organizationID, repositoryID string, item PullRequest) (PullRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := organizationID + ":" + repositoryID
	items := s.pullRequests[key]
	for index := range items {
		if items[index].Number != item.Number {
			continue
		}
		if item.ID == "" {
			item.ID = items[index].ID
		}
		if item.WorkItemID == "" {
			item.WorkItemID = items[index].WorkItemID
		}
		items[index] = item
		s.pullRequests[key] = items
		return item, nil
	}
	if item.ID == "" {
		id, err := ids.New()
		if err != nil {
			return PullRequest{}, err
		}
		item.ID = id
	}
	s.pullRequests[key] = append(items, item)
	return item, nil
}

func (s *MemoryStore) ListCIRuns(_ context.Context, organizationID, repositoryID string) ([]CIRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CIRun(nil), s.ciRuns[organizationID+":"+repositoryID]...), nil
}

func (s *MemoryStore) HasPullRequest(_ context.Context, _ string, _ string, _ string, _ string) (bool, error) {
	return false, nil
}

func (s *MemoryStore) HasSuccessfulCI(_ context.Context, _ string, _ string, _ string, _ string) (bool, error) {
	return false, nil
}

func formatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

var _ Store = (*MemoryStore)(nil)
