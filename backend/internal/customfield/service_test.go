package customfield

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestCustomFieldValuesAreTypedAndScoped(t *testing.T) {
	service := NewService(NewMemoryStore())
	actor := identity.Actor{ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{"*": true}}
	definition, err := service.CreateDefinition(context.Background(), actor, CreateInput{ProjectID: "project-1", Key: "RISK", DisplayName: "Risk", ValueType: Select, Options: []string{"low", "high"}})
	if err != nil {
		t.Fatalf("create field: %v", err)
	}
	value := "high"
	stored, err := service.SetValue(context.Background(), actor, "project-1", "item-1", definition.ID, &value)
	if err != nil {
		t.Fatalf("set value: %v", err)
	}
	if stored.Value != "high" {
		t.Fatalf("stored value = %q, want high", stored.Value)
	}
	if _, err := service.SetValue(context.Background(), actor, "project-1", "item-1", definition.ID, stringPtr("unknown")); err == nil {
		t.Fatal("invalid select option was accepted")
	}
	items, err := service.ListValues(context.Background(), actor, "project-1", "item-1")
	if err != nil || len(items) != 1 || items[0].Value != "high" {
		t.Fatalf("list values = %#v, err=%v", items, err)
	}
}

func stringPtr(value string) *string { return &value }
