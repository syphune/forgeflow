package branchplan

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type ItemReader interface {
	BranchPlanInput(context.Context, string, identity.Actor, string) (WorkItemInput, error)
}

type Handler struct {
	service *Service
	items   ItemReader
	maxBody int64
}

func NewHandler(service *Service, items ItemReader, maxBody int64) http.Handler {
	handler := &Handler{service: service, items: items, maxBody: maxBody}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /projects/{id}/branch-policy", handler.getPolicy)
	mux.HandleFunc("PUT /projects/{id}/branch-policy", handler.savePolicy)
	mux.HandleFunc("GET /work-items/{id}/branch-plan", handler.getPlan)
	mux.HandleFunc("POST /work-items/{id}/branch-plan/preview", handler.preview)
	mux.HandleFunc("POST /work-items/{id}/branch-plan/prepare", handler.prepare)
	mux.HandleFunc("POST /work-items/{id}/branch-plan/pull-request", handler.pullRequest)
	mux.HandleFunc("POST /work-items/{id}/branch-plan/sync", handler.sync)
	mux.HandleFunc("GET /work-items/{id}/branch-plan/operations", handler.operations)
	return mux
}

func (h *Handler) getPolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	projectID := strings.TrimSpace(r.PathValue("id"))
	policy, err := h.service.GetPolicy(r.Context(), actor.ForProject(projectID), projectID)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, policy)
}

func (h *Handler) savePolicy(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input PolicyInput
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	projectID := strings.TrimSpace(r.PathValue("id"))
	policy, err := h.service.SavePolicy(r.Context(), actor.ForProject(projectID), projectID, input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, policy)
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := scopedActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	plan, err := h.service.GetPlan(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, plan)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	actor, projectID, item, input, err := h.itemInput(w, r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	plan, err := h.service.Preview(r.Context(), actor, item, input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	_ = projectID
	httpapi.JSON(w, http.StatusOK, plan)
}

func (h *Handler) prepare(w http.ResponseWriter, r *http.Request) {
	actor, _, item, input, err := h.itemInput(w, r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	plan, err := h.service.Prepare(r.Context(), actor, item, input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, plan)
}

func (h *Handler) pullRequest(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := scopedActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	operation, err := h.service.CreatePullRequest(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, operation)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := scopedActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	operations, err := h.service.Sync(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": operations})
}

func (h *Handler) operations(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := scopedActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	operations, err := h.service.ListOperations(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": operations})
}

func (h *Handler) itemInput(w http.ResponseWriter, r *http.Request) (identity.Actor, string, WorkItemInput, PreviewInput, error) {
	actor, projectID, err := scopedActor(r)
	if err != nil {
		return identity.Actor{}, "", WorkItemInput{}, PreviewInput{}, err
	}
	var input PreviewInput
	if err := decodeOptionalJSON(w, r, h.maxBody, &input); err != nil {
		return identity.Actor{}, "", WorkItemInput{}, PreviewInput{}, err
	}
	if h.items == nil {
		return identity.Actor{}, "", WorkItemInput{}, PreviewInput{}, apperr.New(apperr.CodeInternal, 500, "work item branch context is not configured", nil)
	}
	item, err := h.items.BranchPlanInput(r.Context(), projectID, actor, r.PathValue("id"))
	if err != nil {
		return identity.Actor{}, "", WorkItemInput{}, PreviewInput{}, err
	}
	return actor, projectID, item, input, nil
}

func scopedActor(r *http.Request) (identity.Actor, string, error) {
	actor, err := currentActor(r)
	if err != nil {
		return identity.Actor{}, "", err
	}
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		return identity.Actor{}, "", apperr.New(apperr.CodeInvalidArgument, 422, "X-Project-ID is required", nil)
	}
	return actor.ForProject(projectID), projectID, nil
}

func currentActor(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	return actor, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	return decodeJSONValue(w, r, maxBody, target, true)
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	return decodeJSONValue(w, r, maxBody, target, false)
}

func decodeJSONValue(w http.ResponseWriter, r *http.Request, maxBody int64, target any, required bool) error {
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
			if !required {
				return nil
			}
			return apperr.New(apperr.CodeInvalidArgument, 422, "request body is required", nil)
		}
		return apperr.New(apperr.CodeInvalidArgument, 422, "invalid JSON request body", nil)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body must contain one JSON value", nil)
	}
	return nil
}
