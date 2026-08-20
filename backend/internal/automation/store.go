package automation

import (
	"context"

	"github.com/forgeflow/forgeflow/backend/internal/outbox"
)

type Store interface {
	Create(context.Context, string, CreateInput) (Rule, error)
	List(context.Context, string, string) ([]Rule, error)
	SetEnabled(context.Context, string, string, string, bool) (Rule, error)
	Delete(context.Context, string, string, string) error
	Matching(context.Context, outbox.Event) ([]Rule, error)
	ClaimExecution(context.Context, string, string, string) (bool, error)
	FinishExecution(context.Context, string, string, string, error) error
}
