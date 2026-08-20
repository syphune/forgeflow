//go:build integration

package github

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/intelligence"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/google/uuid"
)

func TestPostgresRepositoryIntelligenceRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := platformdb.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	organizationID, workspaceID, projectID, repositoryID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshotID, documentID, revisionID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO organizations (id,slug,display_name) VALUES ($1,$2,'Intelligence Test')`, organizationID, fmt.Sprintf("intelligence-test-%s", organizationID[:8]))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO workspaces (id,organization_id,key,display_name) VALUES ($1,$2,'TEST','Test')`, workspaceID, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO projects (id,organization_id,workspace_id,key,display_name) VALUES ($1,$2,$3,'INT','Intelligence')`, projectID, organizationID, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO repositories (id,organization_id,github_repository_id,full_name,default_branch) VALUES ($1,$2,99001,'acme/intelligence','main')`, repositoryID, organizationID)
	if err != nil {
		t.Fatal(err)
	}

	indexed, err := intelligence.NewIndexer(intelligence.Config{MaxFiles: 10, MaxFileBytes: 1024, MaxTotalBytes: 2048}).IndexFiles(ctx, "commit-42", map[string][]byte{"cmd/main.go": []byte("package main\nimport \"fmt\"\nfunc Run() { fmt.Println(\"run\") }\n")})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresSnapshotStore(pool)
	now := time.Now().UTC()
	record, err := store.SaveSnapshot(ctx, SnapshotRecord{ID: snapshotID, OrganizationID: organizationID, ProjectID: projectID, RepositoryID: repositoryID, CommitSHA: "commit-42", RefName: "main", Status: "READY", FileCount: len(indexed.Files), SymbolCount: len(indexed.Symbols), StartedAt: now, FinishedAt: &now, CreatedAt: now}, indexed)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != snapshotID {
		t.Fatalf("snapshot id = %q", record.ID)
	}
	file, err := store.GetSnapshotFile(ctx, organizationID, projectID, repositoryID, snapshotID, "cmd/main.go")
	if err != nil || file.Content == "" {
		t.Fatalf("snapshot file = %#v, err = %v", file, err)
	}
	symbols, err := store.ListSnapshotSymbols(ctx, organizationID, projectID, repositoryID, snapshotID, "Run", 10)
	if err != nil || len(symbols) != 1 {
		t.Fatalf("snapshot symbols = %#v, err = %v", symbols, err)
	}
	edges, err := store.ListSnapshotEdges(ctx, organizationID, projectID, repositoryID, snapshotID, "cmd/main.go", 10)
	if err != nil || len(edges) != 1 || edges[0].To != "fmt" {
		t.Fatalf("snapshot edges = %#v, err = %v", edges, err)
	}

	knowledge := NewPostgresKnowledgeStore(pool)
	document, err := knowledge.Create(ctx, KnowledgeDocument{ID: documentID, OrganizationID: organizationID, ProjectID: projectID, RepositoryID: repositoryID, Slug: "testing", Title: "Testing", Kind: "TESTING", CurrentProvenance: "MANUAL", CreatedBy: "user-1", CreatedAt: now, UpdatedAt: now}, KnowledgeRevision{ID: revisionID, DocumentID: documentID, RevisionNumber: 1, Content: "Run go test.", Provenance: "MANUAL", SourceSnapshotID: snapshotID, CreatedBy: "user-1", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if document.LatestRevision == nil || document.LatestRevision.Content != "Run go test." {
		t.Fatalf("knowledge document = %#v", document)
	}
	fetched, err := knowledge.Get(ctx, organizationID, projectID, repositoryID, documentID)
	if err != nil || fetched.LatestRevision == nil || fetched.LatestRevision.RevisionNumber != 1 {
		t.Fatalf("fetched knowledge = %#v, err = %v", fetched, err)
	}
}
