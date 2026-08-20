package notification

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestServiceScopesMemoryNotificationsToActor(t *testing.T) {
	store := NewMemoryStore()
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	if err := store.CreateForProject(context.Background(), actor.OrganizationID, "project-1", "test", "Hello", "Body", "work_item", "item-1"); err != nil {
		t.Fatal(err)
	}
	items, err := NewService(store).List(context.Background(), actor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("memory project notification leaked without a recipient: %#v", items)
	}
	if _, err := NewService(store).List(context.Background(), identity.Actor{Type: "human", ID: "", OrganizationID: actor.OrganizationID}, 10); err == nil {
		t.Fatal("expected unauthenticated actor error")
	}
}
