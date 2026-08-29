package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func RegisterResources(server *sdk.Server, adapter *ServiceAdapter) {
	server.AddResource(&sdk.Resource{
		URI:         AgentSkillResourceURI,
		Name:        "forgeflow-agent-skill",
		Description: "Server-managed Forgeflow workflow for planning, specification, execution, verification, and handoff.",
		MIMEType:    "text/markdown",
	}, func(_ context.Context, request *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		if request == nil || request.Params == nil || request.Params.URI != AgentSkillResourceURI {
			uri := AgentSkillResourceURI
			if request != nil && request.Params != nil && request.Params.URI != "" {
				uri = request.Params.URI
			}
			return nil, sdk.ResourceNotFoundError(uri)
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: AgentSkillResourceURI, MIMEType: "text/markdown", Text: agentSkillText}}}, nil
	})
	handler := func(ctx context.Context, request *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return adapter.readResource(ctx, request.Params.URI)
	}
	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "task-context",
		Description: "Verified and explicitly labeled context for a project work item.",
		MIMEType:    "application/json",
		URITemplate: "task://{project_key}/{number}",
	}, handler)
	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "sprint-context",
		Description: "Sprint or iteration brief and its authorized work items.",
		MIMEType:    "application/json",
		URITemplate: "sprint://{sprint_id}?project_id={project_id}",
	}, handler)
	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "repository-context",
		Description: "Authorized repository architecture, conventions, testing, and issue context.",
		MIMEType:    "application/json",
		URITemplate: "repo://{repository_id}/{topic}?project_id={project_id}",
	}, handler)
	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "repository-module",
		Description: "A bounded untrusted file from an authorized repository.",
		MIMEType:    "text/plain",
		URITemplate: "repo://{repository_id}/module/{path}?project_id={project_id}",
	}, handler)
}

func (a *ServiceAdapter) readResource(ctx context.Context, rawURI string) (*sdk.ReadResourceResult, error) {
	u, err := url.Parse(rawURI)
	if err != nil || u.Host == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	switch u.Scheme {
	case "task":
		return a.readTaskResource(ctx, rawURI, u)
	case "sprint":
		return a.readSprintResource(ctx, rawURI, u)
	case "repo":
		return a.readRepositoryResource(ctx, rawURI, u)
	default:
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
}

func (a *ServiceAdapter) readSprintResource(ctx context.Context, rawURI string, u *url.URL) (*sdk.ReadResourceResult, error) {
	if a.Planning == nil || strings.TrimSpace(u.Host) == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	projectID := strings.TrimSpace(a.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(u.Query().Get("project_id"))
	}
	if projectID == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	value, err := a.Call(ctx, "sprint.get_context", map[string]any{"project_id": projectID, "id": u.Host, "cursor": u.Query().Get("cursor")})
	if err != nil {
		return nil, err
	}
	return jsonResource(rawURI, value)
}

func (a *ServiceAdapter) readTaskResource(ctx context.Context, rawURI string, u *url.URL) (*sdk.ReadResourceResult, error) {
	if a.WorkItems == nil || strings.TrimSpace(u.Host) == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	projectID := strings.TrimSpace(a.ProjectID)
	if projectID == "" {
		var err error
		projectID, err = a.projectIDForKey(ctx, u.Host)
		if err != nil {
			return nil, sdk.ResourceNotFoundError(rawURI)
		}
	}
	actor := a.Actor.ForProject(projectID)
	number, err := strconv.ParseInt(strings.Trim(u.Path, "/"), 10, 64)
	if err != nil || number < 1 || strings.TrimSpace(u.Host) == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	key := strings.ToUpper(strings.TrimSpace(u.Host)) + "-" + strconv.FormatInt(number, 10)
	var cursor string
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		page, listErr := a.WorkItems.ListPage(ctx, workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}, actor, workitem.ListFilter{Limit: 100, Cursor: cursor})
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range page.Items {
			if strings.EqualFold(item.Key, key) {
				value, callErr := a.Call(ctx, "work_item.get_context", map[string]any{"id": item.ID, "project_id": projectID})
				if callErr != nil {
					return nil, callErr
				}
				return jsonResource(rawURI, value)
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return nil, sdk.ResourceNotFoundError(rawURI)
}

func (a *ServiceAdapter) readRepositoryResource(ctx context.Context, rawURI string, u *url.URL) (*sdk.ReadResourceResult, error) {
	if a.GitHub == nil {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	projectID := strings.TrimSpace(a.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(u.Query().Get("project_id"))
	}
	if projectID == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	actor := a.Actor.ForProject(projectID)
	repositoryID := strings.TrimSpace(u.Host)
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if repositoryID == "" || len(pathParts) == 0 || pathParts[0] == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	if pathParts[0] == "module" {
		if len(pathParts) < 2 {
			return nil, sdk.ResourceNotFoundError(rawURI)
		}
		filePath, err := url.PathUnescape(strings.Join(pathParts[1:], "/"))
		if err != nil {
			return nil, sdk.ResourceNotFoundError(rawURI)
		}
		file, fileErr := a.GitHub.RepositoryFile(ctx, actor, projectID, repositoryID, filePath)
		if fileErr != nil {
			return nil, fileErr
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: rawURI, MIMEType: "text/plain", Text: file.Content}}}, nil
	}
	if len(pathParts) != 1 || !allowedResourceTopic(pathParts[0]) {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	repositoryContext, err := a.GitHub.RepositoryContext(ctx, actor, projectID, repositoryID)
	if err != nil {
		return nil, err
	}
	value := map[string]any{
		"topic":         pathParts[0],
		"repository":    repositoryContext.Repository,
		"branches":      repositoryContext.Branches,
		"commits":       repositoryContext.Commits,
		"pull_requests": repositoryContext.PullRequests,
		"ci_runs":       repositoryContext.CIRuns,
		"source":        "github",
		"ref":           repositoryContext.Repository.DefaultBranch,
		"content_trust": "UNTRUSTED_CONTENT",
	}
	if snapshots := a.GitHub.SnapshotService(); snapshots != nil {
		if knowledge := snapshots.KnowledgeService(); knowledge != nil {
			documents, knowledgeErr := knowledge.List(ctx, actor, projectID, repositoryID, 50)
			if knowledgeErr != nil {
				return nil, knowledgeErr
			}
			kind := knowledgeKindForTopic(pathParts[0])
			filtered := make([]githubintegration.KnowledgeDocument, 0, len(documents))
			for _, document := range documents {
				if document.Kind == kind {
					filtered = append(filtered, document)
				}
			}
			value["knowledge_documents"] = filtered
		}
	}
	return jsonResource(rawURI, value)
}

func knowledgeKindForTopic(topic string) string {
	switch topic {
	case "architecture":
		return "ARCHITECTURE"
	case "conventions":
		return "CONVENTIONS"
	case "testing":
		return "TESTING"
	case "known-issues":
		return "KNOWN_ISSUES"
	case "domain-rules":
		return "DOMAIN_RULES"
	default:
		return ""
	}
}

func allowedResourceTopic(topic string) bool {
	switch topic {
	case "architecture", "conventions", "testing", "known-issues", "domain-rules":
		return true
	default:
		return false
	}
}

func jsonResource(uri string, value any) (*sdk.ReadResourceResult, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode MCP resource: %w", err)
	}
	if len(encoded) > 512<<10 {
		return nil, fmt.Errorf("MCP resource exceeds the context limit")
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(encoded)}}}, nil
}
