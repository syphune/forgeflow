package planning

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

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
	mux.HandleFunc("GET /sprints", h.list)
	mux.HandleFunc("POST /sprints", h.create)
	mux.HandleFunc("PATCH /sprints/{id}", h.update)
	mux.HandleFunc("DELETE /sprints/{id}", h.delete)
	mux.HandleFunc("POST /sprints/{id}/start", h.start)
	mux.HandleFunc("POST /sprints/{id}/complete", h.complete)
	return mux
}

func actorFrom(r *http.Request) (identity.Actor, error) {
	a, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	return a, nil
}
func projectID(r *http.Request) string { return strings.TrimSpace(r.Header.Get("X-Project-ID")) }

func scopedProjectID(r *http.Request) (string, error) {
	if value := projectID(r); value != "" {
		return value, nil
	}
	return "", apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "X-Project-ID is required", nil)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.List(r.Context(), a, pid)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

type request struct {
	Name      string `json:"name"`
	Goal      string `json:"goal"`
	ProjectID string `json:"project_id"`
	StartsAt  string `json:"starts_at"`
	EndsAt    string `json:"ends_at"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in request
	if err := decode(w, r, h.maxBody, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if strings.TrimSpace(in.ProjectID) != "" && strings.TrimSpace(in.ProjectID) != pid {
		httpapi.Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "project_id does not match the scoped project", nil))
		return
	}
	startsAt, err := parseDate(in.StartsAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	endsAt, err := parseDate(in.EndsAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Create(r.Context(), a, pid, in.Name, in.Goal, startsAt, endsAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 201, item)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in request
	if err := decode(w, r, h.maxBody, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	startsAt, err := parseDate(in.StartsAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	endsAt, err := parseDate(in.EndsAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Update(r.Context(), a, pid, r.PathValue("id"), in.Name, in.Goal, startsAt, endsAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), a, pid, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseDate(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02", value, time.UTC)
	}
	if err != nil {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "sprint dates must be RFC3339 or YYYY-MM-DD", nil)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
func (h *Handler) start(w http.ResponseWriter, r *http.Request)    { h.transition(w, r, Active) }
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) { h.transition(w, r, Completed) }
func (h *Handler) transition(w http.ResponseWriter, r *http.Request, status Status) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var item Sprint
	if status == Active {
		item, err = h.service.Start(r.Context(), a, pid, r.PathValue("id"))
	} else {
		item, err = h.service.Complete(r.Context(), a, pid, r.PathValue("id"))
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func decode(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apperr.New(apperr.CodeInvalidArgument, http.StatusRequestEntityTooLarge, "request body exceeds limit", nil)
		}
		if errors.Is(err, io.EOF) {
			return apperr.New(apperr.CodeInvalidArgument, 422, "request body is required", nil)
		}
		return apperr.New(apperr.CodeInvalidArgument, 422, "invalid JSON request body", nil)
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body must contain one JSON value", nil)
	}
	return nil
}
