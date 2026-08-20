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

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) < 20 {
		t.Fatalf("tool count = %d, want at least 20", len(tools.Tools))
	}
	if !hasTool(tools.Tools, "work_item.list") || !hasTool(tools.Tools, "specification.verify_field") {
		t.Fatal("core Forgeflow tools were not advertised")
	}
	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) < 2 {
		t.Fatalf("resource templates = %#v, err = %v", templates, err)
	}

	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "work_item.list", Arguments: map[string]any{"limit": 10}})
	if err != nil {
		t.Fatalf("call work_item.list: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("work_item.list returned an MCP error: %#v", result)
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
		"FORGEFLOW_MCP_PROJECT_ID":      true,
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
		"FORGEFLOW_MCP_PROJECT_ID=conformance-project",
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
