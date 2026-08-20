package customfield

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
	mux.HandleFunc("GET /projects/{id}/custom-fields", h.listDefinitions)
	mux.HandleFunc("POST /projects/{id}/custom-fields", h.createDefinition)
	mux.HandleFunc("PATCH /projects/{id}/custom-fields/{field_id}", h.updateDefinition)
	mux.HandleFunc("DELETE /projects/{id}/custom-fields/{field_id}", h.deleteDefinition)
	mux.HandleFunc("GET /work-items/{id}/custom-fields", h.listValues)
	mux.HandleFunc("PUT /work-items/{id}/custom-fields/{field_id}", h.setValue)
	return mux
}

func (h *Handler) listDefinitions(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.ListDefinitions(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createDefinition(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input struct {
		Key         string    `json:"key"`
		DisplayName string    `json:"display_name"`
		ValueType   ValueType `json:"value_type"`
		Options     []string  `json:"options"`
		Required    bool      `json:"required"`
	}
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.CreateDefinition(r.Context(), actor, CreateInput{ProjectID: r.PathValue("id"), Key: input.Key, DisplayName: input.DisplayName, ValueType: input.ValueType, Options: input.Options, Required: input.Required})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) updateDefinition(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input UpdateInput
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.UpdateDefinition(r.Context(), actor, r.PathValue("id"), r.PathValue("field_id"), input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) deleteDefinition(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err == nil {
		err = h.service.DeleteDefinition(r.Context(), actor, r.PathValue("id"), r.PathValue("field_id"))
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listValues(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.ListValues(r.Context(), actor, r.URL.Query().Get("project_id"), r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) setValue(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input struct {
		Value json.RawMessage `json:"value"`
	}
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if len(input.Value) == 0 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "value is required; use null to clear it", nil))
		return
	}
	var value *string
	if string(input.Value) != "null" {
		var text string
		if err := json.Unmarshal(input.Value, &text); err != nil {
			httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "custom field value must be a string or null", nil))
			return
		}
		value = &text
	}
	item, err := h.service.SetValue(r.Context(), actor, r.URL.Query().Get("project_id"), r.PathValue("id"), r.PathValue("field_id"), value)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func actorFrom(r *http.Request) (identity.Actor, error) {
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
			return apperr.New(apperr.CodeInvalidArgument, 413, "request body exceeds limit", nil)
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
