package mcp_test

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/app"
	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/mcp"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type liveRepositoryClient struct {
	head  string
	tree  []githubintegration.RemoteTreeEntry
	files map[string]githubintegration.RemoteFile
}

func (f *liveRepositoryClient) Installation(_ context.Context, installationID int64) (githubintegration.RemoteInstallation, error) {
	return githubintegration.RemoteInstallation{ID: installationID}, nil
}

func (f *liveRepositoryClient) Repositories(context.Context, int64) ([]githubintegration.RemoteRepository, error) {
	return nil, nil
}

func (f *liveRepositoryClient) RepositoryHead(context.Context, int64, string, string) (string, error) {
	return f.head, nil
}

func (f *liveRepositoryClient) RepositoryTree(context.Context, int64, string, string) ([]githubintegration.RemoteTreeEntry, error) {
	return append([]githubintegration.RemoteTreeEntry(nil), f.tree...), nil
}

func (f *liveRepositoryClient) RepositoryFile(_ context.Context, _ int64, _, path, _ string) (githubintegration.RemoteFile, error) {
	return f.files[path], nil
}

func TestRepositoryToolsUseLiveGitHubContentInsteadOfSnapshot(t *testing.T) {
	ctx := context.Background()
	store := githubintegration.NewMemoryStore()
	if _, err := store.UpsertInstallation(ctx, "org-1", 7, "acme"); err != nil {
		t.Fatal(err)
	}
	repository, err := store.UpsertRepository(ctx, "org-1", 7, 11, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}

	client := &liveRepositoryClient{
		head: "commit-1",
		tree: []githubintegration.RemoteTreeEntry{{Path: "README.md", Type: "blob", Size: 16, SHA: "sha-1"}},
		files: map[string]githubintegration.RemoteFile{
			"README.md": {Path: "README.md", Type: "blob", Size: 16, SHA: "sha-1", Content: "snapshot content"},
		},
	}
	service := githubintegration.NewService(store, client, githubintegration.AppConfig{}, nil, nil, nil, nil)
	snapshots := githubintegration.NewSnapshotService(service, githubintegration.NewMemorySnapshotStore())
	service.SetSnapshotService(snapshots)
	actor := identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}
	if _, err := snapshots.Refresh(ctx, actor, "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}

	client.files["README.md"] = githubintegration.RemoteFile{Path: "README.md", Type: "blob", Size: 14, SHA: "sha-2", Content: "github content"}
	application := app.New(1 << 20)
	adapter := mcp.NewServiceAdapter(actor, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "project-1")
	adapter.GitHub = service
	result, err := adapter.Call(ctx, "repository.get_file", map[string]any{"repository_id": repository.ID, "path": "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	file := payload["file"].(githubintegration.RepositoryFile)
	if file.Content != "github content" || file.SHA != "sha-2" || payload["source"] != "github" {
		t.Fatalf("repository file = %#v, payload = %#v; want live GitHub content", file, payload)
	}
}
