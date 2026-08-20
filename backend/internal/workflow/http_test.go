package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestSaveWorkflowHTTP(t *testing.T) {
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	body := `{"name":"Small flow","statuses":[{"key":"RAW","display_name":"Inbox","category":"TODO","position":10},{"key":"DONE","display_name":"Done","category":"DONE","position":20,"is_terminal":true}],"transitions":[{"key":"finish","from_status":"RAW","to_status":"DONE","display_name":"Finish"}]}`
	request := httptest.NewRequest(http.MethodPut, "/workflows/current", strings.NewReader(body)).WithContext(identity.WithActor(context.Background(), actor))
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	NewHandler(NewService(Default()), 4096).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save workflow status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Name        string       `json:"name"`
		Statuses    []Status     `json:"statuses"`
		Transitions []Transition `json:"transitions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "Small flow" || len(payload.Statuses) != 2 || len(payload.Transitions) != 1 {
		t.Fatalf("saved workflow payload = %#v", payload)
	}
}

func TestSaveWorkflowHTTPRequiresProjectManagement(t *testing.T) {
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityProjectRead: true}}
	request := httptest.NewRequest(http.MethodPut, "/workflows/current", strings.NewReader(`{"name":"Flow","statuses":[]}`)).WithContext(identity.WithActor(context.Background(), actor))
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	NewHandler(NewService(Default())).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("permission status = %d, body = %s", response.Code, response.Body.String())
	}
}
