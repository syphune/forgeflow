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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := json.NewEncoder(os.Stdout)
	executor := runner.Executor{}
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
		result, err := (runner.Executor{}).Execute(r.Context(), job, nil)
		status := http.StatusOK
		if err != nil {
			status = http.StatusUnprocessableEntity
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 24 * time.Hour}
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

func authorizedRunnerRequest(r *http.Request, expected string) bool {
	value := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if value == "" || len(value) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}
