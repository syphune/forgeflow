package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler interface {
	Handle(context.Context, Event) error
}

type PostgresProcessor struct {
	pool        *pgxpool.Pool
	handler     Handler
	lease       time.Duration
	maxAttempts int
	now         func() time.Time
}

func NewPostgresProcessor(pool *pgxpool.Pool, handler Handler, now func() time.Time) *PostgresProcessor {
	return &PostgresProcessor{pool: pool, handler: handler, lease: time.Minute, maxAttempts: 10, now: now}
}

func (p *PostgresProcessor) RunOnce(ctx context.Context) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer tx.Rollback(ctx)
	var event Event
	var payload []byte
	err = tx.QueryRow(ctx, `
SELECT id::text, organization_id::text, event_type, aggregate_type, aggregate_id::text, idempotency_key, payload, occurred_at
FROM outbox_events
WHERE (status='PENDING' OR (status='PROCESSING' AND lease_until < now()))
  AND next_attempt_at <= now()
ORDER BY next_attempt_at, occurred_at
FOR UPDATE SKIP LOCKED
LIMIT 1`).Scan(&event.ID, &event.OrganizationID, &event.EventType, &event.AggregateType, &event.AggregateID, &event.IdempotencyKey, &payload, &event.OccurredAt)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim outbox event: %w", err)
	}
	if err := json.Unmarshal(payload, &event.Payload); err != nil {
		return false, fmt.Errorf("decode outbox event %s: %w", event.ID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status='PROCESSING', attempts=attempts+1, lease_until=$2 WHERE id=$1`, event.ID, p.now().Add(p.lease)); err != nil {
		return false, fmt.Errorf("lease outbox event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit outbox claim: %w", err)
	}

	if err := p.handler.Handle(ctx, event); err != nil {
		return true, p.fail(ctx, event.ID, err)
	}
	if _, err := p.pool.Exec(ctx, `UPDATE outbox_events SET status='COMPLETED', lease_until=NULL, processed_at=$2, last_error=NULL WHERE id=$1`, event.ID, p.now()); err != nil {
		return true, fmt.Errorf("complete outbox event: %w", err)
	}
	return true, nil
}

func (p *PostgresProcessor) fail(ctx context.Context, id string, cause error) error {
	message := cause.Error()
	if len(message) > 4096 {
		message = strings.TrimSpace(message[:4096]) + "..."
	}
	_, err := p.pool.Exec(ctx, `UPDATE outbox_events SET status=CASE WHEN attempts >= $2 THEN 'DEAD' ELSE 'PENDING' END, lease_until=NULL, next_attempt_at=$3, last_error=$4 WHERE id=$1`, id, p.maxAttempts, p.now().Add(30*time.Second), message)
	if err != nil {
		return fmt.Errorf("record outbox failure: %v; update failed: %w", cause, err)
	}
	return nil
}

func (p *PostgresProcessor) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, err := p.RunOnce(ctx)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
