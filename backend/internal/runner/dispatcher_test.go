package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPDispatcherRoundTripAndError(t *testing.T) {
	root := t.TempDir()
	job := Job{ID: "job-1", AutonomousRunID: "autonomous-1", AgentRunID: "agent-1", Provider: "codex", Prompt: "run tests", RepositoryURL: "https://github.com/acme/app.git", Branch: "forgeflow/job-1", PushBranch: true, CommitMessage: "fix: run tests", WorkspaceRoot: root, Workspace: filepath.Join(root, "job-1"), TimeoutSeconds: 30, GitToken: "git-token", MCPURL: "https://forgeflow.example/api/v1/mcp?project_id=project-1", MCPToken: "mcp-token"}
	var requestJob Job
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer runner-token" || r.Header.Get("X-Forgeflow-Git-Token") != "git-token" || r.Header.Get("X-Forgeflow-MCP-Token") != "mcp-token" {
			t.Fatalf("authorization headers = %q %q %q", r.Header.Get("Authorization"), r.Header.Get("X-Forgeflow-Git-Token"), r.Header.Get("X-Forgeflow-MCP-Token"))
		}
		if err := json.NewDecoder(r.Body).Decode(&requestJob); err != nil {
			t.Fatalf("decode job: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exit_code":0,"output":"ok"}`))
	}))
	defer server.Close()

	result, err := (HTTPDispatcher{BaseURL: server.URL, Token: "runner-token"}).Dispatch(context.Background(), job)
	if err != nil || result.Output != "ok" || requestJob.ID != job.ID {
		t.Fatalf("dispatch = %#v, err = %v, request = %#v", result, err, requestJob)
	}

	largeOutput := strings.Repeat("\n", maxOutputBytes/2)
	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Result{ExitCode: 0, Output: largeOutput})
	}))
	defer largeServer.Close()
	largeResult, err := (HTTPDispatcher{BaseURL: largeServer.URL, Token: "runner-token"}).Dispatch(context.Background(), job)
	if err != nil || largeResult.Output != largeOutput {
		t.Fatalf("large dispatch = %#v, err = %v", largeResult, err)
	}

	failedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"exit_code":1,"error":"tests failed"}`))
	}))
	defer failedServer.Close()
	failed, err := (HTTPDispatcher{BaseURL: failedServer.URL, Token: "runner-token"}).Dispatch(context.Background(), job)
	if err == nil || failed.Error != "tests failed" || !strings.Contains(err.Error(), "tests failed") {
		t.Fatalf("failed dispatch = %#v, err = %v", failed, err)
	}
}
