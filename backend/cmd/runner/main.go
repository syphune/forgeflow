package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/runner"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if address := strings.TrimSpace(os.Getenv("FORGEFLOW_RUNNER_HTTP_ADDRESS")); address != "" {
		serveHTTP(address, strings.TrimSpace(os.Getenv("FORGEFLOW_RUNNER_TOKEN")), logger)
		return
	}
	var job runner.Job
	if err := json.NewDecoder(os.Stdin).Decode(&job); err != nil {
		logger.Error("decode runner job", "error", err)
		os.Exit(2)
	}
	if err := job.Validate(); err != nil {
		logger.Error("invalid runner job", "error", err)
		os.Exit(2)
	}
	job.MCPToken = strings.TrimSpace(os.Getenv("FORGEFLOW_RUNNER_MCP_TOKEN"))
	if job.MCPToken == "" {
		job.MCPToken = strings.TrimSpace(os.Getenv("FORGEFLOW_MCP_TOKEN"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := json.NewEncoder(os.Stdout)
	executor := runner.Executor{UseDocker: true, DockerWorkspaceRoot: strings.TrimSpace(os.Getenv("FORGEFLOW_RUNNER_DOCKER_WORKSPACE_ROOT")), CodexAuthHostPath: strings.TrimSpace(os.Getenv("FORGEFLOW_CODEX_AUTH_HOST_PATH")), ClaudeAuthHostPath: strings.TrimSpace(os.Getenv("FORGEFLOW_CLAUDE_CONFIG_HOST_PATH"))}
	_, err := executor.Execute(ctx, job, func(event runner.Event) error {
		return encoder.Encode(event)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "runner execution failed:", err)
		os.Exit(1)
	}
}

func serveHTTP(address, token string, logger *slog.Logger) {
	if token == "" {
		logger.Error("FORGEFLOW_RUNNER_TOKEN is required for HTTP runner mode")
		os.Exit(2)
	}
	authManager := runner.NewAuthManager(strings.TrimSpace(os.Getenv("FORGEFLOW_CODEX_HOME")), strings.TrimSpace(os.Getenv("FORGEFLOW_CLAUDE_CONFIG_DIR")))
	defer authManager.Close()
	handler := runnerHTTPHandlerWithAuth(token, nil, authManager)
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 24 * time.Hour}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("runner listening", "address", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("runner stopped", "error", err)
		os.Exit(1)
	}
}

func runnerHTTPHandler(token string, execute func(context.Context, runner.Job) (runner.Result, error)) http.Handler {
	authManager := runner.NewAuthManager(strings.TrimSpace(os.Getenv("FORGEFLOW_CODEX_HOME")), strings.TrimSpace(os.Getenv("FORGEFLOW_CLAUDE_CONFIG_DIR")))
	return runnerHTTPHandlerWithAuth(token, execute, authManager)
}

func runnerHTTPHandlerWithAuth(token string, execute func(context.Context, runner.Job) (runner.Result, error), authenticator runner.ProviderAuthenticator) http.Handler {
	if execute == nil {
		execute = func(ctx context.Context, job runner.Job) (runner.Result, error) {
			return (runner.Executor{UseDocker: true, DockerWorkspaceRoot: strings.TrimSpace(os.Getenv("FORGEFLOW_RUNNER_DOCKER_WORKSPACE_ROOT")), CodexAuthHostPath: strings.TrimSpace(os.Getenv("FORGEFLOW_CODEX_AUTH_HOST_PATH")), ClaudeAuthHostPath: strings.TrimSpace(os.Getenv("FORGEFLOW_CLAUDE_CONFIG_HOST_PATH"))}).Execute(ctx, job, nil)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		if !authorizedRunnerRequest(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var job runner.Job
		body := http.MaxBytesReader(w, r.Body, 2<<20)
		if err := json.NewDecoder(body).Decode(&job); err != nil {
			http.Error(w, "invalid job", http.StatusBadRequest)
			return
		}
		job.GitToken = strings.TrimSpace(r.Header.Get("X-Forgeflow-Git-Token"))
		job.MCPToken = strings.TrimSpace(r.Header.Get("X-Forgeflow-MCP-Token"))
		result, err := execute(r.Context(), job)
		status := http.StatusOK
		if err != nil {
			if strings.TrimSpace(result.Error) == "" {
				result.Error = err.Error()
			}
			if result.ExitCode == 0 {
				result.ExitCode = 1
			}
			status = http.StatusUnprocessableEntity
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	})
	authenticated := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authorizedRunnerRequest(r, token) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if authenticator == nil {
				http.Error(w, "provider authentication is unavailable", http.StatusServiceUnavailable)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /auth", authenticated(func(w http.ResponseWriter, r *http.Request) {
		items, err := authenticator.List(r.Context())
		if err != nil {
			http.Error(w, "provider authentication request failed", http.StatusServiceUnavailable)
			return
		}
		writeProviderAuthJSON(w, runner.AuthList{Items: items})
	}))
	mux.HandleFunc("GET /auth/{provider}", authenticated(func(w http.ResponseWriter, r *http.Request) {
		status, err := authenticator.Status(r.Context(), r.PathValue("provider"))
		if err != nil {
			http.Error(w, "provider authentication request failed", http.StatusUnprocessableEntity)
			return
		}
		writeProviderAuthJSON(w, status)
	}))
	mux.HandleFunc("POST /auth/{provider}/login", authenticated(func(w http.ResponseWriter, r *http.Request) {
		status, err := authenticator.Login(r.Context(), r.PathValue("provider"), false)
		if err != nil {
			http.Error(w, "provider authentication request failed", http.StatusUnprocessableEntity)
			return
		}
		writeProviderAuthJSON(w, status)
	}))
	mux.HandleFunc("POST /auth/{provider}/switch", authenticated(func(w http.ResponseWriter, r *http.Request) {
		status, err := authenticator.Login(r.Context(), r.PathValue("provider"), true)
		if err != nil {
			http.Error(w, "provider authentication request failed", http.StatusUnprocessableEntity)
			return
		}
		writeProviderAuthJSON(w, status)
	}))
	mux.HandleFunc("POST /auth/{provider}/logout", authenticated(func(w http.ResponseWriter, r *http.Request) {
		status, err := authenticator.Logout(r.Context(), r.PathValue("provider"))
		if err != nil {
			http.Error(w, "provider authentication request failed", http.StatusUnprocessableEntity)
			return
		}
		writeProviderAuthJSON(w, status)
	}))
	return mux
}

func writeProviderAuthJSON(w http.ResponseWriter, value any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func authorizedRunnerRequest(r *http.Request, expected string) bool {
	value := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if value == "" || len(value) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}
