package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/runner"
)

func TestRunnerHTTPContract(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "job-1")
	var received runner.Job
	handler := runnerHTTPHandler("runner-secret", func(_ context.Context, job runner.Job) (runner.Result, error) {
		received = job
		return runner.Result{ExitCode: 0, Output: "completed"}, nil
	})

	t.Run("health", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
		if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok"}` {
			t.Fatalf("health = %d %q", response.Code, response.Body.String())
		}
	})

	job := runner.Job{ID: "job-1", AutonomousRunID: "autonomous-1", AgentRunID: "agent-1", Provider: "codex", Prompt: "run the regression suite", Workspace: workspace, WorkspaceRoot: root, TimeoutSeconds: 60}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}

	for _, authorization := range []string{"", "Bearer wrong", "Bearer runner-secret-extra"} {
		t.Run("rejects "+authorization, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(string(body)))
			request.Header.Set("Authorization", authorization)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}

	t.Run("rejects malformed JSON", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("{"))
		request.Header.Set("Authorization", "Bearer runner-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("executes an authorized job", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(string(body)))
		request.Header.Set("Authorization", "Bearer runner-secret")
		request.Header.Set("X-Forgeflow-Git-Token", "short-lived-git-token")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"output":"completed"`) {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		if received.ID != job.ID || received.GitToken != "short-lived-git-token" {
			t.Fatalf("received job = %#v", received)
		}
	})
}
