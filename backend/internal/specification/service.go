package specification

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type Store interface {
	Ensure(context.Context, string, string, string) (*Specification, error)
	Get(context.Context, string, string, string) (*Specification, error)
	FieldVersions(context.Context, string, string, string, int) ([]FieldVersion, error)
	Save(context.Context, string, string, *Specification) error
	AddProposal(context.Context, string, string, Proposal) error
	Proposals(context.Context, string, string, string) ([]Proposal, error)
	AcceptProposal(context.Context, string, string, string, string) (Proposal, error)
	RejectProposal(context.Context, string, string, string, string) (Proposal, error)
	AddAnalysis(context.Context, string, string, Analysis) error
	Analyses(context.Context, string, string, string) ([]Analysis, error)
}

type compareAndSetSaver interface {
	SaveExpectedVersion(context.Context, string, string, *Specification, int) error
}

type MediaReferenceValidator interface {
	ValidateReferences(context.Context, string, string, string, []string) error
}

type Service struct {
	store          Store
	now            func() time.Time
	recorder       MutationRecorder
	transaction    TransactionRunner
	mediaValidator MediaReferenceValidator
	mu             sync.Mutex // ponytail: one process-wide spec mutation lock; use row-level targeted updates for multi-replica throughput.
}

type MutationRecorder struct {
	Audit  audit.Writer
	Outbox outbox.Writer
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type Options struct {
	Recorder                MutationRecorder
	Transaction             TransactionRunner
	MediaReferenceValidator MediaReferenceValidator
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type UpdateInput struct {
	ExpectedVersion   int
	Summary           *string
	Fields            map[FieldKey]string
	ReproductionSteps []ReproductionStep
	Acceptance        []AcceptanceCriterion
	RegressionCases   []RegressionTestCase
	ContextRefs       []ContextRef
	MediaRefs         map[string][]string
	RepositoryID      *string
}

func NewService(store Store, now func() time.Time, options ...Options) *Service {
	configured := Options{}
	if len(options) > 0 {
		configured = options[0]
	}
	if configured.Transaction == nil {
		configured.Transaction = directTransactionRunner{}
	}
	return &Service{store: store, now: now, recorder: configured.Recorder, transaction: configured.Transaction, mediaValidator: configured.MediaReferenceValidator}
}

func (s *Service) SetMediaReferenceValidator(validator MediaReferenceValidator) {
	s.mediaValidator = validator
}

func (s *Service) Ensure(ctx context.Context, organizationID, projectID, workItemID string) (*Specification, error) {
	return s.store.Ensure(ctx, organizationID, projectID, workItemID)
}

func (s *Service) Get(ctx context.Context, organizationID, projectID, workItemID string) (*Specification, error) {
	return s.store.Get(ctx, organizationID, projectID, workItemID)
}

func (s *Service) FieldVersions(ctx context.Context, organizationID, projectID, workItemID string, limit int) ([]FieldVersion, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.store.FieldVersions(ctx, organizationID, projectID, workItemID, limit)
}

func (s *Service) Proposals(ctx context.Context, organizationID, projectID, workItemID string) ([]Proposal, error) {
	return s.store.Proposals(ctx, organizationID, projectID, workItemID)
}

func (s *Service) Analyses(ctx context.Context, organizationID, projectID, workItemID string) ([]Analysis, error) {
	return s.store.Analyses(ctx, organizationID, projectID, workItemID)
}

func (s *Service) AddAnalysis(ctx context.Context, organizationID, projectID, workItemID string, actor identity.Actor, input Analysis) (Analysis, error) {
	if !actor.Has(identity.CapabilitySpecificationPropose) {
		return Analysis{}, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationPropose})
	}
	input.RootCauseHypothesis = strings.TrimSpace(input.RootCauseHypothesis)
	input.BlastRadius = strings.TrimSpace(input.BlastRadius)
	input.ImplementationPlan = strings.TrimSpace(input.ImplementationPlan)
	input.TestPlan = strings.TrimSpace(input.TestPlan)
	if input.RootCauseHypothesis == "" || input.ImplementationPlan == "" || input.TestPlan == "" || input.Confidence < 0 || input.Confidence > 1 {
		return Analysis{}, apperr.New(apperr.CodeInvalidArgument, 422, "analysis hypothesis, implementation plan, test plan, and confidence between 0 and 1 are required", nil)
	}
	if len(input.EvidenceRefs) > 30 {
		return Analysis{}, apperr.New(apperr.CodeInvalidArgument, 422, "analysis evidence is too large", nil)
	}
	for index := range input.EvidenceRefs {
		input.EvidenceRefs[index] = strings.TrimSpace(input.EvidenceRefs[index])
		if len(input.EvidenceRefs[index]) > 512 {
			return Analysis{}, apperr.New(apperr.CodeInvalidArgument, 422, "analysis evidence reference is too long", nil)
		}
	}
	analysisID, err := ids.New()
	if err != nil {
		return Analysis{}, err
	}
	input.ID = analysisID
	input.WorkItemID = workItemID
	input.Provenance = AIHypothesis
	input.CreatedBy = actor.ID
	input.CreatedAt = s.now().UTC()
	if err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.AddAnalysis(txCtx, organizationID, projectID, input); err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, actor.Type, "specification.analysis.created", nil, input)
	}); err != nil {
		return Analysis{}, err
	}
	return input, nil
}

func (s *Service) Evaluate(ctx context.Context, organizationID, projectID, workItemID, itemType, title, repositoryID string) (Readiness, error) {
	spec, err := s.store.Get(ctx, organizationID, projectID, workItemID)
	if err != nil {
		return Readiness{}, err
	}
	if spec == nil {
		return Readiness{Missing: []string{"SPECIFICATION"}}, nil
	}
	return Evaluate(spec, itemType, title, repositoryID), nil
}

func (s *Service) Readiness(ctx context.Context, organizationID, projectID, workItemID, itemType, title, repositoryID string) (Readiness, error) {
	spec, err := s.store.Get(ctx, organizationID, projectID, workItemID)
	if err != nil {
		return Readiness{}, err
	}
	if spec == nil {
		return Readiness{Missing: []string{"SPECIFICATION"}}, nil
	}
	readiness := Evaluate(spec, itemType, title, repositoryID)
	readiness.SpecificationVersion = spec.Version
	readiness.ReviewedVersion = spec.ReviewedVersion
	readiness.Reviewed = spec.Version > 0 && spec.ReviewedVersion == spec.Version
	if readiness.Ready && !readiness.Reviewed {
		readiness.Ready = false
		readiness.Missing = append(readiness.Missing, "HUMAN_REVIEW")
	}
	return readiness, nil
}

func (s *Service) Update(ctx context.Context, organizationID, projectID, workItemID, actorType string, input UpdateInput) (*Specification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actorType != "human" {
		return nil, apperr.New(apperr.CodeForbidden, 403, "AI actors must propose specification changes instead of writing facts", nil)
	}
	var result *Specification
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil {
			return apperr.New(apperr.CodeNotFound, 404, "specification not found", nil)
		}
		if input.ExpectedVersion > 0 && input.ExpectedVersion != spec.Version {
			return staleVersionError(input.ExpectedVersion, spec.Version)
		}
		before := clone(spec)
		if input.MediaRefs != nil {
			mediaRefs, err := normalizeMediaRefs(input.MediaRefs)
			if err != nil {
				return err
			}
			if s.mediaValidator != nil {
				if err := s.mediaValidator.ValidateReferences(txCtx, organizationID, projectID, workItemID, mediaReferenceIDs(mediaRefs)); err != nil {
					return err
				}
			}
			spec.MediaRefs = mediaRefs
		}
		if input.Summary != nil {
			spec.Summary = strings.TrimSpace(*input.Summary)
		}
		if input.Fields != nil {
			spec.Fields = make(map[FieldKey]Field, len(input.Fields))
			for key, value := range input.Fields {
				value = strings.TrimSpace(value)
				if existing, ok := before.Fields[key]; ok && strings.TrimSpace(existing.Value) == value {
					spec.Fields[key] = existing
					continue
				}
				spec.Fields[key] = Field{Key: key, Value: value, Provenance: HumanProvided, VerificationStatus: Unverified}
			}
		}
		if input.ReproductionSteps != nil {
			spec.ReproductionSteps = make([]ReproductionStep, len(input.ReproductionSteps))
			for i, step := range input.ReproductionSteps {
				step.Action = strings.TrimSpace(step.Action)
				step.ExpectedResult = strings.TrimSpace(step.ExpectedResult)
				step.ObservedResult = strings.TrimSpace(step.ObservedResult)
				step.EvidenceRefs, err = normalizeEvidenceRefs(step.EvidenceRefs)
				if err != nil {
					return err
				}
				step.Position = i + 1
				if i < len(before.ReproductionSteps) {
					existing := before.ReproductionSteps[i]
					if existing.Action == step.Action && existing.ExpectedResult == step.ExpectedResult && existing.ObservedResult == step.ObservedResult && sameStrings(existing.EvidenceRefs, step.EvidenceRefs) {
						existing.Position = step.Position
						spec.ReproductionSteps[i] = existing
						continue
					}
				}
				step.Provenance = HumanProvided
				step.VerificationStatus = Unverified
				step.VerifiedBy = ""
				step.VerifiedAt = nil
				spec.ReproductionSteps[i] = step
			}
		}
		if input.Acceptance != nil {
			spec.Acceptance = make([]AcceptanceCriterion, len(input.Acceptance))
			for i, criterion := range input.Acceptance {
				criterion.Statement = strings.TrimSpace(criterion.Statement)
				criterion.Position = i + 1
				if i < len(before.Acceptance) && before.Acceptance[i].Statement == criterion.Statement {
					existing := before.Acceptance[i]
					existing.Position = criterion.Position
					spec.Acceptance[i] = existing
					continue
				}
				criterion.Provenance = HumanProvided
				criterion.VerificationStatus = Unverified
				criterion.VerifiedBy = ""
				criterion.VerifiedAt = nil
				spec.Acceptance[i] = criterion
			}
		}
		if input.RegressionCases != nil {
			spec.RegressionCases = make([]RegressionTestCase, len(input.RegressionCases))
			for i, testCase := range input.RegressionCases {
				testCase.Position = i + 1
				testCase.Scenario = strings.TrimSpace(testCase.Scenario)
				testCase.ExpectedResult = strings.TrimSpace(testCase.ExpectedResult)
				testCase.Provenance = HumanProvided
				if i < len(before.RegressionCases) && before.RegressionCases[i].Scenario == testCase.Scenario && before.RegressionCases[i].ExpectedResult == testCase.ExpectedResult {
					existing := before.RegressionCases[i]
					existing.Position = testCase.Position
					spec.RegressionCases[i] = existing
					continue
				}
				testCase.VerificationStatus = Unverified
				testCase.VerifiedBy = ""
				testCase.VerifiedAt = nil
				spec.RegressionCases[i] = testCase
			}
		}
		if input.ContextRefs != nil {
			spec.ContextRefs = make([]ContextRef, len(input.ContextRefs))
			for i, ref := range input.ContextRefs {
				ref.RepositoryID = strings.TrimSpace(ref.RepositoryID)
				ref.Module = strings.TrimSpace(ref.Module)
				ref.File = strings.TrimSpace(ref.File)
				ref.Symbol = strings.TrimSpace(ref.Symbol)
				ref.Commit = strings.TrimSpace(ref.Commit)
				ref.PullRequest = strings.TrimSpace(ref.PullRequest)
				ref.Rationale = strings.TrimSpace(ref.Rationale)
				ref.Provenance = HumanProvided
				spec.ContextRefs[i] = ref
			}
		}
		if input.RepositoryID != nil {
			spec.RepositoryID = strings.TrimSpace(*input.RepositoryID)
		}
		spec.UpdatedAt = s.now().UTC()
		invalidateReview(spec)
		if err := s.store.Save(txCtx, organizationID, projectID, spec); err != nil {
			return err
		}
		if err := s.recordMutation(txCtx, organizationID, workItemID, actorType, "specification.update", before, spec); err != nil {
			return err
		}
		result = spec
		return nil
	})
	return result, err
}

func (s *Service) saveReviewed(ctx context.Context, organizationID, projectID string, spec *Specification, expectedVersion int) error {
	if store, ok := s.store.(compareAndSetSaver); ok {
		return store.SaveExpectedVersion(ctx, organizationID, projectID, spec, expectedVersion)
	}
	return s.store.Save(ctx, organizationID, projectID, spec)
}

type ReviewInput struct {
	ExpectedVersion int
}

func (s *Service) Review(ctx context.Context, organizationID, projectID, workItemID string, actor identity.Actor, input ReviewInput) (*Specification, error) {
	if actor.Type != "human" {
		return nil, apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot review specifications", nil)
	}
	if !actor.Has(identity.CapabilitySpecificationVerify) {
		return nil, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationVerify})
	}
	if input.ExpectedVersion < 1 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "expected_version is required", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result *Specification
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil {
			return apperr.New(apperr.CodeNotFound, 404, "specification not found", nil)
		}
		if input.ExpectedVersion != spec.Version {
			return staleVersionError(input.ExpectedVersion, spec.Version)
		}
		before := clone(spec)
		reviewedAt := s.now().UTC()
		spec.ReviewedVersion = spec.Version
		spec.ReviewedBy = actor.ID
		spec.ReviewedAt = &reviewedAt
		spec.UpdatedAt = reviewedAt
		if err := s.saveReviewed(txCtx, organizationID, projectID, spec, input.ExpectedVersion); err != nil {
			return err
		}
		if err := s.recordMutation(txCtx, organizationID, workItemID, actor.Type, "specification.review", before, spec); err != nil {
			return err
		}
		result = spec
		return nil
	})
	return result, err
}

func Evaluate(spec *Specification, itemType, title, repositoryID string) Readiness {
	missing := make([]string, 0)
	if strings.TrimSpace(title) == "" {
		missing = append(missing, "TITLE")
	}
	if itemType == "BUG" && strings.TrimSpace(spec.Summary) == "" {
		missing = append(missing, "SUMMARY")
	}
	if itemType == "BUG" {
		for _, key := range []FieldKey{ProblemStatement, ExpectedBehavior, ActualBehavior, Environment} {
			field, ok := spec.Fields[key]
			if !ok || strings.TrimSpace(field.Value) == "" {
				missing = append(missing, string(key))
			} else if field.VerificationStatus != VerifiedByHuman || field.Provenance == AIHypothesis || field.Provenance == AIInferred {
				missing = append(missing, "HUMAN_VERIFIED_"+string(key))
			}
		}
		if strings.TrimSpace(repositoryID) == "" && strings.TrimSpace(spec.RepositoryID) == "" {
			missing = append(missing, "REPOSITORY")
		}
		if len(spec.ReproductionSteps) == 0 {
			missing = append(missing, "REPRODUCTION_STEP")
		}
		hasEvidence := false
		for i, step := range spec.ReproductionSteps {
			prefix := "REPRODUCTION_STEP_" + itoa(i+1)
			if strings.TrimSpace(step.Action) == "" {
				missing = append(missing, prefix+"_ACTION")
			}
			if strings.TrimSpace(step.ExpectedResult) == "" {
				missing = append(missing, prefix+"_EXPECTED_RESULT")
			}
			if strings.TrimSpace(step.ObservedResult) == "" {
				missing = append(missing, prefix+"_OBSERVED_RESULT")
			}
			if step.VerificationStatus != VerifiedByHuman || step.Provenance == AIHypothesis || step.Provenance == AIInferred {
				missing = append(missing, prefix+"_HUMAN_VERIFIED")
			}
			if len(step.EvidenceRefs) > 0 {
				hasEvidence = true
			}
		}
		if !hasEvidence {
			missing = append(missing, "EVIDENCE")
		}
		hasComponent := false
		for _, ref := range spec.ContextRefs {
			if strings.TrimSpace(ref.Module) != "" || strings.TrimSpace(ref.File) != "" || strings.TrimSpace(ref.Symbol) != "" {
				hasComponent = true
				break
			}
		}
		if !hasComponent {
			missing = append(missing, "AFFECTED_COMPONENT")
		}
		if len(spec.Acceptance) == 0 {
			missing = append(missing, "ACCEPTANCE_CRITERION")
		}
		for i, criterion := range spec.Acceptance {
			if strings.TrimSpace(criterion.Statement) == "" {
				missing = append(missing, "ACCEPTANCE_CRITERION_"+itoa(i+1))
			}
			if criterion.VerificationStatus != VerifiedByHuman || criterion.Provenance == AIHypothesis || criterion.Provenance == AIInferred {
				missing = append(missing, "HUMAN_VERIFIED_ACCEPTANCE_CRITERION_"+itoa(i+1))
			}
		}
	} else if itemType == "TASK" || itemType == "STORY" {
		if !hasField(spec, Goal) && strings.TrimSpace(spec.Summary) == "" {
			missing = append(missing, "GOAL_OR_PROBLEM_STATEMENT")
		}
		if len(spec.Acceptance) == 0 {
			missing = append(missing, "ACCEPTANCE_CRITERION")
		}
		for i, criterion := range spec.Acceptance {
			if strings.TrimSpace(criterion.Statement) == "" {
				missing = append(missing, "ACCEPTANCE_CRITERION_"+itoa(i+1))
			}
			if criterion.VerificationStatus != VerifiedByHuman || criterion.Provenance == AIHypothesis || criterion.Provenance == AIInferred {
				missing = append(missing, "HUMAN_VERIFIED_ACCEPTANCE_CRITERION_"+itoa(i+1))
			}
		}
		if strings.TrimSpace(repositoryID) == "" && strings.TrimSpace(spec.RepositoryID) == "" && !hasField(spec, NoCodeChange) {
			missing = append(missing, "REPOSITORY_OR_NO_CODE_CHANGE_RATIONALE")
		}
	}
	return Readiness{Ready: len(missing) == 0, Missing: missing, Quality: qualityDimensions(spec, itemType, title, repositoryID)}
}

func qualityDimensions(spec *Specification, itemType, title, repositoryID string) QualityDimensions {
	if spec == nil {
		return QualityDimensions{}
	}
	contentTotal := 1
	contentFilled := 0
	verificationTotal := 0
	verificationFilled := 0
	clarityValues := []string{title, spec.Summary}

	addField := func(field FieldKey, required bool) {
		value := spec.Fields[field]
		if required {
			contentTotal++
			if strings.TrimSpace(value.Value) != "" {
				contentFilled++
			}
			verificationTotal++
			if value.VerificationStatus == VerifiedByHuman && value.Provenance != AIHypothesis && value.Provenance != AIInferred {
				verificationFilled++
			}
		}
		if strings.TrimSpace(value.Value) != "" {
			clarityValues = append(clarityValues, value.Value)
		}
	}

	if strings.TrimSpace(title) != "" {
		contentFilled++
	}
	if itemType == "BUG" {
		contentTotal++
		if strings.TrimSpace(spec.Summary) != "" {
			contentFilled++
		}
		addField(ProblemStatement, true)
		addField(ExpectedBehavior, true)
		addField(ActualBehavior, true)
	} else if itemType == "TASK" || itemType == "STORY" {
		contentTotal++
		if hasField(spec, Goal) || strings.TrimSpace(spec.Summary) != "" {
			contentFilled++
		}
		addField(Goal, false)
	}

	completeSteps := 0
	evidenceRefs := 0
	for _, step := range spec.ReproductionSteps {
		if strings.TrimSpace(step.Action) != "" && strings.TrimSpace(step.ExpectedResult) != "" && strings.TrimSpace(step.ObservedResult) != "" {
			completeSteps++
		}
		evidenceRefs += len(step.EvidenceRefs)
		verificationTotal++
		if step.VerificationStatus == VerifiedByHuman && step.Provenance != AIHypothesis && step.Provenance != AIInferred {
			verificationFilled++
		}
		clarityValues = append(clarityValues, step.Action, step.ExpectedResult, step.ObservedResult)
	}
	for _, criterion := range spec.Acceptance {
		if strings.TrimSpace(criterion.Statement) != "" {
			contentFilled++
		}
		contentTotal++
		verificationTotal++
		if criterion.VerificationStatus == VerifiedByHuman && criterion.Provenance != AIHypothesis && criterion.Provenance != AIInferred {
			verificationFilled++
		}
		clarityValues = append(clarityValues, criterion.Statement)
	}
	for _, testCase := range spec.RegressionCases {
		if strings.TrimSpace(testCase.Scenario) != "" && strings.TrimSpace(testCase.ExpectedResult) != "" {
			contentFilled++
		}
		contentTotal++
		clarityValues = append(clarityValues, testCase.Scenario, testCase.ExpectedResult)
	}

	repositoryLinked := strings.TrimSpace(repositoryID) != "" || strings.TrimSpace(spec.RepositoryID) != ""
	reproTotal := 1
	if itemType == "BUG" {
		reproTotal = maxInt(1, len(spec.ReproductionSteps))
	}
	reproducibility := ratio(completeSteps, reproTotal)
	if itemType != "BUG" {
		reproducibility = ratio(len(spec.Acceptance), maxInt(1, len(spec.Acceptance)))
	}
	testability := ratio(len(spec.Acceptance)+len(spec.RegressionCases), maxInt(1, len(spec.Acceptance)+1))
	evidenceQuality := ratio(evidenceRefs+len(spec.ContextRefs), 3)
	if repositoryLinked {
		evidenceQuality = minFloat(1, evidenceQuality+0.25)
	}
	clarity := 0.0
	if len(clarityValues) > 0 {
		for _, value := range clarityValues {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				clarity += minFloat(1, float64(len([]rune(trimmed)))/80)
			}
		}
		clarity /= float64(len(clarityValues))
	}
	return QualityDimensions{
		Completeness:              ratio(contentFilled, contentTotal),
		Clarity:                   roundQuality(clarity),
		Reproducibility:           roundQuality(reproducibility),
		EvidenceQuality:           roundQuality(evidenceQuality),
		Testability:               roundQuality(testability),
		RepositoryContext:         boolScore(repositoryLinked),
		HumanVerificationCoverage: ratio(verificationFilled, verificationTotal),
	}
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return roundQuality(float64(numerator) / float64(denominator))
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func roundQuality(value float64) float64 {
	return math.Round(minFloat(1, maxFloat(0, value))*100) / 100
}

func (s *Service) Propose(ctx context.Context, organizationID, projectID, workItemID string, field FieldKey, value string, provenance Provenance) (Proposal, error) {
	if provenance != AIInferred && provenance != AIHypothesis {
		return Proposal{}, apperr.New(apperr.CodeInvalidArgument, 422, "AI proposals must use AI_INFERRED or AI_HYPOTHESIS provenance", nil)
	}
	id, err := ids.New()
	if err != nil {
		return Proposal{}, err
	}
	proposal := Proposal{ID: id, WorkItemID: workItemID, Field: field, Value: value, Provenance: provenance, Status: "PENDING", CreatedAt: s.now().UTC()}
	if err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.AddProposal(txCtx, organizationID, projectID, proposal); err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, "agent", "specification.proposal", nil, proposal)
	}); err != nil {
		return Proposal{}, err
	}
	return proposal, nil
}

func (s *Service) AcceptProposal(ctx context.Context, organizationID, projectID, workItemID, proposalID string, actor identity.Actor) (Proposal, error) {
	if !actor.Has(identity.CapabilitySpecificationPropose) {
		return Proposal{}, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationPropose})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result Proposal
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil {
			return apperr.New(apperr.CodeNotFound, 404, "specification not found", nil)
		}
		proposals, err := s.store.Proposals(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		var pending Proposal
		found := false
		for _, candidate := range proposals {
			if candidate.ID == proposalID && candidate.Status == "PENDING" {
				pending = candidate
				found = true
				break
			}
		}
		if !found {
			return apperr.New(apperr.CodeNotFound, 404, "pending specification proposal not found", nil)
		}
		// An explicit accept may replace unverified content, but never silently
		// replaces a human-verified fact. The reviewer must edit the field first.
		// ponytail: one guard at the shared proposal boundary protects every API/MCP caller.
		if current, ok := spec.Fields[pending.Field]; ok && current.VerificationStatus == VerifiedByHuman {
			return apperr.New(apperr.CodeConflict, 409, "cannot accept a proposal over a human-verified field", map[string]any{"field": current.Key})
		}
		proposal, err := s.store.AcceptProposal(txCtx, organizationID, projectID, workItemID, proposalID)
		if err != nil {
			return err
		}
		before := clone(spec)
		if spec.Fields == nil {
			spec.Fields = make(map[FieldKey]Field)
		}
		spec.Fields[proposal.Field] = Field{Key: proposal.Field, Value: proposal.Value, Provenance: proposal.Provenance, VerificationStatus: Unverified, SourceProposalID: proposal.ID}
		spec.UpdatedAt = s.now().UTC()
		invalidateReview(spec)
		if err := s.store.Save(txCtx, organizationID, projectID, spec); err != nil {
			return err
		}
		if err := s.recordMutation(txCtx, organizationID, workItemID, actor.Type, "specification.proposal.accept", before, spec); err != nil {
			return err
		}
		result = proposal
		return nil
	})
	return result, err
}

func (s *Service) RejectProposal(ctx context.Context, organizationID, projectID, workItemID, proposalID string, actor identity.Actor) (Proposal, error) {
	if !actor.Has(identity.CapabilitySpecificationPropose) {
		return Proposal{}, apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": identity.CapabilitySpecificationPropose})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var proposal Proposal
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		proposal, err = s.store.RejectProposal(txCtx, organizationID, projectID, workItemID, proposalID)
		if err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, actor.Type, "specification.proposal.reject", nil, proposal)
	})
	if err != nil {
		return Proposal{}, err
	}
	return proposal, nil
}

func (s *Service) VerifyField(ctx context.Context, organizationID, projectID, workItemID string, field FieldKey, actorType, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actorType != "human" {
		return apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot verify specification fields", map[string]any{"field": field})
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil {
			return apperr.New(apperr.CodeNotFound, 404, "specification not found", nil)
		}
		before := clone(spec)
		value, ok := spec.Fields[field]
		if !ok || strings.TrimSpace(value.Value) == "" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "cannot verify an empty field", map[string]any{"field": field})
		}
		value.VerificationStatus = VerifiedByHuman
		value.VerifiedBy = actorID
		verifiedAt := s.now().UTC()
		value.VerifiedAt = &verifiedAt
		value.Provenance = HumanVerified
		spec.Fields[field] = value
		spec.UpdatedAt = s.now().UTC()
		invalidateReview(spec)
		if err := s.store.Save(txCtx, organizationID, projectID, spec); err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, actorType, "specification.verify_field", before, spec)
	})
}

func (s *Service) VerifyStep(ctx context.Context, organizationID, projectID, workItemID string, position int, actorType, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actorType != "human" {
		return apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot verify reproduction steps", map[string]any{"position": position})
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil || position < 1 || position > len(spec.ReproductionSteps) {
			return apperr.New(apperr.CodeNotFound, 404, "reproduction step not found", nil)
		}
		before := clone(spec)
		step := &spec.ReproductionSteps[position-1]
		if strings.TrimSpace(step.Action) == "" || strings.TrimSpace(step.ExpectedResult) == "" || strings.TrimSpace(step.ObservedResult) == "" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "cannot verify an incomplete reproduction step", map[string]any{"position": position})
		}
		step.VerificationStatus = VerifiedByHuman
		step.Provenance = HumanVerified
		step.VerifiedBy = actorID
		verifiedAt := s.now().UTC()
		step.VerifiedAt = &verifiedAt
		spec.UpdatedAt = s.now().UTC()
		invalidateReview(spec)
		if err := s.store.Save(txCtx, organizationID, projectID, spec); err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, actorType, "specification.verify_step", before, spec)
	})
}

func (s *Service) VerifyAcceptance(ctx context.Context, organizationID, projectID, workItemID string, position int, actorType, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actorType != "human" {
		return apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot verify acceptance criteria", map[string]any{"position": position})
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil || position < 1 || position > len(spec.Acceptance) {
			return apperr.New(apperr.CodeNotFound, 404, "acceptance criterion not found", nil)
		}
		before := clone(spec)
		criterion := &spec.Acceptance[position-1]
		if strings.TrimSpace(criterion.Statement) == "" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "cannot verify an empty acceptance criterion", map[string]any{"position": position})
		}
		criterion.VerificationStatus = VerifiedByHuman
		criterion.Provenance = HumanVerified
		criterion.VerifiedBy = actorID
		verifiedAt := s.now().UTC()
		criterion.VerifiedAt = &verifiedAt
		spec.UpdatedAt = s.now().UTC()
		invalidateReview(spec)
		if err := s.store.Save(txCtx, organizationID, projectID, spec); err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, actorType, "specification.verify_acceptance", before, spec)
	})
}

func (s *Service) VerifyRegression(ctx context.Context, organizationID, projectID, workItemID string, position int, actorType, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actorType != "human" {
		return apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot verify regression test cases", map[string]any{"position": position})
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		spec, err := s.store.Get(txCtx, organizationID, projectID, workItemID)
		if err != nil {
			return err
		}
		if spec == nil || position < 1 || position > len(spec.RegressionCases) {
			return apperr.New(apperr.CodeNotFound, 404, "regression test case not found", nil)
		}
		before := clone(spec)
		testCase := &spec.RegressionCases[position-1]
		if strings.TrimSpace(testCase.Scenario) == "" || strings.TrimSpace(testCase.ExpectedResult) == "" {
			return apperr.New(apperr.CodeInvalidArgument, 422, "cannot verify an incomplete regression test case", map[string]any{"position": position})
		}
		testCase.VerificationStatus = VerifiedByHuman
		testCase.Provenance = HumanVerified
		testCase.VerifiedBy = actorID
		verifiedAt := s.now().UTC()
		testCase.VerifiedAt = &verifiedAt
		spec.UpdatedAt = s.now().UTC()
		invalidateReview(spec)
		if err := s.store.Save(txCtx, organizationID, projectID, spec); err != nil {
			return err
		}
		return s.recordMutation(txCtx, organizationID, workItemID, actorType, "specification.verify_regression", before, spec)
	})
}

func (s *Service) recordMutation(ctx context.Context, organizationID, workItemID, actorType, action string, before, after any) error {
	if s.recorder.Audit == nil && s.recorder.Outbox == nil {
		return nil
	}
	actorID, source := "", "application"
	if actor, ok := identity.ActorFromContext(ctx); ok {
		actorID, source = actor.ID, actor.Source
	}
	createdAt := s.now().UTC()
	if s.recorder.Audit != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actorType, ActorID: actorID, OrganizationID: organizationID, Source: source, Action: action, ResourceType: "specification", ResourceID: workItemID, Before: before, After: after, CreatedAt: createdAt}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		return s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: organizationID, EventType: action, AggregateType: "specification", AggregateID: workItemID, IdempotencyKey: action + ":" + id, Payload: map[string]any{"work_item_id": workItemID}, OccurredAt: createdAt})
	}
	return nil
}

func hasField(spec *Specification, key FieldKey) bool {
	field, ok := spec.Fields[key]
	return ok && strings.TrimSpace(field.Value) != ""
}

func invalidateReview(spec *Specification) {
	if spec.Version < 1 {
		spec.Version = 1
	}
	spec.Version++
	spec.ReviewedVersion = 0
	spec.ReviewedBy = ""
	spec.ReviewedAt = nil
}

func staleVersionError(expected, current int) error {
	return apperr.New(apperr.CodeStaleSpecification, 409, "specification version is stale", map[string]any{"expected_version": expected, "current_version": current})
}

func normalizeEvidenceRefs(values []string) ([]string, error) {
	if len(values) > 30 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "reproduction evidence is too large", nil)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 512 {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "reproduction evidence reference is too long", nil)
		}
		result = append(result, value)
	}
	return result, nil
}

const (
	maxMediaTargets       = 100
	maxMediaRefsPerTarget = 10
	maxMediaRefsTotal     = 300
)

func normalizeMediaRefs(values map[string][]string) (map[string][]string, error) {
	if len(values) > maxMediaTargets {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "too many multimedia fields", nil)
	}
	result := make(map[string][]string, len(values))
	total := 0
	for target, refs := range values {
		target = strings.TrimSpace(target)
		if target == "" || len(target) > 128 || !validMediaTarget(target) {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "invalid multimedia field target", nil)
		}
		if len(refs) > maxMediaRefsPerTarget {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "too many multimedia references for a field", nil)
		}
		seen := make(map[string]struct{}, len(refs))
		for _, ref := range refs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if len(ref) > 512 {
				return nil, apperr.New(apperr.CodeInvalidArgument, 422, "multimedia reference is too long", nil)
			}
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			result[target] = append(result[target], ref)
			total++
			if total > maxMediaRefsTotal {
				return nil, apperr.New(apperr.CodeInvalidArgument, 422, "too many multimedia references", nil)
			}
		}
	}
	return result, nil
}

func validMediaTarget(value string) bool {
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == ':' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func mediaReferenceIDs(values map[string][]string) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, refs := range values {
		for _, ref := range refs {
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			result = append(result, ref)
		}
	}
	return result
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var result [20]byte
	i := len(result)
	for n > 0 {
		i--
		result[i] = byte('0' + n%10)
		n /= 10
	}
	return string(result[i:])
}

type MemoryStore struct {
	mu        sync.Mutex
	specs     map[string]*Specification
	versions  map[string][]FieldVersion
	proposals map[string][]Proposal
	analyses  map[string][]Analysis
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{specs: make(map[string]*Specification), versions: make(map[string][]FieldVersion), proposals: make(map[string][]Proposal), analyses: make(map[string][]Analysis)}
}

func specKey(organizationID, projectID, workItemID string) string {
	return organizationID + ":" + projectID + ":" + workItemID
}

func (s *MemoryStore) Ensure(_ context.Context, organizationID, projectID, workItemID string) (*Specification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := specKey(organizationID, projectID, workItemID)
	if existing := s.specs[key]; existing != nil {
		return clone(existing), nil
	}
	id, err := ids.New()
	if err != nil {
		return nil, err
	}
	specs := &Specification{ID: id, WorkItemID: workItemID, Version: 1, Fields: make(map[FieldKey]Field), UpdatedAt: time.Now().UTC()}
	s.specs[key] = specs
	return clone(specs), nil
}

func (s *MemoryStore) Get(_ context.Context, organizationID, projectID, workItemID string) (*Specification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.specs[specKey(organizationID, projectID, workItemID)]), nil
}

func (s *MemoryStore) FieldVersions(_ context.Context, organizationID, projectID, workItemID string, limit int) ([]FieldVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.versions[specKey(organizationID, projectID, workItemID)]
	if len(all) == 0 {
		return nil, nil
	}
	start := 0
	if limit > 0 && limit < len(all) {
		start = len(all) - limit
	}
	result := make([]FieldVersion, 0, len(all)-start)
	for index := len(all) - 1; index >= start; index-- {
		result = append(result, all[index])
	}
	return result, nil
}

func (s *MemoryStore) Save(_ context.Context, organizationID, projectID string, spec *Specification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(organizationID, projectID, spec)
}

func (s *MemoryStore) SaveExpectedVersion(_ context.Context, organizationID, projectID string, spec *Specification, expectedVersion int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := specKey(organizationID, projectID, spec.WorkItemID)
	existing := s.specs[key]
	if existing == nil {
		return apperr.New(apperr.CodeNotFound, 404, "specification not found", nil)
	}
	if existing.Version != expectedVersion {
		return staleVersionError(expectedVersion, existing.Version)
	}
	return s.saveLocked(organizationID, projectID, spec)
}

func (s *MemoryStore) saveLocked(organizationID, projectID string, spec *Specification) error {
	key := specKey(organizationID, projectID, spec.WorkItemID)
	s.specs[key] = clone(spec)
	revision := 1
	if existing := s.versions[key]; len(existing) > 0 {
		revision = existing[len(existing)-1].Revision + 1
	}
	createdAt := spec.UpdatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	appendVersion := func(field FieldKey, value string, provenance Provenance, verification VerificationStatus, sourceProposalID, verifiedBy string, verifiedAt *time.Time) error {
		id, err := ids.New()
		if err != nil {
			return err
		}
		s.versions[key] = append(s.versions[key], FieldVersion{ID: id, Revision: revision, Field: field, Value: value, Provenance: provenance, VerificationStatus: verification, SourceProposalID: sourceProposalID, VerifiedBy: verifiedBy, VerifiedAt: verifiedAt, CreatedAt: createdAt})
		return nil
	}
	if err := appendVersion("SUMMARY", spec.Summary, HumanProvided, Unverified, "", "", nil); err != nil {
		return err
	}
	fieldKeys := make([]FieldKey, 0, len(spec.Fields))
	for fieldKey := range spec.Fields {
		fieldKeys = append(fieldKeys, fieldKey)
	}
	sort.Slice(fieldKeys, func(i, j int) bool { return fieldKeys[i] < fieldKeys[j] })
	for _, fieldKey := range fieldKeys {
		field := spec.Fields[fieldKey]
		if err := appendVersion(fieldKey, field.Value, field.Provenance, field.VerificationStatus, field.SourceProposalID, field.VerifiedBy, field.VerifiedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) AddProposal(_ context.Context, organizationID, projectID string, proposal Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proposals[specKey(organizationID, projectID, proposal.WorkItemID)] = append(s.proposals[specKey(organizationID, projectID, proposal.WorkItemID)], proposal)
	return nil
}

func (s *MemoryStore) Proposals(_ context.Context, organizationID, projectID, workItemID string) ([]Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Proposal(nil), s.proposals[specKey(organizationID, projectID, workItemID)]...), nil
}

func (s *MemoryStore) AcceptProposal(_ context.Context, organizationID, projectID, workItemID, proposalID string) (Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.proposals[specKey(organizationID, projectID, workItemID)]
	for index, proposal := range items {
		if proposal.ID == proposalID && proposal.Status == "PENDING" {
			proposal.Status = "ACCEPTED"
			items[index] = proposal
			s.proposals[specKey(organizationID, projectID, workItemID)] = items
			return proposal, nil
		}
	}
	return Proposal{}, apperr.New(apperr.CodeNotFound, 404, "pending specification proposal not found", nil)
}

func (s *MemoryStore) RejectProposal(_ context.Context, organizationID, projectID, workItemID, proposalID string) (Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := specKey(organizationID, projectID, workItemID)
	items := s.proposals[key]
	for index, proposal := range items {
		if proposal.ID == proposalID && proposal.Status == "PENDING" {
			proposal.Status = "REJECTED"
			items[index] = proposal
			s.proposals[key] = items
			return proposal, nil
		}
	}
	return Proposal{}, apperr.New(apperr.CodeNotFound, 404, "pending specification proposal not found", nil)
}

func (s *MemoryStore) AddAnalysis(_ context.Context, organizationID, projectID string, analysis Analysis) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := specKey(organizationID, projectID, analysis.WorkItemID)
	s.analyses[key] = append(s.analyses[key], analysis)
	return nil
}

func (s *MemoryStore) Analyses(_ context.Context, organizationID, projectID, workItemID string) ([]Analysis, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Analysis(nil), s.analyses[specKey(organizationID, projectID, workItemID)]...), nil
}

func clone(spec *Specification) *Specification {
	if spec == nil {
		return nil
	}
	result := *spec
	result.Fields = make(map[FieldKey]Field, len(spec.Fields))
	for key, field := range spec.Fields {
		result.Fields[key] = field
	}
	result.ReproductionSteps = append([]ReproductionStep(nil), spec.ReproductionSteps...)
	for index := range result.ReproductionSteps {
		result.ReproductionSteps[index].EvidenceRefs = append([]string(nil), spec.ReproductionSteps[index].EvidenceRefs...)
	}
	result.Acceptance = append([]AcceptanceCriterion(nil), spec.Acceptance...)
	result.RegressionCases = append([]RegressionTestCase(nil), spec.RegressionCases...)
	result.ContextRefs = append([]ContextRef(nil), spec.ContextRefs...)
	result.MediaRefs = make(map[string][]string, len(spec.MediaRefs))
	for target, refs := range spec.MediaRefs {
		result.MediaRefs[target] = append([]string(nil), refs...)
	}
	return &result
}
