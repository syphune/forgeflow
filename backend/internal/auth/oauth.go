package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type OAuthConfig struct {
	ClientID        string
	ClientSecret    string
	RedirectURL     string
	SuccessRedirect string
	CookieSecure    bool
}

type OAuthHandler struct {
	store    identity.OAuthStore
	sessions identity.SessionStore
	config   OAuthConfig
	client   *http.Client
}

func NewOAuthHandler(store identity.OAuthStore, sessions identity.SessionStore, config OAuthConfig) *OAuthHandler {
	return &OAuthHandler{store: store, sessions: sessions, config: config, client: &http.Client{Timeout: 10 * time.Second}}
}

func (h *OAuthHandler) start(w http.ResponseWriter, r *http.Request) {
	if h.store == nil || h.sessions == nil || h.config.ClientID == "" || h.config.RedirectURL == "" {
		httpapiError(w, r, apperr.New(apperr.CodeInternal, 503, "GitHub OAuth is not configured", nil))
		return
	}
	state, err := randomString(32)
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	verifier, err := randomString(48)
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	if err := h.store.BeginOAuth(r.Context(), hashString(state), verifier, h.config.RedirectURL, time.Now().UTC().Add(10*time.Minute)); err != nil {
		httpapiError(w, r, err)
		return
	}
	query := url.Values{
		"client_id": {h.config.ClientID}, "redirect_uri": {h.config.RedirectURL},
		"scope": {"read:user"}, "state": {state}, "allow_signup": {"true"},
		"code_challenge": {codeChallenge(verifier)}, "code_challenge_method": {"S256"},
	}
	http.SetCookie(w, &http.Cookie{Name: "forgeflow_oauth_state", Value: state, Path: "/api/v1/auth/github", MaxAge: 600, HttpOnly: true, Secure: httpapi.CookieSecure(r, h.config.CookieSecure), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+query.Encode(), http.StatusFound)
}

func (h *OAuthHandler) callback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("forgeflow_oauth_state")
	if err != nil || stateCookie.Value == "" || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		httpapiError(w, r, apperr.New(apperr.CodeUnauthorized, 401, "OAuth state validation failed", nil))
		return
	}
	if r.URL.Query().Get("error") != "" {
		httpapiError(w, r, apperr.New(apperr.CodeUnauthorized, 401, "GitHub OAuth was denied", nil))
		return
	}
	verifier, redirectURI, err := h.store.ConsumeOAuth(r.Context(), hashString(stateCookie.Value))
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	accessToken, err := h.exchangeCode(r.Context(), r.URL.Query().Get("code"), verifier, redirectURI)
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	githubUser, err := h.githubUser(r.Context(), accessToken)
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	userID, err := h.store.UpsertGitHubUser(r.Context(), githubUser.ID, githubUser.Login, githubUser.DisplayName())
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	if _, err := h.store.EnsureDefaultOrganization(r.Context(), userID, githubUser.ID, githubUser.Login); err != nil {
		httpapiError(w, r, err)
		return
	}
	_, sessionToken, err := h.sessions.CreateSession(r.Context(), userID, time.Now().UTC().Add(30*24*time.Hour))
	if err != nil {
		httpapiError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "forgeflow_oauth_state", Value: "", Path: "/api/v1/auth/github", MaxAge: -1, HttpOnly: true, Secure: httpapi.CookieSecure(r, h.config.CookieSecure), SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "forgeflow_session", Value: sessionToken, Path: "/", MaxAge: 30 * 24 * 60 * 60, HttpOnly: true, Secure: httpapi.CookieSecure(r, h.config.CookieSecure), SameSite: http.SameSiteLaxMode})
	redirect := h.config.SuccessRedirect
	if redirect == "" {
		redirect = "/"
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

func (u githubUser) DisplayName() string {
	if strings.TrimSpace(u.Name) != "" {
		return strings.TrimSpace(u.Name)
	}
	return strings.TrimSpace(u.Login)
}

func (h *OAuthHandler) exchangeCode(ctx context.Context, code, verifier, redirectURI string) (string, error) {
	if strings.TrimSpace(code) == "" {
		return "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub OAuth code is missing", nil)
	}
	form := url.Values{"client_id": {h.config.ClientID}, "client_secret": {h.config.ClientSecret}, "code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create GitHub token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange GitHub OAuth code: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub OAuth token exchange failed", nil)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode GitHub token response: %w", err)
	}
	if payload.AccessToken == "" {
		return "", apperr.New(apperr.CodeUnauthorized, 401, "GitHub OAuth token was not returned", nil)
	}
	return payload.AccessToken, nil
}

func (h *OAuthHandler) githubUser(ctx context.Context, accessToken string) (githubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return githubUser{}, fmt.Errorf("create GitHub user request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "forgeflow")
	response, err := h.client.Do(req)
	if err != nil {
		return githubUser{}, fmt.Errorf("load GitHub user: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubUser{}, apperr.New(apperr.CodeUnauthorized, 401, "GitHub user lookup failed", nil)
	}
	var user githubUser
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&user); err != nil {
		return githubUser{}, fmt.Errorf("decode GitHub user: %w", err)
	}
	if user.ID == 0 || strings.TrimSpace(user.Login) == "" {
		return githubUser{}, apperr.New(apperr.CodeUnauthorized, 401, "GitHub user response is invalid", nil)
	}
	return user, nil
}

func randomString(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate OAuth value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func codeChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func httpapiError(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.Error(w, r, err)
}
