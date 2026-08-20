package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/agentrun"
	"github.com/forgeflow/forgeflow/backend/internal/attachment"
	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/auth"
	"github.com/forgeflow/forgeflow/backend/internal/automation"
	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	"github.com/forgeflow/forgeflow/backend/internal/board"
	"github.com/forgeflow/forgeflow/backend/internal/customfield"
	"github.com/forgeflow/forgeflow/backend/internal/environment"
	"github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/mcp"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/planning"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	platformidempotency "github.com/forgeflow/forgeflow/backend/internal/platform/idempotency"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/tenant"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type App struct {
	Handler         http.Handler
	Ready           func(context.Context) error
	Audit           audit.Writer
	Outbox          outbox.Writer
	WorkItems       *workitem.Service
	Specifications  *specification.Service
	AgentRuns       *agentrun.Service
	Autonomous      *autonomous.Service
	Environments    *environment.Service
	Automation      *automation.Service
	Notifications   *notification.Service
	CustomFields    *customfield.Service
	Attachments     *attachment.Service
	GitHub          *github.Service
	RepositoryIndex *github.SnapshotService
	Authenticator   identity.Authenticator
	Close           func() error
}

func New(maxBodyBytes int64) *App {
	now := func() time.Time { return time.Now().UTC() }
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	specStore := specification.NewMemoryStore()
	specService := specification.NewService(specStore, now)
	workItemRepository := workitem.NewMemoryRepository(now)
	workflowService := workflow.NewService(workflow.Default())
	workflowService.SetRecorder(workflow.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, now)
	workItemService := workitem.NewService(workItemRepository, specService, workflowService, auditWriter, outboxWriter, now)
	planningService := planning.NewService(planning.NewMemoryStore(), planning.Options{Recorder: planning.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	agentRunService := agentrun.NewService(agentrun.NewMemoryStore(), workItemService, agentrun.Options{Recorder: agentrun.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Now: now})
	environmentService := environment.NewService(environment.NewMemoryStore(), environment.Options{Recorder: environment.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Now: now})
	autonomousService := autonomous.NewService(autonomous.NewMemoryStore(), workItemService, specService, agentRunService, autonomous.Options{Recorder: autonomous.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Now: now, Policy: environmentService})
	githubStore := github.NewMemoryStore()
	workItemService.SetEngineeringEvidence(githubStore)
	integrationService := github.NewService(githubStore, nil, github.AppConfig{}, auditWriter, outboxWriter, now, nil)
	snapshotService := github.NewSnapshotService(integrationService, github.NewMemorySnapshotStore())
	snapshotService.SetKnowledgeService(github.NewKnowledgeService(integrationService, github.NewMemoryKnowledgeStore()))
	integrationService.SetSnapshotService(snapshotService)
	tenantStore := tenant.NewMemoryStore()
	notificationStore := notification.NewMemoryStore(func(ctx context.Context, organizationID, projectID string) ([]string, error) {
		members, err := tenantStore.ListMembers(ctx, organizationID, projectID)
		if err != nil {
			return nil, err
		}
		users := make([]string, 0, len(members))
		for _, member := range members {
			users = append(users, member.ID)
		}
		return users, nil
	})
	notificationService := notification.NewService(notificationStore)
	automationService := automation.NewService(automation.NewMemoryStore(), notificationStore, automation.Options{Recorder: automation.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	customFieldService := customfield.NewService(customfield.NewMemoryStore(), customfield.Options{Recorder: customfield.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	attachmentService := attachment.NewService(attachment.NewMemoryStore(), attachment.NewMemoryBlobStore(), now, attachment.Options{Recorder: attachment.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	specService.SetMediaReferenceValidator(attachmentService)
	ready := func(context.Context) error { return nil }
	idempotencyStore := platformidempotency.NewMemoryStore()
	return &App{Handler: composeHandler(workItemService, specService, workflowService, auth.NewHandler(nil, nil, maxBodyBytes), tenant.NewHandler(tenant.NewService(tenantStore, tenant.Options{Recorder: tenant.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}}), maxBodyBytes), planning.NewHandler(planningService, maxBodyBytes), board.NewHandler(workItemService, workflowService), agentrun.NewHandler(agentRunService, maxBodyBytes), autonomous.NewHandler(autonomousService, maxBodyBytes), environment.NewHandler(environmentService, maxBodyBytes), environment.NewDeploymentHandler(environmentService, maxBodyBytes), github.NewWebhookHandler(github.NewMemoryWebhookStore(), "", maxBodyBytes), github.NewIntegrationHandlerWithIdempotency(integrationService, maxBodyBytes, "/", idempotencyStore, snapshotService), automation.NewHandler(automationService, maxBodyBytes), notification.NewHandler(notificationService), customfield.NewHandler(customFieldService, maxBodyBytes), attachment.NewHandler(attachmentService), audit.NewHandler(auditWriter), workitem.NewActivityHandler(workItemService, auditWriter), mcp.NewHTTPHandlerWithAutonomous(workItemService, specService, agentRunService, autonomousService, maxBodyBytes, integrationService), nil, true, maxBodyBytes, ready, idempotencyStore), Ready: ready, Audit: auditWriter, Outbox: outboxWriter, WorkItems: workItemService, Specifications: specService, AgentRuns: agentRunService, Autonomous: autonomousService, Environments: environmentService, Automation: automationService, Notifications: notificationService, CustomFields: customFieldService, Attachments: attachmentService, GitHub: integrationService, RepositoryIndex: snapshotService, Close: func() error { return nil }}
}

func NewWithDatabase(ctx context.Context, databaseURL string, maxBodyBytes int64, developmentAuth bool, oauthConfig auth.OAuthConfig, githubWebhookSecret string, attachmentConfig attachment.StorageConfig, githubAppConfigs ...github.AppConfig) (*App, error) {
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	now := func() time.Time { return time.Now().UTC() }
	auditWriter := audit.NewPostgresWriter(pool)
	outboxWriter := outbox.NewPostgresWriter(pool)
	specService := specification.NewService(specification.NewPostgresStore(pool), now, specification.Options{
		Recorder:    specification.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter},
		Transaction: db.NewTransactionRunner(pool),
	})
	workflowService := workflow.NewService(workflow.Default(), workflow.NewPostgresStore(pool))
	workflowService.SetTransaction(db.NewTransactionRunner(pool))
	workflowService.SetRecorder(workflow.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, now)
	githubStore := github.NewPostgresStore(pool)
	workItemService := workitem.NewService(workitem.NewPostgresRepository(pool), specService, workflowService, auditWriter, outboxWriter, now, db.NewTransactionRunner(pool))
	workItemService.SetEngineeringEvidence(githubStore)
	planningService := planning.NewService(planning.NewPostgresStore(pool), planning.Options{Recorder: planning.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool)})
	agentRunStore := agentrun.NewPostgresStore(pool)
	agentRunService := agentrun.NewService(agentRunStore, workItemService, agentrun.Options{Recorder: agentrun.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: now})
	environmentService := environment.NewService(environment.NewPostgresStore(pool), environment.Options{Recorder: environment.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: now})
	autonomousService := autonomous.NewService(autonomous.NewPostgresStore(pool), workItemService, specService, agentRunService, autonomous.Options{Recorder: autonomous.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: now, Policy: environmentService})
	authenticator := identity.NewPostgresAuthenticator(pool, now)
	tokenStore := identity.NewPostgresTokenStore(pool)
	oauthHandler := auth.NewOAuthHandler(identity.NewPostgresOAuthStore(pool), tokenStore, oauthConfig)
	githubAppConfig := github.AppConfig{WebBaseURL: oauthConfig.SuccessRedirect}
	if len(githubAppConfigs) > 0 {
		githubAppConfig = githubAppConfigs[0]
	}
	githubClient, err := github.NewAppClient(githubAppConfig)
	if err != nil {
		pool.Close()
		return nil, err
	}
	integrationService := github.NewService(githubStore, githubClient, githubAppConfig, auditWriter, outboxWriter, now, db.NewTransactionRunner(pool))
	snapshotService := github.NewSnapshotService(integrationService, github.NewPostgresSnapshotStore(pool))
	snapshotService.SetKnowledgeService(github.NewKnowledgeService(integrationService, github.NewPostgresKnowledgeStore(pool)))
	integrationService.SetSnapshotService(snapshotService)
	notificationStore := notification.NewPostgresStore(pool)
	notificationService := notification.NewService(notificationStore)
	automationService := automation.NewService(automation.NewPostgresStore(pool), notificationStore, automation.Options{Recorder: automation.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: now})
	customFieldService := customfield.NewService(customfield.NewPostgresStore(pool), customfield.Options{Recorder: customfield.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool), Now: now})
	blobStore, err := attachment.NewBlobStore(attachmentConfig)
	if err != nil {
		pool.Close()
		return nil, err
	}
	attachmentService := attachment.NewService(attachment.NewPostgresStore(pool), blobStore, now, attachment.Options{Recorder: attachment.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool)})
	specService.SetMediaReferenceValidator(attachmentService)
	ready := func(readyCtx context.Context) error { return db.Ready(readyCtx, pool) }
	webhookStore := github.NewPostgresWebhookStore(pool, outboxWriter)
	webhookStore.SetAgentRunProjector(agentRunStore)
	idempotencyStore := platformidempotency.NewPostgresStore(pool)
	return &App{Handler: composeHandler(workItemService, specService, workflowService, auth.NewHandlerWithOAuth(tokenStore, tokenStore, oauthHandler, maxBodyBytes), tenant.NewHandler(tenant.NewService(tenant.NewPostgresStore(pool), tenant.Options{Recorder: tenant.MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, Transaction: db.NewTransactionRunner(pool)}), maxBodyBytes), planning.NewHandler(planningService, maxBodyBytes), board.NewHandler(workItemService, workflowService), agentrun.NewHandler(agentRunService, maxBodyBytes), autonomous.NewHandler(autonomousService, maxBodyBytes), environment.NewHandler(environmentService, maxBodyBytes), environment.NewDeploymentHandler(environmentService, maxBodyBytes), github.NewWebhookHandler(webhookStore, githubWebhookSecret, maxBodyBytes), github.NewIntegrationHandlerWithIdempotency(integrationService, maxBodyBytes, oauthConfig.SuccessRedirect, idempotencyStore, snapshotService), automation.NewHandler(automationService, maxBodyBytes), notification.NewHandler(notificationService), customfield.NewHandler(customFieldService, maxBodyBytes), attachment.NewHandler(attachmentService), audit.NewHandler(auditWriter), workitem.NewActivityHandler(workItemService, auditWriter), mcp.NewHTTPHandlerWithAutonomous(workItemService, specService, agentRunService, autonomousService, maxBodyBytes, integrationService), authenticator, developmentAuth, maxBodyBytes, ready, idempotencyStore), Ready: ready, Audit: auditWriter, Outbox: outboxWriter, WorkItems: workItemService, Specifications: specService, AgentRuns: agentRunService, Autonomous: autonomousService, Environments: environmentService, Automation: automationService, Notifications: notificationService, CustomFields: customFieldService, Attachments: attachmentService, GitHub: integrationService, RepositoryIndex: snapshotService, Authenticator: authenticator, Close: func() error { pool.Close(); return nil }}, nil
}

func composeHandler(workItems *workitem.Service, specs *specification.Service, workflowService *workflow.Service, authHandler, tenantHandler, planningHandler, boardHandler, agentRunHandler, autonomousHandler, environmentHandler, deploymentHandler, webhookHandler, integrationHandler, automationHandler, notificationHandler, customFieldHandler, attachmentHandler, auditHandler, activityHandler, mcpHandler http.Handler, authenticator identity.Authenticator, developmentAuth bool, maxBodyBytes int64, ready func(context.Context) error, idempotencyStore platformidempotency.Store) http.Handler {
	api := http.NewServeMux()
	workItemAPI := workitem.NewAPIHandler(workItems, specs, maxBodyBytes, idempotencyStore)
	api.Handle("/work-items", workItemAPI)
	api.Handle("/work-items/", workItemRoutes(workItemAPI, activityHandler, customFieldHandler, attachmentHandler))
	api.Handle("/me", authHandler)
	api.Handle("/me/", authHandler)
	api.Handle("/auth/", authHandler)
	api.Handle("/organizations", tenantHandler)
	api.Handle("/organizations/", tenantHandler)
	api.Handle("/workspaces", tenantHandler)
	api.Handle("/workspaces/", tenantHandler)
	api.Handle("/projects", tenantHandler)
	api.Handle("/projects/", projectRoutes(tenantHandler, integrationHandler, automationHandler, customFieldHandler, environmentHandler))
	api.Handle("/sprints", planningHandler)
	api.Handle("/sprints/", planningHandler)
	api.Handle("/boards/", boardHandler)
	workflowHandler := workflow.NewHandler(workflowService, maxBodyBytes)
	api.Handle("/workflows", workflowHandler)
	api.Handle("/workflows/", workflowHandler)
	api.Handle("/agent-runs", agentRunHandler)
	api.Handle("/agent-runs/", agentRunHandler)
	api.Handle("/autonomous-runs", autonomousHandler)
	api.Handle("/autonomous-runs/", autonomousHandler)
	api.Handle("/deployments", deploymentHandler)
	api.Handle("/deployments/", deploymentHandler)
	api.Handle("/audit", auditHandler)
	api.Handle("/notifications", notificationHandler)
	api.Handle("/notifications/", notificationHandler)
	api.Handle("/mcp", mcpHandler)
	api.Handle("/integrations/github/webhooks", webhookHandler)
	api.Handle("/integrations/github/", integrationHandler)

	root := http.NewServeMux()
	root.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ready(readyCtx); err != nil {
			httpapi.Error(w, r, apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "service is not ready", nil))
			return
		}
		httpapi.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	protected := http.Handler(http.StripPrefix("/api/v1", api))
	if developmentAuth {
		protected = httpapi.WithDevelopmentActor(protected)
	} else {
		protected = httpapi.WithAuthenticator(protected, authenticator)
	}
	root.Handle("/api/v1/", protected)
	return httpapi.WithSecurityHeaders(httpapi.WithRequestID(root))
}

func projectRoutes(tenantHandler, integrationHandler, automationHandler, customFieldHandler, environmentHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ai-policy") || strings.Contains(r.URL.Path, "/environments") {
			environmentHandler.ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/custom-fields") {
			customFieldHandler.ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/repositories") {
			integrationHandler.ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/automation-rules") {
			automationHandler.ServeHTTP(w, r)
			return
		}
		tenantHandler.ServeHTTP(w, r)
	})
}

func workItemRoutes(workItemHandler, activityHandler, customFieldHandler, attachmentHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/activity") {
			activityHandler.ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/attachments") {
			attachmentHandler.ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/custom-fields") {
			customFieldHandler.ServeHTTP(w, r)
			return
		}
		workItemHandler.ServeHTTP(w, r)
	})
}
