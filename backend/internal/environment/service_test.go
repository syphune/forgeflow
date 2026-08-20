package environment

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestProductionEnvironmentAlwaysRequiresApproval(t *testing.T) {
	service := NewService(NewMemoryStore())
	actor := identity.Actor{Type: "human", ID: "manager-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}

	item, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project-1", Key: "prod", Name: "Production", Kind: "production", AutoDeploy: true, RequireApproval: false})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if item.AutoDeploy || !item.RequireApproval {
		t.Fatalf("production safeguards were not applied: %#v", item)
	}
}

func TestDeploymentApprovalIsHumanOnlyAndTenantScoped(t *testing.T) {
	service := NewService(NewMemoryStore())
	human := identity.Actor{Type: "human", ID: "manager-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	agent := identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}

	environment, err := service.Create(context.Background(), human, CreateInput{ProjectID: "project-1", Key: "staging", Name: "Staging", Kind: "staging", RequireApproval: true})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	deployment, err := service.CreateDeployment(context.Background(), human, DeploymentInput{ProjectID: "project-1", EnvironmentID: environment.ID, CommitSHA: "abc123"})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if deployment.Status != DeploymentPending {
		t.Fatalf("deployment status = %s, want %s", deployment.Status, DeploymentPending)
	}
	if _, err := service.ApproveDeployment(context.Background(), agent, "project-1", deployment.ID); err == nil {
		t.Fatal("agent approval must be rejected")
	}
	if _, err := service.GetDeployment(context.Background(), identity.Actor{Type: "human", ID: "other", OrganizationID: "org-2", Capabilities: map[string]bool{"*": true}}, "project-1", deployment.ID); err == nil {
		t.Fatal("cross-tenant deployment read must be rejected")
	}
	approved, err := service.ApproveDeployment(context.Background(), human, "project-1", deployment.ID)
	if err != nil {
		t.Fatalf("approve deployment: %v", err)
	}
	if approved.Status != DeploymentDispatch || approved.ApprovedBy != human.ID {
		t.Fatalf("approved deployment = %#v", approved)
	}
}

func TestPolicyServiceReturnsNormalizedDefaults(t *testing.T) {
	service := NewService(NewMemoryStore())
	actor := identity.Actor{Type: "human", ID: "manager-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityProjectRead: true}}
	policy, err := service.GetPolicy(context.Background(), actor, "project-1")
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if policy.Runtime != autonomous.DefaultPolicy().Runtime || policy.MaxAttempts != autonomous.DefaultPolicy().MaxAttempts {
		t.Fatalf("policy = %#v, want defaults", policy)
	}
}
