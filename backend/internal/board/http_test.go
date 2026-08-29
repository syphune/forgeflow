package board

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type boardSettingsStub struct {
	days int
}

func (s *boardSettingsStub) ProjectBoardVisibilityDays(context.Context, identity.Actor, string) (int, error) {
	return s.days, nil
}

func TestBoardPaginatesItems(t *testing.T) {
	now := func() time.Time { return time.Unix(1, 0).UTC() }
	items := workitem.NewService(
		workitem.NewMemoryRepository(now),
		specification.NewService(specification.NewMemoryStore(), now),
		workflow.NewService(workflow.Default()),
		audit.NewMemoryWriter(),
		outbox.NewMemoryWriter(),
		now,
	)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	for index := 0; index < 101; index++ {
		if _, err := items.Create(context.Background(), workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1"}, actor, workitem.CreateInput{Type: workitem.Task, Title: "Task " + strconv.Itoa(index)}); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest("GET", "/boards/current", nil).WithContext(identity.WithActor(context.Background(), actor))
	request.Header.Set("X-Project-ID", "project-1")
	response := httptest.NewRecorder()
	NewHandler(items).ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("board status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Columns []struct {
			Items []json.RawMessage `json:"items"`
		} `json:"columns"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, column := range payload.Columns {
		count += len(column.Items)
	}
	if count != 101 || payload.Truncated {
		t.Fatalf("board items = %d, truncated = %v", count, payload.Truncated)
	}
}

func TestBoardHidesExpiredCompletedItemsAndKeepsDoneColumn(t *testing.T) {
	clock := time.Unix(10*24*60*60, 0).UTC()
	now := func() time.Time { return clock }
	repository := workitem.NewMemoryRepository(now)
	workflowService := workflow.NewService(workflow.Default())
	items := workitem.NewService(
		repository,
		specification.NewService(specification.NewMemoryStore(), now),
		workflowService,
		audit.NewMemoryWriter(),
		outbox.NewMemoryWriter(),
		now,
	)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	_, err := items.Create(context.Background(), scope, actor, workitem.CreateInput{Type: workitem.Task, Title: "Still active"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := items.Create(context.Background(), scope, actor, workitem.CreateInput{Type: workitem.Task, Title: "Expired done"})
	if err != nil {
		t.Fatal(err)
	}
	completedAt := clock.Add(-8 * 24 * time.Hour)
	if _, err := repository.Update(context.Background(), scope, done.ID, done.Version, func(item *workitem.WorkItem) error {
		item.Status = workflow.Done
		item.CompletedAt = &completedAt
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	settings := &boardSettingsStub{days: 7}
	handler := NewHandlerWithProjectSettings(items, workflowService, settings, now)
	boardRequest := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/boards/current", nil).WithContext(identity.WithActor(context.Background(), actor))
		request.Header.Set("X-Project-ID", scope.ProjectID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := boardRequest()
	if response.Code != 200 {
		t.Fatalf("filtered board status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Columns []struct {
			Status string              `json:"status"`
			Items  []workitem.WorkItem `json:"items"`
		} `json:"columns"`
		DoneVisibilityDays int `json:"done_visibility_days"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DoneVisibilityDays != 7 {
		t.Fatalf("done_visibility_days = %d, want 7", payload.DoneVisibilityDays)
	}
	doneItems := 0
	rawItems := 0
	for _, column := range payload.Columns {
		switch column.Status {
		case workflow.Done:
			doneItems = len(column.Items)
		case workflow.Raw:
			rawItems = len(column.Items)
		}
	}
	if doneItems != 0 || rawItems != 1 || payload.Columns == nil {
		t.Fatalf("filtered board columns = %#v", payload.Columns)
	}

	settings.days = 0
	response = boardRequest()
	if response.Code != 200 {
		t.Fatalf("unfiltered board status = %d, body = %s", response.Code, response.Body.String())
	}
	payload = struct {
		Columns []struct {
			Status string              `json:"status"`
			Items  []workitem.WorkItem `json:"items"`
		} `json:"columns"`
		DoneVisibilityDays int `json:"done_visibility_days"`
	}{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, column := range payload.Columns {
		if column.Status == workflow.Done && len(column.Items) != 1 {
			t.Fatalf("unfiltered Done column = %#v", column.Items)
		}
	}
}
