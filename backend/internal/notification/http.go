package notification

import (
	"net/http"
	"strconv"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func NewHandler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /notifications/unread-count", func(w http.ResponseWriter, r *http.Request) {
		actor, err := actorFrom(r)
		if err == nil {
			var count int
			count, err = service.UnreadCount(r.Context(), actor)
			if err == nil {
				httpapi.JSON(w, http.StatusOK, map[string]int{"unread_count": count})
				return
			}
		}
		httpapi.Error(w, r, err)
	})
	mux.HandleFunc("GET /notifications", func(w http.ResponseWriter, r *http.Request) {
		actor, err := actorFrom(r)
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := service.List(r.Context(), actor, limit)
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /notifications/{id}/read", func(w http.ResponseWriter, r *http.Request) {
		actor, err := actorFrom(r)
		if err == nil {
			err = service.MarkRead(r.Context(), actor, r.PathValue("id"))
		}
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /notifications/read-all", func(w http.ResponseWriter, r *http.Request) {
		actor, err := actorFrom(r)
		if err == nil {
			err = service.MarkAllRead(r.Context(), actor)
		}
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func actorFrom(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || actor.ID == "" || actor.OrganizationID == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	return actor, nil
}
