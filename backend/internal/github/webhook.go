package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WebhookEvent struct {
	ID         string
	DeliveryID string
	EventName  string
	Payload    json.RawMessage
	Headers    map[string]string
	ReceivedAt time.Time
}

type WebhookStore interface {
	Persist(context.Context, WebhookEvent) (bool, error)
}

type AgentRunProjector interface {
	LinkPullRequest(context.Context, string, string, string, string, string, string) error
	UpdateCIRun(context.Context, string, string, string, string, string, string, string) error
}

type PostgresWebhookStore struct {
	pool      *pgxpool.Pool
	outbox    outbox.Writer
	agentRuns AgentRunProjector
}

func NewPostgresWebhookStore(pool *pgxpool.Pool, outboxWriters ...outbox.Writer) *PostgresWebhookStore {
	var writer outbox.Writer
	if len(outboxWriters) > 0 {
		writer = outboxWriters[0]
	}
	return &PostgresWebhookStore{pool: pool, outbox: writer}
}

func (s *PostgresWebhookStore) SetAgentRunProjector(projector AgentRunProjector) {
	s.agentRuns = projector
}
func (s *PostgresWebhookStore) Persist(ctx context.Context, event WebhookEvent) (bool, error) {
	headers, _ := json.Marshal(event.Headers)
	executor := platformdb.ExecutorFrom(ctx, s.pool)
	tag, err := executor.Exec(ctx, `INSERT INTO webhook_events (id,github_delivery_id,event_name,payload,headers,received_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (github_delivery_id) DO NOTHING`, event.ID, event.DeliveryID, event.EventName, event.Payload, headers, event.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("persist GitHub webhook: %w", err)
	}
	if s.outbox != nil {
		queued := event
		if tag.RowsAffected() == 0 {
			var headers []byte
			if err := executor.QueryRow(ctx, `SELECT id::text,event_name,payload,headers,received_at FROM webhook_events WHERE github_delivery_id=$1`, event.DeliveryID).Scan(&queued.ID, &queued.EventName, &queued.Payload, &headers, &queued.ReceivedAt); err != nil {
				return false, fmt.Errorf("load existing GitHub webhook: %w", err)
			}
			if err := json.Unmarshal(headers, &queued.Headers); err != nil {
				return false, fmt.Errorf("decode existing GitHub webhook headers: %w", err)
			}
		}
		var envelope webhookEnvelope
		if err := json.Unmarshal(queued.Payload, &envelope); err != nil {
			return false, fmt.Errorf("decode GitHub webhook for queueing: %w", err)
		}
		organizationID, _, scopeErr := s.repositoryScope(ctx, strings.TrimSpace(envelope.Repository.FullName))
		if scopeErr != nil {
			return false, scopeErr
		}
		if organizationID != "" {
			if err := s.outbox.Append(ctx, outbox.Event{ID: queued.ID, OrganizationID: organizationID, EventType: "github.webhook.received", AggregateType: "github_webhook", AggregateID: queued.ID, IdempotencyKey: "github.webhook:" + queued.DeliveryID, Payload: map[string]any{"delivery_id": queued.DeliveryID, "event_name": queued.EventName}, OccurredAt: queued.ReceivedAt}); err != nil {
				return false, fmt.Errorf("queue GitHub webhook: %w", err)
			}
		}
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PostgresWebhookStore) Process(ctx context.Context, eventID string) error {
	var event WebhookEvent
	var headers []byte
	var processedAt *time.Time
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text,github_delivery_id,event_name,payload,headers,received_at,processed_at FROM webhook_events WHERE id=$1`, eventID).Scan(&event.ID, &event.DeliveryID, &event.EventName, &event.Payload, &headers, &event.ReceivedAt, &processedAt)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("GitHub webhook event %s not found", eventID)
	}
	if err != nil {
		return fmt.Errorf("load GitHub webhook event: %w", err)
	}
	if processedAt != nil {
		return nil
	}
	if err := json.Unmarshal(headers, &event.Headers); err != nil {
		return fmt.Errorf("decode GitHub webhook headers: %w", err)
	}
	if err := s.project(ctx, event); err != nil {
		_, _ = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE webhook_events SET processing_error=$2 WHERE id=$1`, eventID, err.Error())
		return err
	}
	if err := s.emitAutomationEvent(ctx, event); err != nil {
		_, _ = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE webhook_events SET processing_error=$2 WHERE id=$1`, eventID, err.Error())
		return err
	}
	if _, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE webhook_events SET processed_at=now(), processing_error=NULL WHERE id=$1 AND processed_at IS NULL`, eventID); err != nil {
		return fmt.Errorf("mark GitHub webhook processed: %w", err)
	}
	return nil
}

func (s *PostgresWebhookStore) emitAutomationEvent(ctx context.Context, event WebhookEvent) error {
	if s.outbox == nil {
		return nil
	}
	eventType := webhookAutomationEvent(event.EventName)
	if eventType == "" {
		return nil
	}
	var envelope webhookEnvelope
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return fmt.Errorf("decode GitHub webhook automation payload: %w", err)
	}
	organizationID, repositoryID, err := s.repositoryScope(ctx, strings.TrimSpace(envelope.Repository.FullName))
	if err != nil {
		return err
	}
	if organizationID == "" || repositoryID == "" {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.outbox.Append(ctx, outbox.Event{
		ID:             id,
		OrganizationID: organizationID,
		EventType:      eventType,
		AggregateType:  "github",
		AggregateID:    repositoryID,
		IdempotencyKey: "github.automation:" + event.ID + ":" + eventType,
		Payload:        map[string]any{"delivery_id": event.DeliveryID, "event_name": event.EventName, "repository_id": repositoryID},
		OccurredAt:     event.ReceivedAt,
	})
}

func webhookAutomationEvent(eventName string) string {
	switch strings.TrimSpace(eventName) {
	case "push":
		return "github.push"
	case "pull_request":
		return "github.pull_request.updated"
	case "workflow_run", "check_run", "check_suite":
		return "github.ci.updated"
	default:
		return ""
	}
}

type webhookRepository struct {
	FullName string `json:"full_name"`
}

type webhookEnvelope struct {
	Repository webhookRepository `json:"repository"`
}

type pushPayload struct {
	webhookEnvelope
	Ref        string `json:"ref"`
	After      string `json:"after"`
	HeadCommit *struct {
		Timestamp string `json:"timestamp"`
	} `json:"head_commit"`
	Commits []struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"author"`
	} `json:"commits"`
}

type pullRequestPayload struct {
	webhookEnvelope
	Number      int64 `json:"number"`
	PullRequest struct {
		Title     string `json:"title"`
		State     string `json:"state"`
		Draft     bool   `json:"draft"`
		HTMLURL   string `json:"html_url"`
		UpdatedAt string `json:"updated_at"`
		Head      struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Body string `json:"body"`
	} `json:"pull_request"`
}

type ciPayload struct {
	webhookEnvelope
	WorkflowRun *struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		HTMLURL    string `json:"html_url"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"workflow_run"`
	CheckRun *struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		HTMLURL    string `json:"html_url"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"check_run"`
	CheckSuite *struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		HTMLURL    string `json:"html_url"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"check_suite"`
}

func (s *PostgresWebhookStore) project(ctx context.Context, event WebhookEvent) error {
	var envelope webhookEnvelope
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return fmt.Errorf("decode GitHub webhook: %w", err)
	}
	fullName := strings.TrimSpace(envelope.Repository.FullName)
	if fullName == "" {
		return nil
	}
	organizationID, repositoryID, err := s.repositoryScope(ctx, fullName)
	if err != nil {
		return err
	}
	if repositoryID == "" {
		// The App can receive an event before the repository has been synced. It
		// is safe to acknowledge it; the next repository refresh will reconcile
		// the current GitHub state.
		return nil
	}

	switch event.EventName {
	case "push":
		var payload pushPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode push webhook: %w", err)
		}
		branch := strings.TrimPrefix(strings.TrimSpace(payload.Ref), "refs/heads/")
		if branch == strings.TrimSpace(payload.Ref) || branch == "" || strings.TrimSpace(payload.After) == "" {
			return nil
		}
		updatedAt := event.ReceivedAt
		if payload.HeadCommit != nil {
			updatedAt = webhookTime(payload.HeadCommit.Timestamp, updatedAt)
		}
		for _, commit := range payload.Commits {
			candidate := webhookTime(commit.Timestamp, updatedAt)
			if candidate.After(updatedAt) {
				updatedAt = candidate
			}
		}
		_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO branches (organization_id, repository_id, name, head_sha, updated_at)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (organization_id, repository_id, name)
DO UPDATE SET head_sha=EXCLUDED.head_sha, updated_at=EXCLUDED.updated_at
WHERE branches.updated_at <= EXCLUDED.updated_at
`, organizationID, repositoryID, branch, strings.TrimSpace(payload.After), updatedAt)
		if err != nil {
			break
		}
		for _, commit := range payload.Commits {
			if strings.TrimSpace(commit.ID) == "" {
				continue
			}
			committedAt := time.Now().UTC()
			if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(commit.Timestamp)); parseErr == nil {
				committedAt = parsed.UTC()
			}
			author := strings.TrimSpace(commit.Author.Username)
			if author == "" {
				author = strings.TrimSpace(commit.Author.Name)
			}
			_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO commits (organization_id, repository_id, sha, message, author_login, committed_at)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (organization_id, repository_id, sha)
DO UPDATE SET message=EXCLUDED.message, author_login=EXCLUDED.author_login, committed_at=EXCLUDED.committed_at
`, organizationID, repositoryID, strings.TrimSpace(commit.ID), commit.Message, author, committedAt)
			if err != nil {
				break
			}
		}
	case "pull_request":
		var payload pullRequestPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode pull request webhook: %w", err)
		}
		if payload.Number <= 0 {
			return nil
		}
		workItemID, err := s.workItemID(ctx, organizationID, repositoryID, payload.PullRequest.Title, payload.PullRequest.Body, payload.PullRequest.Head.Ref)
		if err != nil {
			return err
		}
		updatedAt := webhookTime(payload.PullRequest.UpdatedAt, event.ReceivedAt)
		_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO pull_requests (organization_id, repository_id, work_item_id, number, title, state, draft, head_sha, head_ref, body, url, updated_at)
VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (organization_id, repository_id, number)
DO UPDATE SET work_item_id=COALESCE(EXCLUDED.work_item_id,pull_requests.work_item_id), title=EXCLUDED.title, state=EXCLUDED.state, draft=EXCLUDED.draft, head_sha=EXCLUDED.head_sha, head_ref=EXCLUDED.head_ref, body=EXCLUDED.body, url=EXCLUDED.url, updated_at=EXCLUDED.updated_at
WHERE pull_requests.updated_at <= EXCLUDED.updated_at
`, organizationID, repositoryID, workItemID, payload.Number, payload.PullRequest.Title, payload.PullRequest.State, payload.PullRequest.Draft, payload.PullRequest.Head.SHA, payload.PullRequest.Head.Ref, payload.PullRequest.Body, payload.PullRequest.HTMLURL, updatedAt)
		if err == nil && s.agentRuns != nil {
			var pullRequestID, storedWorkItemID, storedHeadRef, storedHeadSHA string
			err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text, COALESCE(work_item_id::text,''), head_ref, head_sha FROM pull_requests WHERE organization_id=$1 AND repository_id=$2 AND number=$3`, organizationID, repositoryID, payload.Number).Scan(&pullRequestID, &storedWorkItemID, &storedHeadRef, &storedHeadSHA)
			if err == nil && strings.TrimSpace(storedWorkItemID) != "" {
				err = s.agentRuns.LinkPullRequest(ctx, organizationID, repositoryID, storedWorkItemID, storedHeadRef, storedHeadSHA, pullRequestID)
			}
		}
	case "workflow_run", "check_run", "check_suite":
		var payload ciPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode CI webhook: %w", err)
		}
		var id int64
		var status, conclusion, sha, url, prefix, updatedAtValue string
		switch event.EventName {
		case "workflow_run":
			if payload.WorkflowRun == nil {
				return nil
			}
			id, status, conclusion, sha, url, prefix, updatedAtValue = payload.WorkflowRun.ID, payload.WorkflowRun.Status, payload.WorkflowRun.Conclusion, payload.WorkflowRun.HeadSHA, payload.WorkflowRun.HTMLURL, "workflow", payload.WorkflowRun.UpdatedAt
		case "check_run":
			if payload.CheckRun == nil {
				return nil
			}
			id, status, conclusion, sha, url, prefix, updatedAtValue = payload.CheckRun.ID, payload.CheckRun.Status, payload.CheckRun.Conclusion, payload.CheckRun.HeadSHA, payload.CheckRun.HTMLURL, "check-run", payload.CheckRun.UpdatedAt
		case "check_suite":
			if payload.CheckSuite == nil {
				return nil
			}
			id, status, conclusion, sha, url, prefix, updatedAtValue = payload.CheckSuite.ID, payload.CheckSuite.Status, payload.CheckSuite.Conclusion, payload.CheckSuite.HeadSHA, payload.CheckSuite.HTMLURL, "check-suite", payload.CheckSuite.UpdatedAt
		}
		if id <= 0 {
			return nil
		}
		updatedAt := webhookTime(updatedAtValue, event.ReceivedAt)
		_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO ci_runs (organization_id, repository_id, external_id, status, conclusion, sha, url, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (organization_id, repository_id, external_id)
DO UPDATE SET status=EXCLUDED.status, conclusion=EXCLUDED.conclusion, sha=EXCLUDED.sha, url=EXCLUDED.url, updated_at=EXCLUDED.updated_at
WHERE ci_runs.updated_at <= EXCLUDED.updated_at
`, organizationID, repositoryID, prefix+":"+strconv.FormatInt(id, 10), status, conclusion, sha, url, updatedAt)
		if err == nil && s.agentRuns != nil {
			var storedStatus, storedConclusion, storedSHA, storedURL, storedExternalID string
			storedExternalID = prefix + ":" + strconv.FormatInt(id, 10)
			err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT status, conclusion, sha, url FROM ci_runs WHERE organization_id=$1 AND repository_id=$2 AND external_id=$3`, organizationID, repositoryID, storedExternalID).Scan(&storedStatus, &storedConclusion, &storedSHA, &storedURL)
			if err == nil && strings.TrimSpace(storedSHA) != "" {
				err = s.agentRuns.UpdateCIRun(ctx, organizationID, repositoryID, storedSHA, storedExternalID, storedStatus, storedConclusion, storedURL)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("project GitHub %s webhook: %w", event.EventName, err)
	}
	return nil
}

func webhookTime(value string, fallback time.Time) time.Time {
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
		return parsed.UTC()
	}
	return fallback.UTC()
}

func (s *PostgresWebhookStore) repositoryScope(ctx context.Context, fullName string) (string, string, error) {
	var organizationID, repositoryID string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT organization_id::text, id::text FROM repositories WHERE lower(full_name)=lower($1) ORDER BY id LIMIT 1`, fullName).Scan(&organizationID, &repositoryID)
	if err == pgx.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("find GitHub webhook repository: %w", err)
	}
	return organizationID, repositoryID, nil
}

var workItemKeyPattern = regexp.MustCompile(`(?i)\b[A-Z][A-Z0-9_-]{1,31}-[0-9]+\b`)

func (s *PostgresWebhookStore) workItemID(ctx context.Context, organizationID, repositoryID string, texts ...string) (string, error) {
	keys := make([]string, 0)
	seen := make(map[string]struct{})
	for _, text := range texts {
		for _, key := range workItemKeyPattern.FindAllString(text, -1) {
			key = strings.ToUpper(key)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return "", nil
	}
	var id string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT wi.id::text
FROM work_items wi
JOIN projects p ON p.organization_id=wi.organization_id AND p.id=wi.project_id
WHERE wi.organization_id=$1
  AND (wi.repository_id=$2 OR EXISTS (
      SELECT 1 FROM repository_links rl
      WHERE rl.organization_id=wi.organization_id
        AND rl.project_id=wi.project_id
        AND rl.repository_id=$2
  ))
  AND upper(p.key || '-' || wi.number) = ANY($3)
ORDER BY wi.updated_at DESC
LIMIT 1
`, organizationID, repositoryID, keys).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find work item from GitHub key: %w", err)
	}
	return id, nil
}

type MemoryWebhookStore struct {
	mu     sync.Mutex
	events map[string]WebhookEvent
}

func NewMemoryWebhookStore() *MemoryWebhookStore {
	return &MemoryWebhookStore{events: make(map[string]WebhookEvent)}
}
func (s *MemoryWebhookStore) Persist(_ context.Context, event WebhookEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.events[event.DeliveryID]; ok {
		return false, nil
	}
	s.events[event.DeliveryID] = event
	return true, nil
}

type WebhookHandler struct {
	store   WebhookStore
	secret  string
	maxBody int64
}

func NewWebhookHandler(store WebhookStore, secret string, maxBody int64) http.Handler {
	return &WebhookHandler{store: store, secret: secret, maxBody: maxBody}
}
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, http.StatusMethodNotAllowed, "method not allowed", nil))
		return
	}
	if strings.TrimSpace(h.secret) == "" {
		httpapi.Error(w, r, apperr.New(apperr.CodeInternal, 503, "GitHub webhook secret is not configured", nil))
		return
	}
	delivery := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	eventName := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	signature := strings.TrimSpace(r.Header.Get("X-Hub-Signature-256"))
	if delivery == "" || eventName == "" || len(delivery) > 200 {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 400, "GitHub webhook headers are invalid", nil))
		return
	}
	body, err := readBody(r, h.maxBody)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if !validSignature(body, h.secret, signature) {
		httpapi.Error(w, r, apperr.New(apperr.CodeUnauthorized, 401, "GitHub webhook signature is invalid", nil))
		return
	}
	if !json.Valid(body) {
		httpapi.Error(w, r, apperr.New(apperr.CodeInvalidArgument, 400, "GitHub webhook payload is invalid JSON", nil))
		return
	}
	id, err := ids.New()
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	headers := map[string]string{"x-github-delivery": delivery, "x-github-event": eventName, "x-hub-signature-256": signature}
	inserted, err := h.store.Persist(r.Context(), WebhookEvent{ID: id, DeliveryID: delivery, EventName: eventName, Payload: append([]byte(nil), body...), Headers: headers, ReceivedAt: time.Now().UTC()})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpStatus := http.StatusAccepted
	if !inserted {
		httpStatus = http.StatusAccepted
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "duplicate": !inserted})
}

func validSignature(body []byte, secret, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	expectedHex := strings.TrimPrefix(signature, "sha256=")
	expected, err := hex.DecodeString(expectedHex)
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(expected, mac.Sum(nil))
}
func readBody(r *http.Request, maxBody int64) ([]byte, error) {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return nil, apperr.New(apperr.CodeInvalidArgument, 413, "webhook body exceeds limit", nil)
	}
	if int64(len(body)) > maxBody {
		return nil, apperr.New(apperr.CodeInvalidArgument, 413, "webhook body exceeds limit", nil)
	}
	return body, nil
}

var _ WebhookStore = (*PostgresWebhookStore)(nil)
var _ WebhookStore = (*MemoryWebhookStore)(nil)
