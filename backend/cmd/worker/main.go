package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/automation"
	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/config"
	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/runner"
)

type eventHandler struct {
	logger       *slog.Logger
	automation   *automation.Service
	webhooks     *githubintegration.PostgresWebhookStore
	repositories interface {
		ListProjectRepositories(context.Context, string, string) ([]githubintegration.Repository, error)
	}
	agentRuns     *agentrun.Service
	autonomous    *autonomous.Service
	dispatcher    runner.Dispatcher
	workspaceRoot string
}

func (h eventHandler) Handle(ctx context.Context, event outbox.Event) error {
	if event.EventType == "github.webhook.received" && h.webhooks != nil {
		return h.webhooks.Process(ctx, event.AggregateID)
	}
	if event.EventType == "agent_run.autonomous_started" && h.agentRuns != nil && h.autonomous != nil {
		return h.dispatchAutonomousRun(ctx, event)
	}
	if err := h.automation.Handle(ctx, event); err != nil {
		return err
	}
	h.logger.Info("processed outbox event", "event_id", event.ID, "event_type", event.EventType, "aggregate_id", event.AggregateID)
	return nil
}

func (h eventHandler) dispatchAutonomousRun(ctx context.Context, event outbox.Event) error {
	if h.dispatcher == nil {
		h.logger.Warn("autonomous AgentRun queued but runner dispatcher is not configured", "agent_run_id", event.AggregateID)
		projectID, _ := event.Payload["project_id"].(string)
		if strings.TrimSpace(projectID) == "" {
			return fmt.Errorf("autonomous AgentRun event is missing project_id")
		}
		actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: event.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
		run, _, _, err := h.agentRuns.Get(ctx, actor, projectID, event.AggregateID)
		if err != nil {
			return err
		}
		workflowID, _ := run.ExecutionPolicy["workflow_id"].(string)
		if strings.TrimSpace(workflowID) == "" {
			return fmt.Errorf("autonomous AgentRun %s is missing workflow_id", run.ID)
		}
		_, err = h.autonomous.HandleAgentRunResult(ctx, actor, projectID, workflowID, runner.Result{Error: "autonomous runner is not configured"})
		return err
	}
	projectID, _ := event.Payload["project_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("autonomous AgentRun event is missing project_id")
	}
	actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: event.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
	run, _, _, err := h.agentRuns.Get(ctx, actor, projectID, event.AggregateID)
	if err != nil {
		return err
	}
	workflowID, _ := run.ExecutionPolicy["workflow_id"].(string)
	if strings.TrimSpace(workflowID) == "" {
		return fmt.Errorf("autonomous AgentRun %s is missing workflow_id", run.ID)
	}
	if h.repositories == nil {
		return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "repository resolver is not configured")
	}
	repositories, err := h.repositories.ListProjectRepositories(ctx, event.OrganizationID, projectID)
	if err != nil {
		return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "load linked repository: "+err.Error())
	}
	var repository githubintegration.Repository
	for _, candidate := range repositories {
		if candidate.ID == run.RepositoryID {
			repository = candidate
			break
		}
	}
	if repository.ID == "" || strings.TrimSpace(repository.CloneURL) == "" {
		return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "repository is not linked to the project or has no clone URL")
	}
	root := strings.TrimSpace(h.workspaceRoot)
	if root == "" {
		root = "/var/lib/forgeflow/workspaces"
	}
	job := runner.Job{ID: run.ID, AutonomousRunID: workflowID, AgentRunID: run.ID, Provider: run.AgentProvider, Model: run.Model, Prompt: run.ExecutionInputs.Prompt, RepositoryURL: repository.CloneURL, BaseSHA: run.BaseSHA, Branch: run.Branch, WorkspaceRoot: root, Workspace: filepath.Join(root, run.ID), TimeoutSeconds: 3600, NetworkMode: "none", AllowedHosts: []string{"github.com"}, MCPPermissions: run.ExecutionInputs.MCPPermissions}
	result, dispatchErr := h.dispatcher.Dispatch(ctx, job)
	output := result.Output
	if len(output) > 480*1024 {
		output = output[:480*1024]
	}
	var errorValue *string
	if dispatchErr != nil {
		value := result.Error
		if value == "" {
			value = dispatchErr.Error()
		}
		if len(value) > 4000 {
			value = value[:4000]
		}
		errorValue = &value
	}
	_, attachErr := h.agentRuns.AttachResult(ctx, actor, projectID, run.ID, agentrun.ResultInput{Result: map[string]any{"runner_output": output, "content_trust": "UNTRUSTED_CONTENT"}, Error: errorValue})
	if attachErr != nil && dispatchErr == nil {
		return attachErr
	}
	if err := finishAgentRun(ctx, h.agentRuns, actor, projectID, run, dispatchErr != nil); err != nil {
		return err
	}
	_, handleErr := h.autonomous.HandleAgentRunResult(ctx, actor, projectID, workflowID, result)
	if dispatchErr != nil && handleErr == nil {
		return dispatchErr
	}
	return handleErr
}

func (h eventHandler) failAutonomousDispatch(ctx context.Context, actor identity.Actor, projectID, workflowID string, run agentrun.Run, message string) error {
	value := message
	if len(value) > 4000 {
		value = value[:4000]
	}
	if _, err := h.agentRuns.AttachResult(ctx, actor, projectID, run.ID, agentrun.ResultInput{Result: map[string]any{"content_trust": "UNTRUSTED_CONTENT"}, Error: &value}); err != nil {
		return err
	}
	if err := finishAgentRun(ctx, h.agentRuns, actor, projectID, run, true); err != nil {
		return err
	}
	_, err := h.autonomous.HandleAgentRunResult(ctx, actor, projectID, workflowID, runner.Result{Error: value})
	return err
}

func finishAgentRun(ctx context.Context, service *agentrun.Service, actor identity.Actor, projectID string, run agentrun.Run, failed bool) error {
	current := run.Status
	for _, next := range []agentrun.Status{agentrun.Planning, agentrun.Investigating, agentrun.Implementing, agentrun.Testing} {
		if current == next {
			continue
		}
		if current == agentrun.Testing {
			break
		}
		updated, err := service.Transition(ctx, actor, projectID, run.ID, next)
		if err != nil {
			return err
		}
		current = updated.Status
	}
	if failed {
		if current != agentrun.Testing {
			return fmt.Errorf("AgentRun %s did not reach TESTING before failure", run.ID)
		}
		_, err := service.Transition(ctx, actor, projectID, run.ID, agentrun.Failed)
		return err
	}
	if current != agentrun.Testing {
		return fmt.Errorf("AgentRun %s did not reach TESTING", run.ID)
	}
	updated, err := service.Transition(ctx, actor, projectID, run.ID, agentrun.Reviewing)
	if err != nil {
		return err
	}
	_, err = service.Transition(ctx, actor, projectID, run.ID, agentrun.Completed)
	_ = updated
	return err
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.DatabaseURL == "" {
		logger.Warn("DATABASE_URL is empty; worker will idle without claiming jobs")
		<-ctx.Done()
		return
	}
	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := db.Open(startupCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := db.Ready(readyCtx, pool); err != nil {
		readyCancel()
		logger.Error("database is not ready", "error", err)
		os.Exit(1)
	}
	readyCancel()
	notificationStore := notification.NewPostgresStore(pool)
	automationService := automation.NewService(automation.NewPostgresStore(pool), notificationStore)
	githubStore := githubintegration.NewPostgresStore(pool)
	webhookStore := githubintegration.NewPostgresWebhookStore(pool)
	agentRunStore := agentrun.NewPostgresStore(pool)
	webhookStore.SetAgentRunProjector(agentRunStore)
	agentRunWatchdog := agentrun.NewService(agentRunStore, nil, agentrun.Options{Recorder: agentrun.MutationRecorder{Audit: audit.NewPostgresWriter(pool), Outbox: outbox.NewPostgresWriter(pool)}, Now: time.Now})
	autonomousService := autonomous.NewService(autonomous.NewPostgresStore(pool), nil, nil, agentRunWatchdog, autonomous.Options{Recorder: autonomous.MutationRecorder{Audit: audit.NewPostgresWriter(pool), Outbox: outbox.NewPostgresWriter(pool)}, Now: time.Now})
	var dispatcher runner.Dispatcher
	if cfg.RunnerURL != "" {
		dispatcher = runner.HTTPDispatcher{BaseURL: cfg.RunnerURL, Token: cfg.RunnerToken}
	}
	go runAgentRunWatchdog(ctx, logger, agentRunWatchdog)
	processor := outbox.NewPostgresProcessor(pool, eventHandler{logger: logger, automation: automationService, webhooks: webhookStore, repositories: githubStore, agentRuns: agentRunWatchdog, autonomous: autonomousService, dispatcher: dispatcher, workspaceRoot: cfg.RunnerWorkspaceRoot}, time.Now)
	if err := processor.Run(ctx, time.Second); err != nil && err != context.Canceled {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func runAgentRunWatchdog(ctx context.Context, logger *slog.Logger, service *agentrun.Service) {
	check := func() {
		if _, err := service.ReconcileStale(ctx); err != nil && ctx.Err() == nil {
			logger.Error("reconcile stale AgentRuns", "error", err)
		}
	}
	check()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
