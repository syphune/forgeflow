package specification

import (
	"context"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestBugReadinessRequiresHumanVerifiedEvidence(t *testing.T) {
	spec := &Specification{
		Summary: "Login fails",
		Fields: map[FieldKey]Field{
			ProblemStatement: {Key: ProblemStatement, Value: "Users cannot log in", Provenance: HumanProvided, VerificationStatus: VerifiedByHuman},
			ExpectedBehavior: {Key: ExpectedBehavior, Value: "User is signed in", Provenance: HumanProvided, VerificationStatus: VerifiedByHuman},
			ActualBehavior:   {Key: ActualBehavior, Value: "Server returns 500", Provenance: AIInferred, VerificationStatus: Unverified},
			Environment:      {Key: Environment, Value: "Production web", Provenance: HumanProvided, VerificationStatus: VerifiedByHuman},
		},
		ReproductionSteps: []ReproductionStep{{Position: 1, Action: "Open login", ExpectedResult: "Login form appears", ObservedResult: "500 appears", EvidenceRefs: []string{"attachment-1"}, Provenance: HumanProvided, VerificationStatus: VerifiedByHuman}},
		Acceptance:        []AcceptanceCriterion{{Position: 1, Statement: "A valid user can sign in", Provenance: HumanProvided, VerificationStatus: VerifiedByHuman}},
		ContextRefs:       []ContextRef{{Module: "auth"}},
	}
	readiness := Evaluate(spec, "BUG", "Login fails", "repo-1")
	if readiness.Ready {
		t.Fatal("AI-inferred actual behavior must not pass the gate")
	}
	if !contains(readiness.Missing, "HUMAN_VERIFIED_ACTUAL_BEHAVIOR") {
		t.Fatalf("missing = %#v", readiness.Missing)
	}

	actual := spec.Fields[ActualBehavior]
	actual.Provenance = HumanVerified
	actual.VerificationStatus = VerifiedByHuman
	spec.Fields[ActualBehavior] = actual
	if readiness = Evaluate(spec, "BUG", "Login fails", "repo-1"); !readiness.Ready {
		t.Fatalf("complete bug should pass, missing = %#v", readiness.Missing)
	}
}

func TestTaskRequiresVerifiedAcceptance(t *testing.T) {
	spec := &Specification{Fields: map[FieldKey]Field{Goal: {Key: Goal, Value: "Export a report"}}, Acceptance: []AcceptanceCriterion{{Statement: "CSV downloads", Provenance: AIHypothesis, VerificationStatus: Unverified}}}
	readiness := Evaluate(spec, "TASK", "Export", "repo-1")
	if readiness.Ready || !contains(readiness.Missing, "HUMAN_VERIFIED_ACCEPTANCE_CRITERION_1") {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestReadinessIncludesGuidanceQualityWithoutChangingTheGate(t *testing.T) {
	spec := &Specification{
		Summary:    "Export report",
		Fields:     map[FieldKey]Field{Goal: {Key: Goal, Value: "Export a report"}},
		Acceptance: []AcceptanceCriterion{{Statement: "CSV downloads", Provenance: HumanProvided, VerificationStatus: Unverified}},
	}
	readiness := Evaluate(spec, "TASK", "Export", "repo-1")
	if readiness.Ready || readiness.Quality.Completeness <= 0 || readiness.Quality.RepositoryContext != 1 {
		t.Fatalf("readiness quality = %#v", readiness)
	}
}

func TestBugReadinessRequiresSummaryAndAcceptance(t *testing.T) {
	spec := &Specification{
		Fields: map[FieldKey]Field{
			ProblemStatement: {Value: "Users cannot log in", VerificationStatus: VerifiedByHuman, Provenance: HumanVerified},
			ExpectedBehavior: {Value: "User is signed in", VerificationStatus: VerifiedByHuman, Provenance: HumanVerified},
			ActualBehavior:   {Value: "Server returns 500", VerificationStatus: VerifiedByHuman, Provenance: HumanVerified},
		},
		ReproductionSteps: []ReproductionStep{{Action: "Open login", ExpectedResult: "Form appears", ObservedResult: "500 appears", Provenance: HumanVerified, VerificationStatus: VerifiedByHuman}},
	}
	readiness := Evaluate(spec, "BUG", "Login fails", "repo-1")
	if readiness.Ready || !contains(readiness.Missing, "SUMMARY") || !contains(readiness.Missing, "ACCEPTANCE_CRITERION") {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestAIActorCannotVerifyAndProposalStaysUnverified(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	store := NewMemoryStore()
	s := NewService(store, now)
	if _, err := s.Ensure(ctx, "org-1", "project-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	proposal, err := s.Propose(ctx, "org-1", "project-1", "item-1", ActualBehavior, "looks broken", AIHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Provenance != AIHypothesis || proposal.Status != "PENDING" {
		t.Fatalf("proposal = %#v", proposal)
	}
	if err := s.VerifyField(ctx, "org-1", "project-1", "item-1", ActualBehavior, "agent", "agent-1"); err == nil {
		t.Fatal("AI actor must not verify")
	}
}

func TestHumanVerificationRecordsStepAndAcceptanceActor(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	store := NewMemoryStore()
	s := NewService(store, now)
	spec, err := s.Ensure(ctx, "org-1", "project-1", "item-1")
	if err != nil {
		t.Fatal(err)
	}
	spec.ReproductionSteps = []ReproductionStep{{Position: 1, Action: "Open", ExpectedResult: "Ready", ObservedResult: "Broken", Provenance: HumanProvided, VerificationStatus: Unverified}}
	spec.Acceptance = []AcceptanceCriterion{{Position: 1, Statement: "Works", Provenance: HumanProvided, VerificationStatus: Unverified}}
	if err := store.Save(ctx, "org-1", "project-1", spec); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyStep(ctx, "org-1", "project-1", "item-1", 1, "human", "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyAcceptance(ctx, "org-1", "project-1", "item-1", 1, "human", "user-1"); err != nil {
		t.Fatal(err)
	}
	verified, err := s.Get(ctx, "org-1", "project-1", "item-1")
	if err != nil || verified.ReproductionSteps[0].VerifiedBy != "user-1" || verified.Acceptance[0].VerifiedBy != "user-1" {
		t.Fatalf("verified spec = %#v, err = %v", verified, err)
	}
}

func TestHumanVerificationRecordsRegressionCase(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := NewService(store, func() time.Time { return time.Unix(100, 0).UTC() })
	spec, err := s.Ensure(ctx, "org-1", "project-1", "item-1")
	if err != nil {
		t.Fatal(err)
	}
	spec.RegressionCases = []RegressionTestCase{{Scenario: "Submit an invalid form", ExpectedResult: "Validation is shown", Provenance: HumanProvided, VerificationStatus: Unverified}}
	if err := store.Save(ctx, "org-1", "project-1", spec); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyRegression(ctx, "org-1", "project-1", "item-1", 1, "agent", "agent-1"); err == nil {
		t.Fatal("AI actor must not verify regression cases")
	}
	if err := s.VerifyRegression(ctx, "org-1", "project-1", "item-1", 1, "human", "user-1"); err != nil {
		t.Fatal(err)
	}
	verified, err := s.Get(ctx, "org-1", "project-1", "item-1")
	if err != nil || verified.RegressionCases[0].Provenance != HumanVerified || verified.RegressionCases[0].VerifiedBy != "user-1" {
		t.Fatalf("verified regression case = %#v, err = %v", verified, err)
	}
}

func TestAIAnalysisIsStoredAsHypothesis(t *testing.T) {
	ctx := identity.WithActor(context.Background(), identity.Actor{Type: "agent", ID: "agent-1", Source: "test"})
	store := NewMemoryStore()
	s := NewService(store, func() time.Time { return time.Unix(100, 0).UTC() })
	actor := identity.Actor{Type: "agent", ID: "agent-1", Capabilities: map[string]bool{identity.CapabilitySpecificationPropose: true}}
	if _, err := s.Ensure(ctx, "org-1", "project-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	analysis, err := s.AddAnalysis(ctx, "org-1", "project-1", "item-1", actor, Analysis{
		RootCauseHypothesis: "A stale cache may serve an old permission result",
		ImplementationPlan:  "Trace cache invalidation and add a regression test",
		TestPlan:            "Run the permission matrix after invalidation",
		Confidence:          0.7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Provenance != AIHypothesis || analysis.CreatedBy != actor.ID {
		t.Fatalf("analysis = %#v", analysis)
	}
	items, err := s.Analyses(ctx, "org-1", "project-1", "item-1")
	if err != nil || len(items) != 1 || items[0].Provenance != AIHypothesis {
		t.Fatalf("analyses = %#v, err = %v", items, err)
	}
}

func TestHumanCanRejectPendingProposal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := NewService(store, func() time.Time { return time.Unix(100, 0).UTC() })
	if _, err := s.Ensure(ctx, "org-1", "project-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	proposal, err := s.Propose(ctx, "org-1", "project-1", "item-1", ActualBehavior, "maybe broken", AIHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.Actor{Type: "human", ID: "user-1", Capabilities: map[string]bool{identity.CapabilitySpecificationPropose: true}}
	rejected, err := s.RejectProposal(ctx, "org-1", "project-1", "item-1", proposal.ID, actor)
	if err != nil || rejected.Status != "REJECTED" {
		t.Fatalf("rejected = %#v, err = %v", rejected, err)
	}
	items, err := s.Proposals(ctx, "org-1", "project-1", "item-1")
	if err != nil || len(items) != 1 || items[0].Status != "REJECTED" {
		t.Fatalf("proposals = %#v, err = %v", items, err)
	}
}

func TestProposalCannotOverwriteHumanVerifiedField(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := NewService(store, func() time.Time { return time.Unix(100, 0).UTC() })
	spec, err := s.Ensure(ctx, "org-1", "project-1", "item-1")
	if err != nil {
		t.Fatal(err)
	}
	spec.Fields = map[FieldKey]Field{
		ActualBehavior: {Key: ActualBehavior, Value: "Verified report", Provenance: HumanVerified, VerificationStatus: VerifiedByHuman},
	}
	if err := store.Save(ctx, "org-1", "project-1", spec); err != nil {
		t.Fatal(err)
	}
	proposal, err := s.Propose(ctx, "org-1", "project-1", "item-1", ActualBehavior, "AI replacement", AIHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.Actor{Type: "human", ID: "user-1", Capabilities: map[string]bool{identity.CapabilitySpecificationPropose: true}}
	_, err = s.AcceptProposal(ctx, "org-1", "project-1", "item-1", proposal.ID, actor)
	if err == nil || apperr.From(err).Code != apperr.CodeConflict {
		t.Fatalf("accept error = %v, want conflict", err)
	}
	unchanged, err := s.Get(ctx, "org-1", "project-1", "item-1")
	if err != nil || unchanged.Fields[ActualBehavior].Value != "Verified report" {
		t.Fatalf("verified field changed: %#v, err = %v", unchanged, err)
	}
	items, err := s.Proposals(ctx, "org-1", "project-1", "item-1")
	if err != nil || len(items) != 1 || items[0].Status != "PENDING" {
		t.Fatalf("proposal status = %#v, err = %v", items, err)
	}
}

func TestSpecificationUpdatePreservesUnchangedVerificationAndEvidence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := NewService(store, func() time.Time { return time.Unix(100, 0).UTC() })
	spec, err := s.Ensure(ctx, "org-1", "project-1", "item-1")
	if err != nil {
		t.Fatal(err)
	}
	spec.Fields = map[FieldKey]Field{
		ActualBehavior: {Key: ActualBehavior, Value: "Verified report", Provenance: HumanVerified, VerificationStatus: VerifiedByHuman, VerifiedBy: "user-1"},
	}
	spec.ReproductionSteps = []ReproductionStep{{Position: 1, Action: "Open", ExpectedResult: "Ready", ObservedResult: "Broken", EvidenceRefs: []string{"attachment-1"}, Provenance: HumanVerified, VerificationStatus: VerifiedByHuman, VerifiedBy: "user-1"}}
	if err := store.Save(ctx, "org-1", "project-1", spec); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Update(ctx, "org-1", "project-1", "item-1", "human", UpdateInput{
		Fields:            map[FieldKey]string{ActualBehavior: "Verified report"},
		ReproductionSteps: spec.ReproductionSteps,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Fields[ActualBehavior].VerificationStatus != VerifiedByHuman || updated.ReproductionSteps[0].VerificationStatus != VerifiedByHuman || len(updated.ReproductionSteps[0].EvidenceRefs) != 1 {
		t.Fatalf("verification/evidence was reset: %#v", updated)
	}
}

func TestSpecificationUpdateStoresMediaPerField(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	s := NewService(store, func() time.Time { return time.Unix(100, 0).UTC() })
	if _, err := s.Ensure(ctx, "org-1", "project-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Update(ctx, "org-1", "project-1", "item-1", "human", UpdateInput{
		MediaRefs: map[string][]string{
			"field:ACTUAL_BEHAVIOR": {"attachment-1"},
			"acceptance:1":          {"attachment-2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.MediaRefs["field:ACTUAL_BEHAVIOR"]) != 1 || updated.MediaRefs["field:ACTUAL_BEHAVIOR"][0] != "attachment-1" || updated.MediaRefs["acceptance:1"][0] != "attachment-2" {
		t.Fatalf("media refs = %#v", updated.MediaRefs)
	}
	loaded, err := s.Get(ctx, "org-1", "project-1", "item-1")
	if err != nil || loaded.MediaRefs["field:ACTUAL_BEHAVIOR"][0] != "attachment-1" {
		t.Fatalf("stored media refs = %#v, err = %v", loaded.MediaRefs, err)
	}
}

func TestMemoryFieldVersionsAreAppendOnlyAndNewestFirst(t *testing.T) {
	store := NewMemoryStore()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	spec, err := store.Ensure(context.Background(), "org-1", "project-1", "item-1")
	if err != nil {
		t.Fatal(err)
	}
	spec.Summary = "first"
	spec.Fields = map[FieldKey]Field{ProblemStatement: {Key: ProblemStatement, Value: "one", Provenance: HumanProvided, VerificationStatus: Unverified}}
	spec.UpdatedAt = now()
	if err := store.Save(context.Background(), "org-1", "project-1", spec); err != nil {
		t.Fatal(err)
	}
	spec.Summary = "second"
	spec.Fields[ProblemStatement] = Field{Key: ProblemStatement, Value: "two", Provenance: HumanVerified, VerificationStatus: VerifiedByHuman}
	spec.UpdatedAt = now().Add(time.Minute)
	if err := store.Save(context.Background(), "org-1", "project-1", spec); err != nil {
		t.Fatal(err)
	}
	versions, err := store.FieldVersions(context.Background(), "org-1", "project-1", "item-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Revision != 2 || versions[0].Field != ProblemStatement || versions[0].Value != "two" || versions[1].Revision != 2 || versions[1].Field != "SUMMARY" {
		t.Fatalf("versions = %#v", versions)
	}
	all, err := store.FieldVersions(context.Background(), "org-1", "project-1", "item-1", 100)
	if err != nil || len(all) != 4 {
		t.Fatalf("all versions = %#v, err = %v", all, err)
	}
}

func TestSpecificationMutationsRecordAuditAndOutbox(t *testing.T) {
	ctx := identity.WithActor(context.Background(), identity.Actor{Type: "human", ID: "user-1", Source: "test"})
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	store := NewMemoryStore()
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	s := NewService(store, now, Options{Recorder: MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	if _, err := s.Ensure(ctx, "org-1", "project-1", "item-1"); err != nil {
		t.Fatal(err)
	}
	value := "Users cannot sign in"
	if _, err := s.Update(ctx, "org-1", "project-1", "item-1", "human", UpdateInput{Fields: map[FieldKey]string{ActualBehavior: value}}); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyField(ctx, "org-1", "project-1", "item-1", ActualBehavior, "human", "user-1"); err != nil {
		t.Fatal(err)
	}
	if len(auditWriter.Records()) != 2 || len(outboxWriter.Events()) != 2 {
		t.Fatalf("audit/events = %d/%d, want 2/2", len(auditWriter.Records()), len(outboxWriter.Events()))
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
