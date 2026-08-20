package workflow

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type Handler struct {
	service *Service
	maxBody int64
}

func NewHandler(service *Service, maxBodies ...int64) http.Handler {
	maxBody := int64(1 << 20)
	if len(maxBodies) > 0 && maxBodies[0] > 0 {
		maxBody = maxBodies[0]
	}
	h := &Handler{service: service, maxBody: maxBody}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workflows/current", h.current)
	mux.HandleFunc("GET /workflows/current/statuses", h.statuses)
	mux.HandleFunc("GET /workflows/current/transitions", h.transitions)
	mux.HandleFunc("PUT /workflows/current", h.save)
	return mux
}

func (h *Handler) actor(r *http.Request) error {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authenticated actor is required", nil)
	}
	if !actor.Has(identity.CapabilityProjectRead) {
		return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "permission denied", map[string]any{"capability": identity.CapabilityProjectRead})
	}
	if strings.TrimSpace(r.Header.Get("X-Project-ID")) == "" {
		return apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "X-Project-ID is required", nil)
	}
	return nil
}

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	if err := h.actor(r); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	actor, _ := identity.ActorFromContext(r.Context())
	wf, err := h.service.WorkflowFor(r.Context(), actor.OrganizationID, r.Header.Get("X-Project-ID"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, workflowResponse(wf))
}

func (h *Handler) statuses(w http.ResponseWriter, r *http.Request) {
	if err := h.actor(r); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	actor, _ := identity.ActorFromContext(r.Context())
	wf, err := h.service.WorkflowFor(r.Context(), actor.OrganizationID, r.Header.Get("X-Project-ID"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": orderedStatuses(wf)})
}

func (h *Handler) transitions(w http.ResponseWriter, r *http.Request) {
	if err := h.actor(r); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	actor, _ := identity.ActorFromContext(r.Context())
	wf, err := h.service.WorkflowFor(r.Context(), actor.OrganizationID, r.Header.Get("X-Project-ID"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": orderedTransitions(wf)})
}

type saveRequest struct {
	Name        string       `json:"name"`
	Statuses    []Status     `json:"statuses"`
	Transitions []Transition `json:"transitions"`
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actorForMutation(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input saveRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	workflow, err := h.service.SaveForProject(r.Context(), actor, r.Header.Get("X-Project-ID"), SaveInput{Name: input.Name, Statuses: input.Statuses, Transitions: input.Transitions})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, workflowResponse(workflow))
}

func (h *Handler) actorForMutation(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authenticated actor is required", nil)
	}
	if !actor.Has(identity.CapabilityProjectManage) {
		return identity.Actor{}, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "permission denied", map[string]any{"capability": identity.CapabilityProjectManage})
	}
	if strings.TrimSpace(r.Header.Get("X-Project-ID")) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "X-Project-ID is required", nil)
	}
	return actor, nil
}

func workflowResponse(wf Workflow) map[string]any {
	return map[string]any{"id": wf.ID, "name": wf.Name, "statuses": orderedStatuses(wf), "transitions": orderedTransitions(wf)}
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
			return apperr.New(apperr.CodeInvalidArgument, 413, "request body is too large", nil)
		}
		if err == io.EOF {
			return apperr.New(apperr.CodeInvalidArgument, 422, "request body is required", nil)
		}
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body is invalid", nil)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body must contain one JSON value", nil)
	}
	return nil
}

func orderedStatuses(wf Workflow) []Status {
	items := make([]Status, 0, len(wf.Statuses))
	for _, item := range wf.Statuses {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	return items
}

func orderedTransitions(wf Workflow) []Transition {
	items := make([]Transition, 0, len(wf.Transitions))
	for _, item := range wf.Transitions {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}
