package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	platformidempotency "github.com/forgeflow/forgeflow/backend/internal/platform/idempotency"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type IntegrationHandler struct {
	service         *Service
	snapshots       *SnapshotService
	maxBody         int64
	successRedirect string
	idempotency     platformidempotency.Store
}

func NewIntegrationHandler(service *Service, maxBody int64, successRedirect string, snapshotServices ...*SnapshotService) http.Handler {
	return newIntegrationHandler(service, maxBody, successRedirect, nil, snapshotServices...)
}

func NewIntegrationHandlerWithIdempotency(service *Service, maxBody int64, successRedirect string, idempotencyStore platformidempotency.Store, snapshotServices ...*SnapshotService) http.Handler {
	return newIntegrationHandler(service, maxBody, successRedirect, idempotencyStore, snapshotServices...)
}

func newIntegrationHandler(service *Service, maxBody int64, successRedirect string, idempotencyStore platformidempotency.Store, snapshotServices ...*SnapshotService) http.Handler {
	var snapshots *SnapshotService
	if len(snapshotServices) > 0 {
		snapshots = snapshotServices[0]
	}
	h := &IntegrationHandler{service: service, snapshots: snapshots, maxBody: maxBody, successRedirect: successRedirect, idempotency: idempotencyStore}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /integrations/github/install/start", h.startInstallation)
	mux.HandleFunc("GET /integrations/github/install/callback", h.completeInstallation)
	mux.HandleFunc("GET /integrations/github/installations", h.listInstallations)
	mux.HandleFunc("GET /integrations/github/repositories", h.listRepositories)
	mux.HandleFunc("GET /projects/{id}/repositories", h.listProjectRepositories)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/context", h.repositoryContext)
	mux.HandleFunc("POST /projects/{id}/repositories/{repository_id}/pull-requests", h.createDraftPullRequest)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/tree", h.repositoryTree)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/file", h.repositoryFile)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/search", h.repositorySearch)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/related-files", h.relatedRepositoryFiles)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/snapshots", h.listSnapshots)
	mux.HandleFunc("POST /projects/{id}/repositories/{repository_id}/snapshots/refresh", h.refreshSnapshot)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/snapshots/{snapshot_id}", h.getSnapshot)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/snapshots/{snapshot_id}/file", h.getSnapshotFile)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/snapshots/{snapshot_id}/search", h.searchSnapshot)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/snapshots/{snapshot_id}/symbols", h.listSnapshotSymbols)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/snapshots/{snapshot_id}/edges", h.listSnapshotEdges)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/knowledge", h.listKnowledge)
	mux.HandleFunc("POST /projects/{id}/repositories/{repository_id}/knowledge", h.createKnowledge)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/knowledge/{document_id}", h.getKnowledge)
	mux.HandleFunc("GET /projects/{id}/repositories/{repository_id}/knowledge/{document_id}/revisions", h.listKnowledgeRevisions)
	mux.HandleFunc("POST /projects/{id}/repositories/{repository_id}/knowledge/{document_id}/revisions", h.appendKnowledgeRevision)
	mux.HandleFunc("POST /projects/{id}/repositories", h.linkRepository)
	mux.HandleFunc("DELETE /projects/{id}/repositories/{repository_id}", h.unlinkRepository)
	return mux
}

func (h *IntegrationHandler) repositoryContext(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.RepositoryContext(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

type draftPullRequestRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

func (h *IntegrationHandler) createDraftPullRequest(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input draftPullRequestRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	claimed := false
	if h.idempotency != nil {
		if key == "" {
			httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "Idempotency-Key is required for draft pull requests", nil))
			return
		}
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
	item, err := h.service.CreateDraftPullRequest(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), DraftPullRequestInput{Title: input.Title, Body: input.Body, Head: input.Head, Base: input.Base})
	if err != nil {
		if claimed {
			_ = h.idempotency.Release(r.Context(), actor.OrganizationID, actor.ID, key)
		}
		httpapi.Error(w, r, err)
		return
	}
	body, marshalErr := json.Marshal(item)
	if marshalErr != nil {
		if claimed {
			_ = h.idempotency.Release(r.Context(), actor.OrganizationID, actor.ID, key)
		}
		httpapi.Error(w, r, apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "could not encode pull request response", nil))
		return
	}
	if claimed {
		if err := h.idempotency.Complete(r.Context(), actor.OrganizationID, actor.ID, key, http.StatusCreated, body); err != nil {
			httpapi.Error(w, r, apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "could not persist idempotency response", nil))
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

func (h *IntegrationHandler) repositoryTree(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.RepositoryTree(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) repositoryFile(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := h.service.RepositoryFile(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.URL.Query().Get("path"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *IntegrationHandler) repositorySearch(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.SearchRepository(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.URL.Query().Get("q"), limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) relatedRepositoryFiles(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.RelatedRepositoryFiles(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.URL.Query().Get("path"), limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) startInstallation(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	location, err := h.service.StartInstallation(r.Context(), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func (h *IntegrationHandler) completeInstallation(w http.ResponseWriter, r *http.Request) {
	installationID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("installation_id")), 10, 64)
	if err != nil || installationID <= 0 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "installation_id is invalid", nil))
		return
	}
	if err := h.service.CompleteInstallation(r.Context(), r.URL.Query().Get("state"), installationID, r.URL.Query().Get("setup_action")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	redirect := h.successRedirect
	if strings.TrimSpace(redirect) == "" {
		redirect = "/"
	}
	if parsed, parseErr := url.Parse(redirect); parseErr == nil {
		query := parsed.Query()
		query.Set("github", "connected")
		parsed.RawQuery = query.Encode()
		redirect = parsed.String()
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (h *IntegrationHandler) listRepositories(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Repositories(r.Context(), actor, r.URL.Query().Get("project_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) listInstallations(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.Installations(r.Context(), actor)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) listProjectRepositories(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.ProjectRepositories(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type repositoryLinkRequest struct {
	RepositoryID string `json:"repository_id"`
}

func (h *IntegrationHandler) linkRepository(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var input repositoryLinkRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.LinkRepository(r.Context(), actor, r.PathValue("id"), input.RepositoryID); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *IntegrationHandler) unlinkRepository(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.service.UnlinkRepository(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func currentActor(r *http.Request) (identity.Actor, error) {
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
