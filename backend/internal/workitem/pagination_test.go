package workitem

import (
	"context"
	"testing"
	"time"
)

func TestMemoryListPageUsesOpaqueCursorWithoutDuplicates(t *testing.T) {
	now := func() time.Time { return time.Unix(100, 0).UTC() }
	repository := NewMemoryRepository(now)
	scope := Scope{OrganizationID: "org-1", ProjectID: "project-1", ProjectKey: "FF"}
	for _, title := range []string{"One", "Two", "Three"} {
		if _, err := repository.Create(context.Background(), scope, CreateInput{Type: Task, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repository.ListPage(context.Background(), scope, ListFilter{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v, err = %v", first, err)
	}
	second, err := repository.ListPage(context.Background(), scope, ListFilter{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID || second.Items[0].ID == first.Items[1].ID {
		t.Fatalf("second page = %#v, err = %v", second, err)
	}
}
