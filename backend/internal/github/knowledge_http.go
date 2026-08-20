package github

import (
	"net/http"
	"strconv"

	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
)

type knowledgeRequest struct {
	Slug             string `json:"slug"`
	Title            string `json:"title"`
	Kind             string `json:"kind"`
	Content          string `json:"content"`
	Provenance       string `json:"provenance"`
	SourceSnapshotID string `json:"source_snapshot_id"`
}

func (h *IntegrationHandler) knowledgeService() *KnowledgeService {
	if h.snapshots == nil {
		return nil
	}
	return h.snapshots.KnowledgeService()
}

func (h *IntegrationHandler) listKnowledge(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	service := h.knowledgeService()
	if service == nil {
		httpapi.Error(w, r, knowledgeUnavailable())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := service.List(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) createKnowledge(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	service := h.knowledgeService()
	if service == nil {
		httpapi.Error(w, r, knowledgeUnavailable())
		return
	}
	var input knowledgeRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, err := service.Create(r.Context(), actor, r.PathValue("id"), KnowledgeInput{RepositoryID: r.PathValue("repository_id"), Slug: input.Slug, Title: input.Title, Kind: input.Kind, Content: input.Content, Provenance: input.Provenance, SourceSnapshotID: input.SourceSnapshotID})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *IntegrationHandler) getKnowledge(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	service := h.knowledgeService()
	if service == nil {
		httpapi.Error(w, r, knowledgeUnavailable())
		return
	}
	item, err := service.Get(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("document_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *IntegrationHandler) listKnowledgeRevisions(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	service := h.knowledgeService()
	if service == nil {
		httpapi.Error(w, r, knowledgeUnavailable())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := service.Revisions(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("document_id"), limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) appendKnowledgeRevision(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	service := h.knowledgeService()
	if service == nil {
		httpapi.Error(w, r, knowledgeUnavailable())
		return
	}
	var input knowledgeRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	revision, err := service.AppendRevision(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("document_id"), KnowledgeInput{Content: input.Content, Provenance: input.Provenance, SourceSnapshotID: input.SourceSnapshotID})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, revision)
}
