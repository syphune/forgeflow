package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type fakeProviderAuthenticator struct {
	items    []AuthStatus
	provider string
	switchOn bool
}

func (f *fakeProviderAuthenticator) List(context.Context) ([]AuthStatus, error) {
	return f.items, nil
}

func (f *fakeProviderAuthenticator) Status(_ context.Context, provider string) (AuthStatus, error) {
	f.provider = provider
	return AuthStatus{Provider: provider, Status: AuthStatusLoginRequired, Message: "login required"}, nil
}

func (f *fakeProviderAuthenticator) Login(_ context.Context, provider string, switchAccount bool) (AuthStatus, error) {
	f.provider = provider
	f.switchOn = switchAccount
	return AuthStatus{Provider: provider, Status: AuthStatusLoginPending, Message: "pending"}, nil
}

func (f *fakeProviderAuthenticator) Logout(_ context.Context, provider string) (AuthStatus, error) {
	f.provider = provider
	return AuthStatus{Provider: provider, Status: AuthStatusLoginRequired, Message: "signed out"}, nil
}

func TestParseLoginDetails(t *testing.T) {
	uri, code := parseLoginDetails("Open https://auth.openai.com/codex/device. Enter KHRQ-4HCV")
	if uri != "https://auth.openai.com/codex/device" || code != "KHRQ-4HCV" {
		t.Fatalf("login details = %q %q", uri, code)
	}

	status := pendingStatus("codex", "expires in 15 minutes", time.Unix(100, 0), time.Unix(200, 0))
	if status.ExpiresAt == nil || !status.ExpiresAt.Equal(time.Unix(100, 0).Add(15*time.Minute)) {
		t.Fatalf("pending expiry = %v", status.ExpiresAt)
	}
}

func TestStatusFromCommandDoesNotExposeCLIOutput(t *testing.T) {
	status := statusFromCommand("claude", `{"loggedIn":false,"token":"secret-token"}`, context.Canceled)
	if status.Status != AuthStatusLoginRequired || strings.Contains(status.Message, "secret-token") {
		t.Fatalf("status = %#v", status)
	}
	if got := statusFromCommand("claude", `{"loggedIn":true}`, nil); got.Status != AuthStatusAuthenticated {
		t.Fatalf("authenticated status = %#v", got)
	}
}

func TestProviderModelsNormalizeCodexCatalog(t *testing.T) {
	models := providerModels([]codexModel{
		{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6-Sol", IsDefault: true, DefaultReasoningEffort: "medium", SupportedReasoningEfforts: []codexReasoningEffort{{ReasoningEffort: "low", Description: "Fast"}, {ReasoningEffort: "high", Description: "Deep"}}},
		{ID: "hidden-model", Hidden: true},
		{Model: "fallback-model", DisplayName: "Fallback"},
	})
	if len(models) != 2 || models[0].ID != "gpt-5.6-sol" || !models[0].IsDefault || len(models[0].ReasoningEfforts) != 2 || models[1].ID != "fallback-model" {
		t.Fatalf("normalized provider models = %#v", models)
	}
	if got := claudeReasoningEfforts(); len(got) != 5 || got[0].Value != "low" || got[4].Value != "max" {
		t.Fatalf("Claude reasoning efforts = %#v", got)
	}
}

func TestProviderAuthCatalogIsReadableByAutonomousStarter(t *testing.T) {
	fake := &fakeProviderAuthenticator{items: []AuthStatus{{Provider: "codex", Status: AuthStatusAuthenticated, Message: "ok", Models: []ProviderModel{{ID: "gpt-5.6-sol"}}}}}
	handler := NewProviderAuthHandler(fake)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityAutonomousStart: true}}
	request := httptest.NewRequest(http.MethodGet, "/projects/project-1/provider-auth", nil).WithContext(identity.WithActor(context.Background(), actor))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload AuthList
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || len(payload.Items) != 1 || len(payload.Items[0].Models) != 1 {
		t.Fatalf("catalog response = %#v, err = %v", payload, err)
	}
}

func TestProviderAuthHandlerScopesActionsToProjectManager(t *testing.T) {
	fake := &fakeProviderAuthenticator{items: []AuthStatus{{Provider: "codex", Status: AuthStatusAuthenticated, Message: "ok"}}}
	handler := NewProviderAuthHandler(fake)

	tests := []struct {
		name       string
		capability bool
		method     string
		path       string
		wantStatus int
	}{
		{name: "reader cannot inspect", capability: false, method: http.MethodGet, path: "/projects/project-1/provider-auth", wantStatus: http.StatusForbidden},
		{name: "manager can inspect", capability: true, method: http.MethodGet, path: "/projects/project-1/provider-auth", wantStatus: http.StatusOK},
		{name: "manager can switch account", capability: true, method: http.MethodPost, path: "/projects/project-1/provider-auth/codex/switch", wantStatus: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capabilities := map[string]bool{}
			if test.capability {
				capabilities[identity.CapabilityProjectManage] = true
			}
			actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: capabilities}
			request := httptest.NewRequest(test.method, test.path, nil)
			request = request.WithContext(identity.WithActor(request.Context(), actor))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
	if fake.provider != "codex" || !fake.switchOn {
		t.Fatalf("switch action = %q %t", fake.provider, fake.switchOn)
	}
}

func TestHTTPAuthClientUsesRunnerBearerAndDecodesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer runner-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AuthList{Items: []AuthStatus{{Provider: "codex", Status: AuthStatusAuthenticated, Message: "ok"}, {Provider: "claude", Status: AuthStatusLoginRequired, Message: "login"}}})
	}))
	defer server.Close()

	items, err := (HTTPAuthClient{BaseURL: server.URL, Token: "runner-secret"}).List(context.Background())
	if err != nil || len(items) != 2 || items[0].Provider != "codex" {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
}
