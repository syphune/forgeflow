package agentrun_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/app"
)

func TestHTTPCreateUsesScopedProject(t *testing.T) {
	handler := app.New(1 << 20).Handler
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs", strings.NewReader(`{"project_id":"project-2","work_item_id":"item-1","repository_id":"repo-1","agent_provider":"codex","agent_name":"codex"}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("mismatched project status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs", nil)
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing project scope status = %d, body = %s", response.Code, response.Body.String())
	}
}
