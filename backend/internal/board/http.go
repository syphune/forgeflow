package board

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type Handler struct {
	workItems       *workitem.Service
	workflow        *workflow.Service
	projectSettings ProjectSettings
	now             func() time.Time
}

type ProjectSettings interface {
	ProjectBoardVisibilityDays(context.Context, identity.Actor, string) (int, error)
}

func NewHandler(workItems *workitem.Service, workflowServices ...*workflow.Service) http.Handler {
	workflowService := workflow.NewService(workflow.Default())
	if len(workflowServices) > 0 && workflowServices[0] != nil {
		workflowService = workflowServices[0]
	}
	return newHandler(workItems, workflowService, nil, func() time.Time { return time.Now().UTC() })
}

func NewHandlerWithProjectSettings(workItems *workitem.Service, workflowService *workflow.Service, settings ProjectSettings, now func() time.Time) http.Handler {
	if workflowService == nil {
		workflowService = workflow.NewService(workflow.Default())
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return newHandler(workItems, workflowService, settings, now)
}

func newHandler(workItems *workitem.Service, workflowService *workflow.Service, settings ProjectSettings, now func() time.Time) http.Handler {
	h := &Handler{workItems: workItems, workflow: workflowService, projectSettings: settings, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boards/current", h.current)
	return mux
}

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || actor.ID == "" {
		httpapi.Error(w, r, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil))
		return
	}
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		httpapi.Error(w, r, apperr.New(apperr.CodeUnauthorized, 401, "X-Project-ID is required", nil))
		return
	}
	scope := workitem.Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}
	doneVisibilityDays := 0
	if h.projectSettings != nil {
		var settingsErr error
		doneVisibilityDays, settingsErr = h.projectSettings.ProjectBoardVisibilityDays(r.Context(), actor, projectID)
		if settingsErr != nil {
			httpapi.Error(w, r, settingsErr)
			return
		}
	}
	var completedBefore *time.Time
	if doneVisibilityDays > 0 {
		cutoff := h.now().UTC().Add(-time.Duration(doneVisibilityDays) * 24 * time.Hour)
		completedBefore = &cutoff
	}
	items := make([]*workitem.WorkItem, 0)
	cursor := ""
	truncated := false
	for pageNumber := 0; pageNumber < 20; pageNumber++ {
		page, err := h.workItems.ListPage(r.Context(), scope, actor, workitem.ListFilter{Limit: 100, Cursor: cursor, Sort: "backlog", ExcludeTerminalCompletedBefore: completedBefore})
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pageNumber == 19 {
			truncated = true
		}
	}
	projectWorkflow, err := h.workflow.WorkflowFor(r.Context(), actor.OrganizationID, projectID)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	orderingVersions, err := h.workItems.ColumnOrderingVersions(r.Context(), scope, actor, "")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	statuses := make([]workflow.Status, 0, len(projectWorkflow.Statuses))
	for _, status := range projectWorkflow.Statuses {
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Position == statuses[j].Position {
			return statuses[i].Key < statuses[j].Key
		}
		return statuses[i].Position < statuses[j].Position
	})
	columns := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		// ponytail: keep the projection linear in the small V1 board response; add indexed grouping when board limits grow.
		columnItems := make([]*workitem.WorkItem, 0)
		for _, item := range items {
			if item.Status == status.Key {
				columnItems = append(columnItems, item)
			}
		}
		orderingVersion := orderingVersions[status.Key]
		if orderingVersion < 1 {
			orderingVersion = 1
		}
		columns = append(columns, map[string]any{"status": status.Key, "name": status.Name, "position": status.Position, "ordering_version": orderingVersion, "items": columnItems})
	}
	httpapi.JSON(w, 200, map[string]any{"project_id": projectID, "columns": columns, "truncated": truncated, "done_visibility_days": doneVisibilityDays})
}
