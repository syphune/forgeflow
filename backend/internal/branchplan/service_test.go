package branchplan

import (
	"context"
	"errors"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestBuildPreviewUsesConfiguredMatrixAndStableBranchTemplate(t *testing.T) {
	policy := DefaultPolicy("org-1", "project-1")
	item := WorkItemInput{ID: "item-1", Key: "ABC-12", Title: "Add Search", Type: "TASK", ProjectID: "project-1", RepositoryID: "repo-1", Assignee: "Alice Nguyen", Version: 3}

	feature, err := BuildPreview(policy, item, PreviewInput{})
	if err != nil {
		t.Fatal(err)
	}
	if feature.WorkingBranch != "forgeflow/alice-nguyen/feature/abc-12-add-search" || feature.BaseBranch != "develop" || feature.TargetBranch != "develop" {
		t.Fatalf("feature preview = %#v", feature)
	}
	if !feature.CreatePullRequest || feature.RequireSync {
		t.Fatalf("feature policy flags = %#v", feature)
	}

	bug, err := BuildPreview(policy, WorkItemInput{ID: item.ID, Key: item.Key, Title: item.Title, Type: "BUG", ProjectID: item.ProjectID, RepositoryID: item.RepositoryID, AssigneeID: "user-1", Version: item.Version}, PreviewInput{ReleaseBranch: "release/1.5"})
	if err != nil {
		t.Fatal(err)
	}
	if bug.BranchType != Fix || bug.BaseBranch != "release/1.5" || bug.TargetBranch != "release/1.5" || len(bug.SyncBranches) != 1 || bug.SyncBranches[0] != "develop" {
		t.Fatalf("release fix preview = %#v", bug)
	}
	if !bug.CreatePullRequest || !bug.RequireSync {
		t.Fatalf("release fix policy flags = %#v", bug)
	}

	release, err := BuildPreview(policy, item, PreviewInput{BranchType: Release, ReleaseBranch: "release/1.5"})
	if err != nil {
		t.Fatal(err)
	}
	if release.WorkingBranch != "release/1.5" || release.BaseBranch != "develop" || release.TargetBranch != "main" || len(release.SyncBranches) != 1 || release.SyncBranches[0] != "develop" {
		t.Fatalf("release preview = %#v", release)
	}
}

func TestBuildPreviewRejectsUnsafeOrIncompleteInputs(t *testing.T) {
	policy := DefaultPolicy("org-1", "project-1")
	item := WorkItemInput{ID: "item-1", Key: "ABC-12", Title: "Add Search", Type: "TASK", ProjectID: "project-1", RepositoryID: "repo-1", Version: 1}
	if _, err := BuildPreview(policy, item, PreviewInput{}); err == nil {
		t.Fatal("expected missing assignee error")
	}
	if _, err := BuildPreview(policy, item, PreviewInput{ReleaseBranch: "../main"}); err == nil {
		t.Fatal("expected unsafe release branch error")
	}
	policy.WorkBranchTemplate = "main/{unknown}"
	if err := policy.Validate(); err == nil {
		t.Fatal("expected unsafe template error")
	}
	policy = DefaultPolicy("org-1", "project-1")
	policy.Rules[0].TargetRole = Production
	if err := policy.Validate(); err == nil {
		t.Fatal("expected direct production target to be rejected")
	}
	policy = DefaultPolicy("org-1", "project-1")
	policy.Rules[0].RequireSync = true
	if err := policy.Validate(); err == nil {
		t.Fatal("expected missing sync role to be rejected")
	}
	if _, err := BuildPreview(DefaultPolicy("org-1", "project-1"), item, PreviewInput{ReleaseBranch: "hotfix/1.5"}); err == nil {
		t.Fatal("expected release prefix mismatch")
	}
	if _, err := BuildPreview(DefaultPolicy("org-1", "project-1"), item, PreviewInput{BranchType: Feature, ReleaseBranch: "release/1.5"}); err == nil {
		t.Fatal("expected release branch to be rejected for feature work")
	}
	policy = DefaultPolicy("org-1", "project-1")
	policy.WorkBranchTemplate = "forgeflow/{type}/{task-key}-{slug}"
	if _, err := BuildPreview(policy, item, PreviewInput{}); err != nil {
		t.Fatalf("template without assignee should be valid: %v", err)
	}
	policy = DefaultPolicy("org-1", "project-1")
	policy.DevelopmentBranch = policy.ProductionBranch
	if err := policy.Validate(); err == nil {
		t.Fatal("expected development and production branches to differ")
	}
	policy = DefaultPolicy("org-1", "project-1")
	policy.Rules[0].WorkingMode = Managed
	if err := policy.Validate(); err == nil {
		t.Fatal("expected managed non-release rule to be rejected")
	}
	policy = DefaultPolicy("org-1", "project-1")
	policy.Rules[0].SyncRoles = make([]BranchRole, 21)
	if err := policy.Validate(); err == nil {
		t.Fatal("expected excessive sync roles to be rejected")
	}
}

func TestPrepareIsIdempotentAndRetriesRecoverableRemoteFailure(t *testing.T) {
	provider := &fakeProvider{branches: map[string]string{"develop": "sha-develop"}, failCreates: 1}
	service := NewService(NewMemoryStore(nil), provider)
	actor := branchActor()
	item := branchItem("TASK")

	if _, err := service.Prepare(context.Background(), actor, item, PreviewInput{}); err == nil {
		t.Fatal("expected first branch creation to fail")
	}
	failed, err := service.GetPlan(context.Background(), actor, item.ProjectID, item.ID)
	if err != nil || failed.State != PlanFailed {
		t.Fatalf("failed plan = %#v, err=%v", failed, err)
	}
	ready, err := service.Prepare(context.Background(), actor, item, PreviewInput{})
	if err != nil {
		t.Fatal(err)
	}
	if ready.State != PlanReady || provider.createBranchCalls != 2 {
		t.Fatalf("retried plan = %#v, create calls=%d", ready, provider.createBranchCalls)
	}
	again, err := service.Prepare(context.Background(), actor, item, PreviewInput{})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != ready.ID || provider.createBranchCalls != 2 {
		t.Fatalf("idempotent plan = %#v, create calls=%d", again, provider.createBranchCalls)
	}
}

func TestPrepareCreatesMissingConfiguredBranchesFromRepositoryDefault(t *testing.T) {
	provider := &fakeProvider{branches: map[string]string{"main": "sha-main"}}
	service := NewService(NewMemoryStore(nil), provider)

	plan, err := service.Prepare(context.Background(), branchActor(), branchItem("TASK"), PreviewInput{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.State != PlanReady || plan.BaseBranch != "develop" || plan.BaseSHA != "sha-main" {
		t.Fatalf("prepared plan = %#v", plan)
	}
	if provider.branches["develop"] != "sha-main" || provider.branches[plan.WorkingBranch] != "sha-main" {
		t.Fatalf("provisioned branches = %#v", provider.branches)
	}
	if provider.createBranchCalls != 2 {
		t.Fatalf("create branch calls = %d, want base and working branches", provider.createBranchCalls)
	}
}

func TestPrepareProvisionsConfiguredProductionAndDevelopmentBranches(t *testing.T) {
	store := NewMemoryStore(nil)
	policy := DefaultPolicy("org-1", "project-1")
	policy.DevelopmentBranch = "development"
	policy.ProductionBranch = "production"
	if _, err := store.SavePolicy(context.Background(), policy, 0); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{branches: map[string]string{"main": "sha-main"}}
	service := NewService(store, provider)

	plan, err := service.Prepare(context.Background(), branchActor(), branchItem("TASK"), PreviewInput{BranchType: Hotfix})
	if err != nil {
		t.Fatal(err)
	}
	if plan.BaseBranch != "production" || plan.TargetBranch != "production" || len(plan.SyncBranches) != 1 || plan.SyncBranches[0] != "development" {
		t.Fatalf("prepared plan = %#v", plan)
	}
	for _, branch := range []string{"production", "development", plan.WorkingBranch} {
		if provider.branches[branch] != "sha-main" {
			t.Fatalf("branch %q = %q, want sha-main", branch, provider.branches[branch])
		}
	}
}

func TestPrepareDoesNotProvisionWhenConfiguredBranchCannotBeRead(t *testing.T) {
	provider := &fakeProvider{
		branches:   map[string]string{"main": "sha-main"},
		headErrors: map[string]error{"develop": errors.New("GitHub unavailable")},
	}
	service := NewService(NewMemoryStore(nil), provider)

	_, err := service.Prepare(context.Background(), branchActor(), branchItem("TASK"), PreviewInput{})
	if err == nil || apperr.From(err).Code != apperr.CodeConflict {
		t.Fatalf("prepare error = %v", err)
	}
	if provider.createBranchCalls != 0 {
		t.Fatalf("create branch calls = %d, want none", provider.createBranchCalls)
	}
}

func TestPullRequestAndSyncOperationsAreIdempotentAndMergeGated(t *testing.T) {
	provider := &fakeProvider{branches: map[string]string{"main": "sha-main", "develop": "sha-develop", "release/1.5": "sha-release"}}
	service := NewService(NewMemoryStore(nil), provider)
	actor := branchActor()
	item := branchItem("BUG")

	plan, err := service.Prepare(context.Background(), actor, item, PreviewInput{ReleaseBranch: "release/1.5"})
	if err != nil {
		t.Fatal(err)
	}
	firstPR, err := service.CreatePullRequest(context.Background(), actor, item.ProjectID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondPR, err := service.CreatePullRequest(context.Background(), actor, item.ProjectID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstPR.ID != secondPR.ID || provider.createPRCalls != 1 {
		t.Fatalf("PR idempotency failed: first=%#v second=%#v calls=%d", firstPR, secondPR, provider.createPRCalls)
	}

	if _, err := service.Sync(context.Background(), actor, item.ProjectID, item.ID); err != nil {
		t.Fatal(err)
	}
	if synced, err := service.HasSync(context.Background(), actor.OrganizationID, item.ProjectID, item.ID); err != nil || synced {
		t.Fatalf("sync should wait for merge: synced=%v err=%v", synced, err)
	}
	operations, err := service.ListOperations(context.Background(), actor, item.ProjectID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		if operation.Kind == OperationSyncPull {
			if err := service.MarkPullRequestMerged(context.Background(), actor.OrganizationID, plan.RepositoryID, operation.PullRequestNumber); err != nil {
				t.Fatal(err)
			}
		}
	}
	if synced, err := service.HasSync(context.Background(), actor.OrganizationID, item.ProjectID, item.ID); err != nil || !synced {
		t.Fatalf("sync should be satisfied after merge: synced=%v err=%v", synced, err)
	}
}

type fakeProvider struct {
	branches          map[string]string
	headErrors        map[string]error
	createBranchCalls int
	createPRCalls     int
	failCreates       int
}

func (f *fakeProvider) Repository(context.Context, identity.Actor, string, string) (Repository, error) {
	return Repository{ID: "repo-1", FullName: "acme/app", DefaultBranch: "main"}, nil
}

func (f *fakeProvider) Head(_ context.Context, _ identity.Actor, _ Repository, branch string) (string, error) {
	if err, ok := f.headErrors[branch]; ok {
		return "", err
	}
	sha, ok := f.branches[branch]
	if !ok {
		return "", apperr.New(apperr.CodeNotFound, 404, "branch not found", nil)
	}
	return sha, nil
}

func (f *fakeProvider) CreateBranch(_ context.Context, _ identity.Actor, _ Repository, branch, sha string) error {
	f.createBranchCalls++
	if f.failCreates > 0 {
		f.failCreates--
		return errors.New("temporary GitHub failure")
	}
	f.branches[branch] = sha
	return nil
}

func (f *fakeProvider) FindPullRequest(context.Context, identity.Actor, string, string, string, string) (PullRequest, bool, error) {
	return PullRequest{}, false, nil
}

func (f *fakeProvider) CreateDraftPullRequest(_ context.Context, _ identity.Actor, _, _, _, _, head, base string) (PullRequest, error) {
	f.createPRCalls++
	return PullRequest{Number: int64(100 + f.createPRCalls), URL: "https://github.com/acme/app/pull/1", State: "open", Head: head, Base: base}, nil
}

func branchActor() identity.Actor {
	return identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Login: "alice", Capabilities: map[string]bool{
		identity.CapabilityBranchManage: true,
		identity.CapabilityProjectRead:  true,
	}}
}

func branchItem(itemType string) WorkItemInput {
	return WorkItemInput{ID: "item-1", Key: "ABC-12", Title: "Fix issue", Type: itemType, ProjectID: "project-1", RepositoryID: "repo-1", Assignee: "alice", Version: 1}
}
