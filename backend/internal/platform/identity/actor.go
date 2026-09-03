package identity

import (
	"context"
	"sort"
	"strings"
)

const (
	CapabilityOrganizationRead     = "organization.read"
	CapabilityOrganizationManage   = "organization.manage"
	CapabilityWorkspaceRead        = "workspace.read"
	CapabilityWorkspaceManage      = "workspace.manage"
	CapabilityProjectRead          = "project.read"
	CapabilityProjectManage        = "project.manage"
	CapabilityWorkItemCreate       = "work_item.create"
	CapabilityWorkItemEdit         = "work_item.edit"
	CapabilityWorkItemAssign       = "work_item.assign"
	CapabilityWorkItemTransition   = "work_item.transition"
	CapabilityWorkItemDelete       = "work_item.delete"
	CapabilityCommentCreate        = "comment.create"
	CapabilitySprintManage         = "sprint.manage"
	CapabilityRepositoryRead       = "repository.read"
	CapabilityRepositoryManage     = "repository.manage"
	CapabilityBranchManage         = "branch.manage"
	CapabilitySpecificationPropose = "specification.propose"
	CapabilitySpecificationVerify  = "specification.verify"
	CapabilityAgentExecute         = "agent.execute"
	CapabilityAgentApprove         = "agent.approve"
	CapabilityAutonomousStart      = "autonomous.start"
	CapabilityAutonomousRetry      = "autonomous.retry"
	CapabilityAutonomousCancel     = "autonomous.cancel"
	CapabilityAIPolicyManage       = "ai_policy.manage"
	CapabilityEnvironmentManage    = "environment.manage"
	CapabilityDeploymentApprove    = "deployment.approve"
	CapabilitySecurityReview       = "security.review"
	CapabilityReleaseApprove       = "release.approve"
	CapabilityProductionVerify     = "production.verify"
)

var capabilityKeys = []string{
	CapabilityAgentApprove,
	CapabilityAgentExecute,
	CapabilityAutonomousCancel,
	CapabilityAutonomousRetry,
	CapabilityAutonomousStart,
	CapabilityAIPolicyManage,
	CapabilityDeploymentApprove,
	CapabilityEnvironmentManage,
	CapabilityProductionVerify,
	CapabilityReleaseApprove,
	CapabilitySecurityReview,
	CapabilityCommentCreate,
	CapabilityOrganizationManage,
	CapabilityOrganizationRead,
	CapabilityProjectManage,
	CapabilityProjectRead,
	CapabilityRepositoryManage,
	CapabilityRepositoryRead,
	CapabilityBranchManage,
	CapabilitySpecificationPropose,
	CapabilitySpecificationVerify,
	CapabilitySprintManage,
	CapabilityWorkItemAssign,
	CapabilityWorkItemCreate,
	CapabilityWorkItemDelete,
	CapabilityWorkItemEdit,
	CapabilityWorkItemTransition,
	CapabilityWorkspaceManage,
	CapabilityWorkspaceRead,
}

type Actor struct {
	Type            string
	ID              string
	Login           string
	DisplayName     string
	OrganizationID  string
	Source          string
	Capabilities    map[string]bool
	ToolPermissions map[string]bool
	// ProjectCapabilities preserves the effective role for each project when
	// authentication is unscoped. It is intentionally not folded into
	// Capabilities, which would grant the union of project roles.
	ProjectCapabilities map[string]map[string]bool
}

func (a Actor) Has(capability string) bool {
	if a.Capabilities == nil {
		return false
	}
	return a.Capabilities["*"] || a.Capabilities[capability]
}

func (a Actor) AllowsTool(tool string) bool {
	if len(a.ToolPermissions) == 0 {
		return true
	}
	return a.ToolPermissions[strings.TrimSpace(tool)]
}

func (a Actor) ForProject(projectID string) Actor {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || a.ProjectCapabilities == nil {
		return a
	}
	scoped := a
	scoped.Capabilities = a.ProjectCapabilities[projectID]
	return scoped
}

func (a Actor) SortedCapabilities() []string {
	if a.Capabilities["*"] {
		return append([]string(nil), capabilityKeys...)
	}
	result := make([]string, 0, len(a.Capabilities))
	for capability, enabled := range a.Capabilities {
		if enabled {
			result = append(result, capability)
		}
	}
	sort.Strings(result)
	return result
}

type contextKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, contextKey{}, actor)
}

func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(contextKey{}).(Actor)
	return actor, ok
}
