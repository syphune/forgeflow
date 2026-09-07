//go:build integration

package impact

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
)

func TestPostgresImpactStoreKeepsSharedRepositoryScopesTenantBound(t *testing.T) {
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
	const (
		organizationID = "00000000-0000-0000-0000-000000000201"
		workspaceID    = "00000000-0000-0000-0000-000000000202"
		projectOneID   = "00000000-0000-0000-0000-000000000203"
		projectTwoID   = "00000000-0000-0000-0000-000000000204"
		repositoryID   = "00000000-0000-0000-0000-000000000205"
		scopeOneID     = "00000000-0000-0000-0000-000000000206"
		scopeTwoID     = "00000000-0000-0000-0000-000000000207"
	)
	fixtures := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, slug, display_name) VALUES ($1, 'impact-store', 'Impact store') ON CONFLICT (id) DO NOTHING`, []any{organizationID}},
		{`INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1, $2, 'IMPACT', 'Impact workspace') ON CONFLICT (id) DO NOTHING`, []any{workspaceID, organizationID}},
		{`INSERT INTO projects (id, organization_id, workspace_id, key, display_name) VALUES ($1, $2, $3, 'IMPACT_ONE', 'Impact one') ON CONFLICT (id) DO NOTHING`, []any{projectOneID, organizationID, workspaceID}},
		{`INSERT INTO projects (id, organization_id, workspace_id, key, display_name) VALUES ($1, $2, $3, 'IMPACT_TWO', 'Impact two') ON CONFLICT (id) DO NOTHING`, []any{projectTwoID, organizationID, workspaceID}},
		{`INSERT INTO repositories (id, organization_id, github_repository_id, full_name, clone_url) VALUES ($1, $2, 9201, 'impact/shared', 'https://github.com/impact/shared.git') ON CONFLICT (id) DO NOTHING`, []any{repositoryID, organizationID}},
		{`INSERT INTO repository_links (organization_id, project_id, repository_id) VALUES ($1, $2, $3), ($1, $4, $3) ON CONFLICT DO NOTHING`, []any{organizationID, projectOneID, repositoryID, projectTwoID}},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresStore(pool)
	for _, scope := range []RepositoryProjectScope{
		{ID: scopeOneID, OrganizationID: organizationID, ProjectID: projectOneID, RepositoryID: repositoryID, PathPrefixes: []string{"services/core"}, Provenance: ProvenanceHumanVerified, Status: ScopeActive, CreatedBy: "user-one", VerifiedBy: "user-one"},
		{ID: scopeTwoID, OrganizationID: organizationID, ProjectID: projectTwoID, RepositoryID: repositoryID, PathPrefixes: []string{"apps/web"}, Provenance: ProvenanceHumanVerified, Status: ScopeActive, CreatedBy: "user-two", VerifiedBy: "user-two"},
	} {
		if _, err := store.SaveScope(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}
	one, err := store.ListScopes(ctx, organizationID, projectOneID, repositoryID)
	if err != nil || len(one) != 1 || one[0].PathPrefixes[0] != "services/core" {
		t.Fatalf("project one scopes = %#v, err = %v", one, err)
	}
	two, err := store.ListScopes(ctx, organizationID, projectTwoID, repositoryID)
	if err != nil || len(two) != 1 || two[0].PathPrefixes[0] != "apps/web" {
		t.Fatalf("project two scopes = %#v, err = %v", two, err)
	}
	_, err = store.SaveScope(ctx, RepositoryProjectScope{ID: scopeOneID, OrganizationID: organizationID, ProjectID: projectTwoID, RepositoryID: repositoryID, PathPrefixes: []string{"must-not-overwrite"}, Provenance: ProvenanceHumanVerified, Status: ScopeActive})
	if apperr.From(err).Code != apperr.CodeForbidden {
		t.Fatalf("cross-project store update error = %v, want forbidden", err)
	}
}
