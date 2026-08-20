package board

import (
	"net/http"
	"sort"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type Handler struct {
	workItems *workitem.Service
	workflow  *workflow.Service
}

func NewHandler(workItems *workitem.Service, workflowServices ...*workflow.Service) http.Handler {
	workflowService := workflow.NewService(workflow.Default())
	if len(workflowServices) > 0 && workflowServices[0] != nil {
		workflowService = workflowServices[0]
	}
	h := &Handler{workItems: workItems, workflow: workflowService}
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
	items := make([]*workitem.WorkItem, 0)
	cursor := ""
	truncated := false
	for pageNumber := 0; pageNumber < 20; pageNumber++ {
		page, err := h.workItems.ListPage(r.Context(), scope, actor, workitem.ListFilter{Limit: 100, Cursor: cursor, Sort: "backlog"})
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
	httpapi.JSON(w, 200, map[string]any{"project_id": projectID, "columns": columns, "truncated": truncated})
}
