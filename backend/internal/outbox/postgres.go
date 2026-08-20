package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresWriter struct{ pool *pgxpool.Pool }

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter { return &PostgresWriter{pool: pool} }

func (w *PostgresWriter) Append(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = platformdb.ExecutorFrom(ctx, w.pool).Exec(ctx, `INSERT INTO outbox_events (id, organization_id, event_type, aggregate_type, aggregate_id, idempotency_key, payload, occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (idempotency_key) DO NOTHING`, event.ID, event.OrganizationID, event.EventType, event.AggregateType, event.AggregateID, event.IdempotencyKey, payload, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}
