package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	gh "github.com/google/go-github/v68/github"
)

type AppConfig struct {
	ID          int64
	Slug        string
	PrivateKey  string
	CallbackURL string
	WebBaseURL  string
}

func (c AppConfig) Configured() bool {
	return c.ID > 0 && strings.TrimSpace(c.Slug) != "" && strings.TrimSpace(c.PrivateKey) != "" && strings.TrimSpace(c.CallbackURL) != ""
}

type Installation struct {
	ID                   string    `json:"id"`
	GitHubInstallationID int64     `json:"github_installation_id"`
	AccountLogin         string    `json:"account_login"`
	CreatedAt            time.Time `json:"created_at"`
}

type Repository struct {
	ID                  string `json:"id"`
	GitHubRepositoryID  int64  `json:"github_repository_id"`
	FullName            string `json:"full_name"`
	DefaultBranch       string `json:"default_branch"`
	CloneURL            string `json:"clone_url"`
	InstallationID      int64  `json:"installation_id"`
	InstallationAccount string `json:"installation_account"`
	Linked              bool   `json:"linked"`
}

type Branch struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	Name         string    `json:"name"`
	HeadSHA      string    `json:"head_sha"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Commit struct {
	ID           string     `json:"id"`
	RepositoryID string     `json:"repository_id"`
	SHA          string     `json:"sha"`
	Message      string     `json:"message"`
	AuthorLogin  string     `json:"author_login"`
	CommittedAt  *time.Time `json:"committed_at,omitempty"`
}

type PullRequest struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	WorkItemID   string    `json:"work_item_id,omitempty"`
	Number       int64     `json:"number"`
	Title        string    `json:"title"`
	State        string    `json:"state"`
	Draft        bool      `json:"draft"`
	HeadSHA      string    `json:"head_sha"`
	HeadRef      string    `json:"head_ref,omitempty"`
	Body         string    `json:"body,omitempty"`
	URL          string    `json:"url"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CIRun struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	ExternalID   string    `json:"external_id"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	SHA          string    `json:"sha"`
	URL          string    `json:"url"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RepositoryContext struct {
	Repository   Repository    `json:"repository"`
	Branches     []Branch      `json:"branches"`
	Commits      []Commit      `json:"commits"`
	PullRequests []PullRequest `json:"pull_requests"`
	CIRuns       []CIRun       `json:"ci_runs"`
}

type RemoteInstallation struct {
	ID           int64
	AppID        int64
	AccountLogin string
}

type RemoteRepository struct {
	ID            int64
	FullName      string
	DefaultBranch string
	CloneURL      string
}

type RemoteTreeEntry struct {
	Path string
	Type string
	Size int
	SHA  string
}

type RemoteCommit struct {
	SHA         string
	Message     string
	AuthorLogin string
	CommittedAt *time.Time
}

type RemoteFile struct {
	Path    string
	Type    string
	Size    int
	SHA     string
	Content string
}

type RemotePullRequest struct {
	Number    int64
	Title     string
	State     string
	Draft     bool
	HeadSHA   string
	HeadRef   string
	Body      string
	URL       string
	UpdatedAt time.Time
}

type AppClient interface {
	Installation(context.Context, int64) (RemoteInstallation, error)
	Repositories(context.Context, int64) ([]RemoteRepository, error)
}

// ContentClient is optional so repository metadata tests and degraded local
// mode do not need to fake the GitHub Contents API.
type ContentClient interface {
	RepositoryTree(context.Context, int64, string, string) ([]RemoteTreeEntry, error)
	RepositoryFile(context.Context, int64, string, string, string) (RemoteFile, error)
}

// HistoryClient is optional so repository context can still use the GitHub
// history when webhook backfill has not populated the local store yet.
type HistoryClient interface {
	RepositoryCommits(context.Context, int64, string, string) ([]RemoteCommit, error)
	RepositoryPullRequests(context.Context, int64, string) ([]RemotePullRequest, error)
}

type RefClient interface {
	RepositoryHead(context.Context, int64, string, string) (string, error)
}

type PullRequestClient interface {
	CreateDraftPullRequest(context.Context, int64, string, string, string, string, string) (RemotePullRequest, error)
}

type Store interface {
	BeginInstallationState(context.Context, string, string, string, time.Time) error
	ConsumeInstallationState(context.Context, string) (string, string, error)
	UpsertInstallation(context.Context, string, int64, string) (Installation, error)
	ListInstallations(context.Context, string) ([]Installation, error)
	UpsertRepository(context.Context, string, int64, int64, string, string, string) (Repository, error)
	ProjectRepositoryIDs(context.Context, string, string) (map[string]bool, error)
	ListProjectRepositories(context.Context, string, string) ([]Repository, error)
	LinkRepository(context.Context, string, string, string) error
	UnlinkRepository(context.Context, string, string, string) error
	ListBranches(context.Context, string, string) ([]Branch, error)
	ListCommits(context.Context, string, string) ([]Commit, error)
	ListPullRequests(context.Context, string, string) ([]PullRequest, error)
	UpsertPullRequest(context.Context, string, string, PullRequest) (PullRequest, error)
	ListCIRuns(context.Context, string, string) ([]CIRun, error)
	HasPullRequest(context.Context, string, string, string, string) (bool, error)
	HasSuccessfulCI(context.Context, string, string, string, string) (bool, error)
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type Service struct {
	store       Store
	client      AppClient
	config      AppConfig
	audit       audit.Writer
	outbox      outbox.Writer
	transaction TransactionRunner
	now         func() time.Time
	snapshots   *SnapshotService
}

func NewService(store Store, client AppClient, config AppConfig, auditWriter audit.Writer, outboxWriter outbox.Writer, now func() time.Time, transaction TransactionRunner) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if transaction == nil {
		transaction = directTransactionRunner{}
	}
	return &Service{store: store, client: client, config: config, audit: auditWriter, outbox: outboxWriter, transaction: transaction, now: now}
}

func (s *Service) SetSnapshotService(snapshots *SnapshotService) {
	s.snapshots = snapshots
}

func (s *Service) SnapshotService() *SnapshotService {
	return s.snapshots
}

func (s *Service) StartInstallation(ctx context.Context, actor identity.Actor) (string, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryManage); err != nil {
		return "", err
	}
	if !s.config.Configured() || s.store == nil {
		return "", appNotConfigured()
	}
	state, err := randomState()
	if err != nil {
		return "", err
	}
	if err := s.store.BeginInstallationState(ctx, hashState(state), actor.ID, actor.OrganizationID, s.now().UTC().Add(10*time.Minute)); err != nil {
		return "", fmt.Errorf("begin GitHub App installation: %w", err)
	}
	query := url.Values{"state": {state}, "redirect_url": {s.config.CallbackURL}}
	return "https://github.com/apps/" + url.PathEscape(strings.TrimSpace(s.config.Slug)) + "/installations/new?" + query.Encode(), nil
}

func (s *Service) CompleteInstallation(ctx context.Context, state string, installationID int64, setupAction string) error {
	if !s.config.Configured() || s.store == nil || s.client == nil {
		return appNotConfigured()
	}
	if strings.TrimSpace(state) == "" || installationID <= 0 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "GitHub installation callback is invalid", nil)
	}
	if setupAction != "install" && setupAction != "update" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "GitHub installation was not completed", nil)
	}
	userID, organizationID, err := s.store.ConsumeInstallationState(ctx, hashState(state))
	if err != nil {
		return err
	}
	remote, err := s.client.Installation(ctx, installationID)
	if err != nil {
		return fmt.Errorf("load GitHub installation: %w", err)
	}
	if remote.ID != installationID || remote.AppID != s.config.ID || strings.TrimSpace(remote.AccountLogin) == "" {
		return apperr.New(apperr.CodeForbidden, 403, "GitHub installation does not belong to this Forgeflow App", nil)
	}
	var installation Installation
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var saveErr error
		installation, saveErr = s.store.UpsertInstallation(txCtx, organizationID, installationID, remote.AccountLogin)
		if saveErr != nil {
			return saveErr
		}
		if saveErr = s.record(txCtx, identity.Actor{Type: "human", ID: userID, OrganizationID: organizationID, Source: "github"}, "github.installation", installation.ID, nil, installation); saveErr != nil {
			return saveErr
		}
		return s.emit(txCtx, organizationID, installation.ID, "github.installation.connected", map[string]any{"installation_id": installationID, "account_login": remote.AccountLogin})
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) Repositories(ctx context.Context, actor identity.Actor, projectID string) ([]Repository, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return nil, err
	}
	if !s.config.Configured() || s.store == nil || s.client == nil {
		return nil, appNotConfigured()
	}
	installations, err := s.store.ListInstallations(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	if len(installations) == 0 {
		return []Repository{}, nil
	}
	stored := make(map[int64]Repository)
	for _, installation := range installations {
		remoteRepositories, listErr := s.client.Repositories(ctx, installation.GitHubInstallationID)
		if listErr != nil {
			return nil, fmt.Errorf("list GitHub repositories: %w", listErr)
		}
		for _, remote := range remoteRepositories {
			if remote.ID <= 0 || strings.TrimSpace(remote.FullName) == "" {
				continue
			}
			repository, upsertErr := s.upsertRepository(ctx, actor.OrganizationID, installation, remote)
			if upsertErr != nil {
				return nil, upsertErr
			}
			stored[remote.ID] = repository
		}
	}
	linked, err := s.store.ProjectRepositoryIDs(ctx, actor.OrganizationID, strings.TrimSpace(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]Repository, 0, len(stored))
	for _, repository := range stored {
		repository.Linked = linked[repository.ID]
		result = append(result, repository)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].FullName) < strings.ToLower(result[j].FullName) })
	return result, nil
}

func (s *Service) Installations(ctx context.Context, actor identity.Actor) ([]Installation, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, appNotConfigured()
	}
	return s.store.ListInstallations(ctx, actor.OrganizationID)
}

func (s *Service) ProjectRepositories(ctx context.Context, actor identity.Actor, projectID string) ([]Repository, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	return s.store.ListProjectRepositories(ctx, actor.OrganizationID, strings.TrimSpace(projectID))
}

func (s *Service) RepositoryContext(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (RepositoryContext, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return RepositoryContext{}, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(repositoryID) == "" {
		return RepositoryContext{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	repositories, err := s.store.ListProjectRepositories(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return RepositoryContext{}, err
	}
	var repository Repository
	for _, item := range repositories {
		if item.ID == repositoryID {
			repository = item
			break
		}
	}
	if repository.ID == "" {
		return RepositoryContext{}, apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
	}
	branches, err := s.store.ListBranches(ctx, actor.OrganizationID, repositoryID)
	if err != nil {
		return RepositoryContext{}, err
	}
	commits, err := s.store.ListCommits(ctx, actor.OrganizationID, repositoryID)
	if err != nil {
		return RepositoryContext{}, err
	}
	pullRequests, err := s.store.ListPullRequests(ctx, actor.OrganizationID, repositoryID)
	if err != nil {
		return RepositoryContext{}, err
	}
	if historyClient, ok := s.client.(HistoryClient); ok {
		if len(commits) == 0 {
			remoteCommits, historyErr := historyClient.RepositoryCommits(ctx, repository.InstallationID, repository.FullName, repository.DefaultBranch)
			if historyErr == nil {
				for _, remote := range remoteCommits {
					if strings.TrimSpace(remote.SHA) == "" {
						continue
					}
					commits = append(commits, Commit{RepositoryID: repositoryID, SHA: remote.SHA, Message: remote.Message, AuthorLogin: remote.AuthorLogin, CommittedAt: remote.CommittedAt})
				}
			}
		}
		if len(pullRequests) == 0 {
			remotePullRequests, historyErr := historyClient.RepositoryPullRequests(ctx, repository.InstallationID, repository.FullName)
			if historyErr == nil {
				for _, remote := range remotePullRequests {
					if remote.Number <= 0 {
						continue
					}
					pullRequests = append(pullRequests, PullRequest{RepositoryID: repositoryID, Number: remote.Number, Title: remote.Title, State: remote.State, Draft: remote.Draft, HeadSHA: remote.HeadSHA, HeadRef: remote.HeadRef, Body: remote.Body, URL: remote.URL, UpdatedAt: remote.UpdatedAt})
				}
			}
		}
	}
	ciRuns, err := s.store.ListCIRuns(ctx, actor.OrganizationID, repositoryID)
	if err != nil {
		return RepositoryContext{}, err
	}
	return RepositoryContext{Repository: repository, Branches: branches, Commits: commits, PullRequests: pullRequests, CIRuns: ciRuns}, nil
}

func (s *Service) LinkRepository(ctx context.Context, actor identity.Actor, projectID, repositoryID string) error {
	if err := s.authorize(actor, identity.CapabilityRepositoryManage); err != nil {
		return err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(repositoryID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.LinkRepository(txCtx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "repository.link", repositoryID, nil, map[string]string{"project_id": projectID, "repository_id": repositoryID}); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, repositoryID, "repository.linked", map[string]any{"project_id": projectID, "repository_id": repositoryID})
	})
}

func (s *Service) UnlinkRepository(ctx context.Context, actor identity.Actor, projectID, repositoryID string) error {
	if err := s.authorize(actor, identity.CapabilityRepositoryManage); err != nil {
		return err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(repositoryID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.UnlinkRepository(txCtx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "repository.unlink", repositoryID, map[string]string{"project_id": projectID, "repository_id": repositoryID}, nil); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, repositoryID, "repository.unlinked", map[string]any{"project_id": projectID, "repository_id": repositoryID})
	})
}

type DraftPullRequestInput struct {
	Title string
	Body  string
	Head  string
	Base  string
}

func (s *Service) CreateDraftPullRequest(ctx context.Context, actor identity.Actor, projectID, repositoryID string, input DraftPullRequestInput) (PullRequest, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryManage); err != nil {
		return PullRequest{}, err
	}
	if !s.config.Configured() || s.store == nil || s.client == nil {
		return PullRequest{}, appNotConfigured()
	}
	projectID = strings.TrimSpace(projectID)
	repositoryID = strings.TrimSpace(repositoryID)
	if projectID == "" || repositoryID == "" {
		return PullRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	if len([]rune(strings.TrimSpace(input.Title))) == 0 || len([]rune(input.Title)) > 256 {
		return PullRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "pull request title must be between 1 and 256 characters", nil)
	}
	if len([]byte(input.Body)) > 128*1024 {
		return PullRequest{}, apperr.New(apperr.CodeInvalidArgument, 422, "pull request body is too large", nil)
	}
	head, err := validateBranchRef(input.Head, "head")
	if err != nil {
		return PullRequest{}, err
	}
	repositories, err := s.store.ListProjectRepositories(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return PullRequest{}, err
	}
	var repository Repository
	for _, item := range repositories {
		if item.ID == repositoryID {
			repository = item
			break
		}
	}
	if repository.ID == "" {
		return PullRequest{}, apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
	}
	base := strings.TrimSpace(input.Base)
	if base == "" {
		base = repository.DefaultBranch
	}
	base, err = validateBranchRef(base, "base")
	if err != nil {
		return PullRequest{}, err
	}
	creator, ok := s.client.(PullRequestClient)
	if !ok {
		return PullRequest{}, appNotConfigured()
	}
	remote, err := creator.CreateDraftPullRequest(ctx, repository.InstallationID, repository.FullName, strings.TrimSpace(input.Title), input.Body, head, base)
	if err != nil {
		return PullRequest{}, fmt.Errorf("create GitHub draft pull request: %w", err)
	}
	if remote.Number <= 0 {
		return PullRequest{}, fmt.Errorf("GitHub draft pull request response has no number")
	}
	updatedAt := remote.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = s.now().UTC()
	}
	var stored PullRequest
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		stored, err = s.store.UpsertPullRequest(txCtx, actor.OrganizationID, repositoryID, PullRequest{
			RepositoryID: repositoryID,
			Number:       remote.Number,
			Title:        remote.Title,
			State:        remote.State,
			Draft:        remote.Draft,
			HeadSHA:      remote.HeadSHA,
			HeadRef:      remote.HeadRef,
			Body:         remote.Body,
			URL:          remote.URL,
			UpdatedAt:    updatedAt,
		})
		if err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "github.pull_request.created", stored.ID, nil, stored); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, stored.ID, "github.pull_request.created", map[string]any{
			"project_id": projectID, "repository_id": repositoryID, "number": stored.Number,
		})
	})
	if err != nil {
		return PullRequest{}, err
	}
	return stored, nil
}

func (s *Service) upsertRepository(ctx context.Context, organizationID string, installation Installation, remote RemoteRepository) (Repository, error) {
	var repository Repository
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		repository, err = s.store.UpsertRepository(txCtx, organizationID, installation.GitHubInstallationID, remote.ID, remote.FullName, remote.DefaultBranch, remote.CloneURL)
		return err
	})
	return repository, err
}

func (s *Service) authorize(actor identity.Actor, capability string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.audit == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "github", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()})
}

func (s *Service) emit(ctx context.Context, organizationID, aggregateID, eventType string, payload map[string]any) error {
	if s.outbox == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: organizationID, EventType: eventType, AggregateType: "github", AggregateID: aggregateID, IdempotencyKey: eventType + ":" + aggregateID, Payload: payload, OccurredAt: s.now().UTC()})
}

func appNotConfigured() error {
	return apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "GitHub App repository integration is not configured", nil)
}

func validateBranchRef(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "//") || strings.Contains(value, "@{") {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, field+" branch is invalid", nil)
	}
	for _, char := range value {
		if char <= 0x20 || strings.ContainsRune("~^:?*[\\", char) {
			return "", apperr.New(apperr.CodeInvalidArgument, 422, field+" branch is invalid", nil)
		}
	}
	if []byte(value)[0] == '-' {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, field+" branch is invalid", nil)
	}
	return value, nil
}

func randomState() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate GitHub installation state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashState(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}

type goAppClient struct {
	appID      int64
	privateKey *rsa.PrivateKey
	appClient  *gh.Client
}

func NewAppClient(config AppConfig) (AppClient, error) {
	if !config.Configured() {
		return nil, nil
	}
	privateKey, err := parseGitHubAppPrivateKey([]byte(config.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	transport := newGitHubAppTransport(http.DefaultTransport, config.ID, 0, privateKey)
	return &goAppClient{appID: config.ID, privateKey: privateKey, appClient: gh.NewClient(&http.Client{Transport: transport})}, nil
}

func (c *goAppClient) Installation(ctx context.Context, installationID int64) (RemoteInstallation, error) {
	installation, _, err := c.appClient.Apps.GetInstallation(ctx, installationID)
	if err != nil {
		return RemoteInstallation{}, err
	}
	return RemoteInstallation{ID: installation.GetID(), AppID: installation.GetAppID(), AccountLogin: installation.GetAccount().GetLogin()}, nil
}

func (c *goAppClient) Repositories(ctx context.Context, installationID int64) ([]RemoteRepository, error) {
	transport := newGitHubAppTransport(http.DefaultTransport, c.appID, installationID, c.privateKey)
	client := gh.NewClient(&http.Client{Transport: transport})
	result := make([]RemoteRepository, 0)
	for page := 1; page <= 100; page++ {
		response, apiResponse, err := client.Apps.ListRepos(ctx, &gh.ListOptions{Page: page, PerPage: 100})
		if err != nil {
			return nil, err
		}
		for _, repository := range response.Repositories {
			result = append(result, RemoteRepository{ID: repository.GetID(), FullName: repository.GetFullName(), DefaultBranch: repository.GetDefaultBranch(), CloneURL: repository.GetCloneURL()})
		}
		if apiResponse == nil || apiResponse.NextPage == 0 {
			return result, nil
		}
	}
	return nil, fmt.Errorf("GitHub repository list exceeds the supported page limit")
}

func (c *goAppClient) installationClient(installationID int64) (*gh.Client, error) {
	transport := newGitHubAppTransport(http.DefaultTransport, c.appID, installationID, c.privateKey)
	return gh.NewClient(&http.Client{Transport: transport}), nil
}

func (c *goAppClient) RepositoryTree(ctx context.Context, installationID int64, fullName, ref string) ([]RemoteTreeEntry, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, err
	}
	tree, _, err := client.Git.GetTree(ctx, owner, repository, ref, true)
	if err != nil {
		return nil, err
	}
	if tree.GetTruncated() {
		return nil, fmt.Errorf("GitHub repository tree is truncated")
	}
	if len(tree.Entries) > 5000 {
		return nil, fmt.Errorf("GitHub repository tree exceeds the supported file limit")
	}
	result := make([]RemoteTreeEntry, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		result = append(result, RemoteTreeEntry{Path: entry.GetPath(), Type: entry.GetType(), Size: entry.GetSize(), SHA: entry.GetSHA()})
	}
	return result, nil
}

func (c *goAppClient) RepositoryHead(ctx context.Context, installationID int64, fullName, branch string) (string, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return "", err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return "", err
	}
	item, _, err := client.Repositories.GetBranch(ctx, owner, repository, branch, 0)
	if err != nil {
		return "", err
	}
	if item == nil || item.Commit == nil || strings.TrimSpace(item.Commit.GetSHA()) == "" {
		return "", fmt.Errorf("GitHub branch has no commit SHA")
	}
	return item.Commit.GetSHA(), nil
}

func (c *goAppClient) RepositoryCommits(ctx context.Context, installationID int64, fullName, ref string) ([]RemoteCommit, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, err
	}
	items, _, err := client.Repositories.ListCommits(ctx, owner, repository, &gh.CommitsListOptions{SHA: ref, ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil {
		return nil, err
	}
	result := make([]RemoteCommit, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.GetSHA()) == "" {
			continue
		}
		author := item.GetAuthor().GetLogin()
		commit := item.GetCommit()
		var committedAt *time.Time
		if commit != nil {
			gitAuthor := commit.GetAuthor()
			if author == "" {
				author = gitAuthor.GetLogin()
			}
			if author == "" {
				author = gitAuthor.GetName()
			}
			date := gitAuthor.GetDate()
			committedAt = date.GetTime()
		}
		result = append(result, RemoteCommit{SHA: item.GetSHA(), Message: item.GetCommit().GetMessage(), AuthorLogin: author, CommittedAt: committedAt})
	}
	return result, nil
}

func (c *goAppClient) RepositoryPullRequests(ctx context.Context, installationID int64, fullName string) ([]RemotePullRequest, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, err
	}
	items, _, err := client.PullRequests.List(ctx, owner, repository, &gh.PullRequestListOptions{State: "all", ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil {
		return nil, err
	}
	result := make([]RemotePullRequest, 0, len(items))
	for _, item := range items {
		if item == nil || item.GetNumber() <= 0 {
			continue
		}
		head := item.GetHead()
		result = append(result, RemotePullRequest{Number: int64(item.GetNumber()), Title: item.GetTitle(), State: item.GetState(), Draft: item.GetDraft(), HeadSHA: head.GetSHA(), HeadRef: head.GetRef(), Body: item.GetBody(), URL: item.GetHTMLURL(), UpdatedAt: item.GetUpdatedAt().Time.UTC()})
	}
	return result, nil
}

func (c *goAppClient) RepositoryFile(ctx context.Context, installationID int64, fullName, path, ref string) (RemoteFile, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return RemoteFile{}, err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return RemoteFile{}, err
	}
	file, _, response, err := client.Repositories.GetContents(ctx, owner, repository, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return RemoteFile{}, err
	}
	if file == nil {
		return RemoteFile{}, fmt.Errorf("GitHub repository path is a directory")
	}
	content, err := file.GetContent()
	if err != nil {
		return RemoteFile{}, err
	}
	if response != nil && response.StatusCode >= http.StatusBadRequest {
		return RemoteFile{}, fmt.Errorf("GitHub repository file request failed with status %d", response.StatusCode)
	}
	return RemoteFile{Path: file.GetPath(), Type: file.GetType(), Size: file.GetSize(), SHA: file.GetSHA(), Content: content}, nil
}

func (c *goAppClient) CreateDraftPullRequest(ctx context.Context, installationID int64, fullName, title, body, head, base string) (RemotePullRequest, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return RemotePullRequest{}, err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return RemotePullRequest{}, err
	}
	draft := true
	pullRequest, _, err := client.PullRequests.Create(ctx, owner, repository, &gh.NewPullRequest{
		Title: &title,
		Body:  &body,
		Head:  &head,
		Base:  &base,
		Draft: &draft,
	})
	if err != nil {
		return RemotePullRequest{}, err
	}
	if pullRequest == nil {
		return RemotePullRequest{}, fmt.Errorf("GitHub returned an empty pull request")
	}
	result := RemotePullRequest{Number: int64(pullRequest.GetNumber()), Title: pullRequest.GetTitle(), State: pullRequest.GetState(), Draft: pullRequest.GetDraft(), Body: pullRequest.GetBody(), URL: pullRequest.GetHTMLURL(), UpdatedAt: pullRequest.GetUpdatedAt().Time}
	if head := pullRequest.GetHead(); head != nil {
		result.HeadSHA = head.GetSHA()
		result.HeadRef = head.GetRef()
	}
	return result, nil
}

func splitFullName(fullName string) (string, string, error) {
	parts := strings.SplitN(strings.Trim(fullName, "/"), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("GitHub repository full name is invalid")
	}
	return parts[0], parts[1], nil
}
