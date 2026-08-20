package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/app"
	"github.com/forgeflow/forgeflow/backend/internal/attachment"
	"github.com/forgeflow/forgeflow/backend/internal/auth"
	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/platform/config"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		logger.Warn("DATABASE_URL is empty; using development in-memory adapter")
	}
	var application *app.App
	if cfg.DatabaseURL == "" {
		application = app.New(cfg.MaxBodyBytes)
	} else {
		developmentAuth := os.Getenv("FORGEFLOW_DEV_AUTH") == "true"
		if developmentAuth {
			logger.Warn("FORGEFLOW_DEV_AUTH=true; header actor auth is unsafe and only for local development")
		}
		startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		application, err = app.NewWithDatabase(startupCtx, cfg.DatabaseURL, cfg.MaxBodyBytes, developmentAuth, auth.OAuthConfig{
			ClientID: cfg.GitHubOAuthClientID, ClientSecret: cfg.GitHubOAuthClientSecret,
			RedirectURL: cfg.GitHubOAuthRedirectURL, SuccessRedirect: cfg.WebBaseURL, CookieSecure: cfg.SecureCookies,
		}, cfg.GitHubWebhookSecret, attachment.StorageConfig{
			Mode: cfg.AttachmentStorage, LocalDir: cfg.AttachmentDir,
			Endpoint: cfg.AttachmentS3Endpoint, AccessKey: cfg.AttachmentS3AccessKey,
			SecretKey: cfg.AttachmentS3SecretKey, Region: cfg.AttachmentS3Region,
			Bucket: cfg.AttachmentS3Bucket, Prefix: cfg.AttachmentS3Prefix, Secure: cfg.AttachmentS3Secure,
		}, githubintegration.AppConfig{
			ID: cfg.GitHubAppID, Slug: cfg.GitHubAppSlug, PrivateKey: cfg.GitHubAppPrivateKey,
			CallbackURL: cfg.GitHubAppCallbackURL, WebBaseURL: cfg.WebBaseURL,
		})
		cancel()
		if err != nil {
			logger.Error("open database-backed application", "error", err)
			os.Exit(1)
		}
	}
	defer application.Close()
	telemetryShutdown, err := telemetry.Setup(context.Background(), telemetry.Config{Endpoint: cfg.OTELEndpoint, ServiceName: cfg.OTELServiceName})
	if err != nil {
		logger.Error("configure OpenTelemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetryShutdown(shutdownCtx); err != nil {
			logger.Error("flush OpenTelemetry", "error", err)
		}
	}()
	metrics := httpapi.NewMetrics()
	root := http.NewServeMux()
	root.Handle("/metrics", metrics.Endpoint(cfg.MetricsToken))
	protected := httpapi.WithCSRF(application.Handler, cfg.SecureCookies, cfg.WebAllowedOrigins...)
	protected = httpapi.NewRateLimiter(240, time.Minute).Middleware(protected)
	root.Handle("/", metrics.Middleware(httpapi.WithRecovery(httpapi.WithCORS(protected, cfg.WebAllowedOrigins...), logger)))
	handler := telemetry.HTTP(httpapi.WithSecurityHeaders(httpapi.WithRequestID(root)))
	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           httpapi.WithAccessLog(handler, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown api", "error", err)
		}
	}()

	logger.Info("api listening", "address", cfg.HTTPAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve api", "error", err)
		os.Exit(1)
	}
}
