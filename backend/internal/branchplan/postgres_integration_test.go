//go:build integration

package branchplan

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
)

func TestPostgresBranchPlanRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const (
		organizationID = "00000000-0000-0000-0000-000000000401"
		workspaceID    = "00000000-0000-0000-0000-000000000402"
		projectID      = "00000000-0000-0000-0000-000000000403"
		repositoryID   = "00000000-0000-0000-0000-000000000404"
		workflowID     = "00000000-0000-0000-0000-000000000405"
		statusID       = "00000000-0000-0000-0000-000000000406"
		typeID         = "00000000-0000-0000-0000-000000000407"
		workItemID     = "00000000-0000-0000-0000-000000000408"
	)
	fixtures := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, slug, display_name) VALUES ($1, 'branchplan-integration', 'Branch plan integration') ON CONFLICT (id) DO NOTHING`, []any{organizationID}},
		{`INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1, $2, 'BRANCH', 'Branch workspace') ON CONFLICT (id) DO NOTHING`, []any{workspaceID, organizationID}},
		{`INSERT INTO projects (id, organization_id, workspace_id, key, display_name) VALUES ($1, $2, $3, 'BRANCH', 'Branch project') ON CONFLICT (id) DO NOTHING`, []any{projectID, organizationID, workspaceID}},
		{`INSERT INTO repositories (id, organization_id, full_name, default_branch) VALUES ($1, $2, 'forgeflow/branchplan-integration', 'develop') ON CONFLICT (id) DO NOTHING`, []any{repositoryID, organizationID}},
		{`INSERT INTO workflows (id, organization_id, project_id, name) VALUES ($1, $2, $3, 'Branch workflow') ON CONFLICT (id) DO NOTHING`, []any{workflowID, organizationID, projectID}},
		{`INSERT INTO workflow_statuses (id, workflow_id, key, display_name, category, position) VALUES ($1, $2, 'READY', 'Ready', 'TODO', 10) ON CONFLICT (id) DO NOTHING`, []any{statusID, workflowID}},
		{`INSERT INTO work_item_types (id, key, display_name) VALUES ($1, 'BRANCH_TASK', 'Branch task') ON CONFLICT (id) DO NOTHING`, []any{typeID}},
		{`INSERT INTO work_items (id, organization_id, workspace_id, project_id, number, work_item_type_id, title, status_id, repository_id) VALUES ($1, $2, $3, $4, 401, $5, 'Branch plan task', $6, $7) ON CONFLICT (id) DO NOTHING`, []any{workItemID, organizationID, workspaceID, projectID, typeID, statusID, repositoryID}},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}

	store := NewPostgresStore(pool)
	policy := DefaultPolicy(organizationID, projectID)
	saved, err := store.SavePolicy(ctx, policy, 0)
	if err != nil {
		t.Fatal(err)
	}
	loadedPolicy, err := store.GetPolicy(ctx, organizationID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || loadedPolicy.WorkBranchTemplate != policy.WorkBranchTemplate {
		t.Fatalf("policy round trip = %#v", loadedPolicy)
	}

	created, err := store.CreatePlan(ctx, Plan{OrganizationID: organizationID, ProjectID: projectID, WorkItemID: workItemID, RepositoryID: repositoryID, WorkItemKey: "BRANCH-401", BranchType: Feature, BaseBranch: "develop", TargetBranch: "develop", WorkingBranch: "forgeflow/test/feature/branch-401", SyncBranches: []string{"main"}, CreatePullRequest: false, RequireSync: true, BaseSHA: "0123456789abcdef0123456789abcdef01234567", PolicyVersion: saved.Version, WorkItemVersion: 1, State: PlanPending, CreatedBy: "integration-user"})
	if err != nil {
		t.Fatal(err)
	}
	loadedPlan, err := store.GetPlan(ctx, organizationID, projectID, workItemID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedPlan.ID != created.ID || loadedPlan.CreatePullRequest || !loadedPlan.RequireSync {
		t.Fatalf("plan round trip = %#v", loadedPlan)
	}

	operation, err := store.CreateOperation(ctx, Operation{PlanID: created.ID, OrganizationID: organizationID, ProjectID: projectID, RepositoryID: repositoryID, Kind: OperationSyncPull, State: OperationPending, IdempotencyKey: "branchplan.integration.sync", SourceBranch: "develop", TargetBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateOperation(ctx, organizationID, projectID, operation.ID, operation.Version, OperationCompleted, "", 7, "https://github.com/forgeflow/branchplan-integration/pull/7")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PullRequestNumber != 7 || len(mustListOperations(t, ctx, store, organizationID, projectID, created.ID)) != 1 {
		t.Fatalf("operation round trip = %#v", updated)
	}
}

func mustListOperations(t *testing.T, ctx context.Context, store *PostgresStore, organizationID, projectID, planID string) []Operation {
	t.Helper()
	items, err := store.ListOperations(ctx, organizationID, projectID, planID)
	if err != nil {
		t.Fatal(err)
	}
	return items
}
