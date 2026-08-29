package environment

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type releaseEvidenceStub struct{ evidence ReleaseApprovalEvidence }

func (s releaseEvidenceStub) ReleaseApprovalEvidence(context.Context, string, string, string, string) (ReleaseApprovalEvidence, error) {
	return s.evidence, nil
}

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

func TestProductionDeploymentRequiresIndependentApprovalAndVerification(t *testing.T) {
	service := NewService(NewMemoryStore())
	manager := identity.Actor{Type: "human", ID: "manager-1", Login: "release-manager", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	if _, err := service.Create(context.Background(), manager, CreateInput{ProjectID: "project-1", Key: "prod", Name: "Production", Kind: "production"}); err != nil {
		t.Fatalf("create environment: %v", err)
	}
	environments, err := service.List(context.Background(), manager, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := service.CreateDeployment(context.Background(), manager, DeploymentInput{ProjectID: "project-1", EnvironmentID: environments[0].ID, WorkItemID: "work-item-1", CommitSHA: "sha-1"})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	service.SetReleaseApprovalEvidence(releaseEvidenceStub{evidence: ReleaseApprovalEvidence{Found: true, AuthorLogin: "author", HeadSHA: "sha-1", ApprovedByLogins: []string{"reviewer"}}})
	author := manager
	author.Login = "author"
	if _, err := service.ApproveDeployment(context.Background(), author, "project-1", deployment.ID); err == nil || apperr.From(err).Code != apperr.CodeForbidden {
		t.Fatalf("author approval error = %v", err)
	}
	reviewer := manager
	reviewer.Login = "reviewer"
	if _, err := service.ApproveDeployment(context.Background(), reviewer, "project-1", deployment.ID); err == nil || apperr.From(err).Code != apperr.CodeForbidden {
		t.Fatalf("reviewer approval error = %v", err)
	}
	approved, err := service.ApproveDeployment(context.Background(), manager, "project-1", deployment.ID)
	if err != nil || approved.Status != DeploymentDispatch {
		t.Fatalf("independent approval = %#v, err = %v", approved, err)
	}
	system := identity.Actor{Type: "system", ID: "worker", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	if _, err := service.UpdateDeploymentStatus(context.Background(), system, "project-1", deployment.ID, StatusInput{Status: DeploymentRunning}); err != nil {
		t.Fatal(err)
	}
	succeeded, err := service.UpdateDeploymentStatus(context.Background(), system, "project-1", deployment.ID, StatusInput{Status: DeploymentSuccess})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDeploymentStatus(context.Background(), system, "project-1", deployment.ID, StatusInput{Status: DeploymentRunning}); err == nil {
		t.Fatal("deployment must not move backwards after success")
	}
	verified, err := service.VerifyDeployment(context.Background(), manager, "project-1", succeeded.ID)
	if err != nil || verified.Status != DeploymentVerified {
		t.Fatalf("verified deployment = %#v, err = %v", verified, err)
	}
}
