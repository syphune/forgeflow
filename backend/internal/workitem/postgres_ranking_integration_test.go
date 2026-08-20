//go:build integration

package workitem

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
)

func TestPostgresBacklogRankingRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const (
		organizationID = "00000000-0000-0000-0000-000000000301"
		workspaceID    = "00000000-0000-0000-0000-000000000302"
		projectID      = "00000000-0000-0000-0000-000000000303"
		userID         = "00000000-0000-0000-0000-000000000304"
	)
	for _, fixture := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, slug, display_name) VALUES ($1, 'ranking-integration', 'Ranking integration') ON CONFLICT (id) DO NOTHING`, []any{organizationID}},
		{`INSERT INTO users (id, github_user_id, login, display_name) VALUES ($1, 120304, 'ranking-integration-user', 'Ranking integration user') ON CONFLICT (id) DO NOTHING`, []any{userID}},
		{`INSERT INTO organization_memberships (organization_id, user_id, role_key) VALUES ($1, $2, 'owner') ON CONFLICT DO NOTHING`, []any{organizationID, userID}},
		{`INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1, $2, 'RANK', 'Ranking workspace') ON CONFLICT (id) DO NOTHING`, []any{workspaceID, organizationID}},
		{`INSERT INTO projects (id, organization_id, workspace_id, key, display_name) VALUES ($1, $2, $3, 'RANK', 'Ranking project') ON CONFLICT (id) DO NOTHING`, []any{projectID, organizationID, workspaceID}},
	} {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}

	repository := NewPostgresRepository(pool)
	scope := Scope{OrganizationID: organizationID, ProjectID: projectID, ProjectKey: "RANK"}
	created := make([]*WorkItem, 0, 3)
	for _, title := range []string{"First", "Second", "Third"} {
		item, err := repository.Create(ctx, scope, CreateInput{Type: Task, Title: title, ReporterID: userID})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, item)
	}
	if _, err := repository.MoveRank(ctx, scope, created[2].ID, "up", created[2].Version); err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListPage(ctx, scope, ListFilter{Sort: "backlog", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 3 || page.Items[0].Title != "First" || page.Items[1].Title != "Third" || page.Items[2].Title != "Second" {
		t.Fatalf("backlog order = %#v", page.Items)
	}

	current, err := repository.Get(ctx, scope, created[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := repository.ColumnOrderingVersions(ctx, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if versions[workflow.Raw] == 0 {
		versions[workflow.Raw] = 1
	}
	moved, err := repository.Move(ctx, scope, MoveInput{
		ItemID:                             current.ID,
		TargetStatus:                       workflow.Refining,
		ExpectedVersion:                    current.Version,
		ExpectedSourceOrderingVersion:      versions[workflow.Raw],
		ExpectedDestinationOrderingVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Item.Status != workflow.Refining || moved.SourceOrderingVersion != versions[workflow.Raw]+1 || moved.DestinationOrderingVersion != 2 {
		t.Fatalf("atomic cross-column move = %#v", moved)
	}
}
