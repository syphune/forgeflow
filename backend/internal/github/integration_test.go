package github

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformidempotency "github.com/forgeflow/forgeflow/backend/internal/platform/idempotency"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type fakeAppClient struct {
	installations map[int64]RemoteInstallation
	repositories  map[int64][]RemoteRepository
	draft         RemotePullRequest
	draftCalls    int
	draftHead     string
	draftBase     string
}

type fakeHistoryClient struct {
	*fakeAppClient
	commits      []RemoteCommit
	pullRequests []RemotePullRequest
}

func (f *fakeAppClient) Installation(_ context.Context, installationID int64) (RemoteInstallation, error) {
	return f.installations[installationID], nil
}

func (f *fakeAppClient) Repositories(_ context.Context, installationID int64) ([]RemoteRepository, error) {
	return f.repositories[installationID], nil
}

func (f *fakeAppClient) CreateDraftPullRequest(_ context.Context, _ int64, _ string, _ string, _ string, head, base string) (RemotePullRequest, error) {
	f.draftCalls++
	f.draftHead = head
	f.draftBase = base
	return f.draft, nil
}

func (f *fakeHistoryClient) RepositoryCommits(_ context.Context, _ int64, _, _ string) ([]RemoteCommit, error) {
	return f.commits, nil
}

func (f *fakeHistoryClient) RepositoryPullRequests(_ context.Context, _ int64, _ string) ([]RemotePullRequest, error) {
	return f.pullRequests, nil
}

func TestInstallationSyncAndRepositoryLinking(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	client := &fakeAppClient{
		installations: map[int64]RemoteInstallation{42: {ID: 42, AppID: 123, AccountLogin: "acme"}},
		repositories: map[int64][]RemoteRepository{42: {
			{ID: 2, FullName: "acme/zeta", DefaultBranch: "main", CloneURL: "https://github.com/acme/zeta.git"},
			{ID: 1, FullName: "acme/alpha", DefaultBranch: "trunk", CloneURL: "https://github.com/acme/alpha.git"},
		}},
	}
	audits := audit.NewMemoryWriter()
	events := outbox.NewMemoryWriter()
	service := NewService(store, client, AppConfig{ID: 123, Slug: "forgeflow", PrivateKey: "test-key", CallbackURL: "https://forgeflow.test/callback"}, audits, events, nil, nil)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Source: "web", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true, identity.CapabilityRepositoryManage: true}}

	installationURL, err := service.StartInstallation(ctx, actor)
	if err != nil {
		t.Fatalf("start installation: %v", err)
	}
	parsed, err := url.Parse(installationURL)
	if err != nil || parsed.Host != "github.com" || parsed.Query().Get("state") == "" || parsed.Query().Get("redirect_url") == "" {
		t.Fatalf("unexpected installation URL: %s", installationURL)
	}
	if err := service.CompleteInstallation(ctx, parsed.Query().Get("state"), 42, "install"); err != nil {
		t.Fatalf("complete installation: %v", err)
	}
	if err := service.CompleteInstallation(ctx, parsed.Query().Get("state"), 42, "install"); !isCode(err, apperr.CodeUnauthorized) {
		t.Fatalf("replayed callback error = %v, want unauthorized", err)
	}
	installations, err := service.Installations(ctx, actor)
	if err != nil || len(installations) != 1 || installations[0].AccountLogin != "acme" {
		t.Fatalf("installations = %#v, err = %v", installations, err)
	}

	repositories, err := service.Repositories(ctx, actor, "project-1")
	if err != nil {
		t.Fatalf("sync repositories: %v", err)
	}
	if len(repositories) != 2 || repositories[0].FullName != "acme/alpha" || repositories[1].FullName != "acme/zeta" {
		t.Fatalf("repositories = %#v, want sorted repositories", repositories)
	}
	if err := service.LinkRepository(ctx, actor, "project-1", repositories[0].ID); err != nil {
		t.Fatalf("link repository: %v", err)
	}
	linked, err := service.ProjectRepositories(ctx, actor, "project-1")
	if err != nil || len(linked) != 1 || !linked[0].Linked {
		t.Fatalf("linked repositories = %#v, err = %v", linked, err)
	}
	if err := service.UnlinkRepository(ctx, actor, "project-1", repositories[0].ID); err != nil {
		t.Fatalf("unlink repository: %v", err)
	}
	if len(audits.Records()) != 3 || len(events.Events()) != 3 {
		t.Fatalf("audit/events = %d/%d, want installation, link and unlink", len(audits.Records()), len(events.Events()))
	}
}

func TestRepositoryContextFallsBackToGitHubHistory(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	repository, err := store.UpsertRepository(ctx, "org-1", 42, 7, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatalf("link repository: %v", err)
	}
	client := &fakeHistoryClient{
		fakeAppClient: &fakeAppClient{},
		commits:       []RemoteCommit{{SHA: "abc123", Message: "first commit", AuthorLogin: "syphune"}},
		pullRequests:  []RemotePullRequest{{Number: 19, Title: "Improve report", State: "open", URL: "https://github.com/acme/app/pull/19"}},
	}
	service := NewService(store, client, AppConfig{}, nil, nil, nil, nil)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}

	result, err := service.RepositoryContext(ctx, actor, "project-1", repository.ID)
	if err != nil {
		t.Fatalf("repository context: %v", err)
	}
	if len(result.Commits) != 1 || result.Commits[0].SHA != "abc123" {
		t.Fatalf("commits = %#v, want GitHub history", result.Commits)
	}
	if len(result.PullRequests) != 1 || result.PullRequests[0].Number != 19 {
		t.Fatalf("pull requests = %#v, want GitHub history", result.PullRequests)
	}
}

func TestRepositoryAccessIsScopedAndCapabilityChecked(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	client := &fakeAppClient{repositories: map[int64][]RemoteRepository{7: {{ID: 11, FullName: "acme/private", DefaultBranch: "main"}}}}
	service := NewService(store, client, AppConfig{ID: 1, Slug: "forgeflow", PrivateKey: "key", CallbackURL: "https://forgeflow.test/callback"}, nil, nil, nil, nil)
	if _, err := store.UpsertInstallation(ctx, "org-a", 7, "acme"); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	readOnly := identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-a", Source: "mcp", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}
	repositories, err := service.Repositories(ctx, readOnly, "project-a")
	if err != nil || len(repositories) != 1 {
		t.Fatalf("read repositories = %#v, err = %v", repositories, err)
	}
	if err := service.LinkRepository(ctx, readOnly, "project-a", repositories[0].ID); !isCode(err, apperr.CodeForbidden) {
		t.Fatalf("read-only link error = %v, want forbidden", err)
	}
	otherTenant := identity.Actor{Type: "human", ID: "user-2", OrganizationID: "org-b", Source: "web", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}
	otherRepositories, err := service.Repositories(ctx, otherTenant, "project-b")
	if err != nil || len(otherRepositories) != 0 {
		t.Fatalf("cross-tenant repositories = %#v, err = %v", otherRepositories, err)
	}
}

func TestStartInstallationRequiresConfiguredAppAndPermission(t *testing.T) {
	ctx := context.Background()
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryManage: true}}
	service := NewService(NewMemoryStore(), nil, AppConfig{}, nil, nil, nil, nil)
	if _, err := service.StartInstallation(ctx, actor); !isCode(err, apperr.CodeInternal) {
		t.Fatalf("unconfigured app error = %v, want internal/service unavailable", err)
	}
	actor.Capabilities = nil
	configured := NewService(NewMemoryStore(), nil, AppConfig{ID: 1, Slug: "forgeflow", PrivateKey: "key", CallbackURL: "https://forgeflow.test/callback"}, nil, nil, nil, nil)
	if _, err := configured.StartInstallation(ctx, actor); !isCode(err, apperr.CodeForbidden) {
		t.Fatalf("unauthorized start error = %v, want forbidden", err)
	}
}

func TestCreateDraftPullRequestRequiresLinkedRepositoryAndRecordsResult(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	client := &fakeAppClient{draft: RemotePullRequest{Number: 12, Title: "HRM-1 fix", State: "open", Draft: true, HeadSHA: "abc123", HeadRef: "forgeflow/hrm-1", Body: "details", URL: "https://github.com/acme/app/pull/12"}}
	service := NewService(store, client, AppConfig{ID: 1, Slug: "forgeflow", PrivateKey: "key", CallbackURL: "https://forgeflow.test/callback"}, audit.NewMemoryWriter(), outbox.NewMemoryWriter(), nil, nil)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Source: "web", Capabilities: map[string]bool{identity.CapabilityRepositoryManage: true}}
	if _, err := store.UpsertInstallation(ctx, "org-1", 42, "acme"); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	repository, err := store.UpsertRepository(ctx, "org-1", 42, 7, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatalf("link repository: %v", err)
	}
	item, err := service.CreateDraftPullRequest(ctx, actor, "project-1", repository.ID, DraftPullRequestInput{Title: "HRM-1 fix", Body: "details", Head: "forgeflow/hrm-1"})
	if err != nil {
		t.Fatalf("create draft pull request: %v", err)
	}
	if item.Number != 12 || !item.Draft || item.URL == "" || client.draftCalls != 1 || client.draftHead != "forgeflow/hrm-1" || client.draftBase != "main" {
		t.Fatalf("draft result = %#v, calls=%d head=%q base=%q", item, client.draftCalls, client.draftHead, client.draftBase)
	}
	if _, err := service.CreateDraftPullRequest(ctx, actor, "project-1", repository.ID, DraftPullRequestInput{Title: "bad", Head: "../main"}); !isCode(err, apperr.CodeInvalidArgument) {
		t.Fatalf("invalid branch error = %v, want invalid argument", err)
	}
}

func TestDraftPullRequestHTTPRequiresAndReplaysIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	client := &fakeAppClient{draft: RemotePullRequest{Number: 12, Title: "HRM-1 fix", State: "open", Draft: true, HeadRef: "forgeflow/hrm-1", URL: "https://github.com/acme/app/pull/12"}}
	service := NewService(store, client, AppConfig{ID: 1, Slug: "forgeflow", PrivateKey: "key", CallbackURL: "https://forgeflow.test/callback"}, nil, nil, nil, nil)
	if _, err := store.UpsertInstallation(ctx, "org-1", 42, "acme"); err != nil {
		t.Fatal(err)
	}
	repository, err := store.UpsertRepository(ctx, "org-1", 42, 7, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}
	idempotency := platformidempotency.NewMemoryStore()
	handler := NewIntegrationHandlerWithIdempotency(service, 1<<20, "/", idempotency)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryManage: true}}
	path := "/projects/project-1/repositories/" + repository.ID + "/pull-requests"
	request := func(key string) *httptest.ResponseRecorder {
		body := strings.NewReader(`{"title":"HRM-1 fix","head":"forgeflow/hrm-1"}`)
		req := httptest.NewRequest(http.MethodPost, path, body).WithContext(identity.WithActor(ctx, actor))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request(""); response.Code != http.StatusUnprocessableEntity || client.draftCalls != 0 {
		t.Fatalf("missing key response=%d calls=%d", response.Code, client.draftCalls)
	}
	first := request("draft-1")
	second := request("draft-1")
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated || client.draftCalls != 1 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("idempotent responses=%d/%d calls=%d bodies=%q/%q", first.Code, second.Code, client.draftCalls, first.Body.String(), second.Body.String())
	}
}

func isCode(err error, code string) bool {
	var appError *apperr.Error
	return errors.As(err, &appError) && appError.Code == code
}
