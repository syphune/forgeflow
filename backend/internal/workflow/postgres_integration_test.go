//go:build integration

package workflow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestPostgresWorkflowRoundTripAndEdgeReplacement(t *testing.T) {
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
		organizationID = "00000000-0000-0000-0000-000000000201"
		workspaceID    = "00000000-0000-0000-0000-000000000202"
		projectID      = "00000000-0000-0000-0000-000000000203"
	)
	for _, fixture := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id, slug, display_name) VALUES ($1, 'workflow-integration', 'Workflow integration') ON CONFLICT (id) DO NOTHING`, []any{organizationID}},
		{`INSERT INTO workspaces (id, organization_id, key, display_name) VALUES ($1, $2, 'FLOW', 'Flow workspace') ON CONFLICT (id) DO NOTHING`, []any{workspaceID, organizationID}},
		{`INSERT INTO projects (id, organization_id, workspace_id, key, display_name) VALUES ($1, $2, $3, 'FLOW', 'Flow project') ON CONFLICT (id) DO NOTHING`, []any{projectID, organizationID, workspaceID}},
	} {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(Default(), NewPostgresStore(pool))
	service.SetTransaction(db.NewTransactionRunner(pool))
	actor := identity.Actor{ID: "integration-user", OrganizationID: organizationID, Capabilities: map[string]bool{identity.CapabilityProjectManage: true}}
	first, err := service.SaveForProject(ctx, actor, projectID, SaveInput{
		Name: "Product flow",
		Statuses: []Status{
			{Key: Raw, Name: "Inbox", Category: "TODO", Position: 10},
			{Key: Ready, Name: "Ready", Category: "TODO", Position: 20},
			{Key: Done, Name: "Done", Category: "DONE", Position: 30, IsTerminal: true},
		},
		Transitions: []Transition{
			{Key: "send_to_ready", From: Raw, To: Ready, Name: "Send to ready"},
			{Key: "finish", From: Ready, To: Done, Name: "Finish"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || len(first.Statuses) != 3 || len(first.Transitions) != 2 {
		t.Fatalf("saved workflow = %#v", first)
	}
	if got := first.Transitions["send_to_ready"].Required; len(got) != 1 || got[0] != RequireSpecificationReady {
		t.Fatalf("ready rules = %#v", got)
	}

	second, err := service.SaveForProject(ctx, actor, projectID, SaveInput{
		Name: "Product flow v2",
		Statuses: []Status{
			{Key: Raw, Name: "Inbox", Category: "TODO", Position: 10},
			{Key: Ready, Name: "Ready", Category: "TODO", Position: 20},
			{Key: Done, Name: "Done", Category: "DONE", Position: 30, IsTerminal: true},
		},
		Transitions: []Transition{
			{Key: "send_to_ready", From: Raw, To: Done, Name: "Finish early"},
			{Key: "finish", From: Done, To: Ready, Name: "Reopen"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := service.WorkflowFor(ctx, organizationID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "Product flow v2" || loaded.Transitions["send_to_ready"].To != Done {
		t.Fatalf("updated workflow = %#v", loaded)
	}
	_, err = service.SaveForProject(ctx, actor, projectID, SaveInput{
		Name: "Product flow permissions",
		Statuses: []Status{
			{Key: Raw, Name: "Inbox", Category: "TODO", Position: 10},
			{Key: Done, Name: "Done", Category: "DONE", Position: 20, IsTerminal: true},
		},
		Transitions: []Transition{{Key: "finish", From: Raw, To: Done, Name: "Finish", Required: []RuleType{RequirePermission}, RequiredPermissions: []string{"work_item.approve"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = service.WorkflowFor(ctx, organizationID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Transitions["finish"].RequiredPermissions; len(got) != 1 || got[0] != "work_item.approve" {
		t.Fatalf("permission rule = %#v", got)
	}
}
