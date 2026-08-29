package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/app"
)

type contractOperation struct {
	ID     string
	Method string
	Path   string
}

func TestAPIContractSurface(t *testing.T) {
	operations := readContractOperations(t)
	if len(operations) < 150 {
		t.Fatalf("OpenAPI operation count = %d, want the complete public contract", len(operations))
	}

	application := app.New(1 << 20)
	deferredOAuth := map[string]bool{
		"startGitHubOAuth":    true,
		"completeGitHubOAuth": true,
	}
	covered := 0
	for _, operation := range operations {
		operation := operation
		t.Run(operation.ID, func(t *testing.T) {
			if deferredOAuth[operation.ID] {
				t.Skip("OAuth handler is configured only by the database-backed composition root")
			}
			covered++
			path := contractPath(operation)
			var body []byte
			if operation.Method != http.MethodGet && operation.Method != http.MethodDelete {
				body = []byte(`{}`)
			}
			request := httptest.NewRequest(operation.Method, "/api/v1"+path, bytes.NewReader(body))
			request.Header.Set("X-Organization-ID", "org-1")
			request.Header.Set("X-Actor-ID", "user-1")
			request.Header.Set("X-Project-ID", "project-1")
			if len(body) > 0 {
				request.Header.Set("Content-Type", "application/json")
				if operation.ID == "mcpStreamableHTTP" {
					request.Header.Set("Accept", "application/json, text/event-stream")
				}
			}
			response := httptest.NewRecorder()
			application.Handler.ServeHTTP(response, request)

			if response.Code == http.StatusInternalServerError {
				t.Fatalf("%s %s returned HTTP 500: %s", operation.Method, path, response.Body.String())
			}
			if response.Code >= http.StatusInternalServerError && !hasErrorEnvelope(response.Body.Bytes()) {
				t.Fatalf("%s %s returned an unsafe server error %d: %s", operation.Method, path, response.Code, response.Body.String())
			}
			if response.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s was not registered", operation.Method, path)
			}
			if response.Code == http.StatusNotFound && !hasErrorEnvelope(response.Body.Bytes()) {
				t.Fatalf("%s %s returned an unhandled 404: %s", operation.Method, path, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("request ID was not propagated")
			}
		})
	}

	if float64(covered)/float64(len(operations)) < 0.95 {
		t.Fatalf("API contract coverage = %d/%d, want at least 95%%", covered, len(operations))
	}
	t.Logf("API contract coverage: %d/%d (%.1f%%); OAuth routes are covered by auth package conformance tests", covered, len(operations), 100*float64(covered)/float64(len(operations)))
}

func TestAPIHealthSurface(t *testing.T) {
	application := app.New(1 << 20)
	for _, path := range []string{"/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			application.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("health status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func readContractOperations(t *testing.T) []contractOperation {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, "contracts", "openapi", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}

	var path, method string
	operations := make([]contractOperation, 0, 160)
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "/") && strings.HasSuffix(trimmed, ":") {
			path = strings.TrimSuffix(trimmed, ":")
			method = ""
			continue
		}
		for _, candidate := range []string{"get", "post", "put", "patch", "delete"} {
			if trimmed == candidate+":" && strings.HasPrefix(line, "    ") {
				method = strings.ToUpper(candidate)
				break
			}
		}
		if method == "" || !strings.HasPrefix(trimmed, "operationId:") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(trimmed, "operationId:"))
		if path == "" || id == "" {
			t.Fatalf("invalid OpenAPI operation near %q", line)
		}
		operations = append(operations, contractOperation{ID: id, Method: method, Path: path})
		method = ""
	}
	return operations
}

func contractPath(operation contractOperation) string {
	path := operation.Path
	for _, replacement := range []struct{ name, value string }{
		{"{id}", "missing-id"},
		{"{user_id}", "missing-user"},
		{"{repository_id}", "missing-repository"},
		{"{field_id}", "missing-field"},
		{"{rule_id}", "missing-rule"},
		{"{comment_id}", "missing-comment"},
		{"{attachment_id}", "missing-attachment"},
		{"{proposal_id}", "missing-proposal"},
		{"{snapshot_id}", "missing-snapshot"},
		{"{document_id}", "missing-document"},
	} {
		path = strings.ReplaceAll(path, replacement.name, replacement.value)
	}
	switch operation.ID {
	case "completeGitHubAppInstallation":
		return path + "?state=missing-state&installation_id=1&setup_action=install"
	case "completeGitHubUserAuthorization":
		return path + "?error=access_denied"
	case "listGitHubRepositories":
		return path + "?project_id=project-1"
	case "repositoryFile", "getRepositoryFile":
		return path + "?path=README.md"
	case "searchRepository", "searchRepositorySnapshot":
		return path + "?q=pronoun&limit=10"
	case "listRelatedRepositoryFiles":
		return path + "?path=README.md&limit=10"
	case "listRepositorySnapshots", "listKnowledge", "listKnowledgeRevisions", "listNotifications", "listAuditRecords":
		return path + "?limit=10"
	case "getSprintContext":
		return path + "?project_id=project-1"
	default:
		return path
	}
}

func hasErrorEnvelope(body []byte) bool {
	var envelope struct {
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.Code != "" && envelope.RequestID != ""
}
