package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestValidateScopesRejectsUnknownAndDeduplicates(t *testing.T) {
	scopes, err := validateScopes([]string{"project.read", "project.read", "work_item.edit", "autonomous.start", "autonomous.retry", "autonomous.cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 5 {
		t.Fatalf("got %d scopes", len(scopes))
	}
	if _, err := validateScopes([]string{"shell.execute"}); err == nil {
		t.Fatal("expected unsupported scope error")
	}
	if _, err := validateScopes(nil); err == nil {
		t.Fatal("expected explicit token scope requirement")
	}
}

func TestDecodeJSONRejectsTrailingValues(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"} nope`))
	w := httptest.NewRecorder()
	var target struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, 1024, &target); err == nil {
		t.Fatal("expected trailing input error")
	}
}

type oauthStoreStub struct{}

func (oauthStoreStub) BeginOAuth(context.Context, string, string, string, time.Time) error {
	return nil
}

func (oauthStoreStub) ConsumeOAuth(context.Context, string) (string, string, error) {
	return "verifier", "http://localhost/callback", nil
}

func (oauthStoreStub) UpsertGitHubUser(context.Context, int64, string, string) (string, error) {
	return "user-1", nil
}

type sessionStoreStub struct{}

func (sessionStoreStub) CreateSession(context.Context, string, time.Time) (string, string, error) {
	return "session-id", "session-token", nil
}

func (sessionStoreStub) RevokeSession(context.Context, string) error { return nil }

func TestOAuthRoutesAreRegisteredAndValidateState(t *testing.T) {
	handler := NewHandlerWithOAuth(nil, nil, NewOAuthHandler(oauthStoreStub{}, sessionStoreStub{}, OAuthConfig{
		ClientID: "client-id", RedirectURL: "http://localhost/callback", SuccessRedirect: "http://localhost/",
	}), 1<<20)

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/github/start", nil))
	if start.Code != http.StatusFound || !strings.Contains(start.Header().Get("Location"), "client_id=client-id") {
		t.Fatalf("OAuth start = %d %q", start.Code, start.Header().Get("Location"))
	}
	stateCookie := start.Result().Cookies()[0]

	callback := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+stateCookie.Value, nil)
	callback.AddCookie(stateCookie)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, callback)
	if result.Code != http.StatusUnauthorized || !strings.Contains(result.Body.String(), "GitHub OAuth code is missing") {
		t.Fatalf("OAuth callback = %d %s", result.Code, result.Body.String())
	}
}

var _ identity.OAuthStore = oauthStoreStub{}
var _ identity.SessionStore = sessionStoreStub{}
