package impact

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
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/impact-scope", h.listScopes)
	mux.HandleFunc("PUT /projects/{id}/repositories/{repository_id}/impact-scope", h.saveScope)
	mux.HandleFunc("POST /projects/{id}/repositories/{repository_id}/impact-scope/infer", h.inferScope)
	mux.HandleFunc("GET /agent-runs/{id}/impact-report", h.report)
	mux.HandleFunc("POST /projects/{id}/agent-runs/{run_id}/impact-approval", h.approve)
	return mux
}

type scopeRequest struct {
	ID            string   `json:"id,omitempty"`
	PathPrefixes  []string `json:"path_prefixes"`
	Modules       []string `json:"modules"`
	ImpactSummary string   `json:"impact_summary"`
}

func (h *Handler) listScopes(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.ListScopes(r.Context(), actor.ForProject(r.PathValue("id")), r.PathValue("id"), r.PathValue("repository_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) saveScope(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input scopeRequest
	if err := decode(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.SaveScope(r.Context(), actor.ForProject(r.PathValue("id")), ScopeInput{ID: input.ID, ProjectID: r.PathValue("id"), RepositoryID: r.PathValue("repository_id"), PathPrefixes: input.PathPrefixes, Modules: input.Modules, ImpactSummary: input.ImpactSummary})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) inferScope(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input scopeRequest
	if err := decode(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.InferScope(r.Context(), actor.ForProject(r.PathValue("id")), ScopeInput{ProjectID: r.PathValue("id"), RepositoryID: r.PathValue("repository_id"), PathPrefixes: input.PathPrefixes, Modules: input.Modules, ImpactSummary: input.ImpactSummary})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		httpapi.Error(w, r, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "X-Project-ID is required", nil))
		return
	}
	item, err := h.service.GetReport(r.Context(), actor.ForProject(projectID), projectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type approvalRequest struct {
	ReportVersion int64  `json:"report_version"`
	Decision      string `json:"decision"`
	Note          string `json:"note"`
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input approvalRequest
	if err := decode(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	projectID := r.PathValue("id")
	item, err := h.service.Approve(r.Context(), actor.ForProject(projectID), projectID, r.PathValue("run_id"), ApprovalInput{ReportVersion: input.ReportVersion, Decision: input.Decision, Note: input.Note})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func currentActor(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authenticated actor is required", nil)
	}
	return actor, nil
}

func decode(w http.ResponseWriter, r *http.Request, maxBody int64, target any) error {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
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
