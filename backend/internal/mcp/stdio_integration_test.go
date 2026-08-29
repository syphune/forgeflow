package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStdioServerWithOfficialMCPClient(t *testing.T) {
	backendDir := backendDirectory(t)
	binaryName := "forgeflow-mcp"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(t.TempDir(), binaryName)

	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, "./cmd/mcp")
	build.Dir = backendDir
	build.Env = mcpTestEnvironment()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build MCP server: %v\n%s", err, output)
	}

	server := exec.Command(binary)
	server.Dir = backendDir
	server.Env = mcpTestEnvironment()
	client := sdk.NewClient(&sdk.Implementation{Name: "forgeflow-conformance-client", Version: "test"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: server}, nil)
	if err != nil {
		t.Fatalf("connect over stdio: %v", err)
	}
	defer session.Close()
	if session.InitializeResult() == nil {
		t.Fatal("MCP initialize result is missing")
	}
	if !strings.Contains(session.InitializeResult().Instructions, AgentSkillResourceURI) {
		t.Fatalf("MCP instructions do not point to %s", AgentSkillResourceURI)
	}
	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources.Resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(resources.Resources))
	}
	foundSkill := false
	for _, resource := range resources.Resources {
		if resource.URI == AgentSkillResourceURI {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Fatalf("MCP skill resource was not advertised: %#v", resources.Resources)
	}
	skill, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: AgentSkillResourceURI})
	if err != nil || len(skill.Contents) != 1 || skill.Contents[0].Text != agentSkillText {
		t.Fatalf("MCP skill resource = %#v, err = %v", skill, err)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != len(Definitions()) {
		t.Fatalf("tool count = %d, want %d", len(tools.Tools), len(Definitions()))
	}
	for _, definition := range Definitions() {
		if !hasTool(tools.Tools, definition.Name) {
			t.Fatalf("tool %q was not advertised", definition.Name)
		}
	}
	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}
	wantTemplates := map[string]string{
		"task-context":       "task://{project_key}/{number}",
		"sprint-context":     "sprint://{sprint_id}?project_id={project_id}",
		"repository-context": "repo://{repository_id}/{topic}?project_id={project_id}",
		"repository-module":  "repo://{repository_id}/module/{path}?project_id={project_id}",
	}
	if len(templates.ResourceTemplates) != len(wantTemplates) {
		t.Fatalf("resource templates = %#v, err = %v", templates, err)
	}
	for _, template := range templates.ResourceTemplates {
		if want, ok := wantTemplates[template.Name]; !ok || template.URITemplate != want {
			t.Fatalf("unexpected resource template: %#v", template)
		}
	}

	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "project.list"})
	if err != nil {
		t.Fatalf("call project.list: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("project.list returned an MCP error: %#v", result)
	}
	result, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "sprint.list", Arguments: map[string]any{"project_id": "conformance-project"}})
	if err != nil {
		t.Fatalf("call sprint.list: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("sprint.list returned an MCP error: %#v", result)
	}
	result, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "work_item.list", Arguments: map[string]any{"project_id": "conformance-project", "limit": 10}})
	if err != nil {
		t.Fatalf("call work_item.list: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("work_item.list returned an MCP error: %#v", result)
	}

	for _, definition := range Definitions() {
		result, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: definition.Name, Arguments: conformanceArguments(definition.Name)})
		if callErr != nil {
			t.Fatalf("call %s: %v", definition.Name, callErr)
		}
		if len(result.Content) == 0 {
			t.Fatalf("call %s returned no content", definition.Name)
		}
	}
}

func backendDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func mcpTestEnvironment() []string {
	blocked := map[string]bool{
		"DATABASE_URL":                  true,
		"FORGEFLOW_MCP_TOKEN":           true,
		"FORGEFLOW_ENV":                 true,
		"FORGEFLOW_DEV_AUTH":            true,
		"FORGEFLOW_MCP_ACTOR_ID":        true,
		"FORGEFLOW_MCP_ORGANIZATION_ID": true,
	}
	environment := make([]string, 0, len(os.Environ())+7)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && blocked[name] {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"DATABASE_URL=",
		"FORGEFLOW_ENV=development",
		"FORGEFLOW_DEV_AUTH=true",
		"FORGEFLOW_MCP_ACTOR_ID=conformance-agent",
		"FORGEFLOW_MCP_ORGANIZATION_ID=conformance-org",
	)
}

func hasTool(tools []*sdk.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func conformanceArguments(name string) map[string]any {
	project := "conformance-project"
	item := "missing-item"
	base := map[string]any{"project_id": project}
	if name == "sprint.get_context" {
		return map[string]any{"project_id": project, "id": "missing-sprint"}
	}
	switch name {
	case "project.list":
		return nil
	case "sprint.list", "work_item.list", "work_item.search":
		base["limit"] = 1
		return base
	case "work_item.create":
		return map[string]any{"project_id": project, "type": "BUG", "title": "MCP conformance"}
	case "work_item.get", "work_item.get_context", "work_item.update", "work_item.assign", "work_item.transition", "work_item.comment":
		base["id"] = item
		base["assignee_id"] = "missing-user"
		base["transition_key"] = "start_refining"
		base["expected_version"] = 1
		base["body"] = "conformance"
		return base
	case "specification.get", "specification.propose", "specification.propose_analysis", "specification.accept_proposal", "specification.reject_proposal", "specification.verify_field", "specification.request_clarification":
		base["work_item_id"] = item
		base["field"] = "ACTUAL_BEHAVIOR"
		base["value"] = "conformance"
		base["provenance"] = "AI_HYPOTHESIS"
		base["proposal_id"] = "missing-proposal"
		base["root_cause_hypothesis"] = "conformance"
		base["implementation_plan"] = "conformance"
		base["test_plan"] = "conformance"
		base["confidence"] = 0.5
		base["question"] = "Please clarify"
		return base
	case "agent_run.get", "agent_run.create", "agent_run.cancel", "agent_run.attach_result", "agent_run.record_test_results":
		base["id"] = "missing-agent-run"
		base["work_item_id"] = item
		base["repository_id"] = "missing-repository"
		base["agent_provider"] = "codex"
		base["prompt"] = "conformance"
		base["result"] = map[string]any{}
		base["test_cases"] = []any{}
		return base
	case "autonomous.start", "autonomous.get", "autonomous.resume", "autonomous.retry", "autonomous.cancel", "autonomous.add_feedback", "autonomous.record_test_results":
		base["id"] = "missing-autonomous-run"
		base["objective"] = "conformance"
		base["work_item_id"] = item
		base["work_item_type"] = "BUG"
		base["title"] = "MCP conformance"
		base["repository_id"] = "missing-repository"
		base["source"] = "human"
		base["note"] = "conformance"
		base["feedback"] = "conformance"
		base["test_cases"] = []any{}
		return base
	case "repository.get", "repository.get_structure", "repository.search_code", "repository.get_file", "repository.get_symbol", "repository.related_files", "repository.related_commits", "repository.related_pull_requests":
		base["repository_id"] = "missing-repository"
		base["query"] = "conformance"
		base["path"] = "README.md"
		base["symbol"] = "Conformance"
		base["limit"] = 1
		return base
	default:
		return base
	}
}
