package workitem

import (
	"context"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
)

func TestTransitionAndTenantScope(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	specStore := specification.NewMemoryStore()
	specService := specification.NewService(specStore, now)
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	repository := NewMemoryRepository(now)
	service := NewService(repository, specService, workflow.NewService(workflow.Default()), auditWriter, outboxWriter, now)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Source: "test", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "FF"}
	item, err := service.Create(ctx, scope, actor, CreateInput{Type: Bug, Title: "Broken login", RepositoryID: "repo-1"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "FF-1" || item.Status != workflow.Raw {
		t.Fatalf("item = %#v", item)
	}

	for _, transition := range []string{"start_refining", "request_review"} {
		item, err = service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: transition, ExpectedVersion: item.Version})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "mark_ready", ExpectedVersion: item.Version}); err == nil {
		t.Fatal("READY must be rejected without a complete specification")
	}

	spec, err := specService.Get(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec.Summary = "Broken login"
	spec.Fields = map[specification.FieldKey]specification.Field{
		specification.ProblemStatement: {Key: specification.ProblemStatement, Value: "Login returns an error", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman},
		specification.ExpectedBehavior: {Key: specification.ExpectedBehavior, Value: "User signs in", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman},
		specification.ActualBehavior:   {Key: specification.ActualBehavior, Value: "500 response", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman},
		specification.Environment:      {Key: specification.Environment, Value: "Production web", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman},
	}
	spec.ReproductionSteps = []specification.ReproductionStep{{Position: 1, Action: "Submit credentials", ExpectedResult: "Dashboard opens", ObservedResult: "500 response", EvidenceRefs: []string{"attachment-1"}, Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}}
	spec.Acceptance = []specification.AcceptanceCriterion{{Position: 1, Statement: "Valid credentials open dashboard", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}}
	spec.ContextRefs = []specification.ContextRef{{Module: "auth"}}
	if err := specStore.Save(ctx, scope.OrganizationID, scope.ProjectID, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := specService.Review(ctx, scope.OrganizationID, scope.ProjectID, item.ID, actor, specification.ReviewInput{ExpectedVersion: spec.Version}); err != nil {
		t.Fatal(err)
	}
	item, err = service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "mark_ready", ExpectedVersion: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != workflow.Ready {
		t.Fatalf("status = %q", item.Status)
	}

	if _, err := service.Get(ctx, Scope{OrganizationID: "other-org", ProjectID: scope.ProjectID}, identity.Actor{Type: "human", ID: "other", OrganizationID: "other-org", Capabilities: map[string]bool{"*": true}}, item.ID); err == nil {
		t.Fatal("cross-tenant read must be rejected")
	}
	if len(auditWriter.Records()) != 4 || len(outboxWriter.Events()) != 4 {
		t.Fatalf("audit/events = %d/%d, want 4/4", len(auditWriter.Records()), len(outboxWriter.Events()))
	}
}

func TestWorkItemSpecificationStateFollowsConfiguredWorkflow(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	specStore := specification.NewMemoryStore()
	specService := specification.NewService(specStore, now)
	workflowService := workflow.NewService(workflow.Default())
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	if _, err := workflowService.SaveForProject(ctx, actor, "project-1", workflow.SaveInput{
		Name: "Custom flow",
		Statuses: []workflow.Status{
			{Key: workflow.Raw, Name: "Inbox", Category: "TODO", Position: 10},
			{Key: "ANALYSIS", Name: "Analysis", Category: "TODO", Position: 20},
			{Key: "REVIEWING", Name: "Reviewing", Category: "TODO", Position: 30},
			{Key: workflow.Ready, Name: "Ready", Category: "TODO", Position: 40},
		},
		Transitions: []workflow.Transition{
			{Key: "begin", From: workflow.Raw, To: "ANALYSIS", Name: "Begin analysis"},
			{Key: "submit", From: "ANALYSIS", To: "REVIEWING", Name: "Submit for review"},
			{Key: "approve", From: "REVIEWING", To: workflow.Ready, Name: "Approve"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(NewMemoryRepository(now), specService, workflowService, audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	specService.SetWorkItemStateReader(service)
	scope := Scope{OrganizationID: actor.OrganizationID, ProjectID: "project-1"}
	item, err := service.Create(ctx, scope, actor, CreateInput{Type: Task, Title: "Custom state task"})
	if err != nil {
		t.Fatal(err)
	}
	transitionKey, err := service.RefinementTransitionKey(ctx, scope.OrganizationID, scope.ProjectID)
	if err != nil || transitionKey != "begin" {
		t.Fatalf("refinement transition = %q, err = %v", transitionKey, err)
	}
	state, err := service.WorkItemState(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil || state.SpecificationPhase != specification.SpecificationPhaseRaw {
		t.Fatalf("raw state = %#v, err = %v", state, err)
	}
	item, err = service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "begin", ExpectedVersion: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.WorkItemState(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil || state.SpecificationPhase != specification.SpecificationPhaseRefining {
		t.Fatalf("custom refining state = %#v, err = %v", state, err)
	}
	item, err = service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "submit", ExpectedVersion: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.WorkItemState(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil || state.SpecificationPhase != specification.SpecificationPhaseReview {
		t.Fatalf("custom review state = %#v, err = %v", state, err)
	}
	spec, err := specService.Get(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec.Fields[specification.Goal] = specification.Field{Key: specification.Goal, Value: "Ship the task", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}
	spec.Fields[specification.NoCodeChange] = specification.Field{Key: specification.NoCodeChange, Value: "No repository change", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}
	spec.Acceptance = []specification.AcceptanceCriterion{{Position: 1, Statement: "The task is complete", Provenance: specification.HumanProvided, VerificationStatus: specification.VerifiedByHuman}}
	if err := specStore.Save(ctx, scope.OrganizationID, scope.ProjectID, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := specService.Review(ctx, scope.OrganizationID, scope.ProjectID, item.ID, actor, specification.ReviewInput{ExpectedVersion: spec.Version}); err != nil {
		t.Fatal(err)
	}
	item, err = service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "approve", ExpectedVersion: item.Version})
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.WorkItemState(ctx, scope.OrganizationID, scope.ProjectID, item.ID)
	if err != nil || state.SpecificationPhase != specification.SpecificationPhaseLocked {
		t.Fatalf("custom ready state = %#v, err = %v", state, err)
	}
}

func TestStaleVersionCannotTransitionTwice(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	specStore := specification.NewMemoryStore()
	service := NewService(NewMemoryRepository(now), specification.NewService(specStore, now), workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Source: "test", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	item, err := service.Create(ctx, scope, actor, CreateInput{Type: Task, Title: "Task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "start_refining", ExpectedVersion: item.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(ctx, scope, actor, item.ID, TransitionInput{TransitionKey: "request_review", ExpectedVersion: item.Version}); err == nil {
		t.Fatal("stale version must fail")
	}
}

func TestTransitionEnforcesConfiguredPermissionRule(t *testing.T) {
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	custom := workflow.Default()
	custom.Transitions["start_refining"] = workflow.Transition{
		Key: "start_refining", From: workflow.Raw, To: workflow.Refining, Name: "Start refining",
		Required: []workflow.RuleType{workflow.RequirePermission}, RequiredPermissions: []string{"work_item.special"},
	}
	service := NewService(NewMemoryRepository(now), specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(custom), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityWorkItemCreate: true, identity.CapabilityWorkItemTransition: true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	item, err := service.Create(context.Background(), scope, actor, CreateInput{Type: Task, Title: "Restricted task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(context.Background(), scope, actor, item.ID, TransitionInput{TransitionKey: "start_refining", ExpectedVersion: item.Version}); err == nil {
		t.Fatal("expected permission rule to reject the transition")
	}
	actor.Capabilities["work_item.special"] = true
	if _, err := service.Transition(context.Background(), scope, actor, item.ID, TransitionInput{TransitionKey: "start_refining", ExpectedVersion: item.Version}); err != nil {
		t.Fatal(err)
	}
}

func TestPullRequestReviewExcludesAuthorAndEnforcesPathRoles(t *testing.T) {
	evidence := PullRequestEvidence{
		AuthorLogin:      "author",
		ApprovedByLogins: []string{"author", "security-reviewer"},
		ReviewerRoles:    map[string][]string{"security-reviewer": {"security_reviewer"}},
		ChangedPaths:     []string{"payments/checkout.go", "payments/invoices/restore.go"},
	}
	policy := ReviewPolicy{MinApprovals: 1, ForbidSelfApproval: true, PathRules: []PathReviewRule{{Pattern: "payments/**", MinApprovals: 1, RequiredRoles: []string{"security_reviewer"}}}}
	if err := validatePullRequestReview(evidence, policy); err != nil {
		t.Fatalf("valid independent path review rejected: %v", err)
	}
	evidence.ApprovedByLogins = []string{"author"}
	if err := validatePullRequestReview(evidence, policy); err == nil {
		t.Fatal("author approval must not satisfy the review gate")
	}
	evidence.ApprovedByLogins = []string{"security-reviewer"}
	evidence.ReviewerRoles = map[string][]string{"security-reviewer": {"developer"}}
	if err := validatePullRequestReview(evidence, policy); err == nil {
		t.Fatal("path-sensitive role must be enforced")
	}
}

func TestMemoryListIsDeterministic(t *testing.T) {
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	repository := NewMemoryRepository(now)
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "FF"}
	for _, title := range []string{"First", "Second", "Third"} {
		if _, err := repository.Create(context.Background(), scope, CreateInput{Type: Task, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repository.List(context.Background(), scope, ListFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.List(context.Background(), scope, ListFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("list order changed at %d: %q then %q", index, first[index].ID, second[index].ID)
		}
	}
}

func TestListCanExcludeTerminalStatusesWithoutBreakingExplicitStatusFilter(t *testing.T) {
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	repository := NewMemoryRepository(now)
	workflowService := workflow.NewService(workflow.Default())
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflowService, audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	active, err := service.Create(context.Background(), scope, actor, CreateInput{Type: Task, Title: "Still working"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := service.Create(context.Background(), scope, actor, CreateInput{Type: Task, Title: "Already complete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Update(context.Background(), scope, done.ID, done.Version, func(item *WorkItem) error {
		item.Status = workflow.Done
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	activePage, err := service.ListPage(context.Background(), scope, actor, ListFilter{Sort: "backlog", Limit: 1, ExcludeTerminal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(activePage.Items) != 1 || activePage.Items[0].ID != active.ID || activePage.NextCursor != "" {
		t.Fatalf("active backlog page = %#v, want only active item without another page", activePage)
	}

	all, err := service.List(context.Background(), scope, actor, ListFilter{Sort: "backlog", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered list = %d, want 2", len(all))
	}

	doneOnly, err := service.List(context.Background(), scope, actor, ListFilter{Status: workflow.Done, ExcludeTerminal: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(doneOnly) != 1 || doneOnly[0].ID != done.ID {
		t.Fatalf("explicit terminal filter = %#v, want completed item", doneOnly)
	}
}

func TestTerminalCompletionTimestampDrivesBoardRetention(t *testing.T) {
	ctx := context.Background()
	clock := time.Unix(100, 0).UTC()
	now := func() time.Time { return clock }
	repository := NewMemoryRepository(now)
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}

	old, err := service.Create(ctx, scope, actor, CreateInput{Type: Task, Title: "Old completed item"})
	if err != nil {
		t.Fatal(err)
	}
	old, err = service.Transition(ctx, scope, actor, old.ID, TransitionInput{TransitionKey: "cancel", ExpectedVersion: old.Version})
	if err != nil {
		t.Fatal(err)
	}
	if old.CompletedAt == nil || !old.CompletedAt.Equal(clock) {
		t.Fatalf("completed_at = %v, want %v", old.CompletedAt, clock)
	}

	clock = clock.Add(8 * 24 * time.Hour)
	title := "Old completed item renamed"
	updated, err := service.Update(ctx, scope, actor, old.ID, UpdateInput{Title: &title, ExpectedVersion: old.Version})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CompletedAt == nil || !updated.CompletedAt.Equal(*old.CompletedAt) {
		t.Fatalf("completed_at changed during edit: %v, want %v", updated.CompletedAt, old.CompletedAt)
	}

	recent, err := service.Create(ctx, scope, actor, CreateInput{Type: Task, Title: "Recent completed item"})
	if err != nil {
		t.Fatal(err)
	}
	recent, err = service.Transition(ctx, scope, actor, recent.ID, TransitionInput{TransitionKey: "cancel", ExpectedVersion: recent.Version})
	if err != nil {
		t.Fatal(err)
	}
	cutoff := clock.Add(-7 * 24 * time.Hour)
	visible, err := service.List(ctx, scope, actor, ListFilter{Sort: "backlog", Limit: 10, ExcludeTerminalCompletedBefore: &cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ID != recent.ID {
		t.Fatalf("retained completed items = %#v, want only %q", visible, recent.ID)
	}

	explicit, err := service.List(ctx, scope, actor, ListFilter{Status: workflow.Cancelled, Sort: "backlog", Limit: 10, ExcludeTerminalCompletedBefore: &cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit) != 2 {
		t.Fatalf("explicit terminal filter = %#v, want both completed items", explicit)
	}
}

func TestTerminalCompletionTimestampIsSetForBoardMove(t *testing.T) {
	clock := time.Unix(100, 0).UTC()
	now := func() time.Time { return clock }
	repository := NewMemoryRepository(now)
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	item, err := service.Create(context.Background(), scope, actor, CreateInput{Type: Task, Title: "Drag to completed"})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := service.ColumnOrderingVersions(context.Background(), scope, actor, "")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := service.Move(context.Background(), scope, actor, item.ID, MoveInput{TargetStatus: workflow.Cancelled, TransitionKey: "cancel", ExpectedVersion: item.Version, ExpectedSourceOrderingVersion: versions[workflow.Raw], ExpectedDestinationOrderingVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Item.CompletedAt == nil || !moved.Item.CompletedAt.Equal(clock) {
		t.Fatalf("board move completed_at = %v, want %v", moved.Item.CompletedAt, clock)
	}
}

func TestAtomicMoveUsesPerColumnOrderingVersions(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	repository := NewMemoryRepository(now)
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "FF"}
	items := make([]*WorkItem, 0, 3)
	for _, title := range []string{"First", "Second", "Third"} {
		item, err := service.Create(ctx, scope, actor, CreateInput{Type: Task, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	versions, err := service.ColumnOrderingVersions(ctx, scope, actor, "")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := service.Move(ctx, scope, actor, items[2].ID, MoveInput{TargetStatus: workflow.Raw, AfterID: items[0].ID, ExpectedVersion: items[2].Version, ExpectedSourceOrderingVersion: versions[workflow.Raw], ExpectedDestinationOrderingVersion: versions[workflow.Raw]})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Item.BacklogRank >= items[0].BacklogRank || moved.SourceOrderingVersion != versions[workflow.Raw]+1 {
		t.Fatalf("move = %#v, first rank = %d", moved, items[0].BacklogRank)
	}
	if _, err := service.Move(ctx, scope, actor, items[2].ID, MoveInput{TargetStatus: workflow.Raw, AfterID: items[0].ID, ExpectedVersion: items[2].Version, ExpectedSourceOrderingVersion: versions[workflow.Raw], ExpectedDestinationOrderingVersion: versions[workflow.Raw]}); err == nil {
		t.Fatal("stale item and ordering versions must reject a retry")
	}
	current, err := repository.Get(ctx, scope, items[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	versions, err = service.ColumnOrderingVersions(ctx, scope, actor, "")
	if err != nil {
		t.Fatal(err)
	}
	cross, err := service.Move(ctx, scope, actor, current.ID, MoveInput{TargetStatus: workflow.Refining, TransitionKey: "start_refining", ExpectedVersion: current.Version, ExpectedSourceOrderingVersion: versions[workflow.Raw], ExpectedDestinationOrderingVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cross.Item.Status != workflow.Refining || cross.SourceOrderingVersion != versions[workflow.Raw]+1 || cross.DestinationOrderingVersion != 2 {
		t.Fatalf("cross-column move = %#v", cross)
	}
}

func TestReorderMovesItemsWithinBacklogAndRejectsStaleVersion(t *testing.T) {
	now := func() time.Time { return time.Unix(1, 0).UTC() }
	repository := NewMemoryRepository(now)
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	created := make([]*WorkItem, 0, 3)
	for _, title := range []string{"First", "Second", "Third"} {
		item, err := service.Create(context.Background(), scope, actor, CreateInput{Type: Task, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, item)
	}
	updated, err := service.Reorder(context.Background(), scope, actor, created[2].ID, "up", created[2].Version)
	if err != nil {
		t.Fatal(err)
	}
	if updated.BacklogRank >= created[2].BacklogRank {
		t.Fatalf("rank did not move up: before=%d after=%d", created[2].BacklogRank, updated.BacklogRank)
	}
	items, err := service.List(context.Background(), scope, actor, ListFilter{Sort: "backlog", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Title != "First" || items[1].Title != "Third" || items[2].Title != "Second" {
		t.Fatalf("backlog order = %#v", items)
	}
	if _, err := service.Reorder(context.Background(), scope, actor, created[2].ID, "down", created[2].Version); err == nil {
		t.Fatal("expected stale version rejection")
	}
}

func TestUpdateRejectsParentFromAnotherProject(t *testing.T) {
	now := func() time.Time { return time.Unix(1, 0).UTC() }
	repository := NewMemoryRepository(now)
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	firstScope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	secondScope := Scope{OrganizationID: "org-1", ProjectID: "project-2"}
	child, err := service.Create(context.Background(), firstScope, actor, CreateInput{Type: Task, Title: "Child"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := service.Create(context.Background(), secondScope, actor, CreateInput{Type: Epic, Title: "Other project parent"})
	if err != nil {
		t.Fatal(err)
	}
	parentID := parent.ID
	_, err = service.Update(context.Background(), firstScope, actor, child.ID, UpdateInput{ParentID: &parentID, ParentIDSet: true, ExpectedVersion: child.Version})
	if err == nil {
		t.Fatal("expected cross-project parent to be rejected")
	}
	if got := repositoryMustGet(t, repository, firstScope, child.ID).ParentID; got != "" {
		t.Fatalf("child parent changed after rejection: %q", got)
	}
}

func repositoryMustGet(t *testing.T, repository *MemoryRepository, scope Scope, id string) *WorkItem {
	t.Helper()
	item, err := repository.Get(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestArchiveRestoreAndList(t *testing.T) {
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	repository := NewMemoryRepository(now)
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	service := NewService(repository, specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), auditWriter, outboxWriter, now)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "FF"}
	item, err := service.Create(context.Background(), scope, actor, CreateInput{Type: Task, Title: "Archive me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Archive(context.Background(), scope, actor, item.ID, item.Version); err != nil {
		t.Fatal(err)
	}
	if err := service.Archive(context.Background(), scope, actor, item.ID, item.Version+1); err != nil {
		t.Fatal(err)
	}
	items, err := service.List(context.Background(), scope, actor, ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("archived items in default list: %d", len(items))
	}
	archived, err := repository.ListPage(context.Background(), scope, ListFilter{Limit: 10, IncludeArchived: true})
	if err != nil || len(archived.Items) != 1 || archived.Items[0].ArchivedAt == nil {
		t.Fatalf("archived page = %#v, err = %v", archived, err)
	}
	if _, err := service.Restore(context.Background(), scope, actor, item.ID, archived.Items[0].Version); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(context.Background(), scope, actor, item.ID, archived.Items[0].Version+1); err != nil {
		t.Fatal(err)
	}
	items, err = service.List(context.Background(), scope, actor, ListFilter{Limit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("restored list = %#v, err = %v", items, err)
	}
	if got := len(outboxWriter.Events()); got != 3 {
		t.Fatalf("no-op archive/restore emitted extra events: %d", got)
	}
	if got := len(auditWriter.Records()); got != 3 {
		t.Fatalf("no-op archive/restore emitted extra audit records: %d", got)
	}
}

func TestCommentAuthorCanEditAndDeleteOnlyOwnComment(t *testing.T) {
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	service := NewService(NewMemoryRepository(now), specification.NewService(specification.NewMemoryStore(), now), workflow.NewService(workflow.Default()), auditWriter, outboxWriter, now)
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1"}
	author := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	other := identity.Actor{Type: "human", ID: "user-2", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	item, err := service.Create(context.Background(), scope, author, CreateInput{Type: Task, Title: "Discuss"})
	if err != nil {
		t.Fatal(err)
	}
	comment, err := service.CreateComment(context.Background(), scope, author, item.ID, "Initial")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateComment(context.Background(), scope, other, item.ID, comment.ID, "Hijacked"); err == nil {
		t.Fatal("another author must not edit the comment")
	}
	updated, err := service.UpdateComment(context.Background(), scope, author, item.ID, comment.ID, "Updated")
	if err != nil || updated.Body != "Updated" {
		t.Fatalf("updated comment = %#v, err = %v", updated, err)
	}
	deleted, err := service.DeleteComment(context.Background(), scope, author, item.ID, comment.ID)
	if err != nil || deleted.Body != "[deleted]" || deleted.DeletedAt == nil {
		t.Fatalf("deleted comment = %#v, err = %v", deleted, err)
	}
	if _, err := service.DeleteComment(context.Background(), scope, author, item.ID, comment.ID); err == nil {
		t.Fatal("deleted comment must not be deleted twice")
	}
}
