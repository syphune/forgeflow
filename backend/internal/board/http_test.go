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
