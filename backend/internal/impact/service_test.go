package impact

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type testResolver struct {
	projects []ProjectSummary
}

func (r testResolver) ProjectsForRepository(context.Context, string, string) ([]ProjectSummary, error) {
	return append([]ProjectSummary(nil), r.projects...), nil
}

func testActor(id string, capabilities ...string) identity.Actor {
	set := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		set[capability] = true
	}
	return identity.Actor{Type: "human", ID: id, OrganizationID: "org-1", Capabilities: set}
}

func TestRequiredImpactNeedsEveryAffectedProjectApproval(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store, testResolver{projects: []ProjectSummary{{ID: "project-1", Key: "CORE"}, {ID: "project-2", Key: "WEB"}}})
	if _, err := service.SaveScope(ctx, testActor("owner", identity.CapabilityRepositoryManage), ScopeInput{ProjectID: "project-2", RepositoryID: "repo-1", PathPrefixes: []string{"apps/web"}, ImpactSummary: "Web application contract"}); err != nil {
		t.Fatal(err)
	}
	subject := Subject{OrganizationID: "org-1", ProjectID: "project-1", WorkItemID: "item-1", RepositoryID: "repo-1", AgentRunID: "run-1", BaseSHA: "base-1", DiffHash: "diff-1", ChangedPaths: []string{"apps/web/page.tsx"}, GateMode: GateRequired}
	report, err := service.EnsureReport(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnknownImpact || len(report.AffectedProjects) != 2 {
		t.Fatalf("report = %#v, want current and mapped project without unknown impact", report)
	}
	if _, err := service.ReadyForPublish(ctx, subject); apperr.From(err).Code != apperr.CodeImpactReviewRequired {
		t.Fatalf("ready before approval error = %v, want impact review required", err)
	}
	current := testActor("owner", identity.CapabilityAgentApprove, identity.CapabilityProjectRead)
	if _, err := service.Approve(ctx, current, "project-1", "run-1", ApprovalInput{ReportVersion: report.ReportVersion, Decision: DecisionApproved}); err != nil {
		t.Fatal(err)
	}
	pending, err := service.GetReport(ctx, current, "project-1", "run-1")
	if err != nil || pending.Status != ReportPending {
		t.Fatalf("pending report = %#v, err = %v", pending, err)
	}
	related := testActor("web-owner", identity.CapabilityAgentApprove)
	related.OrganizationID = "org-1"
	related.Capabilities[identity.CapabilityProjectRead] = true
	if visible, err := service.GetReport(ctx, related, "project-2", "run-1"); err != nil || visible.ID != report.ID {
		t.Fatalf("related project report = %#v, err = %v", visible, err)
	}
	if _, err := service.Approve(ctx, related, "project-2", "run-1", ApprovalInput{ReportVersion: report.ReportVersion, Decision: DecisionApproved}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ReadyForPublish(ctx, subject)
	if err != nil {
		t.Fatalf("ready after all approvals: %v", err)
	}
	if approved.Status != ReportApproved || len(approved.Approvals) != 2 {
		t.Fatalf("approved report = %#v", approved)
	}
}

func TestMissingMappingFailsClosedAndPathMatchingUsesBoundaries(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store, testResolver{projects: []ProjectSummary{{ID: "project-1"}, {ID: "project-2"}, {ID: "project-3"}}})
	actor := testActor("owner", identity.CapabilityRepositoryManage)
	if _, err := service.SaveScope(ctx, actor, ScopeInput{ProjectID: "project-2", RepositoryID: "repo-1", PathPrefixes: []string{"services/api"}}); err != nil {
		t.Fatal(err)
	}
	subject := Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-2", ChangedPaths: []string{"services/api-client/main.go"}, DiffHash: "diff-2", GateMode: GateRequired}
	report, err := service.EnsureReport(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if !report.UnknownImpact || len(report.AffectedProjects) != 2 {
		t.Fatalf("report = %#v, want current plus unmapped project", report)
	}
	for _, affected := range report.AffectedProjects {
		if affected.Project.ID == "project-2" {
			t.Fatalf("boundary-neighbor path incorrectly matched project-2: %#v", affected)
		}
	}
	if _, err := service.ReadyForPublish(ctx, subject); apperr.From(err).Code != apperr.CodeImpactReviewRequired {
		t.Fatalf("unknown mapping error = %v, want fail-closed impact review", err)
	}
}

func TestUnboundedHumanMappingFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store, testResolver{projects: []ProjectSummary{{ID: "project-1"}, {ID: "project-2"}}})
	if _, err := service.SaveScope(ctx, testActor("owner", identity.CapabilityRepositoryManage), ScopeInput{ProjectID: "project-2", RepositoryID: "repo-1", ImpactSummary: "Shared runtime contract"}); err != nil {
		t.Fatal(err)
	}
	report, err := service.EnsureReport(ctx, Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-unbounded", ChangedPaths: []string{"docs/README.md"}, DiffHash: "diff-unbounded", GateMode: GateRequired})
	if err != nil {
		t.Fatal(err)
	}
	if !report.UnknownImpact || len(report.AffectedProjects) != 2 || report.AffectedProjects[1].Reason != "mapping_unbounded" {
		t.Fatalf("unbounded mapping report = %#v", report)
	}
	if _, err := service.ReadyForPublish(ctx, Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-unbounded", ChangedPaths: []string{"docs/README.md"}, DiffHash: "diff-unbounded", GateMode: GateRequired}); apperr.From(err).Code != apperr.CodeImpactReviewRequired {
		t.Fatalf("unbounded mapping ready error = %v, want impact review required", err)
	}
}

func TestImpactReportBecomesStaleWhenDiffChanges(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), testResolver{projects: []ProjectSummary{{ID: "project-1"}}})
	subject := Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-3", ChangedPaths: []string{"README.md"}, DiffHash: "diff-a"}
	first, err := service.EnsureReport(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	changed := subject
	changed.DiffHash = "diff-b"
	second, err := service.EnsureReport(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if second.ReportVersion != first.ReportVersion+1 || second.InputHash == first.InputHash {
		t.Fatalf("report versions = %d, %d; hashes = %q, %q", first.ReportVersion, second.ReportVersion, first.InputHash, second.InputHash)
	}
	if firstLatest, err := service.store.LatestReport(ctx, "org-1", "run-3"); err != nil || firstLatest.ID != second.ID {
		t.Fatalf("latest report = %#v, err = %v", firstLatest, err)
	}
}

func TestRequiredImpactRejectsMissingDiffHashAndFinalDecisionChanges(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), testResolver{projects: []ProjectSummary{{ID: "project-1"}}})
	subject := Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-4", ChangedPaths: []string{"main.go"}, GateMode: GateRequired}
	if _, err := service.ReadyForPublish(ctx, subject); apperr.From(err).Code != apperr.CodeImpactReviewRequired {
		t.Fatalf("missing diff hash error = %v, want impact review required", err)
	}
	report, err := service.EnsureReport(ctx, Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-5", ChangedPaths: []string{"main.go"}, DiffHash: "diff-5", GateMode: GateRequired})
	if err != nil {
		t.Fatal(err)
	}
	actor := testActor("owner", identity.CapabilityAgentApprove, identity.CapabilityProjectRead)
	if _, err := service.Approve(ctx, actor, "project-1", "run-5", ApprovalInput{ReportVersion: report.ReportVersion, Decision: DecisionApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, actor, "project-1", "run-5", ApprovalInput{ReportVersion: report.ReportVersion, Decision: DecisionRejected}); apperr.From(err).Code != apperr.CodeConflict {
		t.Fatalf("final decision change error = %v, want conflict", err)
	}
}

func TestContextForActorHidesInaccessibleProjectsAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), testResolver{projects: []ProjectSummary{{ID: "project-1"}, {ID: "project-2"}}})
	actor := testActor("owner", identity.CapabilityProjectRead)
	actor.ProjectCapabilities = map[string]map[string]bool{"project-1": {identity.CapabilityProjectRead: true}, "project-2": {identity.CapabilityRepositoryRead: true}}
	result, err := service.ContextForActor(ctx, actor, "project-1", "repo-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RelatedProjects) != 0 || !result.UnknownImpact {
		t.Fatalf("scoped context = %#v, want hidden related project and unknown impact", result)
	}
	runner := testActor("run-1", identity.CapabilityProjectRead)
	runner.Source = "runner-mcp"
	runnerResult, err := service.ContextForActor(ctx, runner, "project-1", "repo-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runnerResult.RelatedProjects) != 1 || !runnerResult.UnknownImpact {
		t.Fatalf("runner context = %#v, want bounded related-project summary with unknown impact", runnerResult)
	}
}

func TestContextMarksUnresolvedCurrentProjectUnknown(t *testing.T) {
	result, err := NewService(NewMemoryStore(), testResolver{projects: []ProjectSummary{{ID: "project-2"}}}).Context(context.Background(), "org-1", "project-1", "repo-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.UnknownImpact || result.CurrentProject.ID != "project-1" {
		t.Fatalf("context = %#v, want unresolved current project to fail closed", result)
	}
}

func TestSaveScopeCannotUpdateAnotherProjectByID(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), testResolver{})
	owner := testActor("owner", identity.CapabilityRepositoryManage)
	scope, err := service.SaveScope(ctx, owner, ScopeInput{ProjectID: "project-2", RepositoryID: "repo-1", PathPrefixes: []string{"apps/web"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveScope(ctx, owner, ScopeInput{ID: scope.ID, ProjectID: "project-1", RepositoryID: "repo-1", PathPrefixes: []string{"services/api"}})
	if apperr.From(err).Code != apperr.CodeForbidden {
		t.Fatalf("cross-project scope update error = %v, want forbidden", err)
	}
}

func TestMemoryScopeStoreCannotOverwriteAnotherTenant(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if _, err := store.SaveScope(ctx, RepositoryProjectScope{ID: "scope-1", OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1"}); err != nil {
		t.Fatal(err)
	}
	_, err := store.SaveScope(ctx, RepositoryProjectScope{ID: "scope-1", OrganizationID: "org-2", ProjectID: "project-2", RepositoryID: "repo-2"})
	if apperr.From(err).Code != apperr.CodeForbidden {
		t.Fatalf("cross-tenant scope store error = %v, want forbidden", err)
	}
}
