package specification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Ensure(ctx context.Context, organizationID, projectID, workItemID string) (*Specification, error) {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	if _, err := exec.Exec(ctx, `INSERT INTO specifications (organization_id, project_id, work_item_id) SELECT $1,$2,$3 WHERE EXISTS (SELECT 1 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND id=$3) ON CONFLICT (work_item_id) DO NOTHING`, organizationID, projectID, workItemID); err != nil {
		return nil, fmt.Errorf("ensure specification: %w", err)
	}
	spec, err := s.Get(ctx, organizationID, projectID, workItemID)
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	return spec, nil
}

func (s *PostgresStore) Get(ctx context.Context, organizationID, projectID, workItemID string) (*Specification, error) {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	var spec Specification
	var repositoryID string
	var mediaRefs []byte
	if err := exec.QueryRow(ctx, `SELECT id::text, work_item_id::text, COALESCE(summary,''), version, COALESCE(reviewed_version,0), COALESCE(reviewed_by::text,''), reviewed_at, COALESCE(repository_id::text,''), COALESCE(media_refs,'{}'::jsonb), updated_at FROM specifications WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3`, organizationID, projectID, workItemID).Scan(&spec.ID, &spec.WorkItemID, &spec.Summary, &spec.Version, &spec.ReviewedVersion, &spec.ReviewedBy, &spec.ReviewedAt, &repositoryID, &mediaRefs, &spec.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load specification: %w", err)
	}
	spec.RepositoryID = repositoryID
	spec.MediaRefs = make(map[string][]string)
	if len(mediaRefs) > 0 {
		if err := json.Unmarshal(mediaRefs, &spec.MediaRefs); err != nil {
			return nil, fmt.Errorf("decode specification multimedia references: %w", err)
		}
	}
	spec.Fields = make(map[FieldKey]Field)
	rows, err := exec.Query(ctx, `SELECT field_key, value_text, provenance, verification_status, COALESCE(source_proposal_id::text,''), COALESCE(verified_by::text,''), verified_at FROM specification_fields WHERE specification_id=$1 ORDER BY field_key`, spec.ID)
	if err != nil {
		return nil, fmt.Errorf("load specification fields: %w", err)
	}
	for rows.Next() {
		var key, value, provenance, verification, sourceProposalID, verifiedBy string
		var verifiedAt *time.Time
		if err := rows.Scan(&key, &value, &provenance, &verification, &sourceProposalID, &verifiedBy, &verifiedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan specification field: %w", err)
		}
		field := Field{Key: FieldKey(key), Value: value, Provenance: Provenance(provenance), VerificationStatus: VerificationStatus(verification), SourceProposalID: sourceProposalID, VerifiedBy: verifiedBy}
		if verifiedAt != nil {
			verifiedAtValue := verifiedAt.UTC()
			field.VerifiedAt = &verifiedAtValue
		}
		spec.Fields[field.Key] = field
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate specification fields: %w", err)
	}

	rows, err = exec.Query(ctx, `SELECT position, action, expected_result, observed_result, evidence_refs, provenance, verification_status, COALESCE(verified_by::text,''), verified_at FROM specification_reproduction_steps WHERE specification_id=$1 ORDER BY position`, spec.ID)
	if err != nil {
		return nil, fmt.Errorf("load reproduction steps: %w", err)
	}
	for rows.Next() {
		var step ReproductionStep
		var evidence []byte
		var provenance, verification, verifiedBy string
		var verifiedAt *time.Time
		if err := rows.Scan(&step.Position, &step.Action, &step.ExpectedResult, &step.ObservedResult, &evidence, &provenance, &verification, &verifiedBy, &verifiedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan reproduction step: %w", err)
		}
		if len(evidence) > 0 {
			if err := json.Unmarshal(evidence, &step.EvidenceRefs); err != nil {
				rows.Close()
				return nil, fmt.Errorf("decode reproduction evidence: %w", err)
			}
		}
		step.Provenance = Provenance(provenance)
		step.VerificationStatus = VerificationStatus(verification)
		step.VerifiedBy = verifiedBy
		if verifiedAt != nil {
			verifiedAtValue := verifiedAt.UTC()
			step.VerifiedAt = &verifiedAtValue
		}
		spec.ReproductionSteps = append(spec.ReproductionSteps, step)
	}
	rows.Close()

	rows, err = exec.Query(ctx, `SELECT position, statement, provenance, verification_status, COALESCE(verified_by::text,''), verified_at FROM specification_acceptance_criteria WHERE specification_id=$1 ORDER BY position`, spec.ID)
	if err != nil {
		return nil, fmt.Errorf("load acceptance criteria: %w", err)
	}
	for rows.Next() {
		var criterion AcceptanceCriterion
		var provenance, verification, verifiedBy string
		var verifiedAt *time.Time
		if err := rows.Scan(&criterion.Position, &criterion.Statement, &provenance, &verification, &verifiedBy, &verifiedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan acceptance criterion: %w", err)
		}
		criterion.Provenance = Provenance(provenance)
		criterion.VerificationStatus = VerificationStatus(verification)
		criterion.VerifiedBy = verifiedBy
		if verifiedAt != nil {
			verifiedAtValue := verifiedAt.UTC()
			criterion.VerifiedAt = &verifiedAtValue
		}
		spec.Acceptance = append(spec.Acceptance, criterion)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate acceptance criteria: %w", err)
	}

	rows, err = exec.Query(ctx, `SELECT position, scenario, expected_result, provenance, verification_status, COALESCE(verified_by::text,''), verified_at FROM specification_regression_cases WHERE specification_id=$1 ORDER BY position`, spec.ID)
	if err != nil {
		return nil, fmt.Errorf("load regression test cases: %w", err)
	}
	for rows.Next() {
		var testCase RegressionTestCase
		var provenance, verification, verifiedBy string
		var verifiedAt *time.Time
		if err := rows.Scan(&testCase.Position, &testCase.Scenario, &testCase.ExpectedResult, &provenance, &verification, &verifiedBy, &verifiedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan regression test case: %w", err)
		}
		testCase.Provenance = Provenance(provenance)
		testCase.VerificationStatus = VerificationStatus(verification)
		testCase.VerifiedBy = verifiedBy
		if verifiedAt != nil {
			verifiedAtValue := verifiedAt.UTC()
			testCase.VerifiedAt = &verifiedAtValue
		}
		spec.RegressionCases = append(spec.RegressionCases, testCase)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate regression test cases: %w", err)
	}

	rows, err = exec.Query(ctx, `SELECT COALESCE(repository_id::text,''), module, file, symbol, commit_sha, pull_request, rationale, provenance FROM specification_context_refs WHERE specification_id=$1 ORDER BY id`, spec.ID)
	if err != nil {
		return nil, fmt.Errorf("load specification context refs: %w", err)
	}
	for rows.Next() {
		var ref ContextRef
		var provenance string
		if err := rows.Scan(&ref.RepositoryID, &ref.Module, &ref.File, &ref.Symbol, &ref.Commit, &ref.PullRequest, &ref.Rationale, &provenance); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan specification context ref: %w", err)
		}
		ref.Provenance = Provenance(provenance)
		spec.ContextRefs = append(spec.ContextRefs, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate specification context refs: %w", err)
	}
	return &spec, nil
}

func (s *PostgresStore) FieldVersions(ctx context.Context, organizationID, projectID, workItemID string, limit int) ([]FieldVersion, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT sfv.id::text, sfv.revision, sfv.field_key, sfv.value_text, sfv.provenance, sfv.verification_status,
       COALESCE(sfv.source_proposal_id::text,''), COALESCE(sfv.verified_by::text,''), sfv.verified_at, sfv.created_at
FROM specification_field_versions sfv
JOIN specifications s ON s.id=sfv.specification_id AND s.organization_id=sfv.organization_id AND s.project_id=$2
JOIN work_items wi ON wi.id=s.work_item_id AND wi.organization_id=$1 AND wi.project_id=$2
WHERE sfv.organization_id=$1 AND sfv.project_id=$2 AND wi.id=$3
ORDER BY sfv.revision DESC, sfv.field_key
LIMIT $4`, organizationID, projectID, workItemID, limit)
	if err != nil {
		return nil, fmt.Errorf("list specification field versions: %w", err)
	}
	defer rows.Close()
	result := make([]FieldVersion, 0)
	for rows.Next() {
		var item FieldVersion
		var field, provenance, verification string
		if err := rows.Scan(&item.ID, &item.Revision, &field, &item.Value, &provenance, &verification, &item.SourceProposalID, &item.VerifiedBy, &item.VerifiedAt, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan specification field version: %w", err)
		}
		item.Field = FieldKey(field)
		item.Provenance = Provenance(provenance)
		item.VerificationStatus = VerificationStatus(verification)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate specification field versions: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) Save(ctx context.Context, organizationID, projectID string, spec *Specification) error {
	if platformdb.InTransaction(ctx) {
		return s.save(ctx, platformdb.ExecutorFrom(ctx, s.pool), organizationID, projectID, spec)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin specification save: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := s.save(ctx, tx, organizationID, projectID, spec); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit specification save: %w", err)
	}
	return nil
}

func (s *PostgresStore) SaveExpectedVersion(ctx context.Context, organizationID, projectID string, spec *Specification, expectedVersion int) error {
	if platformdb.InTransaction(ctx) {
		return s.saveExpectedVersion(ctx, platformdb.ExecutorFrom(ctx, s.pool), organizationID, projectID, spec, expectedVersion)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin compare-and-set specification save: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := s.saveExpectedVersion(ctx, tx, organizationID, projectID, spec, expectedVersion); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit compare-and-set specification save: %w", err)
	}
	return nil
}

func (s *PostgresStore) saveExpectedVersion(ctx context.Context, exec platformdb.Executor, organizationID, projectID string, spec *Specification, expectedVersion int) error {
	var currentVersion int
	if err := exec.QueryRow(ctx, `SELECT version FROM specifications WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 FOR UPDATE`, organizationID, projectID, spec.WorkItemID).Scan(&currentVersion); err != nil {
		if err == pgx.ErrNoRows {
			return apperr.New(apperr.CodeNotFound, 404, "specification not found", nil)
		}
		return fmt.Errorf("lock specification for compare-and-set: %w", err)
	}
	if currentVersion != expectedVersion {
		return staleVersionError(expectedVersion, currentVersion)
	}
	return s.save(ctx, exec, organizationID, projectID, spec)
}

func (s *PostgresStore) save(ctx context.Context, exec platformdb.Executor, organizationID, projectID string, spec *Specification) error {
	var lockedID string
	if err := exec.QueryRow(ctx, `SELECT id::text FROM specifications WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 FOR UPDATE`, organizationID, projectID, spec.WorkItemID).Scan(&lockedID); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("specification not found")
		}
		return fmt.Errorf("lock specification: %w", err)
	}
	var revision int
	if err := exec.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM specification_field_versions WHERE specification_id=$1`, spec.ID).Scan(&revision); err != nil {
		return fmt.Errorf("allocate specification field revision: %w", err)
	}
	var reviewedBy any
	if isUUID(spec.ReviewedBy) {
		reviewedBy = spec.ReviewedBy
	}
	var reviewedAt any
	if spec.ReviewedAt != nil {
		reviewedAt = *spec.ReviewedAt
	}
	mediaRefs, err := json.Marshal(spec.MediaRefs)
	if err != nil {
		return fmt.Errorf("encode specification multimedia references: %w", err)
	}
	if _, err := exec.Exec(ctx, `UPDATE specifications SET summary=$1, repository_id=NULLIF($2,'')::uuid, media_refs=$3, version=$4, reviewed_version=NULLIF($5,0), reviewed_by=$6, reviewed_at=$7, updated_at=$8 WHERE organization_id=$9 AND project_id=$10 AND work_item_id=$11`, spec.Summary, spec.RepositoryID, mediaRefs, spec.Version, spec.ReviewedVersion, reviewedBy, reviewedAt, spec.UpdatedAt, organizationID, projectID, spec.WorkItemID); err != nil {
		return fmt.Errorf("update specification: %w", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO specification_field_versions (organization_id, project_id, specification_id, revision, field_key, value_text, provenance, verification_status) VALUES ($1,$2,$3,$4,'SUMMARY',$5,$6,$7)`, organizationID, projectID, spec.ID, revision, spec.Summary, HumanProvided, Unverified); err != nil {
		return fmt.Errorf("insert specification summary version: %w", err)
	}
	if _, err := exec.Exec(ctx, `DELETE FROM specification_fields WHERE specification_id=$1`, spec.ID); err != nil {
		return fmt.Errorf("replace specification fields: %w", err)
	}
	for _, field := range spec.Fields {
		var verifiedBy any
		if isUUID(field.VerifiedBy) {
			verifiedBy = field.VerifiedBy
		}
		var verifiedAt any
		if field.VerifiedAt != nil {
			verifiedAt = *field.VerifiedAt
		}
		var sourceProposalID any
		if isUUID(field.SourceProposalID) {
			sourceProposalID = field.SourceProposalID
		}
		if _, err := exec.Exec(ctx, `INSERT INTO specification_fields (specification_id, field_key, value_text, provenance, verification_status, source_proposal_id, verified_by, verified_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, spec.ID, field.Key, field.Value, field.Provenance, field.VerificationStatus, sourceProposalID, verifiedBy, verifiedAt); err != nil {
			return fmt.Errorf("insert specification field: %w", err)
		}
		if _, err := exec.Exec(ctx, `INSERT INTO specification_field_versions (organization_id, project_id, specification_id, revision, field_key, value_text, provenance, verification_status, source_proposal_id, verified_by, verified_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, organizationID, projectID, spec.ID, revision, field.Key, field.Value, field.Provenance, field.VerificationStatus, sourceProposalID, verifiedBy, verifiedAt); err != nil {
			return fmt.Errorf("insert specification field version: %w", err)
		}
	}
	if _, err := exec.Exec(ctx, `DELETE FROM specification_reproduction_steps WHERE specification_id=$1`, spec.ID); err != nil {
		return fmt.Errorf("replace reproduction steps: %w", err)
	}
	for _, step := range spec.ReproductionSteps {
		var verifiedBy any
		if isUUID(step.VerifiedBy) {
			verifiedBy = step.VerifiedBy
		}
		var verifiedAt any
		if step.VerifiedAt != nil {
			verifiedAt = *step.VerifiedAt
		}
		evidence, err := json.Marshal(step.EvidenceRefs)
		if err != nil {
			return fmt.Errorf("encode reproduction evidence: %w", err)
		}
		if _, err := exec.Exec(ctx, `INSERT INTO specification_reproduction_steps (specification_id, position, action, expected_result, observed_result, evidence_refs, provenance, verification_status, verified_by, verified_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, spec.ID, step.Position, step.Action, step.ExpectedResult, step.ObservedResult, evidence, step.Provenance, step.VerificationStatus, verifiedBy, verifiedAt); err != nil {
			return fmt.Errorf("insert reproduction step: %w", err)
		}
	}
	if _, err := exec.Exec(ctx, `DELETE FROM specification_acceptance_criteria WHERE specification_id=$1`, spec.ID); err != nil {
		return fmt.Errorf("replace acceptance criteria: %w", err)
	}
	for _, criterion := range spec.Acceptance {
		var verifiedBy any
		if isUUID(criterion.VerifiedBy) {
			verifiedBy = criterion.VerifiedBy
		}
		var verifiedAt any
		if criterion.VerifiedAt != nil {
			verifiedAt = *criterion.VerifiedAt
		}
		if _, err := exec.Exec(ctx, `INSERT INTO specification_acceptance_criteria (specification_id, position, statement, provenance, verification_status, verified_by, verified_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, spec.ID, criterion.Position, criterion.Statement, criterion.Provenance, criterion.VerificationStatus, verifiedBy, verifiedAt); err != nil {
			return fmt.Errorf("insert acceptance criterion: %w", err)
		}
	}
	if _, err := exec.Exec(ctx, `DELETE FROM specification_regression_cases WHERE specification_id=$1`, spec.ID); err != nil {
		return fmt.Errorf("replace regression test cases: %w", err)
	}
	for _, testCase := range spec.RegressionCases {
		var verifiedBy any
		if isUUID(testCase.VerifiedBy) {
			verifiedBy = testCase.VerifiedBy
		}
		var verifiedAt any
		if testCase.VerifiedAt != nil {
			verifiedAt = *testCase.VerifiedAt
		}
		if _, err := exec.Exec(ctx, `INSERT INTO specification_regression_cases (specification_id, position, scenario, expected_result, provenance, verification_status, verified_by, verified_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, spec.ID, testCase.Position, testCase.Scenario, testCase.ExpectedResult, testCase.Provenance, testCase.VerificationStatus, verifiedBy, verifiedAt); err != nil {
			return fmt.Errorf("insert regression test case: %w", err)
		}
	}
	if _, err := exec.Exec(ctx, `DELETE FROM specification_context_refs WHERE specification_id=$1`, spec.ID); err != nil {
		return fmt.Errorf("replace specification context refs: %w", err)
	}
	for _, ref := range spec.ContextRefs {
		var repositoryID any
		if isUUID(ref.RepositoryID) {
			repositoryID = ref.RepositoryID
		}
		if _, err := exec.Exec(ctx, `INSERT INTO specification_context_refs (specification_id, repository_id, module, file, symbol, commit_sha, pull_request, rationale, provenance) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, spec.ID, repositoryID, ref.Module, ref.File, ref.Symbol, ref.Commit, ref.PullRequest, ref.Rationale, ref.Provenance); err != nil {
			return fmt.Errorf("insert specification context ref: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) AddProposal(ctx context.Context, organizationID, projectID string, proposal Proposal) error {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	tag, err := exec.Exec(ctx, `INSERT INTO ai_proposals (id, organization_id, project_id, work_item_id, field_key, proposed_value, provenance) SELECT $1,$2,$3,$4,$5,$6,$7 WHERE EXISTS (SELECT 1 FROM work_items WHERE organization_id=$2 AND project_id=$3 AND id=$4)`, proposal.ID, organizationID, projectID, proposal.WorkItemID, proposal.Field, proposal.Value, proposal.Provenance)
	if err == nil && tag.RowsAffected() != 1 {
		return apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if err != nil {
		return fmt.Errorf("insert AI proposal: %w", err)
	}
	return nil
}

func (s *PostgresStore) Proposals(ctx context.Context, organizationID, projectID, workItemID string) ([]Proposal, error) {
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	rows, err := exec.Query(ctx, `SELECT id::text, work_item_id::text, field_key, proposed_value, provenance, status, created_at FROM ai_proposals WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 ORDER BY created_at`, organizationID, projectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list AI proposals: %w", err)
	}
	defer rows.Close()
	var proposals []Proposal
	for rows.Next() {
		var proposal Proposal
		var field, provenance string
		if err := rows.Scan(&proposal.ID, &proposal.WorkItemID, &field, &proposal.Value, &provenance, &proposal.Status, &proposal.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan AI proposal: %w", err)
		}
		proposal.Field = FieldKey(field)
		proposal.Provenance = Provenance(provenance)
		proposals = append(proposals, proposal)
	}
	return proposals, rows.Err()
}

func (s *PostgresStore) AcceptProposal(ctx context.Context, organizationID, projectID, workItemID, proposalID string) (Proposal, error) {
	var proposal Proposal
	var field, provenance string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE ai_proposals SET status='ACCEPTED' WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 AND id=$4 AND status='PENDING' RETURNING id::text, work_item_id::text, field_key, proposed_value, provenance, status, created_at`, organizationID, projectID, workItemID, proposalID).Scan(&proposal.ID, &proposal.WorkItemID, &field, &proposal.Value, &provenance, &proposal.Status, &proposal.CreatedAt)
	if err == pgx.ErrNoRows {
		return Proposal{}, apperr.New(apperr.CodeNotFound, 404, "pending specification proposal not found", nil)
	}
	if err != nil {
		return Proposal{}, fmt.Errorf("accept specification proposal: %w", err)
	}
	proposal.Field = FieldKey(field)
	proposal.Provenance = Provenance(provenance)
	return proposal, nil
}

func (s *PostgresStore) RejectProposal(ctx context.Context, organizationID, projectID, workItemID, proposalID string) (Proposal, error) {
	var proposal Proposal
	var field, provenance string
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE ai_proposals SET status='REJECTED' WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 AND id=$4 AND status='PENDING' RETURNING id::text, work_item_id::text, field_key, proposed_value, provenance, status, created_at`, organizationID, projectID, workItemID, proposalID).Scan(&proposal.ID, &proposal.WorkItemID, &field, &proposal.Value, &provenance, &proposal.Status, &proposal.CreatedAt)
	if err == pgx.ErrNoRows {
		return Proposal{}, apperr.New(apperr.CodeNotFound, 404, "pending specification proposal not found", nil)
	}
	if err != nil {
		return Proposal{}, fmt.Errorf("reject specification proposal: %w", err)
	}
	proposal.Field = FieldKey(field)
	proposal.Provenance = Provenance(provenance)
	return proposal, nil
}

func (s *PostgresStore) AddAnalysis(ctx context.Context, organizationID, projectID string, analysis Analysis) error {
	evidence, err := json.Marshal(analysis.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("marshal analysis evidence: %w", err)
	}
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO ai_analyses (id, organization_id, project_id, work_item_id, root_cause_hypothesis, blast_radius, implementation_plan, test_plan, evidence_refs, confidence, provenance, created_by) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12 WHERE EXISTS (SELECT 1 FROM work_items WHERE organization_id=$2 AND project_id=$3 AND id=$4)`, analysis.ID, organizationID, projectID, analysis.WorkItemID, analysis.RootCauseHypothesis, analysis.BlastRadius, analysis.ImplementationPlan, analysis.TestPlan, evidence, analysis.Confidence, analysis.Provenance, analysis.CreatedBy)
	if err != nil {
		return fmt.Errorf("insert AI analysis: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	return nil
}

func (s *PostgresStore) Analyses(ctx context.Context, organizationID, projectID, workItemID string) ([]Analysis, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, work_item_id::text, root_cause_hypothesis, blast_radius, implementation_plan, test_plan, evidence_refs, confidence, provenance, created_by::text, created_at FROM ai_analyses WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 ORDER BY created_at DESC, id DESC`, organizationID, projectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list AI analyses: %w", err)
	}
	defer rows.Close()
	result := make([]Analysis, 0)
	for rows.Next() {
		var analysis Analysis
		var evidence []byte
		var provenance string
		if err := rows.Scan(&analysis.ID, &analysis.WorkItemID, &analysis.RootCauseHypothesis, &analysis.BlastRadius, &analysis.ImplementationPlan, &analysis.TestPlan, &evidence, &analysis.Confidence, &provenance, &analysis.CreatedBy, &analysis.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan AI analysis: %w", err)
		}
		if err := json.Unmarshal(evidence, &analysis.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode AI analysis evidence: %w", err)
		}
		analysis.Provenance = Provenance(provenance)
		result = append(result, analysis)
	}
	return result, rows.Err()
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range strings.ToLower(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

var _ Store = (*PostgresStore)(nil)
