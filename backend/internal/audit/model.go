package audit

import (
	"context"
	"time"
)

type Record struct {
	ID             string    `json:"id"`
	ActorType      string    `json:"actor_type"`
	ActorID        string    `json:"actor_id"`
	OrganizationID string    `json:"organization_id"`
	Source         string    `json:"source"`
	Action         string    `json:"action"`
	ResourceType   string    `json:"resource_type"`
	ResourceID     string    `json:"resource_id"`
	Before         any       `json:"before,omitempty"`
	After          any       `json:"after,omitempty"`
	RequestID      string    `json:"request_id,omitempty"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Writer interface {
	Record(context.Context, Record) error
}

type Filter struct {
	ResourceType string
	ResourceID   string
	Limit        int
}

type Reader interface {
	List(context.Context, string, Filter) ([]Record, error)
}
