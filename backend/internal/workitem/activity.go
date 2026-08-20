package workitem

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
)

// NewActivityHandler exposes a work-item-scoped activity feed. The work item
// is resolved first so the normal organization/project authorization applies
// before audit records are read.
func NewActivityHandler(service *Service, reader audit.Reader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := actor(r)
		projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
		if projectID == "" {
			httpapi.Error(w, r, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "X-Project-ID is required", nil))
			return
		}
		workItemID := strings.TrimSpace(r.PathValue("id"))
		if workItemID == "" {
			workItemID = strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/work-items/"), "/activity"), "/")
		}
		item, err := service.Get(r.Context(), Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}, actor, workItemID)
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		if limit > 100 {
			limit = 100
		}
		items, err := reader.List(r.Context(), actor.OrganizationID, audit.Filter{ResourceType: "work_item", ResourceID: item.ID, Limit: limit})
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
}
