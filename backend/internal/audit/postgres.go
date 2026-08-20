package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresWriter struct{ pool *pgxpool.Pool }

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter { return &PostgresWriter{pool: pool} }

func (w *PostgresWriter) Record(ctx context.Context, record Record) error {
	before, err := json.Marshal(record.Before)
	if err != nil {
		return fmt.Errorf("marshal audit before: %w", err)
	}
	after, err := json.Marshal(record.After)
	if err != nil {
		return fmt.Errorf("marshal audit after: %w", err)
	}
	_, err = platformdb.ExecutorFrom(ctx, w.pool).Exec(ctx, `INSERT INTO audit_logs (id, organization_id, actor_type, actor_id, source, action, resource_type, resource_id, before_json, after_json, request_id, correlation_id, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, record.ID, record.OrganizationID, record.ActorType, record.ActorID, record.Source, record.Action, record.ResourceType, record.ResourceID, before, after, record.RequestID, record.CorrelationID, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert audit record: %w", err)
	}
	return nil
}

func (w *PostgresWriter) List(ctx context.Context, organizationID string, filter Filter) ([]Record, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := w.pool.Query(ctx, `SELECT id::text, actor_type, actor_id, organization_id::text, source, action, resource_type, resource_id, before_json, after_json, COALESCE(request_id,''), COALESCE(correlation_id,''), created_at FROM audit_logs WHERE organization_id=$1 AND ($2='' OR resource_type=$2) AND ($3='' OR resource_id=$3) ORDER BY created_at DESC, id DESC LIMIT $4`, organizationID, strings.TrimSpace(filter.ResourceType), strings.TrimSpace(filter.ResourceID), limit)
	if err != nil {
		return nil, fmt.Errorf("list audit records: %w", err)
	}
	defer rows.Close()
	result := make([]Record, 0)
	for rows.Next() {
		var record Record
		var before, after []byte
		if err := rows.Scan(&record.ID, &record.ActorType, &record.ActorID, &record.OrganizationID, &record.Source, &record.Action, &record.ResourceType, &record.ResourceID, &before, &after, &record.RequestID, &record.CorrelationID, &record.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit record: %w", err)
		}
		if len(before) > 0 && string(before) != "null" {
			_ = json.Unmarshal(before, &record.Before)
		}
		if len(after) > 0 && string(after) != "null" {
			_ = json.Unmarshal(after, &record.After)
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

var _ Reader = (*PostgresWriter)(nil)
