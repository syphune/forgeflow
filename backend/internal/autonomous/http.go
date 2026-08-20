package autonomous

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
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
	mux.HandleFunc("GET /autonomous-runs", h.list)
	mux.HandleFunc("POST /autonomous-runs", h.create)
	mux.HandleFunc("GET /autonomous-runs/{id}", h.get)
	mux.HandleFunc("POST /autonomous-runs/{id}/resume", h.resume)
	mux.HandleFunc("POST /autonomous-runs/{id}/retry", h.retry)
	mux.HandleFunc("POST /autonomous-runs/{id}/cancel", h.cancel)
	mux.HandleFunc("POST /autonomous-runs/{id}/feedback", h.feedback)
	mux.HandleFunc("POST /autonomous-runs/{id}/test-results", h.testResults)
	return mux
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	items, err := h.service.List(r.Context(), actor, projectID, r.URL.Query().Get("work_item_id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	var input struct {
		ProjectID         string `json:"project_id"`
		WorkItemID        string `json:"work_item_id"`
		WorkItemType      string `json:"work_item_type"`
		Title             string `json:"title"`
		RepositoryID      string `json:"repository_id"`
		Objective         string `json:"objective"`
		AgentProvider     string `json:"agent_provider"`
		AgentName         string `json:"agent_name"`
		Model             string `json:"model"`
		BaseSHA           string `json:"base_sha"`
		Branch            string `json:"branch"`
		TargetEnvironment string `json:"target_environment"`
		TestCasePositions []int  `json:"test_case_positions"`
		Policy            Policy `json:"policy"`
	}
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		handleError(w, r, err)
		return
	}
	if strings.TrimSpace(input.ProjectID) != "" && strings.TrimSpace(input.ProjectID) != projectID {
		handleError(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "project_id does not match the scoped project", nil))
		return
	}
	run, err := h.service.Start(r.Context(), actor, StartInput{ProjectID: projectID, WorkItemID: input.WorkItemID, WorkItemType: input.WorkItemType, Title: input.Title, RepositoryID: input.RepositoryID, Objective: input.Objective, AgentProvider: input.AgentProvider, AgentName: input.AgentName, Model: input.Model, BaseSHA: input.BaseSHA, Branch: input.Branch, TargetEnvironment: input.TargetEnvironment, TestCasePositions: input.TestCasePositions, Policy: input.Policy})
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, run)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	run, feedback, err := h.service.Get(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"run": run, "feedback": feedback})
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	handleRun(w, r, func(actor identity.Actor, projectID, id string) (Run, error) {
		return h.service.Resume(r.Context(), actor, projectID, id)
	})
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	var input RetryInput
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
			handleError(w, r, err)
			return
		}
	}
	handleRun(w, r, func(actor identity.Actor, projectID, id string) (Run, error) {
		return h.service.Retry(r.Context(), actor, projectID, id, input)
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	handleRun(w, r, func(actor identity.Actor, projectID, id string) (Run, error) {
		return h.service.Cancel(r.Context(), actor, projectID, id)
	})
}

func (h *Handler) feedback(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	var input FeedbackInput
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		handleError(w, r, err)
		return
	}
	result, err := h.service.AddFeedback(r.Context(), actor, projectID, r.PathValue("id"), input)
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, result)
}

func (h *Handler) testResults(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	var input agentrun.TestResultsInput
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		handleError(w, r, err)
		return
	}
	run, err := h.service.RecordTestResults(r.Context(), actor, projectID, r.PathValue("id"), input)
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
}

func handleRun(w http.ResponseWriter, r *http.Request, action func(identity.Actor, string, string) (Run, error)) {
	actor, err := actorFrom(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	projectID, err := scopedProjectID(r)
	if err != nil {
		handleError(w, r, err)
		return
	}
	run, err := action(actor, projectID, r.PathValue("id"))
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, run)
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

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
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

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.Error(w, r, err)
}
