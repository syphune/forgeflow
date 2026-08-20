package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type requestIDKey struct{}
type correlationIDKey struct{}

type rateLimitWindow struct {
	started time.Time
	count   int
}

// RateLimiter is intentionally process-local for the V1 stateless deployment.
// ponytail: one mutex keeps the limiter small; use a shared store only when multi-instance limits are required.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	entries map[string]rateLimitWindow
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{limit: limit, window: window, maxKeys: 10_000, entries: make(map[string]rateLimitWindow)}
}

func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, remaining, reset := l.allow(rateLimitKey(r), time.Now().UTC())
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", l.limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset.Unix()))
		if !allowed {
			seconds := int(time.Until(reset).Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
			Error(w, r, apperr.New(apperr.CodeRateLimited, http.StatusTooManyRequests, "request rate limit exceeded", nil))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RateLimiter) allow(key string, now time.Time) (bool, int, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok || !now.Before(entry.started.Add(l.window)) {
		if len(l.entries) >= l.maxKeys {
			for candidate, value := range l.entries {
				if !now.Before(value.started.Add(l.window)) {
					delete(l.entries, candidate)
				}
			}
		}
		if len(l.entries) >= l.maxKeys && !ok {
			return false, 0, now.Add(l.window)
		}
		entry = rateLimitWindow{started: now}
	}
	entry.count++
	l.entries[key] = entry
	reset := entry.started.Add(l.window)
	if entry.count > l.limit {
		return false, 0, reset
	}
	return true, l.limit - entry.count, reset
}

func rateLimitKey(r *http.Request) string {
	identity := strings.TrimSpace(r.Header.Get("Authorization"))
	if identity == "" {
		if cookie, err := r.Cookie("forgeflow_session"); err == nil {
			identity = cookie.Value
		}
	}
	if identity == "" {
		identity = r.RemoteAddr
		if host, _, err := net.SplitHostPort(identity); err == nil {
			identity = host
		}
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

const csrfCookieName = "forgeflow_csrf"
const csrfHeaderName = "X-CSRF-Token"

func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if !validRequestID(requestID) {
			var err error
			requestID, err = ids.New()
			if err != nil {
				Error(w, r, err)
				return
			}
		}
		w.Header().Set("X-Request-ID", requestID)
		correlationID := r.Header.Get("X-Correlation-ID")
		if !validRequestID(correlationID) {
			correlationID = requestID
		}
		w.Header().Set("X-Correlation-ID", correlationID)
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		ctx = context.WithValue(ctx, correlationIDKey{}, correlationID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func WithCORS(next http.Handler, configuredOrigins ...string) http.Handler {
	allowedOrigins := normalizedOrigins(configuredOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestOrigin := origin(r.Header.Get("Origin"))
		if requestOrigin == "" || !containsOrigin(allowedOrigins, requestOrigin) {
			if r.Method == http.MethodOptions && r.Header.Get("Origin") != "" {
				Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "origin is not allowed", nil))
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Actor-ID, X-Correlation-ID, X-CSRF-Token, X-Organization-ID, X-Project-ID, X-Request-ID, Idempotency-Key")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func WithCSRF(next http.Handler, secureCookies bool, configuredOrigins ...string) http.Handler {
	allowedOrigins := normalizedOrigins(configuredOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSecureCookies := CookieSecure(r, secureCookies)
		sessionCookie, sessionErr := r.Cookie("forgeflow_session")
		if sessionErr != nil || strings.TrimSpace(sessionCookie.Value) == "" || r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			if err := ensureCSRFCookie(w, r, requestSecureCookies); err != nil {
				Error(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		requestOrigin := origin(r.Header.Get("Origin"))
		if requestOrigin == "" {
			requestOrigin = origin(r.Header.Get("Referer"))
		}
		if requestOrigin == "" || !containsOrigin(allowedOrigins, requestOrigin) {
			Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "browser origin is not allowed", nil))
			return
		}
		csrfCookie, csrfErr := r.Cookie(csrfCookieName)
		csrfHeader := strings.TrimSpace(r.Header.Get(csrfHeaderName))
		if csrfErr != nil || strings.TrimSpace(csrfCookie.Value) == "" {
			if err := ensureCSRFCookie(w, r, requestSecureCookies); err != nil {
				Error(w, r, err)
				return
			}
			Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "CSRF token is required", nil))
			return
		}
		if csrfHeader == "" || subtle.ConstantTimeCompare([]byte(csrfCookie.Value), []byte(csrfHeader)) != 1 {
			Error(w, r, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "CSRF token is invalid", nil))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CookieSecure keeps a secure deployment safe while allowing an explicitly
// local HTTP browser to use the same development process as the public HTTPS
// tunnel.
func CookieSecure(r *http.Request, configured bool) bool {
	if !configured || r == nil || r.TLS != nil {
		return configured
	}
	host := strings.TrimSpace(r.Host)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false
	}
	return true
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secureCookies bool) error {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return nil
	}
	token, err := ids.New()
	if err != nil {
		return fmt.Errorf("generate CSRF token: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: token, Path: "/", MaxAge: 30 * 24 * 60 * 60,
		Secure: secureCookies, SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func origin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func normalizedOrigins(configuredOrigins []string) []string {
	origins := make([]string, 0, len(configuredOrigins))
	for _, configured := range configuredOrigins {
		if normalized := origin(configured); normalized != "" && !containsOrigin(origins, normalized) {
			origins = append(origins, normalized)
		}
	}
	return origins
}

func containsOrigin(origins []string, candidate string) bool {
	for _, allowed := range origins {
		if strings.EqualFold(candidate, allowed) {
			return true
		}
	}
	return false
}

func WithRecovery(next http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := &statusWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				panicContext := r.Context()
				if requestID := response.Header().Get("X-Request-ID"); requestID != "" {
					panicContext = context.WithValue(panicContext, requestIDKey{}, requestID)
				}
				if correlationID := response.Header().Get("X-Correlation-ID"); correlationID != "" {
					panicContext = context.WithValue(panicContext, correlationIDKey{}, correlationID)
				}
				panicRequest := r.WithContext(panicContext)
				logger.Error("panic while serving request", "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()), "request_id", RequestID(panicContext), "path", r.URL.Path)
				if !response.wroteHeader {
					Error(response, panicRequest, apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "internal server error", nil))
				}
			}
		}()
		next.ServeHTTP(response, r)
	})
}

func WithAccessLog(next http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)
		requestID := RequestID(r.Context())
		if requestID == "" {
			requestID = response.Header().Get("X-Request-ID")
		}
		correlationID := CorrelationID(r.Context())
		if correlationID == "" {
			correlationID = response.Header().Get("X-Correlation-ID")
		}
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", response.statusCode(), "bytes", response.bytes, "duration_ms", float64(time.Since(started))/float64(time.Millisecond), "request_id", requestID, "correlation_id", correlationID)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func WithAuthenticator(next http.Handler, authenticator identity.Authenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/logout" || r.URL.Path == "/api/v1/auth/github/start" || r.URL.Path == "/api/v1/auth/github/callback" || r.URL.Path == "/api/v1/integrations/github/install/callback" || r.URL.Path == "/api/v1/integrations/github/webhooks" {
			next.ServeHTTP(w, r)
			return
		}
		if authenticator == nil {
			RequireConfiguredAuth(next).ServeHTTP(w, r)
			return
		}
		request := identity.AuthenticationRequest{
			OrganizationID: strings.TrimSpace(r.Header.Get("X-Organization-ID")),
			ProjectID:      projectIDFromRequest(r),
			// /me is part of the bootstrap sequence. It must be able to
			// authenticate a session before the organization selector has
			// persisted a choice when the user belongs to more than one org.
			AllowOrganizationSelection: r.Method == http.MethodGet && (r.URL.Path == "/api/v1/organizations" || r.URL.Path == "/api/v1/me"),
		}
		if authorization := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(authorization, "Bearer ") {
			request.BearerToken = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
		}
		if cookie, err := r.Cookie("forgeflow_session"); err == nil {
			request.SessionToken = strings.TrimSpace(cookie.Value)
		}
		actor, err := authenticator.Authenticate(r.Context(), request)
		if err != nil {
			Error(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(identity.WithActor(r.Context(), actor)))
	})
}

func projectIDFromRequest(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) > 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "projects" {
		if value := strings.TrimSpace(parts[3]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(r.Header.Get("X-Project-ID")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.URL.Query().Get("project_id")); value != "" {
		return value
	}
	return ""
}

func WithDevelopmentActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/github/start" || r.URL.Path == "/api/v1/auth/github/callback" || r.URL.Path == "/api/v1/integrations/github/install/callback" || r.URL.Path == "/api/v1/integrations/github/webhooks" {
			next.ServeHTTP(w, r)
			return
		}
		orgID := strings.TrimSpace(r.Header.Get("X-Organization-ID"))
		actorID := strings.TrimSpace(r.Header.Get("X-Actor-ID"))
		if orgID == "" || actorID == "" {
			Error(w, r, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "organization and actor headers are required", nil))
			return
		}
		actor := identity.Actor{Type: "human", ID: actorID, OrganizationID: orgID, Source: "development-header", Capabilities: map[string]bool{"*": true}}
		next.ServeHTTP(w, r.WithContext(identity.WithActor(r.Context(), actor)))
	})
}

func RequireConfiguredAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Error(w, r, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication adapter is not configured", nil))
	})
}

func Error(w http.ResponseWriter, r *http.Request, err error) {
	appErr := apperr.From(err)
	status := appErr.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	JSON(w, status, ErrorResponse{
		Code:      appErr.Code,
		Message:   appErr.Message,
		Details:   appErr.Details,
		RequestID: RequestID(r.Context()),
	})
}

func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}
