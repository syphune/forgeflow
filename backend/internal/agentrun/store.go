package agentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Create(context.Context, string, CreateInput) (Run, error)
	List(context.Context, string, string, string) ([]Run, error)
	Get(context.Context, string, string, string) (Run, []Step, []Artifact, error)
	Approve(context.Context, string, string, string, string) error
	Transition(context.Context, string, string, Status, Status) (Run, error)
	Heartbeat(context.Context, string, string, time.Time) (Run, error)
	ReconcileStale(context.Context, time.Time) ([]Run, error)
	UpdateResult(context.Context, string, string, ResultInput) (Run, error)
	UpdateTestResults(context.Context, string, string, TestResultSet) (Run, error)
	AddStep(context.Context, string, string, Step) (Step, error)
	AddArtifact(context.Context, string, string, Artifact) (Artifact, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

const runColumns = `id::text, organization_id::text, project_id::text, work_item_id::text, COALESCE(repository_id::text,''), agent_provider, agent_name, model, base_sha, branch, status, started_at, finished_at, COALESCE(commit_sha,''), COALESCE(pull_request_id::text,''), COALESCE(result,'{}'), COALESCE(metadata,'{}'), COALESCE(execution_policy,'{}'), approval_fingerprint_version, COALESCE(approval_fingerprint,''), last_heartbeat_at, COALESCE(interruption_reason,''), created_at`

const runSelect = `SELECT ` + runColumns + ` FROM agent_runs`

type rowScanner interface {
	Scan(...any) error
}

func scanRun(row rowScanner) (Run, error) {
	var result Run
	var rawResult, rawMetadata, rawExecutionPolicy []byte
	if err := row.Scan(
		&result.ID,
		&result.OrganizationID,
		&result.ProjectID,
		&result.WorkItemID,
		&result.RepositoryID,
		&result.AgentProvider,
		&result.AgentName,
		&result.Model,
		&result.BaseSHA,
		&result.Branch,
		&result.Status,
		&result.StartedAt,
		&result.FinishedAt,
		&result.CommitSHA,
		&result.PullRequestID,
		&rawResult,
		&rawMetadata,
		&rawExecutionPolicy,
		&result.ApprovalFingerprintVersion,
		&result.ApprovalFingerprint,
		&result.LastHeartbeatAt,
		&result.InterruptionReason,
		&result.CreatedAt,
	); err != nil {
		return Run{}, err
	}
	_ = json.Unmarshal(rawResult, &result.Result)
	_ = json.Unmarshal(rawMetadata, &result.Metadata)
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if err := decodeExecutionPolicy(rawExecutionPolicy, &result); err != nil {
		return Run{}, err
	}
	if result.ApprovalFingerprintVersion == 0 {
		// Compatibility window: legacy runs have no execution payload. Reconstruct
		// the versioned fingerprint in memory until those rows are naturally retired.
		result.ApprovalFingerprintVersion = ApprovalFingerprintVersion
		fingerprint, err := fingerprintForRun(result)
		if err != nil {
			return Run{}, fmt.Errorf("compute legacy AgentRun fingerprint: %w", err)
		}
		result.ApprovalFingerprint = fingerprint
	}
	return result, nil
}

func (s *PostgresStore) loadApproval(ctx context.Context, organizationID, runID string, run *Run) error {
	if err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agent_run_approvals WHERE organization_id=$1 AND run_id=$2 AND action='approve')`, organizationID, runID).Scan(&run.Approved); err != nil {
		return fmt.Errorf("load AgentRun approval: %w", err)
	}
	return nil
}

func (s *PostgresStore) Create(ctx context.Context, organizationID string, input CreateInput) (Run, error) {
	id, err := ids.New()
	if err != nil {
		return Run{}, err
	}
	executionPolicy, err := encodeExecutionPolicy(input.ExecutionInputs, input.ExecutionPolicy)
	if err != nil {
		return Run{}, fmt.Errorf("encode AgentRun execution policy: %w", err)
	}
	fingerprint, err := approvalFingerprint(input)
	if err != nil {
		return Run{}, fmt.Errorf("compute AgentRun approval fingerprint: %w", err)
	}
	metadata := []byte(`{}`)
	row := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO agent_runs (id, organization_id, project_id, work_item_id, repository_id, agent_provider, agent_name, model, base_sha, branch, status, metadata, approval_fingerprint_version, approval_fingerprint, execution_policy) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING `+runColumns, id, organizationID, input.ProjectID, input.WorkItemID, input.RepositoryID, input.AgentProvider, input.AgentName, input.Model, input.BaseSHA, input.Branch, Queued, metadata, ApprovalFingerprintVersion, fingerprint, executionPolicy)
	result, err := scanRun(row)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "agent_runs_one_active_code_change") {
			return Run{}, apperr.New(apperr.CodeConflict, 409, "an active code-changing AgentRun already exists for this work item", nil)
		}
		return Run{}, fmt.Errorf("create AgentRun: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) Get(ctx context.Context, organizationID, projectID, id string) (Run, []Step, []Artifact, error) {
	result, err := s.getRun(ctx, organizationID, projectID, id)
	if err != nil {
		return Run{}, nil, nil, err
	}
	steps, err := s.listSteps(ctx, organizationID, id)
	if err != nil {
		return Run{}, nil, nil, err
	}
	artifacts, err := s.listArtifacts(ctx, organizationID, id)
	if err != nil {
		return Run{}, nil, nil, err
	}
	return result, steps, artifacts, nil
}

func (s *PostgresStore) getRun(ctx context.Context, organizationID, projectID, id string) (Run, error) {
	result, err := scanRun(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, runSelect+` WHERE organization_id=$1 AND ($2='' OR project_id=$2) AND id=$3`, organizationID, projectID, id))
	if err == pgx.ErrNoRows {
		return Run{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if err != nil {
		return Run{}, fmt.Errorf("get AgentRun: %w", err)
	}
	if err := s.loadApproval(ctx, organizationID, id, &result); err != nil {
		return Run{}, err
	}
	return result, nil
}

func (s *PostgresStore) List(ctx context.Context, organizationID, projectID, workItemID string) ([]Run, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, runSelect+` WHERE organization_id=$1 AND project_id=$2 AND ($3='' OR work_item_id=$3) ORDER BY created_at DESC, id DESC`, organizationID, projectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list AgentRuns: %w", err)
	}
	defer rows.Close()
	result := make([]Run, 0)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan AgentRun: %w", err)
		}
		if err := s.loadApproval(ctx, organizationID, item.ID, &item); err != nil {
			return nil, fmt.Errorf("load AgentRun approval: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) Approve(ctx context.Context, organizationID, runID, approverID, action string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO agent_run_approvals (organization_id,run_id,approver_id,action) VALUES ($1,$2,$3,$4) ON CONFLICT (run_id,action) DO NOTHING`, organizationID, runID, approverID, action)
	if err != nil {
		return fmt.Errorf("approve AgentRun: %w", err)
	}
	return nil
}

func (s *PostgresStore) Transition(ctx context.Context, organizationID, runID string, from, to Status) (Run, error) {
	result, err := scanRun(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE agent_runs SET status=$1, started_at=CASE WHEN $1='PREPARING' AND ($4='INTERRUPTED' OR started_at IS NULL) THEN now() ELSE started_at END, finished_at=CASE WHEN $1 IN ('COMPLETED','FAILED','CANCELLED') THEN now() WHEN $1='PREPARING' AND $4='INTERRUPTED' THEN NULL ELSE finished_at END, last_heartbeat_at=CASE WHEN $1='PREPARING' AND $4='INTERRUPTED' THEN NULL ELSE last_heartbeat_at END, interruption_reason=CASE WHEN $1='PREPARING' AND $4='INTERRUPTED' THEN '' ELSE interruption_reason END WHERE organization_id=$2 AND id=$3 AND status=$4 RETURNING `+runColumns, to, organizationID, runID, from))
	if err == pgx.ErrNoRows {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "AgentRun state is stale or transition is invalid", nil)
	}
	if err != nil {
		return Run{}, fmt.Errorf("transition AgentRun: %w", err)
	}
	if err := s.loadApproval(ctx, organizationID, runID, &result); err != nil {
		return Run{}, err
	}
	return result, nil
}

func (s *PostgresStore) Heartbeat(ctx context.Context, organizationID, runID string, now time.Time) (Run, error) {
	result, err := scanRun(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE agent_runs SET last_heartbeat_at=$3 WHERE organization_id=$1 AND id=$2 AND status IN ('PREPARING','PLANNING','INVESTIGATING','IMPLEMENTING','TESTING','REVIEWING') RETURNING `+runColumns, organizationID, runID, now.UTC()))
	if err == pgx.ErrNoRows {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "AgentRun is not running", nil)
	}
	if err != nil {
		return Run{}, fmt.Errorf("heartbeat AgentRun: %w", err)
	}
	if err := s.loadApproval(ctx, organizationID, runID, &result); err != nil {
		return Run{}, err
	}
	return result, nil
}

func (s *PostgresStore) ReconcileStale(ctx context.Context, now time.Time) ([]Run, error) {
	deadline := now.UTC().Add(-HeartbeatDeadline)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `UPDATE agent_runs SET status='INTERRUPTED', finished_at=$1, interruption_reason='heartbeat timeout' WHERE status IN ('PREPARING','PLANNING','INVESTIGATING','IMPLEMENTING','TESTING','REVIEWING') AND COALESCE(last_heartbeat_at, started_at, created_at) <= $2 RETURNING `+runColumns, now.UTC(), deadline)
	if err != nil {
		return nil, fmt.Errorf("reconcile stale AgentRuns: %w", err)
	}
	defer rows.Close()
	result := make([]Run, 0)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan interrupted AgentRun: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) UpdateResult(ctx context.Context, organizationID, runID string, input ResultInput) (Run, error) {
	var resultJSON, metadataJSON []byte
	var err error
	if input.Result != nil {
		resultJSON, err = json.Marshal(input.Result)
		if err != nil {
			return Run{}, fmt.Errorf("encode AgentRun result: %w", err)
		}
	}
	if input.Metadata != nil {
		metadataJSON, err = json.Marshal(input.Metadata)
		if err != nil {
			return Run{}, fmt.Errorf("encode AgentRun metadata: %w", err)
		}
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
UPDATE agent_runs
SET commit_sha=CASE WHEN NULLIF($3,'') IS NULL THEN commit_sha ELSE $3 END,
    pull_request_id=CASE WHEN NULLIF($4,'') IS NULL THEN pull_request_id ELSE $4::uuid END,
    result=COALESCE($5::jsonb,result),
    error=COALESCE($6,error),
    metadata=metadata || COALESCE($7::jsonb,'{}'::jsonb)
WHERE organization_id=$1 AND id=$2
`, organizationID, runID, input.CommitSHA, input.PullRequestID, resultJSON, input.Error, metadataJSON)
	if err != nil {
		return Run{}, fmt.Errorf("update AgentRun result: %w", err)
	}
	return s.getRun(ctx, organizationID, "", runID)
}

func (s *PostgresStore) UpdateTestResults(ctx context.Context, organizationID, runID string, results TestResultSet) (Run, error) {
	casesJSON, err := json.Marshal(results.Cases)
	if err != nil {
		return Run{}, fmt.Errorf("encode AgentRun test results: %w", err)
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
UPDATE agent_runs
SET result=COALESCE(result,'{}'::jsonb) || jsonb_build_object('test_cases',$3::jsonb,'test_review_note',$4::text)
WHERE organization_id=$1 AND id=$2
`, organizationID, runID, casesJSON, results.ReviewNote)
	if err != nil {
		return Run{}, fmt.Errorf("update AgentRun test results: %w", err)
	}
	return s.getRun(ctx, organizationID, "", runID)
}

func (s *PostgresStore) LinkPullRequest(ctx context.Context, organizationID, repositoryID, workItemID, branch, commitSHA, pullRequestID string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
UPDATE agent_runs
SET pull_request_id=$5::uuid,
    commit_sha=CASE WHEN NULLIF($4,'') IS NULL THEN commit_sha ELSE $4 END,
    metadata=metadata || jsonb_build_object('github_pull_request_synced_at', now())
WHERE id=(
    SELECT id FROM agent_runs
    WHERE organization_id=$1 AND repository_id=$2 AND work_item_id=$3
      AND ($6='' OR branch=$6)
    ORDER BY created_at DESC, id DESC
    LIMIT 1
)
`, organizationID, repositoryID, workItemID, commitSHA, pullRequestID, branch)
	if err != nil {
		return fmt.Errorf("link AgentRun pull request: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateCIRun(ctx context.Context, organizationID, repositoryID, commitSHA, externalID, status, conclusion, url string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
UPDATE agent_runs
SET metadata=metadata || jsonb_build_object(
    'ci_external_id', $4,
    'ci_status', $5,
    'ci_conclusion', $6,
    'ci_url', $7,
    'ci_updated_at', now()
)
WHERE organization_id=$1 AND repository_id=$2 AND commit_sha=$3
`, organizationID, repositoryID, commitSHA, externalID, status, conclusion, url)
	if err != nil {
		return fmt.Errorf("update AgentRun CI result: %w", err)
	}
	return nil
}

func (s *PostgresStore) AddStep(ctx context.Context, organizationID, runID string, step Step) (Step, error) {
	commands, _ := json.Marshal(step.Commands)
	tests, _ := json.Marshal(step.Tests)
	metadata, _ := json.Marshal(step.Metadata)
	if step.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Step{}, err
		}
		step.ID = id
	}
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO agent_run_steps (id,organization_id,run_id,sequence,phase,status,summary,files_read,files_modified,commands,tests,metadata,started_at,finished_at) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14 WHERE EXISTS (SELECT 1 FROM agent_runs WHERE organization_id=$2 AND id=$3) RETURNING id::text,run_id::text,sequence,phase,status,summary,files_read,files_modified,commands,tests,metadata,started_at,finished_at`, step.ID, organizationID, runID, step.Sequence, step.Phase, step.Status, step.Summary, step.FilesRead, step.FilesModified, commands, tests, metadata, step.StartedAt, step.FinishedAt).Scan(&step.ID, &step.RunID, &step.Sequence, &step.Phase, &step.Status, &step.Summary, &step.FilesRead, &step.FilesModified, &commands, &tests, &metadata, &step.StartedAt, &step.FinishedAt)
	if err == pgx.ErrNoRows {
		return Step{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if err != nil {
		return Step{}, fmt.Errorf("attach AgentRun step: %w", err)
	}
	_ = json.Unmarshal(commands, &step.Commands)
	_ = json.Unmarshal(tests, &step.Tests)
	_ = json.Unmarshal(metadata, &step.Metadata)
	return step, nil
}

func (s *PostgresStore) AddArtifact(ctx context.Context, organizationID, runID string, artifact Artifact) (Artifact, error) {
	if artifact.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Artifact{}, err
		}
		artifact.ID = id
	}
	metadata, _ := json.Marshal(artifact.Metadata)
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO agent_run_artifacts (id,organization_id,run_id,artifact_type,name,content_hash,size_bytes,object_key,metadata) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9 WHERE EXISTS (SELECT 1 FROM agent_runs WHERE organization_id=$2 AND id=$3) RETURNING id::text,run_id::text,artifact_type,name,content_hash,size_bytes,object_key,metadata,created_at`, artifact.ID, organizationID, runID, artifact.ArtifactType, artifact.Name, artifact.ContentHash, artifact.SizeBytes, artifact.ObjectKey, metadata).Scan(&artifact.ID, &artifact.RunID, &artifact.ArtifactType, &artifact.Name, &artifact.ContentHash, &artifact.SizeBytes, &artifact.ObjectKey, &metadata, &artifact.CreatedAt)
	if err == pgx.ErrNoRows {
		return Artifact{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("attach AgentRun artifact: %w", err)
	}
	_ = json.Unmarshal(metadata, &artifact.Metadata)
	return artifact, nil
}

func (s *PostgresStore) listSteps(ctx context.Context, organizationID, runID string) ([]Step, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text,run_id::text,sequence,phase,status,summary,files_read,files_modified,commands,tests,metadata,started_at,finished_at FROM agent_run_steps WHERE organization_id=$1 AND run_id=$2 ORDER BY sequence`, organizationID, runID)
	if err != nil {
		return nil, fmt.Errorf("list AgentRun steps: %w", err)
	}
	defer rows.Close()
	var result []Step
	for rows.Next() {
		var item Step
		var commands, tests, metadata []byte
		if err := rows.Scan(&item.ID, &item.RunID, &item.Sequence, &item.Phase, &item.Status, &item.Summary, &item.FilesRead, &item.FilesModified, &commands, &tests, &metadata, &item.StartedAt, &item.FinishedAt); err != nil {
			return nil, fmt.Errorf("scan AgentRun step: %w", err)
		}
		_ = json.Unmarshal(commands, &item.Commands)
		_ = json.Unmarshal(tests, &item.Tests)
		_ = json.Unmarshal(metadata, &item.Metadata)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *PostgresStore) listArtifacts(ctx context.Context, organizationID, runID string) ([]Artifact, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text,run_id::text,artifact_type,name,content_hash,size_bytes,object_key,metadata,created_at FROM agent_run_artifacts WHERE organization_id=$1 AND run_id=$2 ORDER BY created_at,id`, organizationID, runID)
	if err != nil {
		return nil, fmt.Errorf("list AgentRun artifacts: %w", err)
	}
	defer rows.Close()
	var result []Artifact
	for rows.Next() {
		var item Artifact
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.RunID, &item.ArtifactType, &item.Name, &item.ContentHash, &item.SizeBytes, &item.ObjectKey, &metadata, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan AgentRun artifact: %w", err)
		}
		_ = json.Unmarshal(metadata, &item.Metadata)
		result = append(result, item)
	}
	return result, rows.Err()
}

type MemoryStore struct {
	mu        sync.Mutex
	runs      map[string]Run
	steps     map[string][]Step
	artifacts map[string][]Artifact
	approvals map[string]bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]Run), steps: make(map[string][]Step), artifacts: make(map[string][]Artifact), approvals: make(map[string]bool)}
}
func (s *MemoryStore) Create(_ context.Context, organizationID string, input CreateInput) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.OrganizationID == organizationID && r.WorkItemID == input.WorkItemID && isActive(r.Status) {
			return Run{}, apperr.New(apperr.CodeConflict, 409, "an active code-changing AgentRun already exists for this work item", nil)
		}
	}
	id, err := ids.New()
	if err != nil {
		return Run{}, err
	}
	fingerprint, err := approvalFingerprint(input)
	if err != nil {
		return Run{}, err
	}
	item := Run{ID: id, OrganizationID: organizationID, ProjectID: input.ProjectID, WorkItemID: input.WorkItemID, RepositoryID: input.RepositoryID, AgentProvider: input.AgentProvider, AgentName: input.AgentName, Model: input.Model, BaseSHA: input.BaseSHA, Branch: input.Branch, ExecutionInputs: normalizeExecutionInputs(input.ExecutionInputs), ExecutionPolicy: normalizeMap(input.ExecutionPolicy), ApprovalFingerprintVersion: ApprovalFingerprintVersion, ApprovalFingerprint: fingerprint, Status: Queued, Metadata: map[string]any{}, CreatedAt: time.Now().UTC()}
	s.runs[id] = item
	return item, nil
}
func (s *MemoryStore) Get(_ context.Context, organizationID, projectID, id string) (Run, []Step, []Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return Run{}, nil, nil, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	item.Approved = s.approvals[id]
	return item, append([]Step(nil), s.steps[id]...), append([]Artifact(nil), s.artifacts[id]...), nil
}
func (s *MemoryStore) List(_ context.Context, organizationID, projectID, workItemID string) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Run, 0)
	for _, item := range s.runs {
		if item.OrganizationID != organizationID || item.ProjectID != projectID || (workItemID != "" && item.WorkItemID != workItemID) {
			continue
		}
		item.Approved = s.approvals[item.ID]
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}
func (s *MemoryStore) Approve(_ context.Context, organizationID, runID, approverID, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID {
		return apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	s.approvals[runID] = true
	return nil
}
func (s *MemoryStore) Transition(_ context.Context, organizationID, runID string, from, to Status) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID || item.Status != from {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "AgentRun state is stale or transition is invalid", nil)
	}
	item.Status = to
	item.Approved = s.approvals[runID]
	now := time.Now().UTC()
	if to == Preparing && (item.StartedAt == nil || from == Interrupted) {
		item.StartedAt = &now
	}
	if to == Preparing && from == Interrupted {
		item.FinishedAt = nil
		item.LastHeartbeatAt = nil
		item.InterruptionReason = ""
	}
	if to == Completed || to == Failed || to == Cancelled {
		item.FinishedAt = &now
	}
	s.runs[runID] = item
	return item, nil
}

func (s *MemoryStore) Heartbeat(_ context.Context, organizationID, runID string, now time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID {
		return Run{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if !isActive(item.Status) || item.Status == Queued {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "AgentRun is not running", map[string]any{"status": item.Status})
	}
	value := now.UTC()
	item.LastHeartbeatAt = &value
	s.runs[runID] = item
	return item, nil
}

func (s *MemoryStore) ReconcileStale(_ context.Context, now time.Time) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deadline := now.UTC().Add(-HeartbeatDeadline)
	result := make([]Run, 0)
	for id, item := range s.runs {
		if !isActive(item.Status) || item.Status == Queued {
			continue
		}
		last := item.CreatedAt
		if item.StartedAt != nil {
			last = *item.StartedAt
		}
		if item.LastHeartbeatAt != nil {
			last = *item.LastHeartbeatAt
		}
		if last.After(deadline) {
			continue
		}
		finished := now.UTC()
		item.Status = Interrupted
		item.FinishedAt = &finished
		item.InterruptionReason = "heartbeat timeout"
		s.runs[id] = item
		result = append(result, item)
	}
	return result, nil
}
func (s *MemoryStore) UpdateResult(_ context.Context, organizationID, runID string, input ResultInput) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID {
		return Run{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if input.CommitSHA != "" {
		item.CommitSHA = input.CommitSHA
	}
	if input.PullRequestID != "" {
		item.PullRequestID = input.PullRequestID
	}
	if input.Result != nil {
		item.Result = input.Result
	}
	if input.Error != nil {
		item.Error = *input.Error
	}
	for key, value := range input.Metadata {
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata[key] = value
	}
	s.runs[runID] = item
	return item, nil
}

func (s *MemoryStore) UpdateTestResults(_ context.Context, organizationID, runID string, results TestResultSet) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID {
		return Run{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if item.Result == nil {
		item.Result = map[string]any{}
	}
	item.Result["test_cases"] = results.Cases
	item.Result["test_review_note"] = results.ReviewNote
	s.runs[runID] = item
	return item, nil
}
func (s *MemoryStore) AddStep(_ context.Context, organizationID, runID string, step Step) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID {
		return Step{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if step.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Step{}, err
		}
		step.ID = id
	}
	step.RunID = runID
	s.steps[runID] = append(s.steps[runID], step)
	return step, nil
}
func (s *MemoryStore) AddArtifact(_ context.Context, organizationID, runID string, artifact Artifact) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.runs[runID]
	if !ok || item.OrganizationID != organizationID {
		return Artifact{}, apperr.New(apperr.CodeNotFound, 404, "AgentRun not found", nil)
	}
	if artifact.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Artifact{}, err
		}
		artifact.ID = id
	}
	artifact.RunID = runID
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now().UTC()
	}
	s.artifacts[runID] = append(s.artifacts[runID], artifact)
	return artifact, nil
}
func isActive(status Status) bool {
	switch status {
	case Queued, Preparing, Planning, Investigating, Implementing, Testing, Reviewing:
		return true
	default:
		return false
	}
}

var _ Store = (*PostgresStore)(nil)
var _ Store = (*MemoryStore)(nil)
