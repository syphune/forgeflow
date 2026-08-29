package app

import (
	"context"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/environment"
	"github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/tenant"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

type engineeringEvidenceAdapter struct {
	store    github.Store
	tenants  *tenant.Service
	delivery *environment.Service
}

func (a *engineeringEvidenceAdapter) HasPullRequest(ctx context.Context, organizationID, projectID, repositoryID, workItemKey string) (bool, error) {
	return a.store.HasPullRequest(ctx, organizationID, projectID, repositoryID, workItemKey)
}

func (a *engineeringEvidenceAdapter) HasSuccessfulCI(ctx context.Context, organizationID, projectID, repositoryID, workItemKey string) (bool, error) {
	return a.store.HasSuccessfulCI(ctx, organizationID, projectID, repositoryID, workItemKey)
}

func (a *engineeringEvidenceAdapter) HasRequiredCI(ctx context.Context, organizationID, projectID, repositoryID, workItemKey, currentSHA string, required []string) (bool, error) {
	return a.store.HasRequiredCI(ctx, organizationID, projectID, repositoryID, workItemKey, currentSHA, required)
}

func (a *engineeringEvidenceAdapter) PullRequestEvidence(ctx context.Context, organizationID, projectID, repositoryID, workItemKey, currentSHA string) (workitem.PullRequestEvidence, error) {
	item, found, err := a.store.GetPullRequest(ctx, organizationID, projectID, repositoryID, workItemKey, currentSHA)
	if err != nil || !found {
		return workitem.PullRequestEvidence{Found: found}, err
	}
	result := workitem.PullRequestEvidence{
		Found:            true,
		AuthorLogin:      item.AuthorLogin,
		HeadSHA:          item.HeadSHA,
		State:            item.State,
		ApprovedByLogins: append([]string(nil), item.ApprovedByLogins...),
		ChangedPaths:     append([]string(nil), item.ChangedPaths...),
		ChangesRequested: item.ChangesRequested,
		Merged:           item.Merged,
		ReviewerRoles:    make(map[string][]string),
	}
	if a.tenants == nil || len(result.ApprovedByLogins) == 0 {
		return result, nil
	}
	actor := identity.Actor{Type: "system", ID: "evidence", OrganizationID: organizationID, Capabilities: map[string]bool{"*": true}}
	members, err := a.tenants.Members(ctx, actor, projectID)
	if err != nil {
		return workitem.PullRequestEvidence{}, err
	}
	for _, approver := range result.ApprovedByLogins {
		login := strings.ToLower(strings.TrimSpace(approver))
		for _, member := range members {
			if strings.EqualFold(strings.TrimSpace(approver), strings.TrimSpace(member.Login)) {
				result.ReviewerRoles[login] = append(result.ReviewerRoles[login], member.RoleKey)
			}
		}
	}
	return result, nil
}

func (a *engineeringEvidenceAdapter) ReleaseApprovalEvidence(ctx context.Context, organizationID, projectID, workItemID, currentSHA string) (environment.ReleaseApprovalEvidence, error) {
	item, found, err := a.store.GetPullRequest(ctx, organizationID, projectID, "", workItemID, currentSHA)
	if err != nil || !found {
		return environment.ReleaseApprovalEvidence{Found: found}, err
	}
	return environment.ReleaseApprovalEvidence{Found: true, AuthorLogin: item.AuthorLogin, HeadSHA: item.HeadSHA, ApprovedByLogins: append([]string(nil), item.ApprovedByLogins...)}, nil
}

func (a *engineeringEvidenceAdapter) DeliveryPolicy(ctx context.Context, organizationID, projectID string) (workitem.DeliveryPolicy, error) {
	policy, err := a.delivery.DeliveryPolicy(ctx, organizationID, projectID)
	if err != nil {
		return workitem.DeliveryPolicy{}, err
	}
	result := workitem.DeliveryPolicy{ReleaseMode: string(policy.ReleaseMode), RequiredChecks: append([]string(nil), policy.RequiredChecks...), Review: workitem.ReviewPolicy{MinApprovals: policy.Review.MinApprovals, ForbidSelfApproval: policy.Review.ForbidSelfApproval, RequiredRoles: append([]string(nil), policy.Review.RequiredRoles...)}}
	result.Review.PathRules = make([]workitem.PathReviewRule, 0, len(policy.Review.PathRules))
	for _, rule := range policy.Review.PathRules {
		result.Review.PathRules = append(result.Review.PathRules, workitem.PathReviewRule{Pattern: rule.Pattern, MinApprovals: rule.MinApprovals, RequiredRoles: append([]string(nil), rule.RequiredRoles...)})
	}
	result.RiskRules = make(map[workitem.RiskLevel]workitem.RiskRule, len(policy.RiskRules))
	for level, rule := range policy.RiskRules {
		result.RiskRules[workitem.RiskLevel(level)] = workitem.RiskRule{MinApprovals: rule.MinApprovals, RequiredRoles: append([]string(nil), rule.RequiredRoles...), RequireQA: rule.RequireQA, RequireRollbackPlan: rule.RequireRollbackPlan, RequireProductionVerification: rule.RequireProductionVerification}
	}
	return result, nil
}
