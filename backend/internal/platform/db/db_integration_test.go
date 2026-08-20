//go:build integration

package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresPoolReadiness(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var result int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != 1 {
		t.Fatalf("result = %d", result)
	}
	if err := Ready(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
