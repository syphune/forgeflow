package workitem_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/app"
)

func TestHTTPRejectsStatusPatch(t *testing.T) {
	handler := app.New(1 << 20).Handler
	request := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-1","project_key":"FF","type":"TASK","title":"Ship it"}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/work-items/item-1", strings.NewReader(`{"status":"READY","expected_version":1}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status patch = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPBoundsSearchQuery(t *testing.T) {
	handler := app.New(1 << 20).Handler
	request := httptest.NewRequest(http.MethodGet, "/api/v1/work-items?q="+strings.Repeat("x", 257), nil)
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("search status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPRejectsNullStatusAndClearsNullableFields(t *testing.T) {
	handler := app.New(1 << 20).Handler
	create := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-1","project_key":"FF","type":"TASK","title":"Clear fields","due_at":"2030-01-01T00:00:00Z"}`))
	create.Header.Set("X-Organization-ID", "org-1")
	create.Header.Set("X-Actor-ID", "user-1")
	create.Header.Set("X-Project-ID", "project-1")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var item struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/work-items/"+item.ID, strings.NewReader(`{"status":null,"expected_version":1}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("null status patch = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/work-items/"+item.ID, strings.NewReader(`{"due_at":null,"sprint_id":null,"expected_version":1}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "2030") {
		t.Fatalf("nullable fields patch = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPIdempotencyReplaysCreatedWorkItem(t *testing.T) {
	handler := app.New(1 << 20).Handler
	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-1","project_key":"FF","type":"TASK","title":"Only once"}`))
		request.Header.Set("X-Organization-ID", "org-1")
		request.Header.Set("X-Actor-ID", "user-1")
		request.Header.Set("X-Project-ID", "project-1")
		request.Header.Set("Idempotency-Key", "create-only-once")
		return request
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, newRequest())
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, body = %s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, newRequest())
	if second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("replayed create = %d, body = %s; first = %s", second.Code, second.Body.String(), first.Body.String())
	}
}

func TestHTTPListsScopedWorkItemActivity(t *testing.T) {
	handler := app.New(1 << 20).Handler
	create := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-1","project_key":"FF","type":"TASK","title":"Activity"}`))
	create.Header.Set("X-Organization-ID", "org-1")
	create.Header.Set("X-Actor-ID", "user-1")
	create.Header.Set("X-Project-ID", "project-1")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	comment := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/"+item.ID+"/comments", strings.NewReader(`{"body":"Activity comment"}`))
	comment.Header.Set("X-Organization-ID", "org-1")
	comment.Header.Set("X-Actor-ID", "user-1")
	comment.Header.Set("X-Project-ID", "project-1")
	commentResponse := httptest.NewRecorder()
	handler.ServeHTTP(commentResponse, comment)
	if commentResponse.Code != http.StatusCreated {
		t.Fatalf("comment status = %d, body = %s", commentResponse.Code, commentResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/"+item.ID+"/activity?limit=10", nil)
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"action":"create"`) || !strings.Contains(response.Body.String(), `"action":"comment.create"`) {
		t.Fatalf("activity status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPListsSpecificationVersionsWithinProject(t *testing.T) {
	handler := app.New(1 << 20).Handler
	create := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-1","project_key":"FF","type":"TASK","title":"Versioned definition"}`))
	create.Header.Set("X-Organization-ID", "org-1")
	create.Header.Set("X-Actor-ID", "user-1")
	create.Header.Set("X-Project-ID", "project-1")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	update := httptest.NewRequest(http.MethodPatch, "/api/v1/work-items/"+item.ID+"/specification", strings.NewReader(`{"summary":"Version one"}`))
	update.Header.Set("X-Organization-ID", "org-1")
	update.Header.Set("X-Actor-ID", "user-1")
	update.Header.Set("X-Project-ID", "project-1")
	updated := httptest.NewRecorder()
	handler.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("specification update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	versions := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/"+item.ID+"/specification/versions", nil)
	versions.Header.Set("X-Organization-ID", "org-1")
	versions.Header.Set("X-Actor-ID", "user-1")
	versions.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, versions)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"field":"SUMMARY"`) {
		t.Fatalf("versions status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPCreateRequiresMatchingProjectScope(t *testing.T) {
	handler := app.New(1 << 20).Handler
	request := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-2","type":"TASK","title":"Do not cross scope"}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("mismatched project status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/work-items", strings.NewReader(`{"project_id":"project-1","type":"TASK","title":"Scope required"}`))
	request.Header.Set("X-Organization-ID", "org-1")
	request.Header.Set("X-Actor-ID", "user-1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing project scope status = %d, body = %s", response.Code, response.Body.String())
	}
}
