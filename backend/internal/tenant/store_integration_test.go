//go:build integration

package tenant

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
)

func TestPostgresStoreListsProjectsWithoutWorkspaceFilter(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := platformdb.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	_, err = NewPostgresStore(pool).ListProjects(ctx, "00000000-0000-0000-0000-000000000001", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStoreAddsProjectCreatorAsMember(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := platformdb.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const organizationID = "00000000-0000-0000-0000-000000000011"
	const userID = "00000000-0000-0000-0000-000000000012"
	const workspaceID = "00000000-0000-0000-0000-000000000013"
	for _, fixture := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, slug, display_name) VALUES ($1, 'tenant-integration', 'Tenant integration') ON CONFLICT (id) DO NOTHING`, []any{organizationID}},
		{`INSERT INTO users (id, github_user_id, login, display_name) VALUES ($1, 120012, 'tenant-integration-user', 'Tenant integration user') ON CONFLICT (id) DO NOTHING`, []any{userID}},
		{`INSERT INTO organization_memberships (organization_id, user_id, role_key) VALUES ($1, $2, 'owner') ON CONFLICT DO NOTHING`, []any{organizationID, userID}},
		{`INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1, $2, 'TENANT', 'Tenant workspace') ON CONFLICT (id) DO NOTHING`, []any{workspaceID, organizationID}},
	} {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}
	projectKey := fmt.Sprintf("TENANT_%x", time.Now().UnixNano())
	project, err := NewPostgresStore(pool).CreateProject(ctx, organizationID, workspaceID, projectKey, "Tenant app", userID)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := NewPostgresStore(pool).UpdateWorkspace(ctx, organizationID, workspaceID, "Tenant product"); err != nil || updated.DisplayName != "Tenant product" {
		t.Fatalf("updated workspace = %#v, err = %v", updated, err)
	}
	if updated, err := NewPostgresStore(pool).UpdateProject(ctx, organizationID, project.ID, "Tenant application"); err != nil || updated.DisplayName != "Tenant application" {
		t.Fatalf("updated project = %#v, err = %v", updated, err)
	}
	members, err := NewPostgresStore(pool).ListMembers(ctx, organizationID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].ID != userID || members[0].RoleKey != "owner" {
		t.Fatalf("project members = %#v", members)
	}
}
