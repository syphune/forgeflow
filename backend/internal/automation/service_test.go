package automation

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/notification"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type recordingNotifications struct{ count int }

func (r *recordingNotifications) List(context.Context, string, string, int) ([]notification.Notification, error) {
	return nil, nil
}
func (r *recordingNotifications) CountUnread(context.Context, string, string) (int, error) { return 0, nil }
func (r *recordingNotifications) MarkRead(context.Context, string, string, string) error { return nil }
func (r *recordingNotifications) MarkAllRead(context.Context, string, string) error      { return nil }
func (r *recordingNotifications) CreateForProject(context.Context, string, string, string, string, string, string, string) error {
	r.count++
	return nil
}

func TestServiceHandlesMatchingEventOnce(t *testing.T) {
	notifications := &recordingNotifications{}
	service := NewService(NewMemoryStore(), notifications)
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	rule, err := service.Create(context.Background(), actor, CreateInput{
		ProjectID: "project-1",
		Name:      "Notify transitions",
		EventType: "work_item.transitioned",
		Config:    map[string]any{"title": "Moved {aggregate_id}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := outbox.Event{ID: "event-1", OrganizationID: "org-1", EventType: rule.EventType, AggregateType: "work_item", AggregateID: "item-1", Payload: map[string]any{"project_id": "project-1"}}
	if err := service.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if notifications.count != 1 {
		t.Fatalf("notifications = %d, want 1", notifications.count)
	}
}

func TestMatchingDoesNotCrossProjects(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for _, projectID := range []string{"project-1", "project-2"} {
		if _, err := store.Create(ctx, "org-1", CreateInput{ProjectID: projectID, Name: projectID, EventType: "work_item.created"}); err != nil {
			t.Fatal(err)
		}
	}
	matched, err := store.Matching(ctx, outbox.Event{OrganizationID: "org-1", EventType: "work_item.created", AggregateType: "work_item", Payload: map[string]any{"project_id": "project-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].ProjectID != "project-1" {
		t.Fatalf("matched rules = %#v, want only project-1", matched)
	}
}

func TestServiceRejectsRuntimeConfig(t *testing.T) {
	service := NewService(NewMemoryStore(), &recordingNotifications{})
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	if _, err := service.Create(context.Background(), actor, CreateInput{
		ProjectID: "project-1",
		Name:      "Unsafe",
		EventType: "work_item.created",
		Config:    map[string]any{"expression": "exec('rm -rf /')"},
	}); err == nil {
		t.Fatal("expected unsupported runtime config error")
	}
}

func TestMutationsRecordAuditAndOutbox(t *testing.T) {
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	service := NewService(NewMemoryStore(), &recordingNotifications{}, Options{Recorder: MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	rule, err := service.Create(context.Background(), actor, CreateInput{ProjectID: "project-1", Name: "Notify", EventType: "work_item.created"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetEnabled(context.Background(), actor, "project-1", rule.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), actor, "project-1", rule.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(auditWriter.Records()); got != 3 {
		t.Fatalf("audit records = %d, want 3", got)
	}
	if got := len(outboxWriter.Events()); got != 3 {
		t.Fatalf("outbox events = %d, want 3", got)
	}
}
