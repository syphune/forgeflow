package audit

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type Handler struct{ reader Reader }

func NewHandler(reader Reader) http.Handler {
	h := &Handler{reader: reader}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /audit", h.list)
	return mux
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || actor.ID == "" || actor.OrganizationID == "" {
		httpapi.Error(w, r, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authenticated actor is required", nil))
		return
	}
	if !actor.Has("audit.read") {
		httpapi.Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "permission denied", map[string]any{"capability": "audit.read"}))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	items, err := h.reader.List(r.Context(), actor.OrganizationID, Filter{ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")), ResourceID: strings.TrimSpace(r.URL.Query().Get("resource_id")), Limit: limit})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}
