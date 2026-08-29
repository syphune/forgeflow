package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/automation"
	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

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
}
