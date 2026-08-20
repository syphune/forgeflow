package idempotency

import (
	"context"
	"testing"
)

func TestMemoryStoreClaimReplayAndRelease(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if _, err := store.Claim(ctx, "org", "user", "request-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(ctx, "org", "user", "request-1"); err != ErrInProgress {
		t.Fatalf("second claim = %v, want in-progress", err)
	}
	if err := store.Complete(ctx, "org", "user", "request-1", 201, []byte(`{"id":"item-1"}`)); err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(ctx, "org", "user", "request-1")
	if err != nil || !claim.Replay || claim.Status != 201 || string(claim.ResponseBody) != `{"id":"item-1"}` {
		t.Fatalf("replay = %#v, err = %v", claim, err)
	}
	if _, err := store.Claim(ctx, "org", "user", ""); err == nil {
		t.Fatal("empty idempotency key must be rejected")
	}
	if err := store.Release(ctx, "org", "user", "request-2"); err != nil {
		t.Fatal(err)
	}
}
