package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	githubintegration "github.com/forgeflow/forgeflow/backend/internal/github"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/planning"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/specification"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/forgeflow/forgeflow/backend/internal/workitem"
)

func TestResourceTopicsAreAllowlisted(t *testing.T) {
	for _, topic := range []string{"architecture", "conventions", "testing", "known-issues", "domain-rules"} {
		if !allowedResourceTopic(topic) {
			t.Fatalf("resource topic %q was rejected", topic)
		}
	}
	if allowedResourceTopic("instructions") {
		t.Fatal("dynamic resource topics must not be accepted")
	}
}

func TestKnowledgeKindForTopic(t *testing.T) {
	for topic, want := range map[string]string{
		"architecture": "ARCHITECTURE",
		"conventions":  "CONVENTIONS",
		"testing":      "TESTING",
		"known-issues": "KNOWN_ISSUES",
		"domain-rules": "DOMAIN_RULES",
	} {
		if got := knowledgeKindForTopic(topic); got != want {
			t.Fatalf("knowledge kind for %q = %q, want %q", topic, got, want)
		}
	}
	if knowledgeKindForTopic("module") != "" {
		t.Fatal("module resources must not select a knowledge kind")
	}
}

func TestDynamicResourcesReadEachTemplateThroughScopedServices(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Now().UTC() }
	specifications := specification.NewService(specification.NewMemoryStore(), now)
	workItems := workitem.NewService(workitem.NewMemoryRepository(now), specifications, workflow.NewService(workflow.Default()), audit.NewMemoryWriter(), outbox.NewMemoryWriter(), now)
	specifications.SetWorkItemStateReader(workItems)
	planningService := planning.NewService(planning.NewMemoryStore())
	actor := identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	scope := workitem.Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "ASTRO"}
	item, err := workItems.Create(ctx, scope, actor, workitem.CreateInput{Type: workitem.Bug, Title: "Preserve reader language"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "ASTRO-1" {
		t.Fatalf("work item key = %q, want ASTRO-1", item.Key)
	}
	sprint, err := planningService.Create(ctx, actor, "project-1", "Release hardening", "Ship safely", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	store := githubintegration.NewMemoryStore()
	if _, err := store.UpsertInstallation(ctx, "org-1", 7, "acme"); err != nil {
		t.Fatal(err)
	}
	repository, err := store.UpsertRepository(ctx, "org-1", 7, 11, "acme/app", "main", "https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkRepository(ctx, "org-1", "project-1", repository.ID); err != nil {
		t.Fatal(err)
	}
	githubService := githubintegration.NewService(store, resourceGitHubClient{}, githubintegration.AppConfig{}, nil, nil, nil, nil)
	adapter := NewServiceAdapter(actor, workItems, specifications, nil, planningService, "project-1")
	adapter.GitHub = githubService

	for _, testCase := range []struct {
		name string
		uri  string
		mime string
	}{
		{name: "task", uri: "task://ASTRO/1", mime: "application/json"},
		{name: "sprint", uri: "sprint://" + sprint.ID + "?project_id=project-1", mime: "application/json"},
		{name: "repository", uri: "repo://" + repository.ID + "/architecture?project_id=project-1", mime: "application/json"},
		{name: "module", uri: "repo://" + repository.ID + "/module/README.md?project_id=project-1", mime: "text/plain"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resource, err := adapter.readResource(ctx, testCase.uri)
			if err != nil {
				t.Fatal(err)
			}
			if len(resource.Contents) != 1 || resource.Contents[0].URI != testCase.uri || resource.Contents[0].MIMEType != testCase.mime {
				t.Fatalf("resource = %#v, want one %s content", resource, testCase.mime)
			}
		})
	}
}

type resourceGitHubClient struct{}

func (resourceGitHubClient) Installation(context.Context, int64) (githubintegration.RemoteInstallation, error) {
	return githubintegration.RemoteInstallation{ID: 7}, nil
}

func (resourceGitHubClient) Repositories(context.Context, int64) ([]githubintegration.RemoteRepository, error) {
	return nil, nil
}

func (resourceGitHubClient) RepositoryTree(context.Context, int64, string, string) ([]githubintegration.RemoteTreeEntry, error) {
	return []githubintegration.RemoteTreeEntry{{Path: "README.md", Type: "blob", Size: 18, SHA: "readme-sha"}}, nil
}

func (resourceGitHubClient) RepositoryFile(context.Context, int64, string, string, string) (githubintegration.RemoteFile, error) {
	return githubintegration.RemoteFile{Path: "README.md", Type: "blob", Size: 18, SHA: "readme-sha", Content: "untrusted repository"}, nil
}
