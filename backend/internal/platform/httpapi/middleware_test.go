package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type captureAuthenticator struct {
	request identity.AuthenticationRequest
}

func (a *captureAuthenticator) Authenticate(_ context.Context, request identity.AuthenticationRequest) (identity.Actor, error) {
	a.request = request
	return identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1"}, nil
}

func TestWithAuthenticatorAllowsBootstrapSelectionForMe(t *testing.T) {
	authenticator := &captureAuthenticator{}
	handler := WithAuthenticator(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), authenticator)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if !authenticator.request.AllowOrganizationSelection {
		t.Fatal("/me must allow organization selection during session bootstrap")
	}
}

func TestProjectIDFromRequest(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		header string
		want   string
	}{
		{name: "header", path: "/api/v1/work-items", header: "project-header", want: "project-header"},
		{name: "project route", path: "/api/v1/projects/project-path/members", want: "project-path"},
		{name: "project route wins over header", path: "/api/v1/projects/project-path/members", header: "other-project", want: "project-path"},
		{name: "project query", path: "/api/v1/integrations/github/repositories?project_id=project-query", want: "project-query"},
		{name: "unscoped", path: "/api/v1/projects", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			req.Header.Set("X-Project-ID", test.header)
			if got := projectIDFromRequest(req); got != test.want {
				t.Fatalf("projectIDFromRequest() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWithRequestIDPropagatesCorrelationID(t *testing.T) {
	handler := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) != "request-1" || CorrelationID(r.Context()) != "correlation-1" {
			t.Fatalf("unexpected IDs: %q %q", RequestID(r.Context()), CorrelationID(r.Context()))
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "request-1")
	request.Header.Set("X-Correlation-ID", "correlation-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("X-Correlation-ID") != "correlation-1" {
		t.Fatalf("correlation header = %q", response.Header().Get("X-Correlation-ID"))
	}
}

func TestWithSecurityHeaders(t *testing.T) {
	handler := WithSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("security headers missing: %#v", response.Header())
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("unexpected CSP: %q", response.Header().Get("Content-Security-Policy"))
	}
}

func TestWithDevelopmentActorAllowsGitHubPublicRoutes(t *testing.T) {
	for _, path := range []string{
		"/api/v1/auth/github/start",
		"/api/v1/auth/github/callback",
		"/api/v1/integrations/github/install/callback",
		"/api/v1/integrations/github/webhooks",
	} {
		t.Run(path, func(t *testing.T) {
			handler := WithDevelopmentActor(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}))
			request := httptest.NewRequest(http.MethodPost, path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("status = %d", response.Code)
			}
		})
	}
}

func TestWithRecoveryReturnsInternalError(t *testing.T) {
	handler := WithRecovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }), slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestWithCORSAllowsConfiguredWebOrigin(t *testing.T) {
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), "https://app.example.com")
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/work-items", nil)
	request.Header.Set("Origin", "https://app.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("unexpected CORS response: %d %#v", response.Code, response.Header())
	}
}

func TestWithCORSRejectsUnknownPreflightOrigin(t *testing.T) {
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), "https://app.example.com")
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/work-items", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestWithCORSAllowsAnyConfiguredOrigin(t *testing.T) {
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), "http://localhost:13000", "https://app.example.com")
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil)
	request.Header.Set("Origin", "https://app.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("unexpected multi-origin response: %d %#v", response.Code, response.Header())
	}
}

func TestWithCSRFProtectsSessionMutations(t *testing.T) {
	handler := WithCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), true, "https://app.example.com")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", nil)
	request.AddCookie(&http.Cookie{Name: "forgeflow_session", Value: "session"})
	request.Header.Set("Origin", "https://evil.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/work-items", nil)
	request.AddCookie(&http.Cookie{Name: "forgeflow_session", Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})
	request.Header.Set("Origin", "https://app.example.com")
	request.Header.Set(csrfHeaderName, "csrf-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("allowed status = %d", response.Code)
	}
}

func TestWithCSRFSeedsCookieAndRejectsMissingHeader(t *testing.T) {
	handler := WithCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), false, "http://app.example.com")
	get := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	get.AddCookie(&http.Cookie{Name: "forgeflow_session", Value: "session"})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusNoContent || getResponse.Header().Get("Set-Cookie") == "" {
		t.Fatalf("expected CSRF cookie, status=%d headers=%#v", getResponse.Code, getResponse.Header())
	}

	post := httptest.NewRequest(http.MethodPost, "/api/v1/work-items", nil)
	post.AddCookie(&http.Cookie{Name: "forgeflow_session", Value: "session"})
	post.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})
	post.Header.Set("Origin", "http://app.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, post)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing header status = %d", response.Code)
	}
}

func TestCookieSecureAllowsLoopbackHTTPOnly(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://localhost:18080/health/live", nil)
	if CookieSecure(request, true) {
		t.Fatal("expected loopback HTTP cookie to be non-secure")
	}
	request = httptest.NewRequest(http.MethodGet, "https://app.example.com/health/live", nil)
	if !CookieSecure(request, true) {
		t.Fatal("expected HTTPS cookie to be secure")
	}
	request = httptest.NewRequest(http.MethodGet, "http://app.example.com/health/live", nil)
	if !CookieSecure(request, true) {
		t.Fatal("expected non-loopback HTTP to stay secure")
	}
}

func TestMetricsEndpointRequiresTokenAndReportsRequests(t *testing.T) {
	metrics := NewMetrics()
	handler := metrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))

	unauthorized := httptest.NewRecorder()
	metrics.Endpoint("secret").ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusNotFound {
		t.Fatalf("unauthorized metrics status = %d", unauthorized.Code)
	}
	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer secret")
	metrics.Endpoint("secret").ServeHTTP(authorized, request)
	if !strings.Contains(authorized.Body.String(), "forgeflow_http_requests_total 1") {
		t.Fatalf("metrics body = %s", authorized.Body.String())
	}
}

func TestRateLimiterReturnsRetryHeaders(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if first.Code != http.StatusNoContent || first.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("first rate-limited response = %d, headers = %#v", first.Code, first.Header())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("second rate-limited response = %d, headers = %#v", second.Code, second.Header())
	}
}
