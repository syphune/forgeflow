package specification

import "time"

type Provenance string

const (
	HumanProvided Provenance = "HUMAN_PROVIDED"
	ConfirmedFact Provenance = "CONFIRMED_FACT"
	AIInferred    Provenance = "AI_INFERRED"
	AIHypothesis  Provenance = "AI_HYPOTHESIS"
	HumanVerified Provenance = "HUMAN_VERIFIED"
	SystemDerived Provenance = "SYSTEM_DERIVED"
	Extracted     Provenance = "EXTRACTED"
)

type VerificationStatus string

const (
	Unverified      VerificationStatus = "UNVERIFIED"
	VerifiedByHuman VerificationStatus = "HUMAN_VERIFIED"
)

type FieldKey string

const (
	ProblemStatement FieldKey = "PROBLEM_STATEMENT"
	ExpectedBehavior FieldKey = "EXPECTED_BEHAVIOR"
	ActualBehavior   FieldKey = "ACTUAL_BEHAVIOR"
	Environment      FieldKey = "ENVIRONMENT"
	Preconditions    FieldKey = "PRECONDITIONS"
	Frequency        FieldKey = "FREQUENCY"
	AffectedVersion  FieldKey = "AFFECTED_VERSION"
	SuspectedRoot    FieldKey = "SUSPECTED_ROOT_CAUSE"
	SecurityImpact   FieldKey = "SECURITY_IMPACT"
	BusinessImpact   FieldKey = "BUSINESS_IMPACT"
	Goal             FieldKey = "GOAL"
	NoCodeChange     FieldKey = "NO_CODE_CHANGE_RATIONALE"
)

type Field struct {
	Key                FieldKey           `json:"key"`
	Value              string             `json:"value"`
	Provenance         Provenance         `json:"provenance"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	SourceProposalID   string             `json:"source_proposal_id,omitempty"`
	VerifiedBy         string             `json:"verified_by,omitempty"`
	VerifiedAt         *time.Time         `json:"verified_at,omitempty"`
}

type ReproductionStep struct {
	Position           int                `json:"position"`
	Action             string             `json:"action"`
	ExpectedResult     string             `json:"expected_result"`
	ObservedResult     string             `json:"observed_result"`
	EvidenceRefs       []string           `json:"evidence_refs,omitempty"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	Provenance         Provenance         `json:"provenance"`
	VerifiedBy         string             `json:"verified_by,omitempty"`
	VerifiedAt         *time.Time         `json:"verified_at,omitempty"`
}

type AcceptanceCriterion struct {
	Position           int                `json:"position"`
	Statement          string             `json:"statement"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	Provenance         Provenance         `json:"provenance"`
	VerifiedBy         string             `json:"verified_by,omitempty"`
	VerifiedAt         *time.Time         `json:"verified_at,omitempty"`
}

type RegressionTestCase struct {
	Position           int                `json:"position"`
	Scenario           string             `json:"scenario"`
	ExpectedResult     string             `json:"expected_result"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	Provenance         Provenance         `json:"provenance"`
	VerifiedBy         string             `json:"verified_by,omitempty"`
	VerifiedAt         *time.Time         `json:"verified_at,omitempty"`
}

type ContextRef struct {
	RepositoryID string     `json:"repository_id,omitempty"`
	Module       string     `json:"module,omitempty"`
	File         string     `json:"file,omitempty"`
	Symbol       string     `json:"symbol,omitempty"`
	Commit       string     `json:"commit,omitempty"`
	PullRequest  string     `json:"pull_request,omitempty"`
	Rationale    string     `json:"rationale,omitempty"`
	Provenance   Provenance `json:"provenance"`
}

type Specification struct {
	ID                string                `json:"id"`
	WorkItemID        string                `json:"work_item_id"`
	Version           int                   `json:"version"`
	ReviewedVersion   int                   `json:"reviewed_version,omitempty"`
	ReviewedBy        string                `json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time            `json:"reviewed_at,omitempty"`
	Summary           string                `json:"summary"`
	Fields            map[FieldKey]Field    `json:"fields"`
	ReproductionSteps []ReproductionStep    `json:"reproduction_steps"`
	Acceptance        []AcceptanceCriterion `json:"acceptance_criteria"`
	RegressionCases   []RegressionTestCase  `json:"regression_test_cases"`
	ContextRefs       []ContextRef          `json:"context_refs"`
	MediaRefs         map[string][]string   `json:"media_refs,omitempty"`
	RepositoryID      string                `json:"repository_id,omitempty"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

type FieldVersion struct {
	ID                 string             `json:"id"`
	Revision           int                `json:"revision"`
	Field              FieldKey           `json:"field"`
	Value              string             `json:"value"`
	Provenance         Provenance         `json:"provenance"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	SourceProposalID   string             `json:"source_proposal_id,omitempty"`
	VerifiedBy         string             `json:"verified_by,omitempty"`
	VerifiedAt         *time.Time         `json:"verified_at,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
}

type Proposal struct {
	ID         string     `json:"id"`
	WorkItemID string     `json:"work_item_id"`
	Field      FieldKey   `json:"field"`
	Value      string     `json:"value"`
	Provenance Provenance `json:"provenance"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Analysis struct {
	ID                  string     `json:"id"`
	WorkItemID          string     `json:"work_item_id"`
	RootCauseHypothesis string     `json:"root_cause_hypothesis"`
	BlastRadius         string     `json:"blast_radius"`
	ImplementationPlan  string     `json:"implementation_plan"`
	TestPlan            string     `json:"test_plan"`
	EvidenceRefs        []string   `json:"evidence_refs"`
	Confidence          float64    `json:"confidence"`
	Provenance          Provenance `json:"provenance"`
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
}

type Readiness struct {
	Ready                bool              `json:"ready"`
	Reviewed             bool              `json:"reviewed"`
	SpecificationVersion int               `json:"specification_version"`
	ReviewedVersion      int               `json:"reviewed_version"`
	Missing              []string          `json:"missing,omitempty"`
	Quality              QualityDimensions `json:"quality"`
}

// QualityDimensions guide human review; they never replace deterministic
// readiness rules.
type QualityDimensions struct {
	Completeness              float64 `json:"completeness"`
	Clarity                   float64 `json:"clarity"`
	Reproducibility           float64 `json:"reproducibility"`
	EvidenceQuality           float64 `json:"evidence_quality"`
	Testability               float64 `json:"testability"`
	RepositoryContext         float64 `json:"repository_context"`
	HumanVerificationCoverage float64 `json:"human_verification_coverage"`
}
