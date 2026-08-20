//go:build integration

package workitem

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
)

func TestPostgresCommentsCanBeEditedAndSoftDeleted(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const organizationID = "00000000-0000-0000-0000-000000000021"
	const userID = "00000000-0000-0000-0000-000000000022"
	const assigneeID = "00000000-0000-0000-0000-000000000025"
	const workspaceID = "00000000-0000-0000-0000-000000000023"
	const projectID = "00000000-0000-0000-0000-000000000024"
	for _, fixture := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, slug, display_name) VALUES ($1, 'comment-integration', 'Comment integration') ON CONFLICT (id) DO NOTHING`, []any{organizationID}},
		{`INSERT INTO users (id, github_user_id, login, display_name) VALUES ($1, 120022, 'comment-integration-user', 'Comment integration user') ON CONFLICT (id) DO NOTHING`, []any{userID}},
		{`INSERT INTO users (id, github_user_id, login, display_name) VALUES ($1, 120025, 'comment-integration-assignee', 'Comment integration assignee') ON CONFLICT (id) DO NOTHING`, []any{assigneeID}},
		{`INSERT INTO organization_memberships (organization_id, user_id, role_key) VALUES ($1, $2, 'owner') ON CONFLICT DO NOTHING`, []any{organizationID, userID}},
		{`INSERT INTO organization_memberships (organization_id, user_id, role_key) VALUES ($1, $2, 'developer') ON CONFLICT DO NOTHING`, []any{organizationID, assigneeID}},
		{`INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1, $2, 'COMMENT', 'Comment workspace') ON CONFLICT (id) DO NOTHING`, []any{workspaceID, organizationID}},
		{`INSERT INTO projects (id, organization_id, workspace_id, key, display_name) VALUES ($1, $2, $3, 'COMMENT_APP', 'Comment app') ON CONFLICT (id) DO NOTHING`, []any{projectID, organizationID, workspaceID}},
		{`INSERT INTO project_memberships (organization_id, project_id, user_id, role_key) VALUES ($1, $2, $3, 'owner') ON CONFLICT DO NOTHING`, []any{organizationID, projectID, userID}},
	} {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}
	repository := NewPostgresRepository(pool)
	scope := Scope{OrganizationID: organizationID, ProjectID: projectID, ProjectKey: "COMMENT_APP"}
	item, err := repository.Create(ctx, scope, CreateInput{Type: Task, Title: "Commented task", ReporterID: userID, AssigneeID: assigneeID})
	if err != nil {
		t.Fatal(err)
	}
	if item.AssigneeID != assigneeID {
		t.Fatalf("assignee = %q, want %q", item.AssigneeID, assigneeID)
	}
	comment, err := repository.AddComment(ctx, scope, item.ID, userID, "Initial")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateComment(ctx, scope, comment.ID, userID, "Updated")
	if err != nil || updated.Body != "Updated" {
		t.Fatalf("updated comment = %#v, err = %v", updated, err)
	}
	deleted, err := repository.DeleteComment(ctx, scope, comment.ID, userID)
	if err != nil || deleted.Body != "[deleted]" || deleted.DeletedAt == nil {
		t.Fatalf("deleted comment = %#v, err = %v", deleted, err)
	}
	comments, err := repository.ListComments(ctx, scope, item.ID)
	if err != nil || len(comments) != 1 || comments[0].DeletedAt == nil {
		t.Fatalf("comments = %#v, err = %v", comments, err)
	}
}
