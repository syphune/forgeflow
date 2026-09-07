package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
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
	"github.com/forgeflow/forgeflow/backend/internal/impact"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/config"
	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/runner"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/tenant"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type eventHandler struct {
	logger       *slog.Logger
	automation   *automation.Service
	webhooks     *githubintegration.PostgresWebhookStore
	repositories interface {
		ListProjectRepositories(context.Context, string, string) ([]githubintegration.Repository, error)
	}
	credentials interface {
		RunnerGitCredential(context.Context, identity.Actor, string, string) (githubintegration.CloneCredential, error)
	}
	agentRuns           *agentrun.Service
	impact              *impact.Service
	autonomous          *autonomous.Service
	dispatcher          runner.Dispatcher
	workspaceRoot       string
	runnerImage         string
	runnerCPU           string
	runnerMemory        string
	runnerPids          int
	runnerNetwork       string
	runnerToken         string
	runnerMCPURL        string
	runnerMCPToken      string
	runnerGitConfigured bool
}

type workerRepositoryProjectResolver struct {
	tenant *tenant.Service
	github *githubintegration.Service
}

func (r workerRepositoryProjectResolver) ProjectsForRepository(ctx context.Context, organizationID, repositoryID string) ([]impact.ProjectSummary, error) {
	actor := identity.Actor{Type: "system", ID: "impact-resolver", OrganizationID: organizationID, Capabilities: map[string]bool{"*": true}}
	projects, err := r.tenant.Projects(ctx, actor, "")
	if err != nil {
		return nil, err
	}
	result := make([]impact.ProjectSummary, 0)
	for _, project := range projects {
		repositories, listErr := r.github.ProjectRepositories(ctx, actor, project.ID)
		if listErr != nil {
			return nil, listErr
		}
		for _, repository := range repositories {
			if repository.ID == repositoryID {
				result = append(result, impact.ProjectSummary{ID: project.ID, Key: project.Key, DisplayName: project.DisplayName})
				break
			}
		}
	}
	return result, nil
}

const maxStoredRunnerOutputBytes = 64 * 1024

func (h eventHandler) Handle(ctx context.Context, event outbox.Event) error {
	if event.EventType == "github.webhook.received" && h.webhooks != nil {
		return h.webhooks.Process(ctx, event.AggregateID)
	}
	if event.EventType == "agent_run.interrupted" && h.agentRuns != nil && h.autonomous != nil {
		return h.handleAutonomousInterrupt(ctx, event)
	}
	if event.EventType == "agent_run.autonomous_started" && h.agentRuns != nil && h.autonomous != nil {
		return h.dispatchAutonomousRun(ctx, event)
	}
	if event.EventType == "agent_run.impact_approved" && h.agentRuns != nil && h.autonomous != nil {
		return h.dispatchAutonomousPublish(ctx, event)
	}
	if event.EventType == "agent_run.impact_rejected" && h.agentRuns != nil && h.autonomous != nil {
		return h.handleAutonomousImpactRejected(ctx, event)
	}
	if err := h.automation.Handle(ctx, event); err != nil {
		return err
	}
	h.logger.Info("processed outbox event", "event_id", event.ID, "event_type", event.EventType, "aggregate_id", event.AggregateID)
	return nil
}

func (h eventHandler) dispatchAutonomousRun(ctx context.Context, event outbox.Event) error {
	projectID, _ := event.Payload["project_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("autonomous AgentRun event is missing project_id")
	}
	actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: event.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
	run, _, _, err := h.agentRuns.Get(ctx, actor, projectID, event.AggregateID)
	if err != nil {
		return err
	}
	if run.Status == agentrun.Completed || run.Status == agentrun.Failed || run.Status == agentrun.Cancelled || run.Status == agentrun.Interrupted {
		return nil
	}
	workflowID, _ := run.ExecutionPolicy["workflow_id"].(string)
	if strings.TrimSpace(workflowID) == "" {
		return fmt.Errorf("autonomous AgentRun %s is missing workflow_id", run.ID)
	}
	if h.dispatcher == nil {
		h.logger.Warn("autonomous AgentRun queued but runner dispatcher is not configured", "agent_run_id", event.AggregateID)
		return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "autonomous runner is not configured")
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
	pushBranch, _ := run.ExecutionPolicy["push_branch"].(bool)
	networkMode := h.runnerNetwork
	if networkMode == "" {
		networkMode = "none"
	}
	if pushBranch {
		networkMode = "bridge"
	}
	readOnly, _ := run.ExecutionPolicy["read_only"].(bool)
	mcpURL, mcpErr := mcpURLForProject(h.runnerMCPURL, projectID, readOnly)
	if mcpErr != nil {
		return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, mcpErr.Error())
	}
	mcpToken := strings.TrimSpace(h.runnerMCPToken)
	if mcpURL != "" {
		if mcpToken == "" {
			permissions := append([]string(nil), run.ExecutionInputs.MCPPermissions...)
			if len(permissions) == 0 {
				permissions = autonomous.DefaultPolicy().MCPPermissions
			}
			mcpToken, mcpErr = identity.IssueRunnerMCPToken(h.runnerToken, identity.RunnerMCPClaims{
				OrganizationID: event.OrganizationID,
				ProjectID:      projectID,
				AgentRunID:     run.ID,
				Permissions:    permissions,
				ExpiresAt:      time.Now().UTC().Add(2 * time.Hour).Unix(),
			}, time.Now().UTC())
			if mcpErr != nil {
				return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "automatic server MCP authentication is unavailable")
			}
		}
		if strings.EqualFold(strings.TrimSpace(networkMode), "none") {
			return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "server runner MCP requires FORGEFLOW_RUNNER_NETWORK_MODE=bridge")
		}
	}
	var gitToken string
	if !readOnly && (pushBranch || h.runnerGitConfigured) {
		if h.credentials == nil {
			return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "GitHub runner credential provider is not configured")
		}
		credential, credentialErr := h.credentials.RunnerGitCredential(ctx, actor, projectID, repository.ID)
		if credentialErr != nil {
			return h.failAutonomousDispatch(ctx, actor, projectID, workflowID, run, "could not issue GitHub runner credential")
		}
		gitToken = credential.Token
	}
	job := runner.Job{ID: run.ID, AutonomousRunID: workflowID, AgentRunID: run.ID, Provider: run.AgentProvider, Model: run.Model, ReasoningEffort: run.ReasoningEffort, Operation: "execute", CollectChanges: true, Prompt: run.ExecutionInputs.Prompt, RepositoryURL: repository.CloneURL, BaseSHA: run.BaseSHA, Branch: run.Branch, PushBranch: false, ReadOnly: readOnly, CommitMessage: "chore: update Forgeflow work item", WorkspaceRoot: root, Workspace: filepath.Join(root, run.ID), Image: h.runnerImage, TimeoutSeconds: 3600, CPULimit: h.runnerCPU, MemoryLimit: h.runnerMemory, PidsLimit: h.runnerPids, NetworkMode: networkMode, AllowedHosts: []string{"github.com"}, MCPPermissions: run.ExecutionInputs.MCPPermissions, MCPURL: mcpURL, MCPToken: mcpToken, GitToken: gitToken}
	if _, heartbeatErr := h.agentRuns.Heartbeat(ctx, actor, projectID, run.ID); heartbeatErr != nil {
		h.logger.Warn("could not record initial AgentRun heartbeat", "agent_run_id", run.ID, "error", heartbeatErr)
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(agentrun.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if _, heartbeatErr := h.agentRuns.Heartbeat(heartbeatCtx, actor, projectID, run.ID); heartbeatErr != nil && heartbeatCtx.Err() == nil {
					h.logger.Warn("could not record AgentRun heartbeat", "agent_run_id", run.ID, "error", heartbeatErr)
				}
			}
		}
	}()
	result, dispatchErr := h.dispatcher.Dispatch(ctx, job)
	stopHeartbeat()
	<-heartbeatDone
	if dispatchErr == nil && strings.TrimSpace(result.Error) != "" {
		dispatchErr = fmt.Errorf("%s", strings.TrimSpace(result.Error))
	}
	// The runner result is untrusted execution output; the worker derives the
	// publish decision from the persisted impact report below.
	result.ImpactReviewRequired = false
	if dispatchErr == nil && strings.TrimSpace(result.Error) == "" && h.impact != nil {
		_, impactErr := h.impact.EnsureReport(ctx, impact.Subject{OrganizationID: event.OrganizationID, ProjectID: projectID, WorkItemID: run.WorkItemID, RepositoryID: run.RepositoryID, AgentRunID: run.ID, BaseSHA: run.BaseSHA, Branch: run.Branch, ChangedPaths: result.ChangedPaths, DiffHash: result.DiffHash, GateMode: impactGateModeForPolicy(run.ExecutionPolicy)})
		if impactErr != nil {
			dispatchErr = impactErr
			result.Error = "could not record repository impact: " + impactErr.Error()
		} else if pushBranch && !readOnly {
			_, impactErr = h.impact.ReadyForPublish(ctx, impact.Subject{OrganizationID: event.OrganizationID, ProjectID: projectID, WorkItemID: run.WorkItemID, RepositoryID: run.RepositoryID, AgentRunID: run.ID, BaseSHA: run.BaseSHA, Branch: run.Branch, ChangedPaths: result.ChangedPaths, DiffHash: result.DiffHash, GateMode: impactGateModeForPolicy(run.ExecutionPolicy)})
			if impactErr != nil {
				if appErr := apperr.From(impactErr); appErr.Code == apperr.CodeImpactReviewRequired {
					result.ImpactReviewRequired = true
				} else {
					dispatchErr = impactErr
					result.Error = impactErr.Error()
				}
			}
		}
	} else if dispatchErr == nil && pushBranch && !readOnly && impactGateModeForPolicy(run.ExecutionPolicy) == impact.GateRequired {
		dispatchErr = fmt.Errorf("impact review service is unavailable")
		result.Error = dispatchErr.Error()
	}
	if dispatchErr == nil && result.Error == "" && !result.ImpactReviewRequired && pushBranch && !readOnly {
		published, publishErr := h.dispatchPublishJob(ctx, actor, projectID, workflowID, run, repository, root, gitToken)
		if publishErr != nil {
			dispatchErr = publishErr
			result.Error = publishErr.Error()
		} else {
			if published.Output != "" {
				result.Output += "\n" + published.Output
			}
		}
	}
	if dispatchErr != nil {
		if result.Error == "" {
			result.Error = dispatchErr.Error()
		}
		if result.ExitCode == 0 {
			result.ExitCode = 1
		}
	}
	output := result.Output
	if len(output) > maxStoredRunnerOutputBytes {
		output = output[:maxStoredRunnerOutputBytes]
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
	runnerResult := map[string]any{"runner_output": output, "content_trust": "UNTRUSTED_CONTENT"}
	if len(result.ChangedPaths) > 0 {
		runnerResult["changed_paths"] = result.ChangedPaths
	}
	if result.DiffHash != "" {
		runnerResult["diff_hash"] = result.DiffHash
	}
	if result.ImpactReviewRequired {
		runnerResult["impact_review_required"] = true
	}
	if result.ErrorCode != "" {
		runnerResult["error_code"] = result.ErrorCode
	}
	_, attachErr := h.agentRuns.AttachResult(ctx, actor, projectID, run.ID, agentrun.ResultInput{Result: runnerResult, Error: errorValue})
	if attachErr != nil {
		return attachErr
	}
	if result.ImpactReviewRequired && dispatchErr == nil {
		if err := finishAgentRunForImpactReview(ctx, h.agentRuns, actor, projectID, run); err != nil {
			return err
		}
	} else if err := finishAgentRun(ctx, h.agentRuns, actor, projectID, run, dispatchErr != nil); err != nil {
		return err
	}
	_, err = h.autonomous.HandleAgentRunResult(ctx, resultActorForAutonomousRun(actor, run.ExecutionPolicy), projectID, workflowID, result)
	return err
}

func (h eventHandler) dispatchAutonomousPublish(ctx context.Context, event outbox.Event) error {
	projectID, _ := event.Payload["project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("impact approval event is missing project_id")
	}
	actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: event.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
	run, _, _, err := h.agentRuns.Get(ctx, actor, projectID, event.AggregateID)
	if err != nil {
		return err
	}
	if run.Status == agentrun.Completed || run.Status == agentrun.Failed || run.Status == agentrun.Cancelled {
		return nil
	}
	if run.Status != agentrun.Reviewing {
		return fmt.Errorf("AgentRun %s is not waiting for impact publish", run.ID)
	}
	workflowID, _ := run.ExecutionPolicy["workflow_id"].(string)
	if strings.TrimSpace(workflowID) == "" {
		return nil
	}
	if h.dispatcher == nil || h.repositories == nil {
		return h.failPublish(ctx, actor, projectID, workflowID, run, "autonomous publish runner is not configured")
	}
	repositories, err := h.repositories.ListProjectRepositories(ctx, event.OrganizationID, projectID)
	if err != nil {
		return h.failPublish(ctx, actor, projectID, workflowID, run, "load linked repository: "+err.Error())
	}
	var repository githubintegration.Repository
	for _, candidate := range repositories {
		if candidate.ID == run.RepositoryID {
			repository = candidate
			break
		}
	}
	if repository.ID == "" || strings.TrimSpace(repository.CloneURL) == "" {
		return h.failPublish(ctx, actor, projectID, workflowID, run, "repository is not linked to the project or has no clone URL")
	}
	root := strings.TrimSpace(h.workspaceRoot)
	if root == "" {
		root = "/var/lib/forgeflow/workspaces"
	}
	if h.credentials == nil {
		return h.failPublish(ctx, actor, projectID, workflowID, run, "GitHub runner credential provider is not configured")
	}
	credential, err := h.credentials.RunnerGitCredential(ctx, actor, projectID, repository.ID)
	if err != nil {
		return h.failPublish(ctx, actor, projectID, workflowID, run, "could not issue GitHub runner credential")
	}
	result, dispatchErr := h.dispatchPublishJob(ctx, actor, projectID, workflowID, run, repository, root, credential.Token)
	if dispatchErr != nil {
		if result.Error == "" {
			result.Error = dispatchErr.Error()
		}
		return h.failPublish(ctx, actor, projectID, workflowID, run, result.Error)
	}
	if err := attachAndFinishPublished(ctx, h.agentRuns, actor, projectID, run, result); err != nil {
		return err
	}
	_, err = h.autonomous.HandleAgentRunResult(ctx, resultActorForAutonomousRun(actor, run.ExecutionPolicy), projectID, workflowID, result)
	return err
}

func (h eventHandler) handleAutonomousImpactRejected(ctx context.Context, event outbox.Event) error {
	projectID, _ := event.Payload["project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("impact rejection event is missing project_id")
	}
	actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: event.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
	run, _, _, err := h.agentRuns.Get(ctx, actor, projectID, event.AggregateID)
	if err != nil {
		return err
	}
	workflowID, _ := run.ExecutionPolicy["workflow_id"].(string)
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil
	}
	workflow, _, err := h.autonomous.Get(ctx, actor, projectID, workflowID)
	if err != nil {
		return err
	}
	if workflow.Status != autonomous.StatusWaitingImpactReview {
		return nil
	}
	message := "repository impact approval was rejected; review the affected-project feedback and retry"
	if run.Status == agentrun.Reviewing {
		if _, err := h.agentRuns.AttachResult(ctx, actor, projectID, run.ID, agentrun.ResultInput{Result: map[string]any{"content_trust": "UNTRUSTED_CONTENT", "impact_review_rejected": true}, Error: &message}); err != nil {
			return err
		}
		if _, err := h.agentRuns.Transition(ctx, actor, projectID, run.ID, agentrun.Failed); err != nil {
			return err
		}
	} else if run.Status != agentrun.Failed {
		return fmt.Errorf("AgentRun %s is not reviewable after impact rejection", run.ID)
	}
	_, err = h.autonomous.HandleAgentRunResult(ctx, actor, projectID, workflowID, runner.Result{Error: message, ErrorCode: runner.ErrorCodeImpactRejected})
	return err
}

func (h eventHandler) dispatchPublishJob(ctx context.Context, actor identity.Actor, projectID, workflowID string, run agentrun.Run, repository githubintegration.Repository, root, gitToken string) (runner.Result, error) {
	job := runner.Job{ID: run.ID + "-publish", AutonomousRunID: workflowID, AgentRunID: run.ID, Provider: run.AgentProvider, Model: run.Model, ReasoningEffort: run.ReasoningEffort, Operation: "publish", Prompt: "publish the already-reviewed Forgeflow changes", RepositoryURL: repository.CloneURL, BaseSHA: run.BaseSHA, Branch: run.Branch, PushBranch: true, CommitMessage: "chore: update Forgeflow work item", WorkspaceRoot: root, Workspace: filepath.Join(root, run.ID), Image: h.runnerImage, TimeoutSeconds: 3600, CPULimit: h.runnerCPU, MemoryLimit: h.runnerMemory, PidsLimit: h.runnerPids, NetworkMode: "bridge", AllowedHosts: []string{"github.com"}, GitToken: gitToken}
	return h.dispatcher.Dispatch(ctx, job)
}

func (h eventHandler) failPublish(ctx context.Context, actor identity.Actor, projectID, workflowID string, run agentrun.Run, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "autonomous publish failed"
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	if _, err := h.agentRuns.AttachResult(ctx, actor, projectID, run.ID, agentrun.ResultInput{Result: map[string]any{"content_trust": "UNTRUSTED_CONTENT"}, Error: &message}); err != nil {
		return err
	}
	if _, err := h.agentRuns.Transition(ctx, actor, projectID, run.ID, agentrun.Failed); err != nil {
		return err
	}
	_, err := h.autonomous.HandleAgentRunResult(ctx, actor, projectID, workflowID, runner.Result{Error: message})
	return err
}

func attachAndFinishPublished(ctx context.Context, service *agentrun.Service, actor identity.Actor, projectID string, run agentrun.Run, result runner.Result) error {
	output := result.Output
	if len(output) > maxStoredRunnerOutputBytes {
		output = output[:maxStoredRunnerOutputBytes]
	}
	payload := map[string]any{"runner_output": output, "content_trust": "UNTRUSTED_CONTENT", "publish_operation": true}
	if result.DiffHash != "" {
		payload["diff_hash"] = result.DiffHash
	}
	if len(result.ChangedPaths) > 0 {
		payload["changed_paths"] = result.ChangedPaths
	}
	if _, err := service.AttachResult(ctx, actor, projectID, run.ID, agentrun.ResultInput{Result: payload}); err != nil {
		return err
	}
	_, err := service.Transition(ctx, actor, projectID, run.ID, agentrun.Completed)
	return err
}

func resultActorForAutonomousRun(actor identity.Actor, executionPolicy map[string]any) identity.Actor {
	createdBy, _ := executionPolicy["created_by"].(string)
	if strings.TrimSpace(createdBy) != "" {
		actor.ID = strings.TrimSpace(createdBy)
	}
	return actor
}

func impactGateModeForPolicy(policy map[string]any) string {
	value, _ := policy["impact_gate_mode"].(string)
	if strings.EqualFold(strings.TrimSpace(value), impact.GateRequired) {
		return impact.GateRequired
	}
	return impact.GateReportOnly
}

func (h eventHandler) handleAutonomousInterrupt(ctx context.Context, event outbox.Event) error {
	projectID, _ := event.Payload["project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("interrupted AgentRun event is missing project_id")
	}
	actor := identity.Actor{Type: "system", ID: "autonomous-worker", OrganizationID: event.OrganizationID, Source: "worker", Capabilities: map[string]bool{"*": true}}
	run, _, _, err := h.agentRuns.Get(ctx, actor, projectID, event.AggregateID)
	if err != nil {
		return err
	}
	workflowID, _ := run.ExecutionPolicy["workflow_id"].(string)
	if strings.TrimSpace(workflowID) == "" {
		return nil
	}
	autonomousRun, _, err := h.autonomous.Get(ctx, actor, projectID, workflowID)
	if err != nil {
		return err
	}
	if autonomousRun.CurrentAgentRunID != run.ID || autonomousRun.Status == autonomous.StatusCompleted || autonomousRun.Status == autonomous.StatusCancelled || autonomousRun.Status == autonomous.StatusFailed {
		return nil
	}
	reason, _ := event.Payload["reason"].(string)
	if strings.TrimSpace(reason) == "" {
		reason = "AgentRun was interrupted"
	}
	_, err = h.autonomous.HandleAgentRunResult(ctx, actor, projectID, workflowID, runner.Result{Error: reason})
	return err
}

func mcpURLForProject(raw, projectID string, intake bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("server runner MCP URL is invalid")
	}
	query := parsed.Query()
	query.Set("project_id", strings.TrimSpace(projectID))
	query.Del("mode")
	if intake {
		query.Set("mode", "intake")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
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

func finishAgentRunForImpactReview(ctx context.Context, service *agentrun.Service, actor identity.Actor, projectID string, run agentrun.Run) error {
	current := run.Status
	for _, next := range []agentrun.Status{agentrun.Planning, agentrun.Investigating, agentrun.Implementing, agentrun.Testing, agentrun.Reviewing} {
		if current == next {
			continue
		}
		updated, err := service.Transition(ctx, actor, projectID, run.ID, next)
		if err != nil {
			return err
		}
		current = updated.Status
	}
	if current != agentrun.Reviewing {
		return fmt.Errorf("AgentRun %s did not reach REVIEWING for impact approval", run.ID)
	}
	return nil
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
	githubAppConfig := githubintegration.AppConfig{ID: cfg.GitHubAppID, Slug: cfg.GitHubAppSlug, PrivateKey: cfg.GitHubAppPrivateKey, CallbackURL: cfg.GitHubAppCallbackURL, WebBaseURL: cfg.WebBaseURL, ClientID: cfg.GitHubAppClientID, ClientSecret: cfg.GitHubAppClientSecret, UserCallbackURL: cfg.GitHubAppUserCallbackURL, TokenEncryptionKey: cfg.GitHubAppTokenEncryptionKey}
	githubClient, err := githubintegration.NewAppClient(githubAppConfig)
	if err != nil {
		logger.Error("configure GitHub App client", "error", err)
		os.Exit(1)
	}
	githubService := githubintegration.NewService(githubStore, githubClient, githubAppConfig, nil, nil, time.Now, nil)
	tenantService := tenant.NewService(tenant.NewPostgresStore(pool))
	webhookStore := githubintegration.NewPostgresWebhookStore(pool)
	auditWriter := audit.NewPostgresWriter(pool)
	outboxWriter := outbox.NewPostgresWriter(pool)
	specService := specification.NewService(specification.NewPostgresStore(pool), time.Now, specification.Options{Recorder: specification.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool)})
	workflowService := workflow.NewService(workflow.Default(), workflow.NewPostgresStore(pool))
	workflowService.SetTransaction(db.NewTransactionRunner(pool))
	workflowService.SetRecorder(workflow.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, time.Now)
	workItemService := workitem.NewService(workitem.NewPostgresRepository(pool), specService, workflowService, auditWriter, outboxWriter, time.Now, db.NewTransactionRunner(pool))
	specService.SetWorkItemStateReader(workItemService)
	agentRunStore := agentrun.NewPostgresStore(pool)
	webhookStore.SetAgentRunProjector(agentRunStore)
	impactService := impact.NewService(impact.NewPostgresStore(pool), workerRepositoryProjectResolver{tenant: tenantService, github: githubService}, impact.Options{Recorder: impact.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: time.Now})
	agentRunWatchdog := agentrun.NewService(agentRunStore, workItemService, agentrun.Options{Recorder: agentrun.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: time.Now, Impact: impactService})
	autonomousService := autonomous.NewService(autonomous.NewPostgresStore(pool), workItemService, specService, agentRunWatchdog, autonomous.Options{Recorder: autonomous.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: time.Now})
	var dispatcher runner.Dispatcher
	if cfg.RunnerURL != "" {
		dispatcher = runner.HTTPDispatcher{BaseURL: cfg.RunnerURL, Token: cfg.RunnerToken}
	}
	go runAgentRunWatchdog(ctx, logger, agentRunWatchdog)
	processor := outbox.NewPostgresProcessor(pool, eventHandler{logger: logger, automation: automationService, webhooks: webhookStore, repositories: githubStore, credentials: githubService, agentRuns: agentRunWatchdog, impact: impactService, autonomous: autonomousService, dispatcher: dispatcher, workspaceRoot: cfg.RunnerWorkspaceRoot, runnerImage: cfg.RunnerImage, runnerCPU: cfg.RunnerCPULimit, runnerMemory: cfg.RunnerMemoryLimit, runnerPids: cfg.RunnerPidsLimit, runnerNetwork: cfg.RunnerNetworkMode, runnerToken: cfg.RunnerToken, runnerMCPURL: cfg.RunnerMCPURL, runnerMCPToken: cfg.RunnerMCPToken, runnerGitConfigured: githubAppConfig.Configured()}, time.Now)
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
