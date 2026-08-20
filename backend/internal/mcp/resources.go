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
		Name:        "repository-context",
		Description: "Authorized repository architecture, conventions, testing, and issue context.",
		MIMEType:    "application/json",
		URITemplate: "repo://{repository_id}/{topic}",
	}, handler)
	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "repository-module",
		Description: "A bounded untrusted file from an authorized repository.",
		MIMEType:    "text/plain",
		URITemplate: "repo://{repository_id}/module/{path}",
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
	case "repo":
		return a.readRepositoryResource(ctx, rawURI, u)
	default:
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
}

func (a *ServiceAdapter) readTaskResource(ctx context.Context, rawURI string, u *url.URL) (*sdk.ReadResourceResult, error) {
	if a.WorkItems == nil || strings.TrimSpace(a.ProjectID) == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	number, err := strconv.ParseInt(strings.Trim(u.Path, "/"), 10, 64)
	if err != nil || number < 1 || strings.TrimSpace(u.Host) == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	key := strings.ToUpper(strings.TrimSpace(u.Host)) + "-" + strconv.FormatInt(number, 10)
	var cursor string
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		page, listErr := a.WorkItems.ListPage(ctx, workitem.Scope{OrganizationID: a.Actor.OrganizationID, ProjectID: a.ProjectID}, a.Actor, workitem.ListFilter{Limit: 100, Cursor: cursor})
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range page.Items {
			if strings.EqualFold(item.Key, key) {
				value, callErr := a.Call(ctx, "work_item.get_context", map[string]any{"id": item.ID})
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
	if a.GitHub == nil || strings.TrimSpace(a.ProjectID) == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	repositoryID := strings.TrimSpace(u.Host)
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if repositoryID == "" || len(pathParts) == 0 || pathParts[0] == "" {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	var snapshot *githubintegration.SnapshotRecord
	if a.GitHub.SnapshotService() != nil {
		if item, ok, err := a.latestSnapshot(ctx, a.ProjectID, repositoryID); err != nil {
			return nil, err
		} else if ok {
			snapshot = &item
		}
	}
	if pathParts[0] == "module" {
		if len(pathParts) < 2 {
			return nil, sdk.ResourceNotFoundError(rawURI)
		}
		filePath, err := url.PathUnescape(strings.Join(pathParts[1:], "/"))
		if err != nil {
			return nil, sdk.ResourceNotFoundError(rawURI)
		}
		var content string
		var fileErr error
		if snapshot != nil {
			var file githubintegration.SnapshotFile
			file, fileErr = a.GitHub.SnapshotService().File(ctx, a.Actor, a.ProjectID, repositoryID, snapshot.ID, filePath)
			content = file.Content
		} else {
			var file githubintegration.RepositoryFile
			file, fileErr = a.GitHub.RepositoryFile(ctx, a.Actor, a.ProjectID, repositoryID, filePath)
			content = file.Content
		}
		if fileErr != nil {
			return nil, fileErr
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: rawURI, MIMEType: "text/plain", Text: content}}}, nil
	}
	if len(pathParts) != 1 || !allowedResourceTopic(pathParts[0]) {
		return nil, sdk.ResourceNotFoundError(rawURI)
	}
	repositoryContext, err := a.GitHub.RepositoryContext(ctx, a.Actor, a.ProjectID, repositoryID)
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
		"content_trust": "UNTRUSTED_CONTENT",
	}
	if snapshot != nil {
		value["fixed_snapshot"] = snapshot
	}
	if snapshots := a.GitHub.SnapshotService(); snapshots != nil {
		if knowledge := snapshots.KnowledgeService(); knowledge != nil {
			documents, knowledgeErr := knowledge.List(ctx, a.Actor, a.ProjectID, repositoryID, 50)
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
