package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/app"
	"github.com/forgeflow/forgeflow/backend/internal/mcp"
	"github.com/forgeflow/forgeflow/backend/internal/planning"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/tenant"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

func TestToolSnapshotIsStaticAndDescriptive(t *testing.T) {
	definitions := mcp.Definitions()
	if len(definitions) < 20 {
		t.Fatalf("tool count = %d", len(definitions))
	}
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Name) == "" || strings.TrimSpace(definition.Description) == "" {
			t.Fatalf("incomplete definition = %#v", definition)
		}
		if definition.Name == "repository.create_draft_pull_request" {
			t.Fatal("MCP must not expose a direct GitHub pull-request mutation")
		}
		if definition.InputSchema["type"] != "object" {
			t.Fatalf("non-object input schema for %s", definition.Name)
		}
	}
	listSchema := definitions[0].InputSchema["properties"].(map[string]any)
	if listSchema["query"].(map[string]any)["type"] != "string" || listSchema["limit"].(map[string]any)["type"] != "integer" {
		t.Fatal("list tool schema must describe bounded query and numeric limit")
	}
	for _, key := range []string{"type", "priority", "assignee_id", "sprint_id", "repository_id", "cursor", "include_archived"} {
		if _, ok := listSchema[key]; !ok {
			t.Fatalf("list tool schema is missing %s", key)
		}
	}
	var transitionSchema map[string]any
	for _, definition := range definitions {
		if definition.Name == "work_item.transition" {
			transitionSchema = definition.InputSchema["properties"].(map[string]any)
			break
		}
	}
	if transitionSchema == nil {
		t.Fatal("transition tool is missing")
	}
	if transitionSchema["expected_version"].(map[string]any)["type"] != "integer" {
		t.Fatal("transition schema must describe expected_version as an integer")
	}
	first := mcp.SnapshotHash()
	if first == "" || first != mcp.SnapshotHash() {
		t.Fatal("tool snapshot hash is not deterministic")
	}
}

func TestToolSnapshotMatchesPinnedHash(t *testing.T) {
	const want = "2221821593c7e813006652e89eaa507a19c5e81bdd74c1ede36351b32092fc91"
	if got := mcp.SnapshotHash(); got != want {
		t.Fatalf("MCP tool snapshot changed: got %q, want %q", got, want)
	}
}

func TestToolDefinitionsCannotMutateTheRegistry(t *testing.T) {
	first := mcp.SnapshotHash()
	definitions := mcp.Definitions()
	definitions[0].Description = "poisoned description"
	definitions[0].InputSchema["properties"].(map[string]any)["query"] = map[string]any{"type": "string"}
	if mcp.SnapshotHash() != first {
		t.Fatal("mutating returned definitions changed the registry snapshot")
	}
	if mcp.Definitions()[0].Description == "poisoned description" {
		t.Fatal("mutating returned definitions changed the next snapshot")
	}
}

func TestSpecificationToolsCannotBypassCapabilityChecks(t *testing.T) {
	application := app.New(1 << 20)
	adapter := mcp.NewServiceAdapter(identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-1"}, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "project-1")
	if _, err := adapter.Call(context.Background(), "specification.propose", map[string]any{"work_item_id": "item-1", "field": "ACTUAL_BEHAVIOR", "value": "guess", "provenance": "AI_HYPOTHESIS"}); err == nil {
		t.Fatal("specification proposal must require its capability")
	}
}

func TestRunScopedMCPActorCannotCallUnlistedTool(t *testing.T) {
	application := app.New(1 << 20)
	actor := identity.Actor{Type: "agent", ID: "run-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}, ToolPermissions: map[string]bool{"agent_run.get": true}}
	adapter := mcp.NewServiceAdapter(actor, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "project-1")
	if _, err := adapter.Call(context.Background(), "work_item.list", nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unlisted tool error = %v", err)
	}
}

func TestMCPProjectScopeCannotBeChangedByToolArguments(t *testing.T) {
	application := app.New(1 << 20)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	adapter := mcp.NewServiceAdapter(actor, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "project-1")
	if _, err := adapter.Call(context.Background(), "work_item.list", map[string]any{"project_id": "project-2"}); err == nil {
		t.Fatal("MCP tool arguments must not change the authenticated project scope")
	}
}

func TestAccountWideMCPRequiresExplicitProjectSelection(t *testing.T) {
	application := app.New(1 << 20)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	adapter := mcp.NewServiceAdapter(actor, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "")

	if _, err := adapter.Call(context.Background(), "work_item.list", nil); err == nil || !strings.Contains(err.Error(), "project.list") {
		t.Fatalf("missing project selection error = %v", err)
	}

	workspace, err := application.Tenant.CreateWorkspace(context.Background(), actor, "ENG", "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Tenant.CreateProject(context.Background(), actor, workspace.ID, "API", "API")
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Tenant.CreateProject(context.Background(), actor, workspace.ID, "WEB", "Web")
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Call(context.Background(), "project.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	items := result.(map[string]any)["items"].([]tenant.Project)
	if len(items) != 2 || items[0].ID != first.ID || items[1].ID != second.ID {
		t.Fatalf("projects = %#v", items)
	}
}

func TestSprintListUsesProjectScope(t *testing.T) {
	application := app.New(1 << 20)
	ctx := context.Background()
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	created, err := application.Planning.Create(ctx, actor, "project-1", "Sprint 1", "Ship", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.WorkItems.Create(ctx, workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1"}, actor, workitem.CreateInput{Type: workitem.Task, Title: "Implement sprint brief", SprintID: created.ID}); err != nil {
		t.Fatal(err)
	}
	adapter := mcp.NewServiceAdapter(actor, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "project-1")
	result, err := adapter.Call(ctx, "sprint.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	items := result.(map[string]any)["items"].([]planning.Sprint)
	if len(items) != 1 || items[0].ID != created.ID || items[0].Name != "Sprint 1" {
		t.Fatalf("sprints = %#v", items)
	}
	result, err = adapter.Call(ctx, "sprint.get_context", map[string]any{"id": created.ID})
	if err != nil {
		t.Fatal(err)
	}
	contextResult := result.(map[string]any)
	contextItems := contextResult["work_items"].([]*workitem.WorkItem)
	if contextResult["sprint_id"] != created.ID || contextResult["brief"] != "Ship" || len(contextItems) != 1 || contextItems[0].SprintID != created.ID {
		t.Fatalf("sprint context = %#v", contextResult)
	}
}

func TestWorkItemListPassesTypedFiltersAndCursor(t *testing.T) {
	application := app.New(1 << 20)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "FF"}
	if _, err := application.WorkItems.Create(context.Background(), scope, actor, workitem.CreateInput{Type: workitem.Bug, Title: "Critical bug", Priority: "HIGH"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.WorkItems.Create(context.Background(), scope, actor, workitem.CreateInput{Type: workitem.Task, Title: "Routine task", Priority: "LOW"}); err != nil {
		t.Fatal(err)
	}
	adapter := mcp.NewServiceAdapter(actor, application.WorkItems, application.Specifications, application.Tenant, application.Planning, "project-1")
	result, err := adapter.Call(context.Background(), "work_item.search", map[string]any{"type": "BUG", "priority": "HIGH", "limit": 1, "cursor": "", "include_archived": false})
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", result)
	}
	items, ok := payload["items"].([]*workitem.WorkItem)
	if !ok || len(items) != 1 || items[0].Type != workitem.Bug || items[0].Priority != "HIGH" {
		t.Fatalf("filtered items = %#v", payload["items"])
	}
	if payload["next_cursor"] != "" {
		t.Fatalf("unexpected cursor for one filtered item: %#v", payload["next_cursor"])
	}
}
