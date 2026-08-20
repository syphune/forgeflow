package attachment

import (
	"context"
	"fmt"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) List(ctx context.Context, organizationID, projectID, workItemID string) ([]Attachment, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT id::text, organization_id::text, project_id::text, work_item_id::text,
       name, content_type, storage_key, sha256, size_bytes, created_by, created_at
FROM attachments
WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3
ORDER BY created_at DESC, id DESC`, organizationID, projectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	result := make([]Attachment, 0)
	for rows.Next() {
		var item Attachment
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkItemID, &item.Name, &item.ContentType, &item.StorageKey, &item.SHA256, &item.SizeBytes, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) Create(ctx context.Context, item Attachment) (Attachment, error) {
	var result Attachment
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO attachments (id, organization_id, project_id, work_item_id, name, content_type, storage_key, sha256, size_bytes, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING id::text, organization_id::text, project_id::text, work_item_id::text,
          name, content_type, storage_key, sha256, size_bytes, created_by, created_at`, item.ID, item.OrganizationID, item.ProjectID, item.WorkItemID, item.Name, item.ContentType, item.StorageKey, item.SHA256, item.SizeBytes, item.CreatedBy).
		Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.WorkItemID, &result.Name, &result.ContentType, &result.StorageKey, &result.SHA256, &result.SizeBytes, &result.CreatedBy, &result.CreatedAt)
	if err != nil {
		return Attachment{}, fmt.Errorf("create attachment: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) Get(ctx context.Context, organizationID, projectID, workItemID, id string) (Attachment, error) {
	var item Attachment
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT id::text, organization_id::text, project_id::text, work_item_id::text,
       name, content_type, storage_key, sha256, size_bytes, created_by, created_at
FROM attachments
WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 AND id=$4`, organizationID, projectID, workItemID, id).
		Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkItemID, &item.Name, &item.ContentType, &item.StorageKey, &item.SHA256, &item.SizeBytes, &item.CreatedBy, &item.CreatedAt)
	if err == pgx.ErrNoRows {
		return Attachment{}, apperr.New(apperr.CodeNotFound, 404, "attachment not found", nil)
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) Delete(ctx context.Context, organizationID, projectID, workItemID, id string) error {
	result, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
DELETE FROM attachments
WHERE organization_id=$1 AND project_id=$2 AND work_item_id=$3 AND id=$4`, organizationID, projectID, workItemID, id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if result.RowsAffected() == 0 {
		return apperr.New(apperr.CodeNotFound, 404, "attachment not found", nil)
	}
	return nil
}

var _ Store = (*PostgresStore)(nil)
