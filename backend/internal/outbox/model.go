package outbox

import (
	"context"
	"time"
)

type Event struct {
	ID             string
	OrganizationID string
	EventType      string
	AggregateType  string
	AggregateID    string
	IdempotencyKey string
	Payload        map[string]any
	OccurredAt     time.Time
}

type Writer interface {
	Append(context.Context, Event) error
}
