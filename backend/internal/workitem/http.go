package workitem

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	platformidempotency "github.com/forgeflow/forgeflow/backend/internal/platform/idempotency"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
)

type Handler struct {
	service     *Service
	spec        *specification.Service
	maxBody     int64
	idempotency platformidempotency.Store
}

func NewHandler(service *Service, spec *specification.Service, maxBody int64, developmentAuth ...bool) http.Handler {
	api := NewAPIHandler(service, spec, maxBody)
	root := http.NewServeMux()
	root.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { httpapi.JSON(w, 200, map[string]string{"status": "ok"}) })
	root.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.JSON(w, 200, map[string]string{"status": "ready"})
	})
	apiHandler := http.Handler(http.StripPrefix("/api/v1", api))
	if len(developmentAuth) == 0 || developmentAuth[0] {
		apiHandler = httpapi.WithDevelopmentActor(apiHandler)
	} else {
		apiHandler = httpapi.RequireConfiguredAuth(apiHandler)
	}
	root.Handle("/api/v1/", apiHandler)
	return httpapi.WithRequestID(root)
}

func NewAPIHandler(service *Service, spec *specification.Service, maxBody int64, idempotencyStores ...platformidempotency.Store) http.Handler {
	var store platformidempotency.Store
	if len(idempotencyStores) > 0 {
		store = idempotencyStores[0]
	}
	h := &Handler{service: service, spec: spec, maxBody: maxBody, idempotency: store}
	api := http.NewServeMux()
	api.HandleFunc("GET /work-items", h.list)
	api.HandleFunc("POST /work-items", h.create)
	api.HandleFunc("GET /work-items/{id}", h.get)
	api.HandleFunc("PATCH /work-items/{id}", h.update)
	api.HandleFunc("DELETE /work-items/{id}", h.archive)
	api.HandleFunc("POST /work-items/{id}/restore", h.restore)
	api.HandleFunc("POST /work-items/{id}/assignments", h.assign)
	api.HandleFunc("POST /work-items/{id}/transitions", h.transition)
	api.HandleFunc("POST /work-items/{id}/rank", h.rank)
	api.HandleFunc("POST /work-items/{id}/move", h.move)
	api.HandleFunc("GET /work-items/{id}/comments", h.listComments)
	api.HandleFunc("POST /work-items/{id}/comments", h.createComment)
	api.HandleFunc("PATCH /work-items/{id}/comments/{comment_id}", h.updateComment)
	api.HandleFunc("DELETE /work-items/{id}/comments/{comment_id}", h.deleteComment)
	api.HandleFunc("GET /work-items/{id}/links", h.listLinks)
	api.HandleFunc("POST /work-items/{id}/links", h.createLink)
	api.HandleFunc("DELETE /work-items/{id}/links/{link_id}", h.removeLink)
	api.HandleFunc("GET /work-items/{id}/labels", h.listLabels)
	api.HandleFunc("POST /work-items/{id}/labels", h.addLabel)
	api.HandleFunc("DELETE /work-items/{id}/labels/{label_id}", h.removeLabel)
	api.HandleFunc("GET /work-items/{id}/specification", h.getSpecification)
	api.HandleFunc("POST /work-items/{id}/specification/review", h.reviewSpecification)
	api.HandleFunc("GET /work-items/{id}/specification/versions", h.listSpecificationVersions)
	api.HandleFunc("PATCH /work-items/{id}/specification", h.updateSpecification)
	api.HandleFunc("POST /work-items/{id}/specification/proposals", h.propose)
	api.HandleFunc("GET /work-items/{id}/specification/proposals", h.listProposals)
	api.HandleFunc("POST /work-items/{id}/specification/proposals/{proposal_id}/accept", h.acceptProposal)
	api.HandleFunc("POST /work-items/{id}/specification/proposals/{proposal_id}/reject", h.rejectProposal)
	api.HandleFunc("POST /work-items/{id}/specification/verifications", h.verify)
	api.HandleFunc("GET /work-items/{id}/analyses", h.listAnalyses)
	api.HandleFunc("POST /work-items/{id}/analyses", h.addAnalysis)
	return api
}

type createRequest struct {
	ProjectID      string `json:"project_id"`
	WorkspaceID    string `json:"workspace_id"`
	ProjectKey     string `json:"project_key"`
	Type           Type   `json:"type"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	ParentID       string `json:"parent_id"`
	RepositoryID   string `json:"repository_id"`
	AssigneeID     string `json:"assignee_id"`
	Priority       string `json:"priority"`
	DueAt          string `json:"due_at"`
	EstimatePoints *int   `json:"estimate_points"`
	SprintID       string `json:"sprint_id"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var input createRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if strings.TrimSpace(input.ProjectID) != "" && strings.TrimSpace(input.ProjectID) != scope.ProjectID {
		httpapi.Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "project_id does not match the scoped project", nil))
		return
	}
	input.ProjectID = scope.ProjectID
	dueAt, err := parseDueAt(input.DueAt)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	claimed := false
	if key != "" && h.idempotency != nil {
		claim, claimErr := h.idempotency.Claim(r.Context(), actor.OrganizationID, actor.ID, key)
		if claimErr == platformidempotency.ErrInProgress {
			httpapi.Error(w, r, apperr.New(apperr.CodeConflict, http.StatusConflict, "idempotency key is already in progress", nil))
			return
		}
		if claimErr != nil {
			httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "idempotency key is invalid or expired", nil))
			return
		}
		if claim.Replay {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(claim.Status)
			_, _ = w.Write(claim.ResponseBody)
			return
		}
		claimed = true
	}
	item, err := h.service.Create(r.Context(), Scope{OrganizationID: scope.OrganizationID, WorkspaceID: input.WorkspaceID, ProjectID: scope.ProjectID, ProjectKey: input.ProjectKey}, actor, CreateInput{Type: input.Type, Title: input.Title, Description: input.Description, ParentID: input.ParentID, RepositoryID: input.RepositoryID, AssigneeID: input.AssigneeID, ReporterID: actor.ID, Priority: input.Priority, DueAt: dueAt, EstimatePoints: input.EstimatePoints, SprintID: input.SprintID})
	if err != nil {
		if claimed {
			_ = h.idempotency.Release(r.Context(), actor.OrganizationID, actor.ID, key)
		}
		httpapi.Error(w, r, err)
		return
	}
	if claimed {
		body, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			_ = h.idempotency.Release(r.Context(), actor.OrganizationID, actor.ID, key)
			httpapi.Error(w, r, apperr.New(apperr.CodeInternal, 500, "could not persist idempotency response", nil))
			return
		}
		if err := h.idempotency.Complete(r.Context(), actor.OrganizationID, actor.ID, key, http.StatusCreated, body); err != nil {
			// Keep the claim when completion fails. Releasing it here could let a
			// retry create a duplicate after the work item was already committed.
			httpapi.Error(w, r, apperr.New(apperr.CodeInternal, 500, "could not persist idempotency response", nil))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 256 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "search query is too long", nil))
		return
	}
	includeArchived, _ := strconv.ParseBool(r.URL.Query().Get("include_archived"))
	page, err := h.service.ListPage(r.Context(), scope, actor, ListFilter{Status: r.URL.Query().Get("status"), Type: r.URL.Query().Get("type"), Priority: r.URL.Query().Get("priority"), AssigneeID: r.URL.Query().Get("assignee_id"), SprintID: r.URL.Query().Get("sprint_id"), RepositoryID: r.URL.Query().Get("repository_id"), Query: query, Sort: r.URL.Query().Get("sort"), Limit: limit, Cursor: r.URL.Query().Get("cursor"), IncludeArchived: includeArchived})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": page.NextCursor})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type updateRequest struct {
	Title           *string         `json:"title"`
	Description     *string         `json:"description"`
	ParentID        optionalString  `json:"parent_id"`
	RepositoryID    optionalString  `json:"repository_id"`
	ExpectedVersion int64           `json:"expected_version"`
	Status          json.RawMessage `json:"status"`
	Priority        *string         `json:"priority"`
	DueAt           optionalString  `json:"due_at"`
	EstimatePoints  optionalInt     `json:"estimate_points"`
	SprintID        optionalString  `json:"sprint_id"`
}

type optionalString struct {
	Value *string
	Set   bool
}

func (v *optionalString) UnmarshalJSON(data []byte) error {
	v.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		v.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = &value
	return nil
}

type optionalInt struct {
	Value *int
	Set   bool
}

func (v *optionalInt) UnmarshalJSON(data []byte) error {
	v.Set = true
	if string(data) == "null" {
		v.Value = nil
		return nil
	}
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = &value
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var input updateRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if len(input.Status) > 0 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "status changes must use the transition endpoint", nil))
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var dueAt *time.Time
	if input.DueAt.Set && input.DueAt.Value != nil {
		dueAt, err = parseDueAt(*input.DueAt.Value)
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
	}
	item, err := h.service.Update(r.Context(), scope, actor, r.PathValue("id"), UpdateInput{Title: input.Title, Description: input.Description, ParentID: input.ParentID.Value, ParentIDSet: input.ParentID.Set, RepositoryID: input.RepositoryID.Value, RepositoryIDSet: input.RepositoryID.Set, Priority: input.Priority, DueAt: dueAt, DueAtSet: input.DueAt.Set, EstimatePoints: input.EstimatePoints.Value, EstimatePointsSet: input.EstimatePoints.Set, SprintID: input.SprintID.Value, SprintIDSet: input.SprintID.Set, ExpectedVersion: input.ExpectedVersion})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	expectedVersion, _ := strconv.ParseInt(r.URL.Query().Get("expected_version"), 10, 64)
	if err := h.service.Archive(r.Context(), scope, actor, r.PathValue("id"), expectedVersion); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Restore(r.Context(), scope, actor, r.PathValue("id"), input.ExpectedVersion)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type assignmentRequest struct {
	AssigneeID      string `json:"assignee_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	var input assignmentRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Assign(r.Context(), scope, actor, r.PathValue("id"), input.AssigneeID, input.ExpectedVersion)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type transitionRequest struct {
	TransitionKey   string `json:"transition_key"`
	ExpectedVersion int64  `json:"expected_version"`
}

type rankRequest struct {
	Direction       string `json:"direction"`
	ExpectedVersion int64  `json:"expected_version"`
}

type moveRequest struct {
	TargetStatus                       string `json:"target_status"`
	TransitionKey                      string `json:"transition_key"`
	BeforeID                           string `json:"before_id"`
	AfterID                            string `json:"after_id"`
	ExpectedVersion                    int64  `json:"expected_version"`
	ExpectedSourceOrderingVersion      int64  `json:"expected_source_ordering_version"`
	ExpectedDestinationOrderingVersion int64  `json:"expected_destination_ordering_version"`
}

func (h *Handler) move(w http.ResponseWriter, r *http.Request) {
	var input moveRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	result, err := h.service.Move(r.Context(), scope, actor, r.PathValue("id"), MoveInput{TargetStatus: input.TargetStatus, TransitionKey: input.TransitionKey, BeforeID: input.BeforeID, AfterID: input.AfterID, ExpectedVersion: input.ExpectedVersion, ExpectedSourceOrderingVersion: input.ExpectedSourceOrderingVersion, ExpectedDestinationOrderingVersion: input.ExpectedDestinationOrderingVersion})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"item": result.Item, "source_ordering_version": result.SourceOrderingVersion, "destination_ordering_version": result.DestinationOrderingVersion})
}

func (h *Handler) rank(w http.ResponseWriter, r *http.Request) {
	var input rankRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Reorder(r.Context(), scope, actor, r.PathValue("id"), input.Direction, input.ExpectedVersion)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) transition(w http.ResponseWriter, r *http.Request) {
	var input transitionRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Transition(r.Context(), scope, actor, r.PathValue("id"), TransitionInput{TransitionKey: input.TransitionKey, ExpectedVersion: input.ExpectedVersion})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type commentRequest struct {
	Body string `json:"body"`
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Comments(r.Context(), scope, actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createComment(w http.ResponseWriter, r *http.Request) {
	var input commentRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.CreateComment(r.Context(), scope, actor, r.PathValue("id"), input.Body)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) updateComment(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input commentRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.UpdateComment(r.Context(), scope, actor, r.PathValue("id"), r.PathValue("comment_id"), input.Body)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.DeleteComment(r.Context(), scope, actor, r.PathValue("id"), r.PathValue("comment_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type linkRequest struct {
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

func (h *Handler) listLinks(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Links(r.Context(), scope, actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createLink(w http.ResponseWriter, r *http.Request) {
	var input linkRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.CreateLink(r.Context(), scope, actor, r.PathValue("id"), input.TargetID, input.RelationType)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) removeLink(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.RemoveLink(r.Context(), scope, actor, r.PathValue("id"), r.PathValue("link_id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type labelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *Handler) listLabels(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Labels(r.Context(), scope, actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) addLabel(w http.ResponseWriter, r *http.Request) {
	var input labelRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.AddLabel(r.Context(), scope, actor, r.PathValue("id"), input.Name, input.Color)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) removeLabel(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.RemoveLabel(r.Context(), scope, actor, r.PathValue("id"), r.PathValue("label_id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getSpecification(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	spec, err := h.spec.Get(r.Context(), scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	readiness, err := h.service.Readiness(r.Context(), scope, actor, item)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"specification": spec, "readiness": readiness})
}

func (h *Handler) listSpecificationVersions(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.spec.FieldVersions(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type specificationUpdateRequest struct {
	ExpectedVersion   int                                 `json:"expected_version"`
	Summary           *string                             `json:"summary"`
	Fields            map[specification.FieldKey]string   `json:"fields"`
	ReproductionSteps []specification.ReproductionStep    `json:"reproduction_steps"`
	Acceptance        []specification.AcceptanceCriterion `json:"acceptance_criteria"`
	RegressionCases   []specification.RegressionTestCase  `json:"regression_test_cases"`
	ContextRefs       []specification.ContextRef          `json:"context_refs"`
	MediaRefs         map[string][]string                 `json:"media_refs"`
	RepositoryID      *string                             `json:"repository_id"`
}

func (h *Handler) updateSpecification(w http.ResponseWriter, r *http.Request) {
	var input specificationUpdateRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if !actor.Has(identity.CapabilityWorkItemEdit) {
		httpapi.Error(w, r, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilityWorkItemEdit}))
		return
	}
	spec, err := h.spec.Update(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), actor.Type, specification.UpdateInput{ExpectedVersion: input.ExpectedVersion, Summary: input.Summary, Fields: input.Fields, ReproductionSteps: input.ReproductionSteps, Acceptance: input.Acceptance, RegressionCases: input.RegressionCases, ContextRefs: input.ContextRefs, MediaRefs: input.MediaRefs, RepositoryID: input.RepositoryID})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, spec)
}

type specificationReviewRequest struct {
	ExpectedVersion int `json:"expected_version"`
}

func (h *Handler) reviewSpecification(w http.ResponseWriter, r *http.Request) {
	var input specificationReviewRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	spec, err := h.spec.Review(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), actor, specification.ReviewInput{ExpectedVersion: input.ExpectedVersion})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, spec)
}

type proposalRequest struct {
	Field      specification.FieldKey   `json:"field"`
	Value      string                   `json:"value"`
	Provenance specification.Provenance `json:"provenance"`
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	var input proposalRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if !actor.Has(identity.CapabilitySpecificationPropose) {
		httpapi.Error(w, r, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationPropose}))
		return
	}
	proposal, err := h.spec.Propose(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), input.Field, input.Value, input.Provenance)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, proposal)
}

func (h *Handler) listProposals(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.spec.Proposals(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) listAnalyses(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.spec.Analyses(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) addAnalysis(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input specification.Analysis
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.spec.AddAnalysis(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), actor, input)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) acceptProposal(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	proposal, err := h.spec.AcceptProposal(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), r.PathValue("proposal_id"), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, proposal)
}

func (h *Handler) rejectProposal(w http.ResponseWriter, r *http.Request) {
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if _, err := h.service.Get(r.Context(), scope, actor, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	proposal, err := h.spec.RejectProposal(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), r.PathValue("proposal_id"), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, proposal)
}

type verifyRequest struct {
	Kind     string                 `json:"kind"`
	Field    specification.FieldKey `json:"field"`
	Position int                    `json:"position"`
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	var input verifyRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	scope, actor, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if !actor.Has(identity.CapabilitySpecificationVerify) {
		httpapi.Error(w, r, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationVerify}))
		return
	}
	switch input.Kind {
	case "", "field":
		err = h.spec.VerifyField(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), input.Field, actor.Type, actor.ID)
	case "reproduction_step":
		err = h.spec.VerifyStep(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), input.Position, actor.Type, actor.ID)
	case "acceptance_criterion":
		err = h.spec.VerifyAcceptance(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), input.Position, actor.Type, actor.ID)
	case "regression_case":
		err = h.spec.VerifyRegression(r.Context(), scope.OrganizationID, scope.ProjectID, r.PathValue("id"), input.Position, actor.Type, actor.ID)
	default:
		err = apperr.New(apperr.CodeInvalidArgument, 422, "unsupported verification kind", map[string]any{"kind": input.Kind})
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requestScope(r *http.Request) (Scope, identity.Actor, error) {
	actor := actor(r)
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		return Scope{}, actor, apperr.New(apperr.CodeUnauthorized, 401, "X-Project-ID is required", nil)
	}
	return Scope{OrganizationID: actor.OrganizationID, ProjectID: projectID}, actor, nil
}

func actor(r *http.Request) identity.Actor {
	value, _ := identity.ActorFromContext(r.Context())
	return value
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

func parseDueAt(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "due_at must be an RFC3339 timestamp", nil)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
