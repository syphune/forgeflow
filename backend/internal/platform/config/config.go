package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddress             string
	DatabaseURL             string
	Environment             string
	MaxBodyBytes            int64
	RequestIDName           string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
	GitHubOAuthRedirectURL  string
	WebBaseURL              string
	WebAllowedOrigins       []string
	SecureCookies           bool
	GitHubWebhookSecret     string
	GitHubAppID             int64
	GitHubAppSlug           string
	GitHubAppPrivateKey     string
	GitHubAppCallbackURL    string
	MetricsToken            string
	AttachmentDir           string
	AttachmentStorage       string
	AttachmentS3Endpoint    string
	AttachmentS3AccessKey   string
	AttachmentS3SecretKey   string
	AttachmentS3Region      string
	AttachmentS3Bucket      string
	AttachmentS3Prefix      string
	AttachmentS3Secure      bool
	OTELEndpoint            string
	OTELServiceName         string
	RunnerURL               string
	RunnerToken             string
	RunnerWorkspaceRoot     string
}

func Load() (Config, error) {
	webBaseURL := value("FORGEFLOW_WEB_BASE_URL", "http://localhost:3000")
	webAllowedOrigins, err := parseWebOrigins(webBaseURL, os.Getenv("FORGEFLOW_ALLOWED_WEB_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	privateKey := strings.ReplaceAll(os.Getenv("FORGEFLOW_GITHUB_APP_PRIVATE_KEY"), `\n`, "\n")
	if privateKey == "" {
		if path := strings.TrimSpace(os.Getenv("FORGEFLOW_GITHUB_APP_PRIVATE_KEY_FILE")); path != "" {
			contents, err := os.ReadFile(path)
			if err != nil {
				return Config{}, fmt.Errorf("read FORGEFLOW_GITHUB_APP_PRIVATE_KEY_FILE: %w", err)
			}
			privateKey = string(contents)
		}
	}
	c := Config{
		HTTPAddress:             value("FORGEFLOW_HTTP_ADDRESS", ":8080"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		Environment:             value("FORGEFLOW_ENV", "development"),
		MaxBodyBytes:            1 << 20,
		RequestIDName:           "X-Request-ID",
		GitHubOAuthClientID:     value("FORGEFLOW_GITHUB_OAUTH_CLIENT_ID", ""),
		GitHubOAuthClientSecret: value("FORGEFLOW_GITHUB_OAUTH_CLIENT_SECRET", ""),
		GitHubOAuthRedirectURL:  value("FORGEFLOW_GITHUB_OAUTH_REDIRECT_URL", "http://localhost:8080/api/v1/auth/github/callback"),
		WebBaseURL:              webBaseURL,
		WebAllowedOrigins:       webAllowedOrigins,
		SecureCookies:           os.Getenv("FORGEFLOW_SECURE_COOKIES") == "true",
		GitHubWebhookSecret:     os.Getenv("FORGEFLOW_GITHUB_WEBHOOK_SECRET"),
		GitHubAppSlug:           strings.TrimSpace(os.Getenv("FORGEFLOW_GITHUB_APP_SLUG")),
		GitHubAppPrivateKey:     privateKey,
		GitHubAppCallbackURL:    value("FORGEFLOW_GITHUB_APP_CALLBACK_URL", strings.TrimRight(webBaseURL, "/")+"/api/v1/integrations/github/install/callback"),
		MetricsToken:            os.Getenv("FORGEFLOW_METRICS_TOKEN"),
		AttachmentDir:           value("FORGEFLOW_ATTACHMENTS_DIR", "./data/attachments"),
		AttachmentStorage:       value("FORGEFLOW_ATTACHMENT_STORAGE", "local"),
		AttachmentS3Endpoint:    strings.TrimSpace(os.Getenv("FORGEFLOW_ATTACHMENT_S3_ENDPOINT")),
		AttachmentS3AccessKey:   strings.TrimSpace(os.Getenv("FORGEFLOW_ATTACHMENT_S3_ACCESS_KEY")),
		AttachmentS3SecretKey:   os.Getenv("FORGEFLOW_ATTACHMENT_S3_SECRET_KEY"),
		AttachmentS3Region:      value("FORGEFLOW_ATTACHMENT_S3_REGION", "us-east-1"),
		AttachmentS3Bucket:      strings.TrimSpace(os.Getenv("FORGEFLOW_ATTACHMENT_S3_BUCKET")),
		AttachmentS3Prefix:      strings.TrimSpace(os.Getenv("FORGEFLOW_ATTACHMENT_S3_PREFIX")),
		AttachmentS3Secure:      os.Getenv("FORGEFLOW_ATTACHMENT_S3_SECURE") == "true",
		OTELEndpoint:            strings.TrimSpace(os.Getenv("FORGEFLOW_OTEL_ENDPOINT")),
		OTELServiceName:         value("FORGEFLOW_OTEL_SERVICE_NAME", "forgeflow-api"),
		RunnerURL:               strings.TrimRight(strings.TrimSpace(os.Getenv("FORGEFLOW_RUNNER_URL")), "/"),
		RunnerToken:             os.Getenv("FORGEFLOW_RUNNER_TOKEN"),
		RunnerWorkspaceRoot:     value("FORGEFLOW_RUNNER_WORKSPACE_ROOT", "/var/lib/forgeflow/workspaces"),
	}
	if raw := strings.TrimSpace(os.Getenv("FORGEFLOW_GITHUB_APP_ID")); raw != "" {
		appID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || appID <= 0 {
			return Config{}, fmt.Errorf("FORGEFLOW_GITHUB_APP_ID must be a positive integer")
		}
		c.GitHubAppID = appID
	}
	if raw := os.Getenv("FORGEFLOW_MAX_BODY_BYTES"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1024 {
			return Config{}, fmt.Errorf("FORGEFLOW_MAX_BODY_BYTES must be an integer >= 1024")
		}
		c.MaxBodyBytes = n
	}
	if !strings.EqualFold(c.AttachmentStorage, "local") && !strings.EqualFold(c.AttachmentStorage, "s3") {
		return Config{}, fmt.Errorf("FORGEFLOW_ATTACHMENT_STORAGE must be local or s3")
	}
	if strings.EqualFold(c.AttachmentStorage, "s3") && (c.AttachmentS3Endpoint == "" || c.AttachmentS3AccessKey == "" || c.AttachmentS3SecretKey == "" || c.AttachmentS3Bucket == "") {
		return Config{}, fmt.Errorf("S3 attachment storage requires endpoint, access key, secret key, and bucket")
	}
	if c.Environment == "production" {
		if c.DatabaseURL == "" {
			return Config{}, fmt.Errorf("DATABASE_URL is required in production")
		}
		if os.Getenv("FORGEFLOW_DEV_AUTH") == "true" {
			return Config{}, fmt.Errorf("FORGEFLOW_DEV_AUTH must be false in production")
		}
		if !c.SecureCookies {
			return Config{}, fmt.Errorf("FORGEFLOW_SECURE_COOKIES must be true in production")
		}
		webURL, err := url.Parse(c.WebBaseURL)
		if err != nil || webURL.Scheme == "" || webURL.Host == "" || (webURL.Scheme != "https" && webURL.Hostname() != "localhost") {
			return Config{}, fmt.Errorf("FORGEFLOW_WEB_BASE_URL must be an HTTPS origin in production")
		}
	}
	return c, nil
}

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func parseWebOrigins(webBaseURL, configured string) ([]string, error) {
	candidates := []string{webBaseURL}
	if strings.TrimSpace(configured) != "" {
		candidates = append(candidates, strings.Split(configured, ",")...)
	}

	seen := make(map[string]struct{}, len(candidates))
	origins := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		parsed, err := url.Parse(strings.TrimSpace(candidate))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("web origin is invalid: %q", candidate)
		}
		normalized := parsed.Scheme + "://" + parsed.Host
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		origins = append(origins, normalized)
	}
	return origins, nil
}
