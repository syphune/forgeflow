package planning

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestMutationsRecordAuditAndOutbox(t *testing.T) {
	auditWriter := audit.NewMemoryWriter()
	outboxWriter := outbox.NewMemoryWriter()
	service := NewService(NewMemoryStore(), Options{Recorder: MutationRecorder{Audit: auditWriter, Outbox: outboxWriter}})
	actor := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Source: "web", Capabilities: map[string]bool{identity.CapabilitySprintManage: true}}

	sprint, err := service.Create(context.Background(), actor, "project-1", "Sprint 1", "Ship", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), actor, "project-1", sprint.ID); err != nil {
		t.Fatal(err)
	}

	if got := len(auditWriter.Records()); got != 2 {
		t.Fatalf("audit records = %d, want 2", got)
	}
	if got := len(outboxWriter.Events()); got != 2 {
		t.Fatalf("outbox events = %d, want 2", got)
	}
}
