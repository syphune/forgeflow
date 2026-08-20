package tenant

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type Handler struct {
	service *Service
	maxBody int64
}

func NewHandler(service *Service, maxBody int64) http.Handler {
	h := &Handler{service: service, maxBody: maxBody}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /organizations", h.listOrganizations)
	mux.HandleFunc("GET /organizations/current", h.organization)
	mux.HandleFunc("GET /organizations/{id}/authorization", h.organizationAuthorization)
	mux.HandleFunc("GET /organizations/current/members", h.listOrganizationMembers)
	mux.HandleFunc("POST /organizations/current/members", h.addOrganizationMember)
	mux.HandleFunc("PUT /organizations/current/members/{user_id}", h.setOrganizationMember)
	mux.HandleFunc("DELETE /organizations/current/members/{user_id}", h.removeOrganizationMember)
	mux.HandleFunc("GET /workspaces", h.listWorkspaces)
	mux.HandleFunc("POST /workspaces", h.createWorkspace)
	mux.HandleFunc("PATCH /workspaces/{id}", h.updateWorkspace)
	mux.HandleFunc("GET /workspaces/{id}/authorization", h.workspaceAuthorization)
	mux.HandleFunc("GET /projects", h.listProjects)
	mux.HandleFunc("POST /projects", h.createProject)
	mux.HandleFunc("PATCH /projects/{id}", h.updateProject)
	mux.HandleFunc("GET /projects/{id}/authorization", h.projectAuthorization)
	mux.HandleFunc("GET /projects/{id}/members", h.listMembers)
	mux.HandleFunc("PUT /projects/{id}/members/{user_id}", h.setMember)
	mux.HandleFunc("DELETE /projects/{id}/members/{user_id}", h.removeMember)
	return mux
}

func (h *Handler) organizationAuthorization(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err == nil {
		var context AuthorizationContext
		context, err = h.service.OrganizationAuthorization(r.Context(), actor, r.PathValue("id"))
		if err == nil {
			httpapi.JSON(w, http.StatusOK, context)
			return
		}
	}
	httpapi.Error(w, r, err)
}

func (h *Handler) workspaceAuthorization(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err == nil {
		var context AuthorizationContext
		context, err = h.service.WorkspaceAuthorization(r.Context(), actor, r.PathValue("id"))
		if err == nil {
			httpapi.JSON(w, http.StatusOK, context)
			return
		}
	}
	httpapi.Error(w, r, err)
}

func (h *Handler) projectAuthorization(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err == nil {
		var context AuthorizationContext
		context, err = h.service.ProjectAuthorization(r.Context(), actor, r.PathValue("id"))
		if err == nil {
			httpapi.JSON(w, http.StatusOK, context)
			return
		}
	}
	httpapi.Error(w, r, err)
}

func (h *Handler) listOrganizations(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Organizations(r.Context(), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) listOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.OrganizationMembers(r.Context(), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type organizationMemberRequest struct {
	Login   string `json:"login"`
	RoleKey string `json:"role_key"`
}

func (h *Handler) addOrganizationMember(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input organizationMemberRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.AddOrganizationMember(r.Context(), actor, input.Login, input.RoleKey)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) setOrganizationMember(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input memberRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.SetOrganizationMember(r.Context(), actor, r.PathValue("user_id"), input.RoleKey)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) removeOrganizationMember(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.RemoveOrganizationMember(r.Context(), actor, r.PathValue("user_id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) organization(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Organization(r.Context(), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Workspaces(r.Context(), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type workspaceRequest struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}

type renameRequest struct {
	DisplayName string `json:"display_name"`
}

func (h *Handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input workspaceRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.CreateWorkspace(r.Context(), actor, input.Key, input.DisplayName)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input renameRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.UpdateWorkspace(r.Context(), actor, r.PathValue("id"), input.DisplayName)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Projects(r.Context(), actor, r.URL.Query().Get("workspace_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type projectRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input projectRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.CreateProject(r.Context(), actor, input.WorkspaceID, input.Key, input.DisplayName)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input renameRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.UpdateProject(r.Context(), actor, r.PathValue("id"), input.DisplayName)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Members(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type memberRequest struct {
	UserID  string `json:"user_id"`
	RoleKey string `json:"role_key"`
}

func (h *Handler) setMember(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input memberRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if input.UserID == "" {
		input.UserID = r.PathValue("user_id")
	}
	item, err := h.service.SetMember(r.Context(), actor, r.PathValue("id"), input.UserID, input.RoleKey)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.RemoveMember(r.Context(), actor, r.PathValue("id"), r.PathValue("user_id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func currentActor(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	return actor, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apperr.New(apperr.CodeInvalidArgument, http.StatusRequestEntityTooLarge, "request body exceeds limit", nil)
		}
		if errors.Is(err, io.EOF) {
			return apperr.New(apperr.CodeInvalidArgument, 422, "request body is required", nil)
		}
		return apperr.New(apperr.CodeInvalidArgument, 422, "invalid JSON request body", nil)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body must contain one JSON value", nil)
	}
	return nil
}
