package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/automation"
	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/runner"
)

type workerRepositoryResolverFunc func(context.Context, string, string) ([]githubintegration.Repository, error)

func (f workerRepositoryResolverFunc) ListProjectRepositories(ctx context.Context, organizationID, projectID string) ([]githubintegration.Repository, error) {
	return f(ctx, organizationID, projectID)
}

type workerDispatcherFunc func(context.Context, runner.Job) (runner.Result, error)

func (f workerDispatcherFunc) Dispatch(ctx context.Context, job runner.Job) (runner.Result, error) {
	return f(ctx, job)
}

func TestEventHandlerProcessesAutomationOnce(t *testing.T) {
	ctx := context.Background()
	notifications := notification.NewMemoryStore(func(context.Context, string, string) ([]string, error) {
		return []string{"user-1"}, nil
	})
	service := automation.NewService(automation.NewMemoryStore(), notifications)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	rule, err := service.Create(ctx, actor, automation.CreateInput{ProjectID: "project-1", Name: "Notify transitions", EventType: "work_item.transitioned"})
	if err != nil {
		t.Fatal(err)
	}
	handler := eventHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), automation: service}
	event := outbox.Event{ID: "event-1", OrganizationID: "org-1", EventType: rule.EventType, AggregateType: "work_item", AggregateID: "item-1", Payload: map[string]any{"project_id": "project-1"}}
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	items, err := notifications.List(ctx, "org-1", "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("notifications = %d, want one idempotent delivery", len(items))
	}
}

func TestEventHandlerFailsAutonomousRunWhenRunnerIsUnavailable(t *testing.T) {
	ctx := context.Background()
	actor := identity.Actor{Type: "system", ID: "worker", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	autonomousStore := autonomous.NewMemoryStore()
	workflow, err := autonomousStore.Create(ctx, "org-1", autonomous.StartInput{ProjectID: "project-1", WorkItemID: "item-1", RepositoryID: "repo-1", Objective: "Run the regression suite"}, autonomous.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	agentRuns := agentrun.NewService(agentrun.NewMemoryStore(), nil)
	run, err := agentRuns.Create(ctx, actor, agentrun.CreateInput{ProjectID: "project-1", WorkItemID: "item-1", RepositoryID: "repo-1", AgentProvider: "codex", AgentName: "codex", ExecutionPolicy: map[string]any{"workflow_id": workflow.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentRuns.Transition(ctx, actor, "project-1", run.ID, agentrun.Preparing); err != nil {
		t.Fatal(err)
	}
	handler := eventHandler{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentRuns:  agentRuns,
		autonomous: autonomous.NewService(autonomousStore, nil, nil, agentRuns),
	}
	event := outbox.Event{ID: "event-2", OrganizationID: "org-1", EventType: "agent_run.autonomous_started", AggregateType: "agent_run", AggregateID: run.ID, Payload: map[string]any{"project_id": "project-1"}}
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	finished, _, _, err := agentRuns.Get(ctx, actor, "project-1", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != agentrun.Failed {
		t.Fatalf("AgentRun status = %s, want FAILED", finished.Status)
	}
	updated, feedback, err := handler.autonomous.Get(ctx, actor, "project-1", workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != autonomous.StatusWaitingTestFeedback || len(feedback) != 1 {
		t.Fatalf("autonomous fallback = status %s, feedback %d; want WAITING_TEST_FEEDBACK and one feedback", updated.Status, len(feedback))
	}
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("duplicate autonomous event: %v", err)
	}
}

func TestEventHandlerRecordsRunnerDispatchErrorWithoutHeartbeatTimeout(t *testing.T) {
	ctx := context.Background()
	actor := identity.Actor{Type: "system", ID: "worker", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	autonomousStore := autonomous.NewMemoryStore()
	workflow, err := autonomousStore.Create(ctx, "org-1", autonomous.StartInput{ProjectID: "project-1", WorkItemID: "item-1", RepositoryID: "repo-1", Objective: "Inspect the repository"}, autonomous.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	agentRuns := agentrun.NewService(agentrun.NewMemoryStore(), nil)
	run, err := agentRuns.Create(ctx, actor, agentrun.CreateInput{ProjectID: "project-1", WorkItemID: "item-1", RepositoryID: "repo-1", AgentProvider: "codex", AgentName: "codex", ExecutionInputs: agentrun.ExecutionInputs{Prompt: "inspect"}, ExecutionPolicy: map[string]any{"workflow_id": workflow.ID, "read_only": true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentRuns.Transition(ctx, actor, "project-1", run.ID, agentrun.Preparing); err != nil {
		t.Fatal(err)
	}
	handler := eventHandler{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentRuns:           agentRuns,
		autonomous:          autonomous.NewService(autonomousStore, nil, nil, agentRuns),
		runnerGitConfigured: true,
		repositories: workerRepositoryResolverFunc(func(context.Context, string, string) ([]githubintegration.Repository, error) {
			return []githubintegration.Repository{{ID: "repo-1", CloneURL: "https://github.com/acme/private.git"}}, nil
		}),
		dispatcher: workerDispatcherFunc(func(context.Context, runner.Job) (runner.Result, error) {
			return runner.Result{}, errors.New("runner unavailable")
		}),
	}
	event := outbox.Event{ID: "event-dispatch-error", OrganizationID: "org-1", EventType: "agent_run.autonomous_started", AggregateType: "agent_run", AggregateID: run.ID, Payload: map[string]any{"project_id": "project-1"}}
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	finished, _, _, err := agentRuns.Get(ctx, actor, "project-1", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != agentrun.Failed || finished.Error != "runner unavailable" || finished.InterruptionReason != "" {
		t.Fatalf("dispatch failure result = %#v", finished)
	}
}

func TestMCPURLForProjectPinsAutonomousProject(t *testing.T) {
	got, err := mcpURLForProject("https://forgeflow.example/api/v1/mcp?transport=streamable", "project-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://forgeflow.example/api/v1/mcp?project_id=project-1&transport=streamable" {
		t.Fatalf("MCP URL = %q", got)
	}
	if _, err := mcpURLForProject("https://user:pass@forgeflow.example/mcp", "project-1", false); err == nil {
		t.Fatal("MCP URL with embedded credentials must be rejected")
	}
	intake, err := mcpURLForProject("https://forgeflow.example/api/v1/mcp", "project-1", true)
	if err != nil || intake != "https://forgeflow.example/api/v1/mcp?mode=intake&project_id=project-1" {
		t.Fatalf("intake MCP URL = %q, err = %v", intake, err)
	}
}

func TestResultActorForAutonomousRunUsesWorkflowCreator(t *testing.T) {
	actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: "org-1"}
	got := resultActorForAutonomousRun(actor, map[string]any{"created_by": "user-1"})
	if got.ID != "user-1" || got.Type != actor.Type || got.OrganizationID != actor.OrganizationID {
		t.Fatalf("result actor = %#v", got)
	}
	if resultActorForAutonomousRun(actor, map[string]any{}).ID != actor.ID {
		t.Fatal("missing creator should preserve the worker actor")
	}
}
