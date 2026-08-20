package workflow

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type projectStoreFunc func(context.Context, string, string) (Workflow, error)

func (f projectStoreFunc) LoadWorkflow(ctx context.Context, organizationID, projectID string) (Workflow, error) {
	return f(ctx, organizationID, projectID)
}

func TestDefaultTransitions(t *testing.T) {
	s := NewService(Default())
	tests := []struct {
		name       string
		current    string
		key        string
		wantTarget string
		wantRule   RuleType
	}{
		{name: "raw to refining", current: Raw, key: "start_refining", wantTarget: Refining},
		{name: "review to ready", current: ReviewRequired, key: "mark_ready", wantTarget: Ready, wantRule: RequireSpecificationReady},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transition, err := s.Transition(tt.current, tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if transition.To != tt.wantTarget {
				t.Fatalf("target = %q, want %q", transition.To, tt.wantTarget)
			}
			if tt.wantRule != "" && len(transition.Required) != 1 || (tt.wantRule != "" && transition.Required[0] != tt.wantRule) {
				t.Fatalf("required rules = %#v, want %q", transition.Required, tt.wantRule)
			}
		})
	}
}

func TestTransitionRejectsWrongCurrentStatus(t *testing.T) {
	_, err := NewService(Default()).Transition(Raw, "mark_ready")
	if err == nil {
		t.Fatal("expected invalid transition")
	}
}

func TestProjectWorkflowIsUsedForTransitions(t *testing.T) {
	custom := Default()
	custom.Transitions["start_refining"] = Transition{Key: "start_refining", From: Raw, To: ReviewRequired, Name: "Skip refinement"}
	service := NewService(Default(), projectStoreFunc(func(_ context.Context, organizationID, projectID string) (Workflow, error) {
		if organizationID != "org-1" || projectID != "project-1" {
			t.Fatalf("unexpected scope: %s/%s", organizationID, projectID)
		}
		return custom, nil
	}))
	transition, err := service.TransitionForProject(context.Background(), "org-1", "project-1", Raw, "start_refining")
	if err != nil {
		t.Fatal(err)
	}
	if transition.To != ReviewRequired {
		t.Fatalf("target = %q, want %q", transition.To, ReviewRequired)
	}
}

func TestSaveForProjectNormalizesAndProtectsReady(t *testing.T) {
	service := NewService(Default())
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityProjectManage: true}}
	workflow, err := service.SaveForProject(context.Background(), actor, "project-1", SaveInput{
		Name: " Product flow ",
		Statuses: []Status{
			{Key: Raw, Name: "Inbox", Category: "TODO", Position: 10},
			{Key: Ready, Name: "Ready for build", Category: "TODO", Position: 20},
			{Key: Done, Name: "Done", Category: "DONE", Position: 30, IsTerminal: true},
		},
		Transitions: []Transition{
			{Key: "send_to_ready", From: Raw, To: Ready, Name: "Send to ready"},
			{Key: "finish", From: Ready, To: Done, Name: "Finish"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "Product flow" || workflow.Statuses[Raw].Name != "Inbox" {
		t.Fatalf("saved workflow = %#v", workflow)
	}
	readyTransition := workflow.Transitions["send_to_ready"]
	if len(readyTransition.Required) != 1 || readyTransition.Required[0] != RequireSpecificationReady {
		t.Fatalf("READY transition rules = %#v", readyTransition.Required)
	}
	loaded, err := service.WorkflowFor(context.Background(), "org-1", "project-1")
	if err != nil {
		t.Fatal(err)
	}
	loaded.Statuses[Raw] = Status{Name: "mutated copy"}
	again, err := service.WorkflowFor(context.Background(), "org-1", "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Statuses[Raw].Name != "Inbox" {
		t.Fatal("workflow store exposed mutable state")
	}
}

func TestPermissionRuleRequiresConfiguredCapabilities(t *testing.T) {
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityProjectManage: true}}
	_, err := NewService(Default()).SaveForProject(context.Background(), actor, "project-1", SaveInput{
		Name:     "Flow",
		Statuses: []Status{{Key: Raw, Name: "Raw", Category: "TODO"}, {Key: Done, Name: "Done", Category: "DONE"}},
		Transitions: []Transition{{
			Key: "finish", From: Raw, To: Done, Name: "Finish", Required: []RuleType{RequirePermission},
		}},
	})
	if err == nil {
		t.Fatal("expected permission rule without capabilities to be rejected")
	}
	service := NewService(Default())
	saved, err := service.SaveForProject(context.Background(), actor, "project-1", SaveInput{
		Name:     "Flow",
		Statuses: []Status{{Key: Raw, Name: "Raw", Category: "TODO"}, {Key: Done, Name: "Done", Category: "DONE"}},
		Transitions: []Transition{{
			Key: "finish", From: Raw, To: Done, Name: "Finish", Required: []RuleType{RequirePermission}, RequiredPermissions: []string{"Work_Item.Approve", "work_item.approve"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Transitions["finish"].RequiredPermissions; len(got) != 1 || got[0] != "work_item.approve" {
		t.Fatalf("permissions = %#v", got)
	}
}

func TestSaveForProjectRecordsMutation(t *testing.T) {
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	service := NewService(Default())
	service.SetRecorder(MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}, nil)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Source: "web", Capabilities: map[string]bool{identity.CapabilityProjectManage: true}}
	_, err := service.SaveForProject(context.Background(), actor, "project-1", SaveInput{
		Name:        "Flow",
		Statuses:    []Status{{Key: Raw, Name: "Raw", Category: "TODO"}, {Key: Done, Name: "Done", Category: "DONE"}},
		Transitions: []Transition{{Key: "finish", From: Raw, To: Done, Name: "Finish"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(auditWriter.Records()) != 1 || len(outboxWriter.Events()) != 1 {
		t.Fatalf("audit/events = %d/%d, want 1/1", len(auditWriter.Records()), len(outboxWriter.Events()))
	}
	if got := auditWriter.Records()[0].ResourceID; got != "project-1" {
		t.Fatalf("resource id = %q, want project-1", got)
	}
}

func TestSaveForProjectRejectsUnsafeDefinitions(t *testing.T) {
	tests := []struct {
		name  string
		input SaveInput
	}{
		{name: "missing initial status", input: SaveInput{Name: "Flow", Statuses: []Status{{Key: Done, Name: "Done", Category: "DONE"}}}},
		{name: "unknown transition status", input: SaveInput{Name: "Flow", Statuses: []Status{{Key: Raw, Name: "Raw", Category: "TODO"}}, Transitions: []Transition{{Key: "move", From: Raw, To: Done, Name: "Move"}}}},
		{name: "unknown rule", input: SaveInput{Name: "Flow", Statuses: []Status{{Key: Raw, Name: "Raw", Category: "TODO"}, {Key: Done, Name: "Done", Category: "DONE"}}, Transitions: []Transition{{Key: "move", From: Raw, To: Done, Name: "Move", Required: []RuleType{"arbitrary_runtime"}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityProjectManage: true}}
			if _, err := NewService(Default()).SaveForProject(context.Background(), actor, "project-1", tt.input); err == nil {
				t.Fatal("expected invalid workflow definition")
			}
		})
	}
}
