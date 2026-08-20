package notification

import (
	"context"
	"fmt"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) List(ctx context.Context, organizationID, userID string, limit int) ([]Notification, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, organization_id::text, user_id::text, COALESCE(project_id::text,''), notification_type, title, body, resource_type, resource_id, read_at, created_at FROM notifications WHERE organization_id=$1 AND user_id=$2 ORDER BY created_at DESC, id DESC LIMIT $3`, organizationID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	result := make([]Notification, 0)
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.UserID, &item.ProjectID, &item.NotificationType, &item.Title, &item.Body, &item.ResourceType, &item.ResourceID, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) CountUnread(ctx context.Context, organizationID, userID string) (int, error) {
	var count int
	if err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE organization_id=$1 AND user_id=$2 AND read_at IS NULL`, organizationID, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

func (s *PostgresStore) MarkRead(ctx context.Context, organizationID, userID, id string) error {
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE notifications SET read_at=COALESCE(read_at, now()) WHERE id=$1 AND organization_id=$2 AND user_id=$3`, id, organizationID, userID)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperr.New(apperr.CodeNotFound, 404, "notification not found", nil)
	}
	return nil
}

func (s *PostgresStore) MarkAllRead(ctx context.Context, organizationID, userID string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE notifications SET read_at=now() WHERE organization_id=$1 AND user_id=$2 AND read_at IS NULL`, organizationID, userID)
	if err != nil {
		return fmt.Errorf("mark notifications read: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateForProject(ctx context.Context, organizationID, projectID, notificationType, title, body, resourceType, resourceID string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `
INSERT INTO notifications (id, organization_id, user_id, project_id, notification_type, title, body, resource_type, resource_id)
SELECT gen_random_uuid(), $1, om.user_id, $2, $3, $4, $5, $6, $7
FROM organization_memberships om
JOIN projects p ON p.organization_id=om.organization_id AND p.id=$2
WHERE om.organization_id=$1
`, organizationID, projectID, strings.TrimSpace(notificationType), strings.TrimSpace(title), strings.TrimSpace(body), strings.TrimSpace(resourceType), strings.TrimSpace(resourceID))
	if err != nil {
		return fmt.Errorf("create project notifications: %w", err)
	}
	return nil
}

var _ Store = (*PostgresStore)(nil)
