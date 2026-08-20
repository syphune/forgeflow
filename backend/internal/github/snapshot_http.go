package github

import (
	"net/http"
	"strconv"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
)

func (h *IntegrationHandler) listSnapshots(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.snapshots.List(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), limit)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *IntegrationHandler) refreshSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	item, err := h.snapshots.Refresh(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"))
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusAccepted, item)
}

func (h *IntegrationHandler) getSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	item, err := h.snapshots.Get(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("snapshot_id"))
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *IntegrationHandler) getSnapshotFile(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	item, err := h.snapshots.File(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("snapshot_id"), r.URL.Query().Get("path"))
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, item)
}

func (h *IntegrationHandler) searchSnapshot(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.snapshots.Search(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("snapshot_id"), r.URL.Query().Get("q"), limit)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "content_trust": "UNTRUSTED_CONTENT"})
}

func (h *IntegrationHandler) listSnapshotSymbols(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.snapshots.Symbols(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("snapshot_id"), r.URL.Query().Get("name"), limit)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "provenance": "EXTRACTED", "content_trust": "UNTRUSTED_CONTENT"})
}

func (h *IntegrationHandler) listSnapshotEdges(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	if h.snapshots == nil {
		h.writeSnapshotError(w, r, snapshotServiceUnavailable())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.snapshots.Edges(r.Context(), actor, r.PathValue("id"), r.PathValue("repository_id"), r.PathValue("snapshot_id"), r.URL.Query().Get("from"), limit)
	if err != nil {
		h.writeSnapshotError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "provenance": "EXTRACTED", "content_trust": "UNTRUSTED_CONTENT"})
}

func (h *IntegrationHandler) writeSnapshotError(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.Error(w, r, err)
}

func snapshotServiceUnavailable() error {
	return apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "repository indexing is not configured", nil)
}
