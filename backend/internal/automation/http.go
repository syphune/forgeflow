package automation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

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
	mux.HandleFunc("GET /projects/{id}/automation-rules", h.list)
	mux.HandleFunc("POST /projects/{id}/automation-rules", h.create)
	mux.HandleFunc("PATCH /projects/{id}/automation-rules/{rule_id}", h.toggle)
	mux.HandleFunc("DELETE /projects/{id}/automation-rules/{rule_id}", h.delete)
	return mux
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.List(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type createRequest struct {
	Name       string         `json:"name"`
	EventType  string         `json:"event_type"`
	ActionType string         `json:"action_type"`
	Config     map[string]any `json:"config"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input createRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Create(r.Context(), actor, CreateInput{ProjectID: r.PathValue("id"), Name: input.Name, EventType: input.EventType, ActionType: input.ActionType, Config: input.Config})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) toggle(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.SetEnabled(r.Context(), actor, r.PathValue("id"), r.PathValue("rule_id"), input.Enabled)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err == nil {
		err = h.service.Delete(r.Context(), actor, r.PathValue("id"), r.PathValue("rule_id"))
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func actorFrom(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || actor.ID == "" || actor.OrganizationID == "" {
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
			return apperr.New(apperr.CodeInvalidArgument, 413, "request body exceeds limit", nil)
		}
		return apperr.New(apperr.CodeInvalidArgument, 422, "invalid JSON request body", nil)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body must contain one JSON value", nil)
	}
	return nil
}
