package environment

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
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
	mux.HandleFunc("GET /projects/{id}/ai-policy", h.getPolicy)
	mux.HandleFunc("PUT /projects/{id}/ai-policy", h.setPolicy)
	mux.HandleFunc("GET /projects/{id}/environments", h.list)
	mux.HandleFunc("POST /projects/{id}/environments", h.create)
	return mux
}

func NewDeploymentHandler(service *Service, maxBody int64) http.Handler {
	h := &Handler{service: service, maxBody: maxBody}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /deployments", h.createDeployment)
	mux.HandleFunc("GET /deployments/{id}", h.getDeployment)
	mux.HandleFunc("POST /deployments/{id}/approve", h.approveDeployment)
	mux.HandleFunc("POST /deployments/{id}/status", h.updateDeployment)
	return mux
}

func (h *Handler) getPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	policy, err := h.service.GetPolicy(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, policy)
}

func (h *Handler) setPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var policy autonomous.Policy
	if err := decode(w, r, h.maxBody, &policy); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.service.SetPolicy(r.Context(), actor, r.PathValue("id"), policy)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.service.List(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input CreateInput
	if err := decode(w, r, h.maxBody, &input); err != nil {
		writeError(w, r, err)
		return
	}
	input.ProjectID = r.PathValue("id")
	item, err := h.service.Create(r.Context(), actor, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) createDeployment(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input DeploymentInput
	if err := decode(w, r, h.maxBody, &input); err != nil {
		writeError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	input.ProjectID = projectID
	item, err := h.service.CreateDeployment(r.Context(), actor, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) getDeployment(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.service.GetDeployment(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) approveDeployment(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.service.ApproveDeployment(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) updateDeployment(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input StatusInput
	if err := decode(w, r, h.maxBody, &input); err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.service.UpdateDeploymentStatus(r.Context(), actor, projectID, r.PathValue("id"), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func actorFrom(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authenticated actor is required", nil)
	}
	return actor, nil
}

func scopedProjectID(r *http.Request) (string, error) {
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		return "", apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "X-Project-ID is required", nil)
	}
	return projectID, nil
}

func decode(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return apperr.New(apperr.CodeInvalidArgument, 422, "request body is required", nil)
		}
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body is invalid", nil)
	}
	return nil
}

func writeError(w http.ResponseWriter, r *http.Request, err error) { httpapi.Error(w, r, err) }
