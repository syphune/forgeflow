package agentrun

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
	mux.HandleFunc("GET /agent-runs", h.list)
	mux.HandleFunc("GET /agent-runs/{id}", h.get)
	mux.HandleFunc("POST /agent-runs", h.create)
	mux.HandleFunc("POST /agent-runs/{id}/approve", h.approve)
	mux.HandleFunc("POST /agent-runs/{id}/start", h.start)
	mux.HandleFunc("POST /agent-runs/{id}/resume", h.resume)
	mux.HandleFunc("POST /agent-runs/{id}/heartbeat", h.heartbeat)
	mux.HandleFunc("POST /agent-runs/{id}/transition", h.transition)
	mux.HandleFunc("POST /agent-runs/{id}/cancel", h.cancel)
	mux.HandleFunc("POST /agent-runs/{id}/result", h.result)
	mux.HandleFunc("POST /agent-runs/{id}/test-results", h.testResults)
	mux.HandleFunc("POST /agent-runs/{id}/steps", h.step)
	mux.HandleFunc("POST /agent-runs/{id}/artifacts", h.artifact)
	return mux
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
	items, err := h.service.List(r.Context(), a, pid, r.URL.Query().Get("work_item_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func actorFrom(r *http.Request) (identity.Actor, error) {
	a, ok := identity.ActorFromContext(r.Context())
	if !ok || a.ID == "" || a.OrganizationID == "" {
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

type createRequest struct {
	ProjectID       string          `json:"project_id"`
	WorkItemID      string          `json:"work_item_id"`
	RepositoryID    string          `json:"repository_id"`
	AgentProvider   string          `json:"agent_provider"`
	AgentName       string          `json:"agent_name"`
	Model           string          `json:"model"`
	BaseSHA         string          `json:"base_sha"`
	Branch          string          `json:"branch"`
	ExecutionInputs ExecutionInputs `json:"execution_inputs"`
	ExecutionPolicy map[string]any  `json:"execution_policy"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in createRequest
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
	in.ProjectID = pid
	item, err := h.service.Create(r.Context(), a, CreateInput{
		ProjectID:       in.ProjectID,
		WorkItemID:      in.WorkItemID,
		RepositoryID:    in.RepositoryID,
		AgentProvider:   in.AgentProvider,
		AgentName:       in.AgentName,
		Model:           in.Model,
		BaseSHA:         in.BaseSHA,
		Branch:          in.Branch,
		ExecutionInputs: in.ExecutionInputs,
		ExecutionPolicy: in.ExecutionPolicy,
	})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
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
	run, steps, artifacts, err := h.service.Get(r.Context(), a, pid, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"run": run, "steps": steps, "artifacts": artifacts})
}
func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.service.Approve(r.Context(), a, pid, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 200, run)
}
func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.service.Start(r.Context(), a, pid, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 200, run)
}

func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.service.Heartbeat(r.Context(), a, pid, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.service.Resume(r.Context(), a, pid, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
}

func (h *Handler) transition(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input struct {
		Status Status `json:"status"`
	}
	if err := decode(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	run, err := h.service.Transition(r.Context(), a, pid, r.PathValue("id"), input.Status)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
}
func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.service.Cancel(r.Context(), a, pid, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 200, run)
}
func (h *Handler) result(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input ResultInput
	if err := decode(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	run, err := h.service.AttachResult(r.Context(), a, pid, r.PathValue("id"), input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
}

func (h *Handler) testResults(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input TestResultsInput
	if err := decode(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	run, err := h.service.RecordTestResults(r.Context(), a, pid, r.PathValue("id"), input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
}

func (h *Handler) step(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in Step
	if err := decode(w, r, h.maxBody, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.AttachStep(r.Context(), a, pid, r.PathValue("id"), in)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *Handler) artifact(w http.ResponseWriter, r *http.Request) {
	a, err := actorFrom(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in Artifact
	if err := decode(w, r, h.maxBody, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	pid, err := scopedProjectID(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.AttachArtifact(r.Context(), a, pid, r.PathValue("id"), in)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func decode(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
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
