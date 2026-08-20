package identity

import (
	"context"
	"sort"
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
	CapabilityCommentCreate,
	CapabilityOrganizationManage,
	CapabilityOrganizationRead,
	CapabilityProjectManage,
	CapabilityProjectRead,
	CapabilityRepositoryManage,
	CapabilityRepositoryRead,
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
	Type           string
	ID             string
	OrganizationID string
	Source         string
	Capabilities   map[string]bool
}

func (a Actor) Has(capability string) bool {
	if a.Capabilities == nil {
		return false
	}
	return a.Capabilities["*"] || a.Capabilities[capability]
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
