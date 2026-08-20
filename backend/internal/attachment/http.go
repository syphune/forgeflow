package attachment

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) http.Handler {
	h := &Handler{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /work-items/{id}/attachments", h.list)
	mux.HandleFunc("POST /work-items/{id}/attachments", h.create)
	mux.HandleFunc("GET /work-items/{id}/attachments/{attachment_id}", h.download)
	mux.HandleFunc("DELETE /work-items/{id}/attachments/{attachment_id}", h.delete)
	return mux
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	items, err := h.service.List(r.Context(), actor, projectID, r.PathValue("id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBytes+1<<20)
	if err := r.ParseMultipartForm(MaxBytes); err != nil {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "multipart attachment upload is invalid", nil))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "file is required", nil))
		return
	}
	defer file.Close()
	item, err := h.service.Create(r.Context(), actor, projectID, r.PathValue("id"), header.Filename, header.Header.Get("Content-Type"), file)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := requestScope(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	item, content, err := h.service.Open(r.Context(), actor, projectID, r.PathValue("id"), r.PathValue("attachment_id"))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	defer content.Close()
	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(item.SizeBytes, 10))
	w.Header().Set("Content-Disposition", contentDisposition(item.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, content)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	actor, projectID, err := requestScope(r)
	if err == nil {
		err = h.service.Delete(r.Context(), actor, projectID, r.PathValue("id"), r.PathValue("attachment_id"))
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requestScope(r *http.Request) (identity.Actor, string, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, "", apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if projectID == "" {
		return identity.Actor{}, "", apperr.New(apperr.CodeUnauthorized, 401, "X-Project-ID is required", nil)
	}
	return actor, projectID, nil
}

func contentDisposition(name string) string {
	value := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	if value != "" {
		return value
	}
	return fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(name, `"`, ""))
}
