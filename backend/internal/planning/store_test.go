package planning

import (
	"context"
	"testing"
)

func TestMemoryStorePlannedSprintLifecycle(t *testing.T) {
	store := NewMemoryStore()
	sprint, err := store.Create(context.Background(), "org-1", "project-1", "Sprint 1", "Ship", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(context.Background(), "org-1", "project-1", sprint.ID, "Sprint 2", "Review", nil, nil)
	if err != nil || updated.Name != "Sprint 2" {
		t.Fatalf("updated sprint = %#v, err = %v", updated, err)
	}
	if err := store.Delete(context.Background(), "org-1", "project-1", sprint.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(context.Background(), "org-1", "project-1", sprint.ID, "Gone", "", nil, nil); err == nil {
		t.Fatal("deleted sprint should not be editable")
	}
}

func TestMemoryStoreAllowsOnlyOneActiveSprintPerProject(t *testing.T) {
	store := NewMemoryStore()
	first, err := store.Create(context.Background(), "org-1", "project-1", "Sprint 1", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(context.Background(), "org-1", "project-1", "Sprint 2", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(context.Background(), "org-1", "project-1", first.ID, Active); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(context.Background(), "org-1", "project-1", second.ID, Active); err == nil {
		t.Fatal("expected a project to reject a second active sprint")
	}
}
