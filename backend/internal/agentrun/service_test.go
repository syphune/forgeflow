package agentrun

import (
	"context"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

func TestApprovalFingerprintCanonicalizesInputsAndPolicy(t *testing.T) {
	first, err := approvalFingerprint(CreateInput{
		RepositoryID: " repo ",
		BaseSHA:      " base ",
		ExecutionInputs: ExecutionInputs{
			Prompt:          " ship it ",
			ToolPermissions: []string{"write", "read", "write"},
			AgentConfiguration: map[string]any{
				"model":       "fast",
				"temperature": 0,
			},
		},
		ExecutionPolicy: map[string]any{"network": "restricted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := approvalFingerprint(CreateInput{
		RepositoryID: "repo",
		BaseSHA:      "base",
		ExecutionInputs: ExecutionInputs{
			Prompt:          "ship it",
			ToolPermissions: []string{"read", "write"},
			AgentConfiguration: map[string]any{
				"temperature": 0,
				"model":       "fast",
			},
		},
		ExecutionPolicy: map[string]any{"network": "restricted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("canonical inputs produced different fingerprints: %s != %s", first, second)
	}

	changed, err := approvalFingerprint(CreateInput{
		RepositoryID: "repo",
		BaseSHA:      "base",
		ExecutionInputs: ExecutionInputs{
			Prompt: "ship it",
		},
		ExecutionPolicy: map[string]any{"network": "open"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == changed {
		t.Fatal("changing execution policy did not change the fingerprint")
	}
}

func TestAgentRunHeartbeatDeadlineBoundary(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	store := NewMemoryStore()
	service := NewService(store, nil, Options{Now: func() time.Time { return now }})
	actor := identity.Actor{Type: "human", ID: "user", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	run, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project", WorkItemID: "item", RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(context.Background(), actor, "project", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), actor, "project", run.ID); err != nil {
		t.Fatal(err)
	}

	setHeartbeat := func(age time.Duration) {
		store.mu.Lock()
		item := store.runs[run.ID]
		last := now.Add(-age)
		item.LastHeartbeatAt = &last
		store.runs[run.ID] = item
		store.mu.Unlock()
	}
	setHeartbeat(HeartbeatDeadline - time.Second)
	interrupted, err := service.ReconcileStale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(interrupted) != 0 {
		t.Fatalf("run interrupted before deadline: %+v", interrupted)
	}

	setHeartbeat(HeartbeatDeadline)
	interrupted, err = service.ReconcileStale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(interrupted) != 1 || interrupted[0].Status != Interrupted {
		t.Fatalf("run was not interrupted at deadline: %+v", interrupted)
	}
	resumed, err := service.Resume(context.Background(), actor, "project", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != Preparing || resumed.FinishedAt != nil || resumed.InterruptionReason != "" {
		t.Fatalf("unexpected resumed run: %+v", resumed)
	}
}

func TestStartRejectsChangedApprovedExecutionInput(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, nil)
	actor := identity.Actor{Type: "human", ID: "user", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	run, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project", WorkItemID: "item", RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex", ExecutionInputs: ExecutionInputs{Prompt: "original"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(context.Background(), actor, "project", run.ID); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	changed := store.runs[run.ID]
	changed.ExecutionInputs.Prompt = "changed"
	store.runs[run.ID] = changed
	store.mu.Unlock()
	if _, err := service.Start(context.Background(), actor, "project", run.ID); err == nil {
		t.Fatal("expected changed execution input to invalidate approval")
	}
}

func TestServiceRequiresApprovalBeforeStart(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, nil)
	actor := identity.Actor{Type: "human", ID: "user", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	run, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project", WorkItemID: "item", RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), actor, "project", run.ID); err == nil {
		t.Fatal("expected approval error")
	}
	if _, err := service.Approve(context.Background(), actor, "project", run.ID); err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), actor, "project", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != Preparing {
		t.Fatalf("got %s", started.Status)
	}
}

func TestAttachResultStoresUntrustedClaims(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, nil)
	actor := identity.Actor{Type: "ai", ID: "agent", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	run, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project", WorkItemID: "item", RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	errorMessage := "tests failed"
	updated, err := service.AttachResult(context.Background(), actor, "project", run.ID, ResultInput{CommitSHA: "abc1234", Error: &errorMessage, Result: map[string]any{"claim": "untrusted"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CommitSHA != "abc1234" || updated.Error != errorMessage || updated.Result["claim"] != "untrusted" {
		t.Fatalf("unexpected result: %+v", updated)
	}
}

func TestAttachResultRejectsTerminalRun(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, nil)
	actor := identity.Actor{Type: "human", ID: "user", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	run, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project", WorkItemID: "item", RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(context.Background(), actor, "project", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), actor, "project", run.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []Status{Planning, Investigating, Implementing, Testing, Reviewing, Completed} {
		if _, err := service.Transition(context.Background(), actor, "project", run.ID, status); err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	if _, err := service.AttachResult(context.Background(), actor, "project", run.ID, ResultInput{Result: map[string]any{"retry": true}}); err == nil {
		t.Fatal("expected terminal AgentRun result rejection")
	}
	if _, err := service.AttachStep(context.Background(), actor, "project", run.ID, Step{Sequence: 1, Phase: "testing", Status: "failed"}); err == nil {
		t.Fatal("expected terminal AgentRun step rejection")
	}
	if _, err := service.AttachArtifact(context.Background(), actor, "project", run.ID, Artifact{ArtifactType: "diff", Name: "patch", SizeBytes: 1}); err == nil {
		t.Fatal("expected terminal AgentRun artifact rejection")
	}
}

func TestRecordTestResultsMergesPassesAndRequiresFailureNotes(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, nil)
	actor := identity.Actor{Type: "human", ID: "qa", OrganizationID: "org", Capabilities: map[string]bool{identity.CapabilityWorkItemEdit: true, identity.CapabilityAgentExecute: true}}
	run, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project", WorkItemID: "item", RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordTestResults(context.Background(), actor, "project", run.ID, TestResultsInput{Cases: []TestCaseResultInput{{Position: 1, Status: TestPassed}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordTestResults(context.Background(), actor, "project", run.ID, TestResultsInput{Cases: []TestCaseResultInput{{Position: 2, Status: TestFailed}}}); err == nil {
		t.Fatal("expected failed test case note requirement")
	}
	updated, err := service.RecordTestResults(context.Background(), actor, "project", run.ID, TestResultsInput{Cases: []TestCaseResultInput{{Position: 2, Status: TestFailed, Note: "Expected 200, received 500"}}})
	if err != nil {
		t.Fatal(err)
	}
	results, ok := updated.Result["test_cases"].([]TestCaseResult)
	if !ok || len(results) != 2 || results[0].Status != TestPassed || results[1].Note == "" {
		t.Fatalf("merged test results = %#v", updated.Result["test_cases"])
	}
}

func TestStartRechecksWorkItemReadiness(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	specStore := specification.NewMemoryStore()
	specService := specification.NewService(specStore, now)
	workItemService := workitem.NewService(
		workitem.NewMemoryRepository(now),
		specService,
		workflow.NewService(workflow.Default()),
		audit.NewMemoryWriter(),
		outbox.NewMemoryWriter(),
		now,
	)
	actor := identity.Actor{Type: "human", ID: "user", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	scope := workitem.Scope{OrganizationID: "org", ProjectID: "project", ProjectKey: "FF"}
	item, err := workItemService.Create(ctx, scope, actor, workitem.CreateInput{Type: workitem.Task, Title: "Ready task", RepositoryID: "repo", AssigneeID: actor.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"start_refining", "request_review"} {
		item, err = workItemService.Transition(ctx, scope, actor, item.ID, workitem.TransitionInput{TransitionKey: key, ExpectedVersion: item.Version})
		if err != nil {
			t.Fatal(err)
		}
	}
	spec, err := specService.Get(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec.Fields[specification.Goal] = specification.Field{Key: specification.Goal, Value: "Ship the fix", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}
	spec.Acceptance = []specification.AcceptanceCriterion{{Position: 1, Statement: "The fix is covered", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}}
	if err := specStore.Save(ctx, scope.OrganizationID, scope.ProjectID, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := specService.Review(ctx, scope.OrganizationID, scope.ProjectID, item.ID, actor, specification.ReviewInput{ExpectedVersion: spec.Version}); err != nil {
		t.Fatal(err)
	}
	item, err = workItemService.Transition(ctx, scope, actor, item.ID, workitem.TransitionInput{TransitionKey: "mark_ready", ExpectedVersion: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(NewMemoryStore(), workItemService)
	if _, err := service.Create(ctx, actor, CreateInput{ProjectID: scope.ProjectID, WorkItemID: item.ID, RepositoryID: "other-repo", AgentProvider: "codex", AgentName: "codex"}); err == nil {
		t.Fatal("expected AgentRun creation to reject an unrelated repository")
	}
	run, err := service.Create(ctx, actor, CreateInput{ProjectID: scope.ProjectID, WorkItemID: item.ID, RepositoryID: "repo", AgentProvider: "codex", AgentName: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = workItemService.Transition(ctx, scope, actor, item.ID, workitem.TransitionInput{TransitionKey: "start_work", ExpectedVersion: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != workflow.InProgress {
		t.Fatalf("work item status = %s", item.Status)
	}
	if _, err := service.Approve(ctx, actor, scope.ProjectID, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx, actor, scope.ProjectID, run.ID); err == nil {
		t.Fatal("expected start to reject a work item that left READY")
	}
}

func TestServiceRequiresProjectScopeForRunReads(t *testing.T) {
	service := NewService(NewMemoryStore(), nil)
	actor := identity.Actor{Type: "human", ID: "user", OrganizationID: "org", Capabilities: map[string]bool{"*": true}}
	if _, _, _, err := service.Get(context.Background(), actor, "", "run"); err == nil {
		t.Fatal("expected missing project scope error")
	}
}
