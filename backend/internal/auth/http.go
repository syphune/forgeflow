package auth

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
	tokens   identity.TokenStore
	sessions identity.SessionStore
	oauth    *OAuthHandler
	maxBody  int64
	secure   bool
}

func NewHandler(tokens identity.TokenStore, sessions identity.SessionStore, maxBody int64) http.Handler {
	return newHandler(&Handler{tokens: tokens, sessions: sessions, maxBody: maxBody})
}

func NewHandlerWithOAuth(tokens identity.TokenStore, sessions identity.SessionStore, oauth *OAuthHandler, maxBody int64) http.Handler {
	secure := false
	if oauth != nil {
		secure = oauth.config.CookieSecure
	}
	return newHandler(&Handler{tokens: tokens, sessions: sessions, oauth: oauth, maxBody: maxBody, secure: secure})
}

func newHandler(h *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /me", h.me)
	mux.HandleFunc("GET /me/tokens", h.listTokens)
	mux.HandleFunc("POST /me/tokens", h.createToken)
	mux.HandleFunc("DELETE /me/tokens/{id}", h.revokeToken)
	mux.HandleFunc("POST /auth/logout", h.logout)
	if h.oauth != nil {
		mux.HandleFunc("GET /auth/github/start", h.oauth.start)
		mux.HandleFunc("GET /auth/github/callback", h.oauth.callback)
	}
	return mux
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"id": actor.ID, "type": actor.Type, "organization_id": actor.OrganizationID,
		"source": actor.Source,
	})
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if h.tokens == nil {
		httpapi.Error(w, r, apperr.New(apperr.CodeInternal, 503, "token store is unavailable", nil))
		return
	}
	tokens, err := h.tokens.ListPAT(r.Context(), actor.OrganizationID, actor.ID)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

type createTokenRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays int      `json:"expires_in_days"`
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if h.tokens == nil {
		httpapi.Error(w, r, apperr.New(apperr.CodeInternal, 503, "token store is unavailable", nil))
		return
	}
	var input createTokenRequest
	if err := decodeJSON(w, r, h.maxBody, &input); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 100 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "token name is required and must be at most 100 characters", nil))
		return
	}
	if input.ExpiresInDays == 0 {
		input.ExpiresInDays = 90
	}
	if input.ExpiresInDays < 1 || input.ExpiresInDays > 365 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 422, "expires_in_days must be between 1 and 365", nil))
		return
	}
	scopes, err := validateScopes(input.Scopes)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	token, err := h.tokens.CreatePAT(r.Context(), actor.OrganizationID, actor.ID, name, scopes, time.Now().UTC().AddDate(0, 0, input.ExpiresInDays))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, token)
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if h.tokens == nil {
		httpapi.Error(w, r, apperr.New(apperr.CodeInternal, 503, "token store is unavailable", nil))
		return
	}
	if err := h.tokens.RevokePAT(r.Context(), actor.OrganizationID, actor.ID, r.PathValue("id")); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.sessions != nil {
		if cookie, err := r.Cookie("forgeflow_session"); err == nil && cookie.Value != "" {
			if err := h.sessions.RevokeSession(r.Context(), cookie.Value); err != nil {
				httpapi.Error(w, r, err)
				return
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "forgeflow_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: httpapi.CookieSecure(r, h.secure)})
	http.SetCookie(w, &http.Cookie{Name: "forgeflow_csrf", Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteStrictMode, Secure: httpapi.CookieSecure(r, h.secure)})
	w.WriteHeader(http.StatusNoContent)
}

func currentActor(r *http.Request) (identity.Actor, error) {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return identity.Actor{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	return actor, nil
}

var allowedScopes = map[string]bool{
	"organization.read": true, "organization.manage": true, "workspace.read": true, "workspace.manage": true, "project.read": true, "project.manage": true,
	"work_item.create": true, "work_item.edit": true, "work_item.assign": true,
	"work_item.transition": true, "work_item.delete": true, "comment.create": true, "sprint.manage": true, "repository.read": true, "repository.manage": true,
	"specification.propose": true, "specification.verify": true, "agent.execute": true,
	"autonomous.start": true, "autonomous.retry": true, "autonomous.cancel": true,
	"agent.approve": true, "audit.read": true,
}

func validateScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "at least one token scope is required", nil)
	}
	seen := make(map[string]bool, len(scopes))
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if !allowedScopes[scope] {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported token scope", map[string]any{"scope": scope})
		}
		if !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	return result, nil
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
		return apperr.New(apperr.CodeInvalidArgument, 422, "invalid JSON request body", nil)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "request body must contain one JSON value", nil)
	}
	return nil
}
