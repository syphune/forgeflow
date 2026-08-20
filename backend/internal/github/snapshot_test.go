package github

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type snapshotFakeClient struct {
	fakeAppClient
	head  string
	tree  []RemoteTreeEntry
	files map[string]RemoteFile
}

func (f *snapshotFakeClient) RepositoryHead(context.Context, int64, string, string) (string, error) {
	return f.head, nil
}

func (f *snapshotFakeClient) RepositoryTree(context.Context, int64, string, string) ([]RemoteTreeEntry, error) {
	return append([]RemoteTreeEntry(nil), f.tree...), nil
}

func (f *snapshotFakeClient) RepositoryFile(_ context.Context, _ int64, _, path, _ string) (RemoteFile, error) {
	return f.files[path], nil
}

func TestRefreshPersistsFixedCommitSnapshot(t *testing.T) {
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
	client := &snapshotFakeClient{
		head: "commit-42",
		tree: []RemoteTreeEntry{{Path: "cmd/main.go", Type: "blob", Size: 40}, {Path: "README.md", Type: "blob", Size: 12}},
		files: map[string]RemoteFile{
			"cmd/main.go": {Path: "cmd/main.go", Type: "blob", Content: "package main\nimport \"fmt\"\nfunc Run() { fmt.Println(\"run\") }\n"},
			"README.md":   {Path: "README.md", Type: "blob", Content: "run the app\n"},
		},
	}
	service := NewService(store, client, AppConfig{}, nil, nil, nil, nil)
	snapshots := NewSnapshotService(service, NewMemorySnapshotStore())
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true}}
	record, err := snapshots.Refresh(ctx, actor, "project-1", repository.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.CommitSHA != "commit-42" || record.Status != "READY" || record.FileCount != 2 {
		t.Fatalf("snapshot = %#v", record)
	}
	file, err := snapshots.File(ctx, actor, "project-1", repository.ID, record.ID, "cmd/main.go")
	if err != nil || file.Content == "" {
		t.Fatalf("snapshot file = %#v, err = %v", file, err)
	}
	matches, err := snapshots.Search(ctx, actor, "project-1", repository.ID, record.ID, "func Run", 10)
	if err != nil || len(matches) != 1 {
		t.Fatalf("snapshot search = %#v, err = %v", matches, err)
	}
	symbols, err := snapshots.Symbols(ctx, actor, "project-1", repository.ID, record.ID, "Run", 10)
	if err != nil || len(symbols) != 1 || symbols[0].Provenance != "EXTRACTED" {
		t.Fatalf("snapshot symbols = %#v, err = %v", symbols, err)
	}
	edges, err := snapshots.Edges(ctx, actor, "project-1", repository.ID, record.ID, "cmd/main.go", 10)
	if err != nil || len(edges) != 1 || edges[0].To != "fmt" {
		t.Fatalf("snapshot edges = %#v, err = %v", edges, err)
	}
}
