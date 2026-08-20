package automation

import (
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/outbox"
)

const ActionNotify = "notify"

var allowedEvents = map[string]bool{
	"work_item.created":             true,
	"work_item.updated":             true,
	"work_item.assigned":            true,
	"work_item.transitioned":        true,
	"work_item.comment.created":     true,
	"work_item.link.created":        true,
	"work_item.link.deleted":        true,
	"work_item.label.created":       true,
	"work_item.label.removed":       true,
	"github.installation.connected": true,
	"repository.linked":             true,
	"repository.unlinked":           true,
	"github.push":                   true,
	"github.pull_request.updated":   true,
	"github.ci.updated":             true,
}

type Rule struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	ProjectID      string         `json:"project_id"`
	Name           string         `json:"name"`
	EventType      string         `json:"event_type"`
	ActionType     string         `json:"action_type"`
	Config         map[string]any `json:"config"`
	Enabled        bool           `json:"enabled"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type CreateInput struct {
	ProjectID  string
	Name       string
	EventType  string
	ActionType string
	Config     map[string]any
}

func eventProjectID(event outbox.Event) string {
	projectID, _ := event.Payload["project_id"].(string)
	return strings.TrimSpace(projectID)
}
