package tenant

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestProjectAndWorkspaceMutationsRecordAuditAndOutbox(t *testing.T) {
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	service := NewService(NewMemoryStore(), Options{Recorder: MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Source: "web", Capabilities: map[string]bool{"*": true}}

	workspace, err := service.CreateWorkspace(context.Background(), actor, "MAIN", "Main")
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.CreateProject(context.Background(), actor, workspace.ID, "APP", "Application")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateProject(context.Background(), actor, project.ID, "Forgeflow"); err != nil {
		t.Fatal(err)
	}

	if got := len(auditWriter.Records()); got != 3 {
		t.Fatalf("audit records = %d, want 3", got)
	}
	if got := len(outboxWriter.Events()); got != 3 {
		t.Fatalf("outbox events = %d, want 3", got)
	}
}

func TestRemoveProjectMemberRecordsMutation(t *testing.T) {
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	store := NewMemoryStore()
	store.organizationMembers["org-1"] = []Member{{ID: "user-1", Login: "user-1", DisplayName: "User", RoleKey: "owner"}}
	service := NewService(store, Options{Recorder: MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	workspace, err := service.CreateWorkspace(context.Background(), actor, "MAIN", "Main")
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.CreateProject(context.Background(), actor, workspace.ID, "APP", "Application")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetMember(context.Background(), actor, project.ID, actor.ID, "qa"); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveMember(context.Background(), actor, project.ID, actor.ID); err != nil {
		t.Fatal(err)
	}
	if len(auditWriter.Records()) != 4 || len(outboxWriter.Events()) != 4 {
		t.Fatalf("mutation records = %d/%d", len(auditWriter.Records()), len(outboxWriter.Events()))
	}
}

func TestOrganizationMemberMutationsRecordAuditAndOutbox(t *testing.T) {
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	store := NewMemoryStore()
	store.organizationMembers["org-1"] = []Member{
		{ID: "user-1", Login: "owner", DisplayName: "Owner", RoleKey: "owner"},
		{ID: "user-2", Login: "developer", DisplayName: "Developer", RoleKey: "developer"},
	}
	service := NewService(store, Options{Recorder: MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Source: "web", Capabilities: map[string]bool{"*": true}}
	if _, err := service.SetOrganizationMember(context.Background(), actor, "user-2", "qa"); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveOrganizationMember(context.Background(), actor, "user-2"); err != nil {
		t.Fatal(err)
	}
	if got := len(auditWriter.Records()); got != 2 {
		t.Fatalf("audit records = %d, want 2", got)
	}
	if got := len(outboxWriter.Events()); got != 2 {
		t.Fatalf("outbox events = %d, want 2", got)
	}
}

func TestOrganizationMemberRoleValidation(t *testing.T) {
	store := NewMemoryStore()
	store.organizationMembers["org-1"] = []Member{{ID: "user-1", Login: "owner", RoleKey: "owner"}}
	service := NewService(store)
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	if _, err := service.SetOrganizationMember(context.Background(), actor, "user-1", "not-a-role"); err == nil {
		t.Fatal("expected invalid organization role to fail")
	}
}
