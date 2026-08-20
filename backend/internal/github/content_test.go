package github

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type contentFakeClient struct {
	fakeAppClient
	tree  []RemoteTreeEntry
	files map[string]RemoteFile
}

func (f *contentFakeClient) RepositoryTree(context.Context, int64, string, string) ([]RemoteTreeEntry, error) {
	return append([]RemoteTreeEntry(nil), f.tree...), nil
}

func (f *contentFakeClient) RepositoryFile(_ context.Context, _ int64, _, path, _ string) (RemoteFile, error) {
	return f.files[path], nil
}

func TestRepositoryContentIsScopedBoundedAndSearchable(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if _, err := store.UpsertInstallation(ctx, "org-1", 7, "acme"); err != nil {
		t.Fatal(err)
	}
	repository, err := store.UpsertRepository(ctx, "org-1", 7, 11, "acme/app", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}
	client := &contentFakeClient{
		tree:  []RemoteTreeEntry{{Path: "cmd/main.go", Type: "blob", Size: 25, SHA: "sha-1"}, {Path: "README.md", Type: "blob", Size: 10, SHA: "sha-2"}},
		files: map[string]RemoteFile{"cmd/main.go": {Path: "cmd/main.go", Type: "blob", Size: 25, SHA: "sha-1", Content: "package main\n// TODO: ship\n"}, "README.md": {Path: "README.md", Type: "blob", Size: 10, SHA: "sha-2", Content: "hello\n"}},
	}
	service := NewService(store, client, AppConfig{}, nil, nil, nil, nil)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}

	tree, err := service.RepositoryTree(ctx, actor, "project-1", repository.ID)
	if err != nil || len(tree) != 2 {
		t.Fatalf("tree = %#v, err = %v", tree, err)
	}
	matches, err := service.SearchRepository(ctx, actor, "project-1", repository.ID, "TODO", 10)
	if err != nil || len(matches) != 1 || matches[0].Line != 2 {
		t.Fatalf("matches = %#v, err = %v", matches, err)
	}
	if _, err := service.RepositoryFile(ctx, actor, "project-1", repository.ID, "../secret"); !isCode(err, apperr.CodeInvalidArgument) {
		t.Fatalf("parent traversal error = %v", err)
	}
	other := actor
	other.OrganizationID = "org-2"
	if _, err := service.RepositoryTree(ctx, other, "project-1", repository.ID); !isCode(err, apperr.CodeNotFound) {
		t.Fatalf("cross-tenant error = %v", err)
	}
}

func TestRelatedRepositoryFilesIncludesRootLevelPeers(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if _, err := store.UpsertInstallation(ctx, "org-1", 7, "acme"); err != nil {
		t.Fatal(err)
	}
	repository, err := store.UpsertRepository(ctx, "org-1", 7, 11, "acme/app", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}
	client := &contentFakeClient{tree: []RemoteTreeEntry{
		{Path: "README.md", Type: "blob", Size: 10, SHA: "sha-readme"},
		{Path: "LICENSE", Type: "blob", Size: 10, SHA: "sha-license"},
		{Path: "cmd/main.go", Type: "blob", Size: 10, SHA: "sha-main"},
	}}
	service := NewService(store, client, AppConfig{}, nil, nil, nil, nil)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}
	files, err := service.RelatedRepositoryFiles(ctx, actor, "project-1", repository.ID, "README.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "LICENSE" || files[1].Path != "cmd/main.go" {
		t.Fatalf("root-level related files = %#v", files)
	}
}
