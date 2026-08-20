package autonomous

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
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Create(context.Context, string, StartInput, Policy) (Run, error)
	Get(context.Context, string, string, string) (Run, error)
	List(context.Context, string, string, string) ([]Run, error)
	Update(context.Context, string, string, string, StateUpdate) (Run, error)
	AddFeedback(context.Context, string, string, string, Feedback) (Feedback, error)
	ListFeedback(context.Context, string, string, string) ([]Feedback, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

const runColumns = `id::text, organization_id::text, project_id::text, work_item_id::text,
COALESCE(repository_id::text,''), COALESCE(base_sha,''), COALESCE(branch,''), objective, agent_provider, agent_name, COALESCE(model,''),
COALESCE(target_environment,''), policy, status, phase, COALESCE(gate,''), attempt, max_attempts,
COALESCE(current_agent_run_id::text,''), COALESCE(pull_request_id::text,''), COALESCE(commit_sha,''),
unresolved_positions, COALESCE(last_error,''), version, created_at, updated_at, finished_at`

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var run Run
	var policyJSON []byte
	if err := row.Scan(&run.ID, &run.OrganizationID, &run.ProjectID, &run.WorkItemID, &run.RepositoryID, &run.BaseSHA, &run.Branch,
		&run.Objective, &run.AgentProvider, &run.AgentName, &run.Model, &run.TargetEnvironment, &policyJSON,
		&run.Status, &run.Phase, &run.Gate, &run.Attempt, &run.MaxAttempts, &run.CurrentAgentRunID,
		&run.PullRequestID, &run.CommitSHA, &run.UnresolvedPositions, &run.LastError, &run.Version,
		&run.CreatedAt, &run.UpdatedAt, &run.FinishedAt); err != nil {
		return Run{}, err
	}
	if len(policyJSON) > 0 {
		if err := json.Unmarshal(policyJSON, &run.Policy); err != nil {
			return Run{}, fmt.Errorf("decode autonomous policy: %w", err)
		}
	}
	run.Policy = run.Policy.Normalize()
	return run, nil
}

func (s *PostgresStore) Create(ctx context.Context, organizationID string, input StartInput, policy Policy) (Run, error) {
	id, err := ids.New()
	if err != nil {
		return Run{}, err
	}
	policy = policy.Normalize()
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return Run{}, fmt.Errorf("encode autonomous policy: %w", err)
	}
	status, phase, gate := initialState(input)
	row := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO autonomous_runs
 (id, organization_id, project_id, work_item_id, repository_id, base_sha, branch, objective, agent_provider, agent_name, model,
  target_environment, policy, status, phase, gate, attempt, max_attempts, unresolved_positions)
VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16,''),0,$17,$18)
RETURNING `+runColumns, id, organizationID, input.ProjectID, input.WorkItemID, input.RepositoryID,
		strings.TrimSpace(input.BaseSHA), strings.TrimSpace(input.Branch), strings.TrimSpace(input.Objective), strings.ToLower(strings.TrimSpace(input.AgentProvider)), strings.TrimSpace(input.AgentName), strings.TrimSpace(input.Model), strings.TrimSpace(input.TargetEnvironment), policyJSON, status, phase, gate, policy.MaxAttempts, input.TestCasePositions)
	return scanRun(row)
}

func (s *PostgresStore) Get(ctx context.Context, organizationID, projectID, id string) (Run, error) {
	return scanRun(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT `+runColumns+` FROM autonomous_runs WHERE organization_id=$1 AND project_id=$2 AND id=$3`, organizationID, projectID, id))
}

func (s *PostgresStore) List(ctx context.Context, organizationID, projectID, workItemID string) ([]Run, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT `+runColumns+` FROM autonomous_runs WHERE organization_id=$1 AND project_id=$2 AND ($3='' OR work_item_id=$3::uuid) ORDER BY created_at DESC, id DESC`, organizationID, projectID, strings.TrimSpace(workItemID))
	if err != nil {
		return nil, fmt.Errorf("list autonomous runs: %w", err)
	}
	defer rows.Close()
	result := make([]Run, 0)
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan autonomous run: %w", scanErr)
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (s *PostgresStore) Update(ctx context.Context, organizationID, projectID, id string, input StateUpdate) (Run, error) {
	finishedAt := any(nil)
	if input.Finished {
		finishedAt = time.Now().UTC()
	}
	row := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
UPDATE autonomous_runs SET status=$1, phase=$2, gate=NULLIF($3,''),
 current_agent_run_id=NULLIF($4,'')::uuid, pull_request_id=NULLIF($5,'')::uuid, commit_sha=NULLIF($6,''),
 attempt=CASE WHEN $7<0 THEN attempt ELSE $7 END, unresolved_positions=$8, last_error=$9,
 version=version+1, updated_at=now(), finished_at=CASE WHEN $10 THEN $11 ELSE finished_at END
WHERE organization_id=$12 AND project_id=$13 AND id=$14 AND ($15=0 OR version=$15)
RETURNING `+runColumns, input.Status, input.Phase, input.Gate, input.CurrentAgentRunID, input.PullRequestID, input.CommitSHA,
		input.Attempt, input.UnresolvedPositions, strings.TrimSpace(input.LastError), input.Finished, finishedAt, organizationID, projectID, id, input.ExpectedVersion)
	result, err := scanRun(row)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return Run{}, apperr.New(apperr.CodeConflict, 409, "autonomous run is stale or not found", nil)
		}
		return Run{}, fmt.Errorf("update autonomous run: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) AddFeedback(ctx context.Context, organizationID, projectID, runID string, feedback Feedback) (Feedback, error) {
	row := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO autonomous_feedback (id, organization_id, autonomous_run_id, source, note, severity, commit_sha, test_case_positions, evidence_refs, created_by)
SELECT $1,$2,id,$4,$5,$6,NULLIF($7,''),$8,$9,$10 FROM autonomous_runs WHERE organization_id=$2 AND project_id=$3 AND id=$11
RETURNING id::text, autonomous_run_id::text, source, note, severity, COALESCE(commit_sha,''), test_case_positions, evidence_refs, created_by, created_at`, feedback.ID, organizationID, projectID, feedback.Source, feedback.Note, feedback.Severity, feedback.CommitSHA, feedback.TestCasePositions, feedback.EvidenceRefs, feedback.CreatedBy, runID)
	var result Feedback
	if err := row.Scan(&result.ID, &result.RunID, &result.Source, &result.Note, &result.Severity, &result.CommitSHA, &result.TestCasePositions, &result.EvidenceRefs, &result.CreatedBy, &result.CreatedAt); err != nil {
		return Feedback{}, fmt.Errorf("create autonomous feedback: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListFeedback(ctx context.Context, organizationID, projectID, runID string) ([]Feedback, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT f.id::text, f.autonomous_run_id::text, f.source, f.note, f.severity, COALESCE(f.commit_sha,''), f.test_case_positions, f.evidence_refs, f.created_by, f.created_at FROM autonomous_feedback f JOIN autonomous_runs r ON r.organization_id=f.organization_id AND r.id=f.autonomous_run_id WHERE f.organization_id=$1 AND r.project_id=$2 AND f.autonomous_run_id=$3 ORDER BY f.created_at, f.id`, organizationID, projectID, runID)
	if err != nil {
		return nil, fmt.Errorf("list autonomous feedback: %w", err)
	}
	defer rows.Close()
	result := make([]Feedback, 0)
	for rows.Next() {
		var item Feedback
		if err := rows.Scan(&item.ID, &item.RunID, &item.Source, &item.Note, &item.Severity, &item.CommitSHA, &item.TestCasePositions, &item.EvidenceRefs, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan autonomous feedback: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type MemoryStore struct {
	mu       sync.Mutex
	runs     map[string]Run
	feedback map[string][]Feedback
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]Run), feedback: make(map[string][]Feedback)}
}

func (s *MemoryStore) Create(_ context.Context, organizationID string, input StartInput, policy Policy) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := ids.New()
	if err != nil {
		return Run{}, err
	}
	status, phase, gate := initialState(input)
	now := time.Now().UTC()
	run := Run{ID: id, OrganizationID: organizationID, ProjectID: input.ProjectID, WorkItemID: input.WorkItemID, RepositoryID: input.RepositoryID, BaseSHA: strings.TrimSpace(input.BaseSHA), Branch: strings.TrimSpace(input.Branch), Objective: strings.TrimSpace(input.Objective), AgentProvider: strings.ToLower(strings.TrimSpace(input.AgentProvider)), AgentName: strings.TrimSpace(input.AgentName), Model: strings.TrimSpace(input.Model), TargetEnvironment: strings.TrimSpace(input.TargetEnvironment), Policy: policy.Normalize(), Status: status, Phase: phase, Gate: gate, MaxAttempts: policy.MaxAttempts, UnresolvedPositions: append([]int(nil), input.TestCasePositions...), Version: 1, CreatedAt: now, UpdatedAt: now}
	s.runs[id] = run
	return run, nil
}

func (s *MemoryStore) Get(_ context.Context, organizationID, projectID, id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok || run.OrganizationID != organizationID || run.ProjectID != projectID {
		return Run{}, apperr.New(apperr.CodeNotFound, 404, "autonomous run not found", nil)
	}
	return run, nil
}

func (s *MemoryStore) List(_ context.Context, organizationID, projectID, workItemID string) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Run, 0)
	for _, run := range s.runs {
		if run.OrganizationID == organizationID && run.ProjectID == projectID && (workItemID == "" || run.WorkItemID == workItemID) {
			result = append(result, run)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryStore) Update(_ context.Context, organizationID, projectID, id string, input StateUpdate) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok || run.OrganizationID != organizationID || run.ProjectID != projectID {
		return Run{}, apperr.New(apperr.CodeNotFound, 404, "autonomous run not found", nil)
	}
	if input.ExpectedVersion > 0 && input.ExpectedVersion != run.Version {
		return Run{}, apperr.New(apperr.CodeConflict, 409, "autonomous run version is stale", nil)
	}
	run.Status, run.Phase, run.Gate = input.Status, input.Phase, input.Gate
	run.CurrentAgentRunID, run.PullRequestID, run.CommitSHA = input.CurrentAgentRunID, input.PullRequestID, input.CommitSHA
	if input.Attempt >= 0 {
		run.Attempt = input.Attempt
	}
	run.UnresolvedPositions = append([]int(nil), input.UnresolvedPositions...)
	run.LastError = strings.TrimSpace(input.LastError)
	run.Version++
	run.UpdatedAt = time.Now().UTC()
	if input.Finished {
		value := run.UpdatedAt
		run.FinishedAt = &value
	}
	s.runs[id] = run
	return run, nil
}

func (s *MemoryStore) AddFeedback(_ context.Context, organizationID, projectID, runID string, feedback Feedback) (Feedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok || run.OrganizationID != organizationID || run.ProjectID != projectID {
		return Feedback{}, apperr.New(apperr.CodeNotFound, 404, "autonomous run not found", nil)
	}
	if feedback.ID == "" {
		id, err := ids.New()
		if err != nil {
			return Feedback{}, err
		}
		feedback.ID = id
	}
	feedback.RunID = runID
	feedback.CreatedAt = time.Now().UTC()
	s.feedback[runID] = append(s.feedback[runID], feedback)
	return feedback, nil
}

func (s *MemoryStore) ListFeedback(_ context.Context, organizationID, projectID, runID string) ([]Feedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok || run.OrganizationID != organizationID || run.ProjectID != projectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "autonomous run not found", nil)
	}
	return append([]Feedback(nil), s.feedback[runID]...), nil
}

func initialState(input StartInput) (Status, Phase, Gate) {
	if strings.TrimSpace(input.WorkItemID) == "" {
		return StatusIntake, PhaseIntake, GateNone
	}
	return StatusWaitingSpecReview, PhaseSpecification, GateSpecification
}

var _ Store = (*MemoryStore)(nil)
var _ Store = (*PostgresStore)(nil)
