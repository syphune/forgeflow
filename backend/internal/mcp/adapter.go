package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/planning"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/tenant"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type ServiceAdapter struct {
	Actor      identity.Actor
	WorkItems  *workitem.Service
	Spec       *specification.Service
	Tenant     *tenant.Service
	Planning   *planning.Service
	AgentRuns  *agentrun.Service
	Autonomous *autonomous.Service
	GitHub     *githubintegration.Service
	ProjectID  string
}

func (a *ServiceAdapter) SetAutonomous(service *autonomous.Service) {
	a.Autonomous = service
}

func NewServiceAdapter(actor identity.Actor, workItems *workitem.Service, spec *specification.Service, tenantService *tenant.Service, planningService *planning.Service, projectID string, agentRuns ...*agentrun.Service) *ServiceAdapter {
	var runs *agentrun.Service
	if len(agentRuns) > 0 {
		runs = agentRuns[0]
	}
	return &ServiceAdapter{Actor: actor, WorkItems: workItems, Spec: spec, Tenant: tenantService, Planning: planningService, AgentRuns: runs, ProjectID: projectID}
}

func (a ServiceAdapter) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	if name == "project.list" {
		if a.Tenant == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "tenant adapter is unavailable", nil)
		}
		items, err := a.Tenant.Projects(ctx, a.Actor, stringArg(args, "workspace_id"))
		return map[string]any{"items": items}, err
	}
	requestedProjectID := stringArg(args, "project_id")
	projectID := a.ProjectID
	if projectID == "" {
		projectID = requestedProjectID
	} else if requestedProjectID != "" && requestedProjectID != projectID {
		return nil, apperr.New(apperr.CodeForbidden, 403, "MCP project scope cannot be changed by a tool argument", map[string]any{"project_id": projectID})
	}
	if projectID == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required; call project.list and ask the user to choose one project before retrying", map[string]any{"requires_project_selection": true})
	}
	a.Actor = a.Actor.ForProject(projectID)
	if name == "sprint.list" {
		if a.Planning == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "planning adapter is unavailable", nil)
		}
		items, err := a.Planning.List(ctx, a.Actor, projectID)
		return map[string]any{"items": items}, err
	}
	if name == "sprint.get_context" {
		if a.Planning == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "planning adapter is unavailable", nil)
		}
		if a.WorkItems == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "work-item adapter is unavailable", nil)
		}
		sprint, err := a.sprintByID(ctx, projectID, stringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		page, err := a.WorkItems.ListPage(ctx, workitem.Scope{OrganizationID: a.Actor.OrganizationID, ProjectID: projectID}, a.Actor, workitem.ListFilter{SprintID: sprint.ID, Limit: 100, Cursor: stringArg(args, "cursor")})
		if err != nil {
			return nil, err
		}
		return map[string]any{"sprint_id": sprint.ID, "brief": sprint.Goal, "sprint": sprint, "work_items": page.Items, "next_cursor": page.NextCursor}, nil
	}
	scope := workitem.Scope{OrganizationID: a.Actor.OrganizationID, ProjectID: projectID}
	if a.WorkItems == nil {
		return nil, apperr.New(apperr.CodeInternal, 503, "work-item adapter is unavailable", nil)
	}
	switch name {
	case "work_item.list", "work_item.search":
		page, err := a.WorkItems.ListPage(ctx, scope, a.Actor, workitem.ListFilter{
			Status:          stringArg(args, "status"),
			Type:            stringArg(args, "type"),
			Priority:        stringArg(args, "priority"),
			AssigneeID:      stringArg(args, "assignee_id"),
			SprintID:        stringArg(args, "sprint_id"),
			RepositoryID:    stringArg(args, "repository_id"),
			Query:           stringArg(args, "query"),
			Limit:           intArg(args, "limit"),
			Cursor:          stringArg(args, "cursor"),
			IncludeArchived: boolArg(args, "include_archived"),
		})
		return map[string]any{"items": page.Items, "next_cursor": page.NextCursor}, err
	case "work_item.get":
		return a.WorkItems.Get(ctx, scope, a.Actor, stringArg(args, "id"))
	case "work_item.get_context":
		item, err := a.WorkItems.Get(ctx, scope, a.Actor, stringArg(args, "id"))
		if err != nil {
			return nil, err
		}
		result := map[string]any{"work_item": item, "content_trust": "UNTRUSTED_CONTENT"}
		if a.Spec != nil {
			spec, specErr := a.Spec.Get(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
			if specErr != nil {
				return nil, specErr
			}
			readiness, readyErr := a.WorkItems.Readiness(ctx, scope, a.Actor, item)
			if readyErr != nil {
				return nil, readyErr
			}
			result["specification"] = spec
			result["readiness"] = readiness
			repositoryID := strings.TrimSpace(item.RepositoryID)
			if repositoryID == "" {
				if currentSpec, specErr := a.Spec.Get(ctx, scope.OrganizationID, scope.ProjectID, item.ID); specErr == nil && currentSpec != nil {
					repositoryID = strings.TrimSpace(currentSpec.RepositoryID)
				}
			}
			if a.GitHub != nil && repositoryID != "" {
				context, contextErr := a.GitHub.RepositoryContext(ctx, a.Actor, scope.ProjectID, repositoryID)
				if contextErr != nil {
					return nil, contextErr
				}
				result["repository_context"] = context
				result["related_commits"] = context.Commits
				result["related_pull_requests"] = context.PullRequests
				result["related_ci_runs"] = context.CIRuns
				result["repository_source"] = "github"
				result["repository_ref"] = context.Repository.DefaultBranch
				result["content_trust"] = "UNTRUSTED_CONTENT"
				result["context_budget"] = map[string]any{"max_items": 50, "max_file_bytes": 262144, "max_total_bytes": 1048576}
				query := strings.TrimSpace(item.Title)
				if len(query) > 120 {
					query = query[:120]
				}
				if query != "" {
					files, searchErr := a.GitHub.SearchRepository(ctx, a.Actor, scope.ProjectID, repositoryID, query, 10)
					if searchErr != nil {
						return nil, searchErr
					}
					result["relevant_files"] = files
				}
				if snapshots := a.GitHub.SnapshotService(); snapshots != nil {
					if knowledge := snapshots.KnowledgeService(); knowledge != nil {
						documents, knowledgeErr := knowledge.List(ctx, a.Actor, scope.ProjectID, repositoryID, 20)
						if knowledgeErr != nil {
							return nil, knowledgeErr
						}
						result["knowledge_documents"] = documents
					}
				}
			}
		}
		return result, nil
	case "work_item.create":
		itemType := workitem.Type(stringArg(args, "type"))
		dueAt, err := timeArgPtr(args, "due_at")
		if err != nil {
			return nil, err
		}
		item, err := a.WorkItems.Create(ctx, scope, a.Actor, workitem.CreateInput{Type: itemType, Title: stringArg(args, "title"), Description: stringArg(args, "description"), ParentID: stringArg(args, "parent_id"), RepositoryID: stringArg(args, "repository_id"), Priority: stringArg(args, "priority"), RiskLevel: workitem.RiskLevel(stringArg(args, "risk_level")), DueAt: dueAt, EstimatePoints: intPtr(args, "estimate_points"), SprintID: stringArg(args, "sprint_id")})
		return item, err
	case "work_item.update":
		title := stringArgPtr(args, "title")
		description := stringArgPtr(args, "description")
		dueAt, err := timeArgPtr(args, "due_at")
		if err != nil {
			return nil, err
		}
		dueAtSet := args["due_at"] != nil
		riskLevel := workitem.RiskLevel(strings.ToUpper(strings.TrimSpace(stringArg(args, "risk_level"))))
		item, err := a.WorkItems.Update(ctx, scope, a.Actor, stringArg(args, "id"), workitem.UpdateInput{Title: title, Description: description, ParentID: stringArgPtr(args, "parent_id"), ParentIDSet: args["parent_id"] != nil, RepositoryID: stringArgPtr(args, "repository_id"), RepositoryIDSet: args["repository_id"] != nil, Priority: stringArgPtr(args, "priority"), RiskLevel: &riskLevel, RiskLevelSet: args["risk_level"] != nil, DueAt: dueAt, DueAtSet: dueAtSet, EstimatePoints: intPtr(args, "estimate_points"), EstimatePointsSet: args["estimate_points"] != nil, SprintID: stringArgPtr(args, "sprint_id"), SprintIDSet: args["sprint_id"] != nil, ExpectedVersion: int64Arg(args, "expected_version")})
		return item, err
	case "work_item.assign":
		return a.WorkItems.Assign(ctx, scope, a.Actor, stringArg(args, "id"), stringArg(args, "assignee_id"), int64Arg(args, "expected_version"))
	case "work_item.transition":
		return a.WorkItems.Transition(ctx, scope, a.Actor, stringArg(args, "id"), workitem.TransitionInput{TransitionKey: stringArg(args, "transition_key"), ExpectedVersion: int64Arg(args, "expected_version")})
	case "work_item.comment":
		return a.WorkItems.CreateComment(ctx, scope, a.Actor, stringArg(args, "id"), stringArg(args, "body"))
	case "specification.get":
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, stringArg(args, "work_item_id")); err != nil {
			return nil, err
		}
		return a.Spec.Get(ctx, scope.OrganizationID, scope.ProjectID, stringArg(args, "work_item_id"))
	case "specification.propose":
		if !a.Actor.Has(identity.CapabilitySpecificationPropose) {
			return nil, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationPropose})
		}
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, stringArg(args, "work_item_id")); err != nil {
			return nil, err
		}
		return a.Spec.Propose(ctx, scope.OrganizationID, scope.ProjectID, stringArg(args, "work_item_id"), specification.FieldKey(stringArg(args, "field")), stringArg(args, "value"), specification.Provenance(stringArg(args, "provenance")))
	case "specification.propose_analysis":
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		workItemID := stringArg(args, "work_item_id")
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, workItemID); err != nil {
			return nil, err
		}
		return a.Spec.AddAnalysis(ctx, scope.OrganizationID, scope.ProjectID, workItemID, a.Actor, specification.Analysis{
			RootCauseHypothesis: stringArg(args, "root_cause_hypothesis"),
			BlastRadius:         stringArg(args, "blast_radius"),
			ImplementationPlan:  stringArg(args, "implementation_plan"),
			TestPlan:            stringArg(args, "test_plan"),
			EvidenceRefs:        stringSliceArg(args, "evidence_refs"),
			Confidence:          numberArg(args, "confidence"),
		})
	case "specification.accept_proposal":
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		workItemID := stringArg(args, "work_item_id")
		if workItemID == "" {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "work_item_id is required", nil)
		}
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, workItemID); err != nil {
			return nil, err
		}
		return a.Spec.AcceptProposal(ctx, scope.OrganizationID, scope.ProjectID, workItemID, stringArg(args, "proposal_id"), a.Actor)
	case "specification.reject_proposal":
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		workItemID := stringArg(args, "work_item_id")
		if workItemID == "" {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "work_item_id is required", nil)
		}
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, workItemID); err != nil {
			return nil, err
		}
		return a.Spec.RejectProposal(ctx, scope.OrganizationID, scope.ProjectID, workItemID, stringArg(args, "proposal_id"), a.Actor)
	case "specification.verify_field":
		if !a.Actor.Has(identity.CapabilitySpecificationVerify) {
			return nil, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationVerify})
		}
		if a.Actor.Type != "human" {
			return nil, apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot verify specification fields", nil)
		}
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, stringArg(args, "work_item_id")); err != nil {
			return nil, err
		}
		return nil, a.Spec.VerifyField(ctx, scope.OrganizationID, scope.ProjectID, stringArg(args, "work_item_id"), specification.FieldKey(stringArg(args, "field")), a.Actor.Type, a.Actor.ID)
	case "specification.request_clarification":
		if a.Spec == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "specification adapter is unavailable", nil)
		}
		if _, err := a.WorkItems.Get(ctx, scope, a.Actor, stringArg(args, "work_item_id")); err != nil {
			return nil, err
		}
		comment, err := a.WorkItems.CreateComment(ctx, scope, a.Actor, stringArg(args, "work_item_id"), "[Clarification requested]\n"+stringArg(args, "question"))
		return comment, err
	case "agent_run.get":
		if a.AgentRuns == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
		}
		run, steps, artifacts, err := a.AgentRuns.Get(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"))
		return map[string]any{"run": run, "steps": steps, "artifacts": artifacts}, err
	case "agent_run.create":
		if a.AgentRuns == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
		}
		return a.AgentRuns.Create(ctx, a.Actor, agentrun.CreateInput{ProjectID: scope.ProjectID, WorkItemID: stringArg(args, "work_item_id"), RepositoryID: stringArg(args, "repository_id"), AgentProvider: stringArg(args, "agent_provider"), AgentName: stringArg(args, "agent_name"), Model: stringArg(args, "model"), BaseSHA: stringArg(args, "base_sha"), Branch: stringArg(args, "branch"), ExecutionInputs: agentrun.ExecutionInputs{Prompt: stringArg(args, "prompt"), TestCasePositions: intSliceArg(args, "test_case_positions")}})
	case "agent_run.cancel":
		if a.AgentRuns == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
		}
		return a.AgentRuns.Cancel(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"))
	case "agent_run.attach_result":
		if a.AgentRuns == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
		}
		result := args["result"]
		if result == nil || mapArg(args, "result") == nil {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "result must be an object", nil)
		}
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > 512*1024 {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "agent result is invalid or exceeds the size limit", nil)
		}
		metadata := mapArg(args, "metadata")
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["content_trust"] = "UNTRUSTED_CONTENT"
		return a.AgentRuns.AttachResult(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"), agentrun.ResultInput{
			CommitSHA: stringArg(args, "commit_sha"), PullRequestID: stringArg(args, "pull_request_id"), Result: mapArg(args, "result"), Error: stringArgPtr(args, "error"), Metadata: metadata,
		})
	case "agent_run.record_test_results":
		if a.AgentRuns == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "AgentRun adapter is unavailable", nil)
		}
		cases, err := testCaseResultsArg(args)
		if err != nil {
			return nil, err
		}
		return a.AgentRuns.RecordTestResults(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"), agentrun.TestResultsInput{Cases: cases, ReviewNote: stringArg(args, "review_note")})
	case "autonomous.start":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		policy, err := autonomousPolicyArg(args)
		if err != nil {
			return nil, err
		}
		return a.Autonomous.Start(ctx, a.Actor, autonomous.StartInput{ProjectID: scope.ProjectID, WorkItemID: stringArg(args, "work_item_id"), WorkItemType: stringArg(args, "work_item_type"), Title: stringArg(args, "title"), RepositoryID: stringArg(args, "repository_id"), Objective: stringArg(args, "objective"), AgentProvider: stringArg(args, "agent_provider"), AgentName: stringArg(args, "agent_name"), Model: stringArg(args, "model"), BaseSHA: stringArg(args, "base_sha"), Branch: stringArg(args, "branch"), TargetEnvironment: stringArg(args, "target_environment"), TestCasePositions: intSliceArg(args, "test_case_positions"), Policy: policy})
	case "autonomous.get":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		run, feedback, err := a.Autonomous.Get(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"))
		return map[string]any{"run": run, "feedback": feedback}, err
	case "autonomous.resume":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		return a.Autonomous.Resume(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"))
	case "autonomous.retry":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		return a.Autonomous.Retry(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"), autonomous.RetryInput{Feedback: stringArg(args, "feedback"), TestCasePositions: intSliceArg(args, "test_case_positions")})
	case "autonomous.cancel":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		return a.Autonomous.Cancel(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"))
	case "autonomous.add_feedback":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		return a.Autonomous.AddFeedback(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"), autonomous.FeedbackInput{Source: stringArg(args, "source"), Note: stringArg(args, "note"), Severity: stringArg(args, "severity"), CommitSHA: stringArg(args, "commit_sha"), TestCasePositions: intSliceArg(args, "test_case_positions"), EvidenceRefs: stringSliceArg(args, "evidence_refs")})
	case "autonomous.record_test_results":
		if a.Autonomous == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "autonomous adapter is unavailable", nil)
		}
		cases, err := testCaseResultsArg(args)
		if err != nil {
			return nil, err
		}
		return a.Autonomous.RecordTestResults(ctx, a.Actor, scope.ProjectID, stringArg(args, "id"), agentrun.TestResultsInput{Cases: cases, ReviewNote: stringArg(args, "review_note")})
	case "repository.get", "repository.get_structure", "repository.search_code", "repository.get_file", "repository.get_symbol", "repository.related_files", "repository.related_commits", "repository.related_pull_requests":
		if a.GitHub == nil {
			return nil, apperr.New(apperr.CodeInternal, 503, "repository adapter is unavailable", nil)
		}
		repositoryID := stringArg(args, "repository_id")
		if repositoryID == "" {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "repository_id is required", nil)
		}
		switch name {
		case "repository.get":
			repositoryContext, err := a.GitHub.RepositoryContext(ctx, a.Actor, scope.ProjectID, repositoryID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"repository": repositoryContext.Repository, "branches": repositoryContext.Branches, "commits": repositoryContext.Commits, "pull_requests": repositoryContext.PullRequests, "ci_runs": repositoryContext.CIRuns, "source": "github"}, nil
		case "repository.related_pull_requests":
			repositoryContext, err := a.GitHub.RepositoryContext(ctx, a.Actor, scope.ProjectID, repositoryID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": repositoryContext.PullRequests, "source": "github", "content_trust": "UNTRUSTED_CONTENT"}, nil
		case "repository.related_commits":
			repositoryContext, err := a.GitHub.RepositoryContext(ctx, a.Actor, scope.ProjectID, repositoryID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": repositoryContext.Commits, "source": "github", "content_trust": "UNTRUSTED_CONTENT"}, nil
		case "repository.get_structure":
			tree, err := a.GitHub.RepositoryTree(ctx, a.Actor, scope.ProjectID, repositoryID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": tree, "source": "github", "ref": "default_branch", "content_trust": "UNTRUSTED_CONTENT"}, nil
		case "repository.search_code":
			matches, err := a.GitHub.SearchRepository(ctx, a.Actor, scope.ProjectID, repositoryID, stringArg(args, "query"), intArg(args, "limit"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": matches, "source": "github", "ref": "default_branch", "content_trust": "UNTRUSTED_CONTENT"}, nil
		case "repository.get_file":
			file, err := a.GitHub.RepositoryFile(ctx, a.Actor, scope.ProjectID, repositoryID, stringArg(args, "path"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"file": file, "source": "github", "ref": file.Ref, "content_trust": "UNTRUSTED_CONTENT"}, nil
		case "repository.get_symbol":
			matches, err := a.GitHub.SearchRepository(ctx, a.Actor, scope.ProjectID, repositoryID, stringArg(args, "symbol"), intArg(args, "limit"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": matches, "source": "github", "ref": "default_branch", "match_type": "lexical_candidate", "provenance": "EXTRACTED", "content_trust": "UNTRUSTED_CONTENT"}, nil
		case "repository.related_files":
			files, err := a.GitHub.RelatedRepositoryFiles(ctx, a.Actor, scope.ProjectID, repositoryID, stringArg(args, "path"), intArg(args, "limit"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": files, "source": "github", "ref": "default_branch", "content_trust": "UNTRUSTED_CONTENT"}, nil
		default:
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "repository tool is not implemented in this adapter", map[string]any{"tool": name})
		}
	default:
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "MCP tool is not implemented in this adapter", map[string]any{"tool": name})
	}
}

func (a ServiceAdapter) projectIDForKey(ctx context.Context, key string) (string, error) {
	if a.Tenant == nil {
		return "", apperr.New(apperr.CodeInternal, 503, "tenant adapter is unavailable", nil)
	}
	items, err := a.Tenant.Projects(ctx, a.Actor, "")
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Key), strings.TrimSpace(key)) {
			return item.ID, nil
		}
	}
	return "", apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
}

func (a ServiceAdapter) sprintByID(ctx context.Context, projectID, id string) (planning.Sprint, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return planning.Sprint{}, apperr.New(apperr.CodeInvalidArgument, 422, "id is required", nil)
	}
	items, err := a.Planning.List(ctx, a.Actor, projectID)
	if err != nil {
		return planning.Sprint{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return planning.Sprint{}, apperr.New(apperr.CodeNotFound, 404, "sprint not found", nil)
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func mapArg(args map[string]any, key string) map[string]any {
	value, ok := args[key].(map[string]any)
	if !ok {
		return nil
	}
	return value
}

func autonomousPolicyArg(args map[string]any) (autonomous.Policy, error) {
	value := mapArg(args, "policy")
	if value == nil {
		return autonomous.Policy{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return autonomous.Policy{}, apperr.New(apperr.CodeInvalidArgument, 422, "policy is invalid", nil)
	}
	var policy autonomous.Policy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return autonomous.Policy{}, apperr.New(apperr.CodeInvalidArgument, 422, "policy is invalid", nil)
	}
	return policy, nil
}
func stringArgPtr(args map[string]any, key string) *string {
	if _, ok := args[key]; !ok {
		return nil
	}
	value, ok := args[key].(string)
	if !ok {
		return nil
	}
	return &value
}
func intArg(args map[string]any, key string) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(value)
		return parsed
	default:
		return 0
	}
}
func int64Arg(args map[string]any, key string) int64 {
	switch value := args[key].(type) {
	case float64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	default:
		return 0
	}
}

func boolArg(args map[string]any, key string) bool {
	value, ok := args[key].(bool)
	return ok && value
}

func numberArg(args map[string]any, key string) float64 {
	switch value := args[key].(type) {
	case float64:
		return value
	case string:
		parsed, _ := strconv.ParseFloat(value, 64)
		return parsed
	default:
		return 0
	}
}

func stringSliceArg(args map[string]any, key string) []string {
	values, ok := args[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func intSliceArg(args map[string]any, key string) []int {
	values, ok := args[key].([]any)
	if !ok {
		return nil
	}
	result := make([]int, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			result = append(result, int(typed))
		case string:
			parsed, _ := strconv.Atoi(typed)
			result = append(result, parsed)
		}
	}
	return result
}

func testCaseResultsArg(args map[string]any) ([]agentrun.TestCaseResultInput, error) {
	value, ok := args["test_cases"]
	if !ok || value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "test_cases must be an array", nil)
	}
	var result []agentrun.TestCaseResultInput
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "test_cases must be an array of result objects", nil)
	}
	return result, nil
}

func intPtr(args map[string]any, key string) *int {
	if _, ok := args[key]; !ok {
		return nil
	}
	value := intArg(args, key)
	return &value
}

func timeArgPtr(args map[string]any, key string) (*time.Time, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return nil, nil
	}
	textValue, ok := value.(string)
	if !ok || strings.TrimSpace(textValue) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, key+" must be an RFC3339 timestamp", nil)
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(textValue))
	if err != nil {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, key+" must be an RFC3339 timestamp", nil)
	}
	return &parsed, nil
}

var _ Adapter = (*ServiceAdapter)(nil)
