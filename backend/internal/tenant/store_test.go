package tenant

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreListsOnlyOrganizationsForUser(t *testing.T) {
	store := NewMemoryStore()
	store.organizations["org-a"] = Organization{ID: "org-a", DisplayName: "Zulu", CreatedAt: time.Unix(1, 0).UTC()}
	store.organizations["org-b"] = Organization{ID: "org-b", DisplayName: "Alpha", CreatedAt: time.Unix(2, 0).UTC()}
	store.organizations["org-c"] = Organization{ID: "org-c", DisplayName: "Other", CreatedAt: time.Unix(3, 0).UTC()}
	store.organizationMembers["org-a"] = []Member{{ID: "user-1"}}
	store.organizationMembers["org-b"] = []Member{{ID: "user-1"}}
	store.organizationMembers["org-c"] = []Member{{ID: "user-2"}}

	organizations, err := store.ListOrganizations(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(organizations) != 2 || organizations[0].ID != "org-b" || organizations[1].ID != "org-a" {
		t.Fatalf("organizations = %#v", organizations)
	}
}

func TestMemoryStoreAddsProjectCreatorAsMember(t *testing.T) {
	store := NewMemoryStore()
	store.workspaces["workspace-1"] = Workspace{ID: "workspace-1", OrganizationID: "org-1", Key: "MAIN"}
	store.organizationMembers["org-1"] = []Member{{ID: "user-1", Login: "user-1", DisplayName: "User", RoleKey: "owner"}}
	project, err := store.CreateProject(context.Background(), "org-1", "workspace-1", "APP", "App", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	members, err := store.ListMembers(context.Background(), "org-1", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].ID != "user-1" || members[0].RoleKey != "owner" {
		t.Fatalf("project members = %#v", members)
	}
}

func TestMemoryStoreUpdatesProjectAndWorkspaceNames(t *testing.T) {
	store := NewMemoryStore()
	workspace, err := store.CreateWorkspace(context.Background(), "org-1", "MAIN", "Main")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(context.Background(), "org-1", workspace.ID, "APP", "Application", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	updatedWorkspace, err := store.UpdateWorkspace(context.Background(), "org-1", workspace.ID, "Product")
	if err != nil || updatedWorkspace.DisplayName != "Product" {
		t.Fatalf("updated workspace = %#v, err = %v", updatedWorkspace, err)
	}
	updatedProject, err := store.UpdateProject(context.Background(), "org-1", project.ID, "Forgeflow")
	if err != nil || updatedProject.DisplayName != "Forgeflow" {
		t.Fatalf("updated project = %#v, err = %v", updatedProject, err)
	}
}

func TestMemoryStoreRemovesProjectRoleOverride(t *testing.T) {
	store := NewMemoryStore()
	store.workspaces["workspace-1"] = Workspace{ID: "workspace-1", OrganizationID: "org-1", Key: "MAIN"}
	store.organizationMembers["org-1"] = []Member{{ID: "user-1", Login: "user-1", DisplayName: "User", RoleKey: "developer"}}
	project, err := store.CreateProject(context.Background(), "org-1", "workspace-1", "APP", "App", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProjectMember(context.Background(), "org-1", project.ID, "user-1", "qa"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveProjectMember(context.Background(), "org-1", project.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	members, err := store.ListMembers(context.Background(), "org-1", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].RoleKey != "developer" {
		t.Fatalf("members after removing override = %#v", members)
	}
	if err := store.RemoveProjectMember(context.Background(), "org-1", project.ID, "user-1"); err == nil {
		t.Fatal("expected missing project role override error")
	}
}

func TestMemoryStoreProtectsLastOrganizationOwner(t *testing.T) {
	store := NewMemoryStore()
	store.organizationMembers["org-1"] = []Member{{ID: "user-1", Login: "owner", RoleKey: "owner"}}
	if _, err := store.SetOrganizationMember(context.Background(), "org-1", "user-1", "developer"); err == nil {
		t.Fatal("expected last owner role change to fail")
	}
	if err := store.RemoveOrganizationMember(context.Background(), "org-1", "user-1"); err == nil {
		t.Fatal("expected last owner removal to fail")
	}
}

func TestMemoryStoreRemovingOrganizationMemberRemovesProjectOverride(t *testing.T) {
	store := NewMemoryStore()
	store.workspaces["workspace-1"] = Workspace{ID: "workspace-1", OrganizationID: "org-1", Key: "MAIN"}
	store.organizationMembers["org-1"] = []Member{
		{ID: "owner-1", Login: "owner", RoleKey: "owner"},
		{ID: "user-1", Login: "user", RoleKey: "developer"},
	}
	project, err := store.CreateProject(context.Background(), "org-1", "workspace-1", "APP", "App", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProjectMember(context.Background(), "org-1", project.ID, "user-1", "qa"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveOrganizationMember(context.Background(), "org-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	members, err := store.ListMembers(context.Background(), "org-1", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].ID != "owner-1" {
		t.Fatalf("members after organization removal = %#v", members)
	}
}
