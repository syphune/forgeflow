package attachment

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestServiceStoresScopedAttachmentAndRejectsUnsafeName(t *testing.T) {
	store := NewMemoryStore()
	blobs := NewMemoryBlobStore()
	service := NewService(store, blobs, nil)
	actor := testActor()
	item, err := service.Create(context.Background(), actor, "project-1", "work-1", "notes.txt", "text/plain; charset=utf-8", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if item.ContentType != "text/plain" || item.SizeBytes != 5 || item.SHA256 == "" {
		t.Fatalf("unexpected attachment: %#v", item)
	}
	items, err := service.List(context.Background(), actor, "project-1", "work-1")
	if err != nil || len(items) != 1 {
		t.Fatalf("list attachment: %v %#v", err, items)
	}
	got, reader, err := service.Open(context.Background(), actor, "project-1", "work-1", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "hello" || got.ID != item.ID {
		t.Fatalf("read attachment: %v %#v %q", err, got, content)
	}
	if _, err := service.Create(context.Background(), actor, "project-1", "work-1", "../secret", "text/plain", strings.NewReader("no")); err == nil {
		t.Fatal("expected unsafe filename rejection")
	}
}

func TestServiceValidatesReferencesWithinWorkItem(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, NewMemoryBlobStore(), nil)
	if _, err := store.Create(context.Background(), Attachment{ID: "attachment-1", OrganizationID: "org-1", ProjectID: "project-1", WorkItemID: "work-1"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateReferences(context.Background(), "org-1", "project-1", "work-1", []string{"attachment-1"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateReferences(context.Background(), "org-1", "project-1", "work-1", []string{"attachment-2"}); err == nil {
		t.Fatal("expected foreign attachment reference to be rejected")
	}
}

func TestLocalBlobStoreBoundsAndCleansUp(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "attachment-1", strings.NewReader("12345"), 4); err == nil {
		t.Fatal("expected size limit")
	}
	if _, err := store.Put(context.Background(), "attachment-1", strings.NewReader("1234"), 4); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), "attachment-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), "attachment-1"); err == nil {
		t.Fatal("expected deleted attachment")
	}
}

func TestBlobStoreConfiguration(t *testing.T) {
	store, err := NewBlobStore(StorageConfig{Mode: "local", LocalDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(*LocalBlobStore); !ok {
		t.Fatalf("expected local blob store, got %T", store)
	}
	if _, err := NewBlobStore(StorageConfig{Mode: "s3"}); err == nil {
		t.Fatal("expected incomplete S3 configuration to fail")
	}
}

func testActor() identity.Actor {
	return identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
}
