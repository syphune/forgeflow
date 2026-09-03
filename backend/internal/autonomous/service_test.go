package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/runner"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

func TestPolicyNormalizeUsesSafeDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input Policy
		want  Policy
	}{
		{name: "zero value keeps explicit booleans", input: Policy{}, want: Policy{Enabled: false, Providers: DefaultPolicy().Providers, Runtime: "server", MaxAttempts: 3, TimeoutSeconds: 3600, AutoRetry: false, AutoCreatePR: false, TestScope: "unresolved_only", MCPPermissions: DefaultPolicy().MCPPermissions, ExecutionProfile: "default"}},
		{name: "bounded values are preserved", input: Policy{Enabled: false, Providers: []string{"claude"}, Runtime: "desktop", MaxAttempts: 2, TimeoutSeconds: 90, AutoRetry: false, AutoCreatePR: false, TestScope: "full_regression", ExecutionProfile: "safe"}, want: Policy{Enabled: false, Providers: []string{"claude"}, Runtime: "desktop", MaxAttempts: 2, TimeoutSeconds: 90, AutoRetry: false, AutoCreatePR: false, TestScope: "full_regression", MCPPermissions: DefaultPolicy().MCPPermissions, ExecutionProfile: "safe"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.Normalize()
			if got.Enabled != tt.want.Enabled || got.Runtime != tt.want.Runtime || got.MaxAttempts != tt.want.MaxAttempts || got.TimeoutSeconds != tt.want.TimeoutSeconds || got.TestScope != tt.want.TestScope || got.ExecutionProfile != tt.want.ExecutionProfile {
				t.Fatalf("normalized policy = %#v, want %#v", got, tt.want)
			}
			if len(got.Providers) != len(tt.want.Providers) || len(got.MCPPermissions) != len(tt.want.MCPPermissions) {
				t.Fatalf("normalized policy collections = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeStartInputTrimsAndBoundsModel(t *testing.T) {
	got, err := normalizeStartInput(StartInput{ProjectID: " project-1 ", Objective: " run ", AgentProvider: " CODEX ", Model: " gpt-5-codex ", ReasoningEffort: " high "})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "project-1" || got.Objective != "run" || got.AgentProvider != "codex" || got.Model != "gpt-5-codex" || got.ReasoningEffort != "high" {
		t.Fatalf("normalized input = %#v", got)
	}

	if _, err := normalizeStartInput(StartInput{ProjectID: "project-1", Objective: "run", Model: strings.Repeat("m", 121)}); err == nil {
		t.Fatal("expected an overlong model to be rejected")
	}
	if _, err := normalizeStartInput(StartInput{ProjectID: "project-1", Objective: "run", ReasoningEffort: strings.Repeat("r", 65)}); err == nil {
		t.Fatal("expected an overlong reasoning effort to be rejected")
	}
	invalid := string([]byte{'r', 0xe1})
	got, err = normalizeStartInput(StartInput{ProjectID: "project-1", Objective: invalid})
	if err != nil || got.Objective != "r�" {
		t.Fatalf("invalid UTF-8 objective = %q, err = %v", got.Objective, err)
	}
}

func TestParseIntakeDraftPrefersCompleteResponseOverPromptSchema(t *testing.T) {
	output := `startup text
{"summary":"short draft summary","fields":{"GOAL|PROBLEM_STATEMENT|EXPECTED_BEHAVIOR":"draft value"},"reproduction_steps":[{"action":"","expected_result":"","observed_result":""}],"acceptance_criteria":[{"statement":"specific observable outcome"}],"regression_test_cases":[{"scenario":"","expected_result":"","gate_class":"REQUIRED"}],"context_refs":[{"module":"","file":"","symbol":"","commit":"","rationale":""}],"analysis":{"root_cause_hypothesis":"","blast_radius":"","implementation_plan":"","test_plan":"","confidence":0},"clarification_questions":[]}
{"summary":"actual draft summary","fields":{"PROBLEM_STATEMENT":"real problem","EXPECTED_BEHAVIOR":"real behavior"},"reproduction_steps":[{"action":"open the screen","expected_result":"the value is shown","observed_result":"the value is missing"}],"acceptance_criteria":[{"statement":"the value is shown after reopening"}],"regression_test_cases":[{"scenario":"reopen the screen","expected_result":"the value remains visible","gate_class":"REQUIRED"}],"context_refs":[{"module":"web","file":"src/menu.ts","symbol":"loadMenu","rationale":"reads the saved value"}],"analysis":{"root_cause_hypothesis":"the read mapping omits the value","implementation_plan":"trace the save and read mapping","test_plan":"add a persistence regression test","confidence":0.6},"clarification_questions":[]}`

	draft, err := parseIntakeDraft(output)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Summary != "actual draft summary" || draft.Fields["PROBLEM_STATEMENT"] != "real problem" {
		t.Fatalf("parsed draft = %#v", draft)
	}
}

func TestHandleAgentRunResultMarksCodexAuthAsFailed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	run, err := store.Create(ctx, "org-1", StartInput{ProjectID: "project-1", WorkItemID: "item-1", RepositoryID: "repo-1", Objective: "run the agent"}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil, nil)
	actor := identity.Actor{Type: "system", ID: "worker", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	updated, err := service.HandleAgentRunResult(ctx, actor, "project-1", run.ID, runner.Result{Error: "Codex CLI authentication is required", ErrorCode: runner.ErrorCodeAuthRequired})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusFailed || updated.Gate != GateRunner {
		t.Fatalf("auth result = status %s gate %s", updated.Status, updated.Gate)
	}
}

func TestAutonomousStartRunsReadOnlyIntakeBeforeSpecificationReview(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	spec := specification.NewService(specification.NewMemoryStore(), now)
	workflowService := workflow.NewService(workflow.Default())
	workItems := workitem.NewService(workitem.NewMemoryRepository(now), spec, workflowService, audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	spec.SetWorkItemStateReader(workItems)
	agentRuns := agentrun.NewService(agentrun.NewMemoryStore(), workItems)
	autonomousStore := NewMemoryStore()
	service := NewService(autonomousStore, workItems, spec, agentRuns)
	actor := identity.Actor{Type: "human", ID: "manager-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}

	run, err := service.Start(ctx, actor, StartInput{ProjectID: "project-1", WorkItemType: "BUG", Title: "Menu quantity is missing", RepositoryID: "repo-1", Objective: "The saved menu quantity does not appear on the POS screen.", AgentProvider: "codex", Policy: DefaultPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusIntake || run.Phase != PhaseIntake || run.CurrentAgentRunID == "" {
		t.Fatalf("intake run = %#v", run)
	}
	child, _, _, err := agentRuns.Get(ctx, actor, "project-1", run.CurrentAgentRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ExecutionPolicy["mode"] != "autonomous-intake" || child.ExecutionPolicy["read_only"] != true {
		t.Fatalf("intake policy = %#v", child.ExecutionPolicy)
	}
	if child.ExecutionPolicy["created_by"] != actor.ID {
		t.Fatalf("intake creator = %#v, want %q", child.ExecutionPolicy["created_by"], actor.ID)
	}
	if child.ExecutionInputs.SpecificationVersion != 1 || !strings.Contains(child.ExecutionInputs.Prompt, "Return exactly one JSON object") || !strings.Contains(child.ExecutionInputs.Prompt, "no checked-out repository") || !strings.Contains(child.ExecutionInputs.Prompt, "work_item.get") || !strings.Contains(child.ExecutionInputs.Prompt, run.WorkItemID) {
		t.Fatalf("intake inputs = %#v", child.ExecutionInputs)
	}
	for _, permission := range child.ExecutionInputs.MCPPermissions {
		if permission == "specification.propose" || permission == "work_item.update" || permission == "work_item.get_context" || permission == "repository.get" {
			t.Fatalf("intake received out-of-scope permission %q", permission)
		}
	}
	for _, permission := range intakeReadPermissions {
		if !strings.Contains(strings.Join(child.ExecutionInputs.MCPPermissions, "\n"), permission) {
			t.Fatalf("intake is missing GitHub read permission %q: %#v", permission, child.ExecutionInputs.MCPPermissions)
		}
	}
	legacyItem, err := workItems.Create(ctx, workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1"}, actor, workitem.CreateInput{Type: workitem.Bug, Title: "Legacy menu bug", Description: run.Objective, RepositoryID: "repo-1"})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := autonomousStore.Create(ctx, "org-1", StartInput{ProjectID: "project-1", WorkItemID: legacyItem.ID, RepositoryID: "repo-1", Objective: run.Objective, AgentProvider: "codex", AgentName: "codex"}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	legacyUpdated, err := service.Resume(ctx, actor, "project-1", legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacyUpdated.Status != StatusIntake || legacyUpdated.Phase != PhaseIntake || legacyUpdated.CurrentAgentRunID == "" {
		t.Fatalf("legacy review run was not re-queued for intake = %#v", legacyUpdated)
	}

	worker := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	updated, err := service.HandleAgentRunResult(ctx, worker, "project-1", run.ID, runner.Result{Output: `{"type":"item.completed","item":{"type":"agent_message","text":"{\"summary\":\"Saved menu quantities are not visible on POS\",\"fields\":{\"PROBLEM_STATEMENT\":\"A saved menu quantity is missing from the POS screen.\",\"EXPECTED_BEHAVIOR\":\"The configured quantity is displayed.\",\"ACTUAL_BEHAVIOR\":\"The quantity is absent.\"},\"acceptance_criteria\":[{\"statement\":\"A saved quantity is displayed after reopening the POS menu.\"}],\"analysis\":{\"root_cause_hypothesis\":\"The menu read path may omit the persisted quantity.\",\"implementation_plan\":\"Trace the save and read mappings, then add a regression test.\",\"test_plan\":\"Run the menu persistence unit and browser regression tests.\",\"confidence\":0.6}}"}}`})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusWaitingSpecReview || updated.Phase != PhaseSpecification || updated.Gate != GateSpecification {
		t.Fatalf("post-intake run = %#v", updated)
	}
	item, err := workItems.Get(ctx, workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1"}, actor, run.WorkItemID)
	if err != nil {
		t.Fatal(err)
	}
	currentSpec, err := spec.Get(ctx, "org-1", "project-1", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentSpec.Summary == "" || currentSpec.Fields[specification.ProblemStatement].Provenance != specification.AIInferred || currentSpec.Fields[specification.ProblemStatement].VerificationStatus != specification.Unverified {
		t.Fatalf("AI draft = %#v", currentSpec)
	}
	if len(currentSpec.Acceptance) != 1 || currentSpec.Acceptance[0].VerificationStatus != specification.Unverified {
		t.Fatalf("AI acceptance = %#v", currentSpec.Acceptance)
	}
	analyses, err := spec.Analyses(ctx, "org-1", "project-1", item.ID)
	if err != nil || len(analyses) != 1 || analyses[0].Provenance != specification.AIHypothesis {
		t.Fatalf("AI analyses = %#v, err=%v", analyses, err)
	}
}

func TestPositionsAreSortedUniqueAndBounded(t *testing.T) {
	got := uniquePositions([]int{4, 1, 4, 0, 10001, 2})
	want := []int{1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("positions = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("positions = %#v, want %#v", got, want)
		}
	}
}

func TestIntersectionNeverReintroducesPassedPositions(t *testing.T) {
	got := intersection([]int{3, 1, 2}, []int{1, 3})
	want := []int{1, 3}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("intersection = %#v, want %#v", got, want)
	}
}
