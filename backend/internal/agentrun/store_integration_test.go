//go:build integration

package agentrun

import (
	"context"
	"os"
	"testing"
	"time"

	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
)

func TestPostgresStoreListAcceptsOptionalWorkItemFilter(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := platformdb.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	store := NewPostgresStore(pool)
	for _, testCase := range []struct {
		name       string
		workItemID string
	}{
		{name: "all", workItemID: ""},
		{name: "one work item", workItemID: "00000000-0000-0000-0000-000000000001"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := store.List(ctx, "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", testCase.workItemID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
