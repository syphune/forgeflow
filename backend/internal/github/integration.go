package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
	ID                 int64
	Slug               string
	PrivateKey         string
	CallbackURL        string
	WebBaseURL         string
	ClientID           string
	ClientSecret       string
	UserCallbackURL    string
	TokenEncryptionKey string
}

func (c AppConfig) Configured() bool {
	return c.ID > 0 && strings.TrimSpace(c.Slug) != "" && strings.TrimSpace(c.PrivateKey) != "" && strings.TrimSpace(c.CallbackURL) != ""
}

func (c AppConfig) UserAuthorizationConfigured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != "" && strings.TrimSpace(c.UserCallbackURL) != "" && strings.TrimSpace(c.TokenEncryptionKey) != ""
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
	ID               string    `json:"id"`
	RepositoryID     string    `json:"repository_id"`
	WorkItemID       string    `json:"work_item_id,omitempty"`
	Number           int64     `json:"number"`
	Title            string    `json:"title"`
	State            string    `json:"state"`
	Draft            bool      `json:"draft"`
	HeadSHA          string    `json:"head_sha"`
	HeadRef          string    `json:"head_ref,omitempty"`
	BaseRef          string    `json:"base_ref,omitempty"`
	Body             string    `json:"body,omitempty"`
	URL              string    `json:"url"`
	AuthorLogin      string    `json:"author_login,omitempty"`
	ChangedPaths     []string  `json:"changed_paths,omitempty"`
	ApprovedByLogins []string  `json:"approved_by_logins,omitempty"`
	ChangesRequested bool      `json:"changes_requested"`
	Merged           bool      `json:"merged"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CIRun struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	ExternalID   string    `json:"external_id"`
	CheckName    string    `json:"check_name"`
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

type UserAccessToken struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

type GitHubUser struct {
	ID    int64
	Login string
}

type StoredUserCredential struct {
	GitHubUserID          int64
	EncryptedAccessToken  string
	EncryptedRefreshToken string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

type CloneCredential struct {
	RepositoryID string    `json:"repository_id"`
	CloneURL     string    `json:"clone_url"`
	Username     string    `json:"username"`
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
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

type RepositoryInitialization struct {
	RepositoryID string `json:"repository_id"`
	FullName     string `json:"full_name"`
	Branch       string `json:"branch"`
	Path         string `json:"path"`
	CommitSHA    string `json:"commit_sha"`
}

type RemoteRepositoryInitialization struct {
	Path      string
	CommitSHA string
}

type RemotePullRequest struct {
	Number       int64
	Title        string
	State        string
	Draft        bool
	HeadSHA      string
	HeadRef      string
	BaseRef      string
	Body         string
	AuthorLogin  string
	ChangedPaths []string
	Merged       bool
	URL          string
	UpdatedAt    time.Time
}

type AppClient interface {
	Installation(context.Context, int64) (RemoteInstallation, error)
	Repositories(context.Context, int64) ([]RemoteRepository, error)
}

type UserAuthClient interface {
	ExchangeUserCode(context.Context, string, string, string) (UserAccessToken, error)
	RefreshUserToken(context.Context, string) (UserAccessToken, error)
	GitHubUser(context.Context, string) (GitHubUser, error)
}

// ContentClient is optional so repository metadata tests and degraded local
// mode do not need to fake the GitHub Contents API.
type ContentClient interface {
	RepositoryTree(context.Context, int64, string, string) ([]RemoteTreeEntry, error)
	RepositoryFile(context.Context, int64, string, string, string) (RemoteFile, error)
}

// HistoryClient is optional for degraded local/test clients. The production
// GitHub App client implements it so repository history stays live.
type HistoryClient interface {
	RepositoryCommits(context.Context, int64, string, string) ([]RemoteCommit, error)
	RepositoryPullRequests(context.Context, int64, string) ([]RemotePullRequest, error)
}

type RefClient interface {
	RepositoryHead(context.Context, int64, string, string) (string, error)
}

type BranchWriter interface {
	CreateBranch(context.Context, int64, string, string, string) error
}

type InstallationTokenClient interface {
	InstallationToken(context.Context, int64) (string, time.Time, error)
}

type RepositoryInitializer interface {
	InitializeRepository(context.Context, int64, string, string) (RemoteRepositoryInitialization, error)
}

type PullRequestClient interface {
	CreateDraftPullRequest(context.Context, int64, string, string, string, string, string) (RemotePullRequest, error)
}

type Store interface {
	BeginInstallationState(context.Context, string, string, string, time.Time) error
	ConsumeInstallationState(context.Context, string) (string, string, error)
	InstallationOrganization(context.Context, int64) (string, error)
	BeginUserAuthorizationState(context.Context, string, string, string, string, string, time.Time) error
	ConsumeUserAuthorizationState(context.Context, string) (string, string, string, string, error)
	GitHubUserID(context.Context, string) (int64, error)
	UpsertUserCredential(context.Context, string, StoredUserCredential) error
	GetUserCredential(context.Context, string) (StoredUserCredential, error)
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
	GetPullRequest(context.Context, string, string, string, string, string) (PullRequest, bool, error)
	HasRequiredCI(context.Context, string, string, string, string, string, []string) (bool, error)
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

func (s *Service) StartUserAuthorization(ctx context.Context, actor identity.Actor) (string, error) {
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return "", err
	}
	if !s.config.UserAuthorizationConfigured() || s.store == nil {
		return "", userAuthorizationNotConfigured()
	}
	if _, ok := s.client.(UserAuthClient); !ok {
		return "", userAuthorizationNotConfigured()
	}
	state, err := randomState()
	if err != nil {
		return "", err
	}
	verifier, err := randomState()
	if err != nil {
		return "", err
	}
	if err := s.store.BeginUserAuthorizationState(ctx, hashState(state), actor.ID, actor.OrganizationID, verifier, s.config.UserCallbackURL, s.now().UTC().Add(10*time.Minute)); err != nil {
		return "", fmt.Errorf("begin GitHub user authorization: %w", err)
	}
	query := url.Values{
		"client_id":             {strings.TrimSpace(s.config.ClientID)},
		"redirect_uri":          {strings.TrimSpace(s.config.UserCallbackURL)},
		"state":                 {state},
		"allow_signup":          {"false"},
		"code_challenge":        {codeChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	return "https://github.com/login/oauth/authorize?" + query.Encode(), nil
}

func (s *Service) CompleteUserAuthorization(ctx context.Context, state, code string) error {
	if !s.config.UserAuthorizationConfigured() || s.store == nil {
		return userAuthorizationNotConfigured()
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "GitHub user authorization callback is invalid", nil)
	}
	userID, organizationID, verifier, redirectURI, err := s.store.ConsumeUserAuthorizationState(ctx, hashState(state))
	if err != nil {
		return err
	}
	client, ok := s.client.(UserAuthClient)
	if !ok {
		return userAuthorizationNotConfigured()
	}
	credential, err := client.ExchangeUserCode(ctx, code, verifier, redirectURI)
	if err != nil {
		return fmt.Errorf("exchange GitHub user authorization: %w", err)
	}
	githubUser, err := client.GitHubUser(ctx, credential.AccessToken)
	if err != nil {
		return fmt.Errorf("verify GitHub user authorization: %w", err)
	}
	linkedGitHubUserID, err := s.store.GitHubUserID(ctx, userID)
	if err != nil {
		return err
	}
	if linkedGitHubUserID != githubUser.ID {
		return apperr.New(apperr.CodeForbidden, 403, "GitHub authorization belongs to a different account", nil)
	}
	accessToken, err := encryptCredential(s.config.TokenEncryptionKey, credential.AccessToken)
	if err != nil {
		return err
	}
	refreshToken := ""
	if strings.TrimSpace(credential.RefreshToken) != "" {
		refreshToken, err = encryptCredential(s.config.TokenEncryptionKey, credential.RefreshToken)
		if err != nil {
			return err
		}
	}
	authorizedActor := identity.Actor{Type: "human", ID: userID, OrganizationID: organizationID, Source: "github"}
	authorizedAfter := map[string]any{"github_user_id": githubUser.ID, "github_login": githubUser.Login}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.UpsertUserCredential(txCtx, userID, StoredUserCredential{
			GitHubUserID:          githubUser.ID,
			EncryptedAccessToken:  accessToken,
			EncryptedRefreshToken: refreshToken,
			AccessTokenExpiresAt:  credential.AccessTokenExpiresAt,
			RefreshTokenExpiresAt: credential.RefreshTokenExpiresAt,
		}); err != nil {
			return err
		}
		if err := s.record(txCtx, authorizedActor, "github.user_authorized", userID, nil, authorizedAfter); err != nil {
			return err
		}
		return s.emit(txCtx, organizationID, userID, "github.user.authorized", authorizedAfter)
	})
}

func (s *Service) CloneCredential(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (CloneCredential, error) {
	if actor.Type != "" && actor.Type != "human" {
		return CloneCredential{}, apperr.New(apperr.CodeForbidden, 403, "GitHub code credentials are only available to human users", nil)
	}
	if err := s.authorize(actor, identity.CapabilityRepositoryRead); err != nil {
		return CloneCredential{}, err
	}
	if !s.config.UserAuthorizationConfigured() || s.store == nil {
		return CloneCredential{}, userAuthorizationNotConfigured()
	}
	projectID = strings.TrimSpace(projectID)
	repositoryID = strings.TrimSpace(repositoryID)
	if projectID == "" || repositoryID == "" {
		return CloneCredential{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	repositories, err := s.store.ListProjectRepositories(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return CloneCredential{}, err
	}
	var repository Repository
	for _, item := range repositories {
		if item.ID == repositoryID {
			repository = item
			break
		}
	}
	if repository.ID == "" {
		return CloneCredential{}, apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
	}
	cloneURL, err := validateCloneURL(repository.CloneURL)
	if err != nil {
		return CloneCredential{}, err
	}
	client, ok := s.client.(UserAuthClient)
	if !ok {
		return CloneCredential{}, userAuthorizationNotConfigured()
	}
	accessToken, expiresAt, err := s.userAccessToken(ctx, actor.ID, client)
	if err != nil {
		return CloneCredential{}, err
	}
	return CloneCredential{RepositoryID: repository.ID, CloneURL: cloneURL, Username: "x-access-token", Token: accessToken, ExpiresAt: expiresAt}, nil
}

func (s *Service) userAccessToken(ctx context.Context, userID string, client UserAuthClient) (string, time.Time, error) {
	stored, err := s.store.GetUserCredential(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return "", time.Time{}, userAuthorizationRequired()
		}
		return "", time.Time{}, err
	}
	accessToken, err := decryptCredential(s.config.TokenEncryptionKey, stored.EncryptedAccessToken)
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now().UTC()
	if stored.AccessTokenExpiresAt.IsZero() || stored.AccessTokenExpiresAt.After(now.Add(30*time.Second)) {
		return accessToken, stored.AccessTokenExpiresAt, nil
	}
	if strings.TrimSpace(stored.EncryptedRefreshToken) == "" || (!stored.RefreshTokenExpiresAt.IsZero() && !stored.RefreshTokenExpiresAt.After(now)) {
		return "", time.Time{}, userAuthorizationRequired()
	}
	refreshToken, err := decryptCredential(s.config.TokenEncryptionKey, stored.EncryptedRefreshToken)
	if err != nil {
		return "", time.Time{}, err
	}
	refreshed, err := client.RefreshUserToken(ctx, refreshToken)
	if err != nil {
		return "", time.Time{}, userAuthorizationRequired()
	}
	newAccessToken, err := encryptCredential(s.config.TokenEncryptionKey, refreshed.AccessToken)
	if err != nil {
		return "", time.Time{}, err
	}
	newRefreshToken := stored.EncryptedRefreshToken
	refreshTokenExpiresAt := stored.RefreshTokenExpiresAt
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		newRefreshToken, err = encryptCredential(s.config.TokenEncryptionKey, refreshed.RefreshToken)
		if err != nil {
			return "", time.Time{}, err
		}
		refreshTokenExpiresAt = refreshed.RefreshTokenExpiresAt
	}
	if err := s.store.UpsertUserCredential(ctx, userID, StoredUserCredential{
		GitHubUserID:          stored.GitHubUserID,
		EncryptedAccessToken:  newAccessToken,
		EncryptedRefreshToken: newRefreshToken,
		AccessTokenExpiresAt:  refreshed.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}); err != nil {
		return "", time.Time{}, err
	}
	return refreshed.AccessToken, refreshed.AccessTokenExpiresAt, nil
}

func (s *Service) CompleteInstallation(ctx context.Context, state string, installationID int64, setupAction string) error {
	if !s.config.Configured() || s.store == nil || s.client == nil {
		return appNotConfigured()
	}
	if installationID <= 0 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "GitHub installation callback is invalid", nil)
	}
	if setupAction != "install" && setupAction != "update" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "GitHub installation was not completed", nil)
	}
	var userID, organizationID string
	if strings.TrimSpace(state) == "" {
		if setupAction != "update" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "GitHub installation callback is invalid", nil)
		}
		var err error
		organizationID, err = s.store.InstallationOrganization(ctx, installationID)
		if err != nil {
			return err
		}
	} else {
		var err error
		userID, organizationID, err = s.store.ConsumeInstallationState(ctx, hashState(state))
		if err != nil {
			return err
		}
	}
	remote, err := s.client.Installation(ctx, installationID)
	if err != nil {
		return fmt.Errorf("load GitHub installation: %w", err)
	}
	if remote.ID != installationID || remote.AppID != s.config.ID || strings.TrimSpace(remote.AccountLogin) == "" {
		return apperr.New(apperr.CodeForbidden, 403, "GitHub installation does not belong to this Forgeflow App", nil)
	}
	if strings.TrimSpace(state) == "" {
		return nil
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

// BranchPlanRepository exposes only the linked repository metadata needed by
// the branch-plan application service. The caller still supplies the actor so
// repository scope and capability checks remain at the GitHub boundary.
func (s *Service) BranchPlanRepository(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (Repository, error) {
	if err := s.authorize(actor, identity.CapabilityBranchManage); err != nil {
		return Repository{}, err
	}
	repositories, err := s.store.ListProjectRepositories(ctx, actor.OrganizationID, strings.TrimSpace(projectID))
	if err != nil {
		return Repository{}, err
	}
	for _, repository := range repositories {
		if repository.ID == strings.TrimSpace(repositoryID) {
			return repository, nil
		}
	}
	return Repository{}, apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
}

func (s *Service) BranchPlanHead(ctx context.Context, actor identity.Actor, repository Repository, branch string) (string, error) {
	if err := s.authorize(actor, identity.CapabilityBranchManage); err != nil {
		return "", err
	}
	if s.client == nil {
		return "", appNotConfigured()
	}
	refClient, ok := s.client.(RefClient)
	if !ok {
		return "", appNotConfigured()
	}
	return refClient.RepositoryHead(ctx, repository.InstallationID, repository.FullName, branch)
}

func (s *Service) BranchPlanCreateBranch(ctx context.Context, actor identity.Actor, repository Repository, branch, sha string) error {
	if err := s.authorize(actor, identity.CapabilityBranchManage); err != nil {
		return err
	}
	if s.client == nil {
		return appNotConfigured()
	}
	writer, ok := s.client.(BranchWriter)
	if !ok {
		return appNotConfigured()
	}
	return writer.CreateBranch(ctx, repository.InstallationID, repository.FullName, branch, sha)
}

func (s *Service) BranchPlanFindPullRequest(ctx context.Context, actor identity.Actor, projectID, repositoryID, head, base string) (PullRequest, bool, error) {
	if err := s.authorize(actor, identity.CapabilityBranchManage); err != nil {
		return PullRequest{}, false, err
	}
	repository, err := s.BranchPlanRepository(ctx, actor, projectID, repositoryID)
	if err != nil {
		return PullRequest{}, false, err
	}
	if history, ok := s.client.(HistoryClient); ok {
		items, historyErr := history.RepositoryPullRequests(ctx, repository.InstallationID, repository.FullName)
		if historyErr != nil {
			return PullRequest{}, false, historyErr
		}
		for _, item := range items {
			if item.HeadRef == head && item.BaseRef == base && strings.ToLower(item.State) == "open" {
				return PullRequest{RepositoryID: repositoryID, Number: item.Number, State: item.State, HeadRef: item.HeadRef, BaseRef: item.BaseRef, URL: item.URL, UpdatedAt: item.UpdatedAt}, true, nil
			}
		}
		return PullRequest{}, false, nil
	}
	items, err := s.store.ListPullRequests(ctx, actor.OrganizationID, repositoryID)
	if err != nil {
		return PullRequest{}, false, err
	}
	for _, item := range items {
		if item.HeadRef == head && (item.BaseRef == base || item.BaseRef == "") && strings.ToLower(item.State) == "open" {
			return item, true, nil
		}
	}
	return PullRequest{}, false, nil
}

func (s *Service) BranchPlanCreateDraftPullRequest(ctx context.Context, actor identity.Actor, projectID, repositoryID, title, body, head, base string) (PullRequest, error) {
	return s.CreateDraftPullRequest(ctx, actor, projectID, repositoryID, DraftPullRequestInput{Title: title, Body: body, Head: head, Base: base})
}

// RunnerGitCredential issues a short-lived GitHub App installation token for
// the trusted runner. It is deliberately separate from human clone tokens.
func (s *Service) RunnerGitCredential(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (CloneCredential, error) {
	if err := s.authorize(actor, identity.CapabilityBranchManage); err != nil {
		return CloneCredential{}, err
	}
	repository, err := s.BranchPlanRepository(ctx, actor, projectID, repositoryID)
	if err != nil {
		return CloneCredential{}, err
	}
	client, ok := s.client.(InstallationTokenClient)
	if !ok {
		return CloneCredential{}, appNotConfigured()
	}
	token, expiresAt, err := client.InstallationToken(ctx, repository.InstallationID)
	if err != nil {
		return CloneCredential{}, fmt.Errorf("issue GitHub runner credential: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return CloneCredential{}, fmt.Errorf("GitHub runner credential is empty")
	}
	cloneURL, err := validateCloneURL(repository.CloneURL)
	if err != nil {
		return CloneCredential{}, err
	}
	return CloneCredential{RepositoryID: repository.ID, CloneURL: cloneURL, Username: "x-access-token", Token: token, ExpiresAt: expiresAt}, nil
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
		remoteCommits, historyErr := historyClient.RepositoryCommits(ctx, repository.InstallationID, repository.FullName, repository.DefaultBranch)
		if historyErr == nil {
			commits = make([]Commit, 0, len(remoteCommits))
			for _, remote := range remoteCommits {
				if strings.TrimSpace(remote.SHA) == "" {
					continue
				}
				commits = append(commits, Commit{RepositoryID: repositoryID, SHA: remote.SHA, Message: remote.Message, AuthorLogin: remote.AuthorLogin, CommittedAt: remote.CommittedAt})
			}
		} else if apperr.From(historyErr).Code == apperr.CodeRepositoryEmpty {
			return RepositoryContext{}, historyErr
		}
		remotePullRequests, historyErr := historyClient.RepositoryPullRequests(ctx, repository.InstallationID, repository.FullName)
		if historyErr == nil {
			pullRequests = make([]PullRequest, 0, len(remotePullRequests))
			for _, remote := range remotePullRequests {
				if remote.Number <= 0 {
					continue
				}
				pullRequests = append(pullRequests, PullRequest{RepositoryID: repositoryID, Number: remote.Number, Title: remote.Title, State: remote.State, Draft: remote.Draft, HeadSHA: remote.HeadSHA, HeadRef: remote.HeadRef, BaseRef: remote.BaseRef, Body: remote.Body, URL: remote.URL, AuthorLogin: remote.AuthorLogin, ChangedPaths: append([]string(nil), remote.ChangedPaths...), Merged: remote.Merged, UpdatedAt: remote.UpdatedAt})
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

func (s *Service) InitializeRepository(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (RepositoryInitialization, error) {
	if actor.Type != "" && actor.Type != "human" {
		return RepositoryInitialization{}, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "only human users can initialize a GitHub repository", nil)
	}
	if err := s.authorize(actor, identity.CapabilityRepositoryManage); err != nil {
		return RepositoryInitialization{}, err
	}
	if !s.config.Configured() || s.store == nil || s.client == nil {
		return RepositoryInitialization{}, appNotConfigured()
	}
	projectID = strings.TrimSpace(projectID)
	repositoryID = strings.TrimSpace(repositoryID)
	if projectID == "" || repositoryID == "" {
		return RepositoryInitialization{}, apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "project_id and repository_id are required", nil)
	}
	repositories, err := s.store.ListProjectRepositories(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return RepositoryInitialization{}, err
	}
	var repository Repository
	for _, item := range repositories {
		if item.ID == repositoryID {
			repository = item
			break
		}
	}
	if repository.ID == "" {
		return RepositoryInitialization{}, apperr.New(apperr.CodeNotFound, http.StatusNotFound, "repository is not linked to this project", nil)
	}
	const branch = "main"
	if historyClient, ok := s.client.(HistoryClient); ok {
		commits, historyErr := historyClient.RepositoryCommits(ctx, repository.InstallationID, repository.FullName, repository.DefaultBranch)
		if historyErr != nil && apperr.From(historyErr).Code != apperr.CodeRepositoryEmpty {
			return RepositoryInitialization{}, fmt.Errorf("check GitHub repository before initialization: %w", historyErr)
		}
		if historyErr == nil && len(commits) > 0 {
			return RepositoryInitialization{}, apperr.New(apperr.CodeConflict, http.StatusConflict, "repository already has commits; initialization is only for empty repositories", map[string]any{"branch": branch})
		}
	} else if refClient, ok := s.client.(RefClient); ok {
		if _, headErr := refClient.RepositoryHead(ctx, repository.InstallationID, repository.FullName, branch); headErr == nil {
			return RepositoryInitialization{}, apperr.New(apperr.CodeConflict, http.StatusConflict, "repository already has a main branch", map[string]any{"branch": branch})
		} else if apperr.From(headErr).Code != apperr.CodeRepositoryEmpty {
			return RepositoryInitialization{}, fmt.Errorf("check GitHub repository before initialization: %w", headErr)
		}
	} else {
		return RepositoryInitialization{}, appNotConfigured()
	}
	initializer, ok := s.client.(RepositoryInitializer)
	if !ok {
		return RepositoryInitialization{}, appNotConfigured()
	}
	remote, err := initializer.InitializeRepository(ctx, repository.InstallationID, repository.FullName, branch)
	if err != nil {
		return RepositoryInitialization{}, fmt.Errorf("initialize GitHub repository: %w", err)
	}
	if strings.TrimSpace(remote.CommitSHA) == "" {
		return RepositoryInitialization{}, fmt.Errorf("GitHub repository initialization returned no commit SHA")
	}
	path := strings.TrimSpace(remote.Path)
	if path == "" {
		path = "README.md"
	}
	result := RepositoryInitialization{RepositoryID: repositoryID, FullName: repository.FullName, Branch: branch, Path: path, CommitSHA: remote.CommitSHA}
	after := map[string]any{"project_id": projectID, "repository_id": repositoryID, "full_name": repository.FullName, "branch": branch, "path": path, "commit_sha": remote.CommitSHA}
	if err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := s.store.UpsertRepository(txCtx, actor.OrganizationID, repository.InstallationID, repository.GitHubRepositoryID, repository.FullName, branch, repository.CloneURL); err != nil {
			return err
		}
		if err := s.record(txCtx, actor, "github.repository.initialized", repositoryID, nil, after); err != nil {
			return err
		}
		return s.emit(txCtx, actor.OrganizationID, repositoryID, "github.repository.initialized", after)
	}); err != nil {
		return RepositoryInitialization{}, err
	}
	return result, nil
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
			BaseRef:      remote.BaseRef,
			Body:         remote.Body,
			AuthorLogin:  remote.AuthorLogin,
			ChangedPaths: append([]string(nil), remote.ChangedPaths...),
			Merged:       remote.Merged,
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

func mapGitHubRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	var response *gh.ErrorResponse
	if !errors.As(err, &response) || response == nil {
		return err
	}
	status := 0
	if response.Response != nil {
		status = response.Response.StatusCode
	}
	message := strings.ToLower(strings.TrimSpace(response.Message))
	if !strings.Contains(message, "repository is empty") || (status != 0 && status != http.StatusNotFound && status != http.StatusConflict) {
		if status == http.StatusNotFound {
			return apperr.New(apperr.CodeNotFound, http.StatusNotFound, "GitHub resource was not found or is not accessible", nil)
		}
		return err
	}
	return apperr.New(
		apperr.CodeRepositoryEmpty,
		http.StatusConflict,
		"GitHub repository is empty; initialize it to create the first commit on main",
		map[string]any{"action": "initialize_repository"},
	)
}

func mapGitHubRepositoryResponseError(response *gh.Response, err error) error {
	if response != nil && response.Response != nil {
		switch response.Response.StatusCode {
		case http.StatusNotFound:
			return apperr.New(apperr.CodeNotFound, http.StatusNotFound, "GitHub resource was not found or is not accessible", nil)
		case http.StatusForbidden:
			return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "GitHub App cannot read this repository or branch", map[string]any{"permission": "contents:read"})
		}
	}
	return mapGitHubRepositoryError(err)
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
	appID        int64
	privateKey   *rsa.PrivateKey
	appClient    *gh.Client
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

const githubAPIRequestTimeout = 20 * time.Second

func NewAppClient(config AppConfig) (AppClient, error) {
	if !config.Configured() {
		return nil, nil
	}
	privateKey, err := parseGitHubAppPrivateKey([]byte(config.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	transport := newGitHubAppTransport(http.DefaultTransport, config.ID, 0, privateKey)
	return &goAppClient{appID: config.ID, privateKey: privateKey, appClient: gh.NewClient(&http.Client{Transport: transport, Timeout: githubAPIRequestTimeout}), clientID: strings.TrimSpace(config.ClientID), clientSecret: config.ClientSecret, httpClient: &http.Client{Timeout: 10 * time.Second}}, nil
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
	client := gh.NewClient(&http.Client{Transport: transport, Timeout: githubAPIRequestTimeout})
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
	return gh.NewClient(&http.Client{Transport: transport, Timeout: githubAPIRequestTimeout}), nil
}

func (c *goAppClient) InstallationToken(ctx context.Context, installationID int64) (string, time.Time, error) {
	if installationID <= 0 {
		return "", time.Time{}, fmt.Errorf("GitHub installation ID is invalid")
	}
	token, _, err := c.appClient.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return "", time.Time{}, mapGitHubRepositoryError(err)
	}
	if token == nil || strings.TrimSpace(token.GetToken()) == "" {
		return "", time.Time{}, fmt.Errorf("GitHub installation token is empty")
	}
	return token.GetToken(), token.GetExpiresAt().Time.UTC(), nil
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
		return nil, mapGitHubRepositoryError(err)
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
	item, response, err := client.Repositories.GetBranch(ctx, owner, repository, branch, 0)
	if err != nil {
		return "", mapGitHubRepositoryResponseError(response, err)
	}
	if item == nil || item.Commit == nil || strings.TrimSpace(item.Commit.GetSHA()) == "" {
		return "", fmt.Errorf("GitHub branch has no commit SHA")
	}
	return item.Commit.GetSHA(), nil
}

func (c *goAppClient) CreateBranch(ctx context.Context, installationID int64, fullName, branch, sha string) error {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return err
	}
	branch, err = validateBranchRef(branch, "working")
	if err != nil {
		return err
	}
	sha = strings.TrimSpace(sha)
	if sha == "" || len(sha) < 7 || len(sha) > 64 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "branch base SHA is invalid", nil)
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return err
	}
	_, _, err = client.Git.CreateRef(ctx, owner, repository, &gh.Reference{Ref: gh.String("refs/heads/" + branch), Object: &gh.GitObject{SHA: gh.String(sha)}})
	if err != nil {
		var response *gh.ErrorResponse
		if errors.As(err, &response) && response != nil && response.Response != nil && response.Response.StatusCode == http.StatusUnprocessableEntity {
			return apperr.New(apperr.CodeConflict, http.StatusConflict, "GitHub working branch already exists or the base commit is invalid", map[string]any{"branch": branch})
		}
		return mapGitHubRepositoryError(err)
	}
	return nil
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
		return nil, mapGitHubRepositoryError(err)
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
	result := make([]RemotePullRequest, 0)
	for page := 1; page <= 100; page++ {
		items, response, err := client.PullRequests.List(ctx, owner, repository, &gh.PullRequestListOptions{State: "all", ListOptions: gh.ListOptions{Page: page, PerPage: 100}})
		if err != nil {
			return nil, mapGitHubRepositoryError(err)
		}
		for _, item := range items {
			if item == nil || item.GetNumber() <= 0 {
				continue
			}
			head := item.GetHead()
			changedPaths, pathErr := c.pullRequestChangedPaths(ctx, client, owner, repository, item.GetNumber())
			if pathErr != nil {
				return nil, pathErr
			}
			result = append(result, RemotePullRequest{Number: int64(item.GetNumber()), Title: item.GetTitle(), State: item.GetState(), Draft: item.GetDraft(), HeadSHA: head.GetSHA(), HeadRef: head.GetRef(), BaseRef: item.GetBase().GetRef(), Body: item.GetBody(), AuthorLogin: item.GetUser().GetLogin(), ChangedPaths: changedPaths, Merged: item.GetMerged(), URL: item.GetHTMLURL(), UpdatedAt: item.GetUpdatedAt().Time.UTC()})
		}
		if response == nil || response.NextPage == 0 {
			return result, nil
		}
	}
	return nil, fmt.Errorf("GitHub pull request list exceeds the supported page limit")
}

func (c *goAppClient) pullRequestChangedPaths(ctx context.Context, client *gh.Client, owner, repository string, number int) ([]string, error) {
	paths := make([]string, 0)
	for page := 1; page <= 100; page++ {
		files, response, err := client.PullRequests.ListFiles(ctx, owner, repository, number, &gh.ListOptions{Page: page, PerPage: 100})
		if err != nil {
			return nil, mapGitHubRepositoryError(err)
		}
		for _, file := range files {
			if file != nil && strings.TrimSpace(file.GetFilename()) != "" {
				paths = append(paths, file.GetFilename())
			}
		}
		if response == nil || response.NextPage == 0 {
			return paths, nil
		}
	}
	return nil, fmt.Errorf("GitHub pull request file list exceeds the supported page limit")
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
		return RemoteFile{}, mapGitHubRepositoryError(err)
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

func (c *goAppClient) InitializeRepository(ctx context.Context, installationID int64, fullName, branch string) (RemoteRepositoryInitialization, error) {
	owner, repository, err := splitFullName(fullName)
	if err != nil {
		return RemoteRepositoryInitialization{}, err
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return RemoteRepositoryInitialization{}, err
	}
	message := "chore: initialize repository"
	committerName := "Forgeflow"
	committerEmail := "forgeflow@users.noreply.github.com"
	committer := &gh.CommitAuthor{Name: &committerName, Email: &committerEmail}
	response, _, err := client.Repositories.CreateFile(ctx, owner, repository, "README.md", &gh.RepositoryContentFileOptions{Message: &message, Content: []byte(fmt.Sprintf("# %s\n\nInitialized by Forgeflow.\n", repository)), Branch: &branch, Committer: committer})
	if err != nil {
		return RemoteRepositoryInitialization{}, mapGitHubInitializationError(err)
	}
	if response == nil || strings.TrimSpace(response.Commit.GetSHA()) == "" {
		return RemoteRepositoryInitialization{}, fmt.Errorf("GitHub repository initialization response has no commit SHA")
	}
	path := "README.md"
	if response.Content != nil && strings.TrimSpace(response.Content.GetPath()) != "" {
		path = response.Content.GetPath()
	}
	return RemoteRepositoryInitialization{Path: path, CommitSHA: response.Commit.GetSHA()}, nil
}

func mapGitHubInitializationError(err error) error {
	if mapped := mapGitHubRepositoryError(err); apperr.From(mapped).Code == apperr.CodeRepositoryEmpty {
		return mapped
	}
	var response *gh.ErrorResponse
	if !errors.As(err, &response) || response == nil || response.Response == nil {
		return err
	}
	switch response.Response.StatusCode {
	case http.StatusForbidden:
		return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "GitHub App cannot write this repository; grant Contents: write and update the installation", map[string]any{"permission": "contents:write"})
	case http.StatusNotFound:
		return apperr.New(apperr.CodeNotFound, http.StatusNotFound, "GitHub repository was not found or is no longer available to the App", nil)
	case http.StatusConflict:
		return apperr.New(apperr.CodeConflict, http.StatusConflict, "GitHub repository is not ready for initialization; try again shortly", nil)
	case http.StatusUnprocessableEntity:
		return apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "GitHub rejected the initial repository commit", nil)
	default:
		return err
	}
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
	result := RemotePullRequest{Number: int64(pullRequest.GetNumber()), Title: pullRequest.GetTitle(), State: pullRequest.GetState(), Draft: pullRequest.GetDraft(), Body: pullRequest.GetBody(), AuthorLogin: pullRequest.GetUser().GetLogin(), Merged: pullRequest.GetMerged(), URL: pullRequest.GetHTMLURL(), UpdatedAt: pullRequest.GetUpdatedAt().Time}
	if head := pullRequest.GetHead(); head != nil {
		result.HeadSHA = head.GetSHA()
		result.HeadRef = head.GetRef()
	}
	result.BaseRef = pullRequest.GetBase().GetRef()
	return result, nil
}

func splitFullName(fullName string) (string, string, error) {
	parts := strings.SplitN(strings.Trim(fullName, "/"), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("GitHub repository full name is invalid")
	}
	return parts[0], parts[1], nil
}
