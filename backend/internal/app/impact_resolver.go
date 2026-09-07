package app

import (
	"context"

	"github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/impact"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/tenant"
)

type repositoryProjectResolver struct {
	tenant *tenant.Service
	github *github.Service
}

func (r repositoryProjectResolver) ProjectsForRepository(ctx context.Context, organizationID, repositoryID string) ([]impact.ProjectSummary, error) {
	if r.tenant == nil || r.github == nil {
		return nil, apperr.New(apperr.CodeInternal, 503, "impact project resolver is unavailable", nil)
	}
	actor := identity.Actor{Type: "system", ID: "impact-resolver", OrganizationID: organizationID, Capabilities: map[string]bool{"*": true}}
	projects, err := r.tenant.Projects(ctx, actor, "")
	if err != nil {
		return nil, err
	}
	result := make([]impact.ProjectSummary, 0)
	for _, project := range projects {
		repositories, listErr := r.github.ProjectRepositories(ctx, actor, project.ID)
		if listErr != nil {
			return nil, listErr
		}
		for _, repository := range repositories {
			if repository.ID == repositoryID {
				result = append(result, impact.ProjectSummary{ID: project.ID, Key: project.Key, DisplayName: project.DisplayName})
				break
			}
		}
	}
	return result, nil
}
