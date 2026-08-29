package app

import (
	"context"

	"github.com/forgeflow/forgeflow/backend/internal/branchplan"
	"github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type branchPlanProvider struct{ github *github.Service }

func (p branchPlanProvider) Repository(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (branchplan.Repository, error) {
	item, err := p.github.BranchPlanRepository(ctx, actor, projectID, repositoryID)
	if err != nil {
		return branchplan.Repository{}, err
	}
	return branchplan.Repository{ID: item.ID, FullName: item.FullName, DefaultBranch: item.DefaultBranch, InstallationID: item.InstallationID}, nil
}

func (p branchPlanProvider) Head(ctx context.Context, actor identity.Actor, repository branchplan.Repository, branch string) (string, error) {
	return p.github.BranchPlanHead(ctx, actor, github.Repository{ID: repository.ID, FullName: repository.FullName, DefaultBranch: repository.DefaultBranch, InstallationID: repository.InstallationID}, branch)
}

func (p branchPlanProvider) CreateBranch(ctx context.Context, actor identity.Actor, repository branchplan.Repository, branch, sha string) error {
	return p.github.BranchPlanCreateBranch(ctx, actor, github.Repository{ID: repository.ID, FullName: repository.FullName, DefaultBranch: repository.DefaultBranch, InstallationID: repository.InstallationID}, branch, sha)
}

func (p branchPlanProvider) FindPullRequest(ctx context.Context, actor identity.Actor, projectID, repositoryID, head, base string) (branchplan.PullRequest, bool, error) {
	item, found, err := p.github.BranchPlanFindPullRequest(ctx, actor, projectID, repositoryID, head, base)
	if err != nil {
		return branchplan.PullRequest{}, false, err
	}
	return branchplan.PullRequest{Number: item.Number, URL: item.URL, State: item.State, Head: item.HeadRef, Base: item.BaseRef}, found, nil
}

func (p branchPlanProvider) CreateDraftPullRequest(ctx context.Context, actor identity.Actor, projectID, repositoryID, title, body, head, base string) (branchplan.PullRequest, error) {
	item, err := p.github.BranchPlanCreateDraftPullRequest(ctx, actor, projectID, repositoryID, title, body, head, base)
	if err != nil {
		return branchplan.PullRequest{}, err
	}
	return branchplan.PullRequest{Number: item.Number, URL: item.URL, State: item.State, Head: item.HeadRef, Base: item.BaseRef}, nil
}

var _ branchplan.Provider = branchPlanProvider{}
