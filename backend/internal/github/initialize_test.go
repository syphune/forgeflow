package github

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type initializeClient struct {
	*fakeAppClient
	head        string
	headErr     error
	initialized bool
}

type initializeHistoryClient struct {
	*initializeClient
	commits    []RemoteCommit
	commitsErr error
}

func (c *initializeClient) RepositoryHead(context.Context, int64, string, string) (string, error) {
	if c.head != "" {
		return c.head, nil
	}
	return "", c.headErr
}

func (c *initializeClient) InitializeRepository(context.Context, int64, string, string) (RemoteRepositoryInitialization, error) {
	c.initialized = true
	c.head = "sha-init"
	return RemoteRepositoryInitialization{Path: "README.md", CommitSHA: c.head}, nil
}

func (c *initializeHistoryClient) RepositoryCommits(context.Context, int64, string, string) ([]RemoteCommit, error) {
	return c.commits, c.commitsErr
}

func TestInitializeRepositoryCreatesMainCommitForEmptyRepository(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	repository, err := store.UpsertRepository(ctx, "org-1", 42, 7, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}
	client := &initializeClient{
		fakeAppClient: &fakeAppClient{},
		headErr:       apperr.New(apperr.CodeRepositoryEmpty, 409, "repository is empty", nil),
	}
	audits := audit.NewMemoryWriter()
	events := outbox.NewMemoryWriter()
	service := NewService(store, client, AppConfig{ID: 1, Slug: "forgeflow", PrivateKey: "key", CallbackURL: "https://forgeflow.test/callback"}, audits, events, nil, nil)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryManage: true}}

	result, err := service.InitializeRepository(ctx, actor, "project-1", repository.ID)
	if err != nil {
		t.Fatalf("initialize repository: %v", err)
	}
	if result.Branch != "main" || result.Path != "README.md" || result.CommitSHA != "sha-init" || !client.initialized {
		t.Fatalf("result = %#v, initialized = %t", result, client.initialized)
	}
	linked, err := store.ListProjectRepositories(ctx, "org-1", "project-1")
	if err != nil || len(linked) != 1 || linked[0].DefaultBranch != "main" {
		t.Fatalf("linked repositories = %#v, err = %v", linked, err)
	}
	if len(audits.Records()) != 1 || len(events.Events()) != 1 {
		t.Fatalf("audit/events = %d/%d, want one initialization record each", len(audits.Records()), len(events.Events()))
	}

	if _, err := service.InitializeRepository(ctx, actor, "project-1", repository.ID); !isCode(err, apperr.CodeConflict) {
		t.Fatalf("second initialization error = %v, want conflict", err)
	}
}

func TestInitializeRepositoryRejectsNonHumanActor(t *testing.T) {
	service := NewService(NewMemoryStore(), &initializeClient{fakeAppClient: &fakeAppClient{}}, AppConfig{}, nil, nil, nil, nil)
	actor := identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryManage: true}}

	if _, err := service.InitializeRepository(context.Background(), actor, "project-1", "repository-1"); !isCode(err, apperr.CodeForbidden) {
		t.Fatalf("initialization error = %v, want forbidden", err)
	}
}

func TestInitializeRepositoryRejectsNonEmptyLiveHistory(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	repository, err := store.UpsertRepository(ctx, "org-1", 42, 7, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}
	client := &initializeHistoryClient{initializeClient: &initializeClient{fakeAppClient: &fakeAppClient{}}, commits: []RemoteCommit{{SHA: "existing"}}}
	service := NewService(store, client, AppConfig{ID: 1, Slug: "forgeflow", PrivateKey: "key", CallbackURL: "https://forgeflow.test/callback"}, nil, nil, nil, nil)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryManage: true}}

	if _, err := service.InitializeRepository(ctx, actor, "project-1", repository.ID); !isCode(err, apperr.CodeConflict) || client.initialized {
		t.Fatalf("initialization error = %v, initialized = %t, want conflict without remote write", err, client.initialized)
	}
}
