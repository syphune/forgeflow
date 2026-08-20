package planning

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Create(context.Context, string, string, string, string, *time.Time, *time.Time) (Sprint, error)
	List(context.Context, string, string) ([]Sprint, error)
	Update(context.Context, string, string, string, string, string, *time.Time, *time.Time) (Sprint, error)
	Delete(context.Context, string, string, string) error
	Transition(context.Context, string, string, string, Status) (Sprint, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Create(ctx context.Context, organizationID, projectID, name, goal string, startsAt, endsAt *time.Time) (Sprint, error) {
	var result Sprint
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO sprints (organization_id, project_id, name, goal, starts_at, ends_at) SELECT $1,$2,$3,$4,$5,$6 WHERE EXISTS (SELECT 1 FROM projects WHERE organization_id=$1 AND id=$2) RETURNING id::text, organization_id::text, project_id::text, name, goal, starts_at, ends_at, status, created_at, updated_at`, organizationID, projectID, name, goal, startsAt, endsAt).Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.Name, &result.Goal, &result.StartsAt, &result.EndsAt, &result.Status, &result.CreatedAt, &result.UpdatedAt)
	if err == pgx.ErrNoRows {
		return Sprint{}, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
	}
	if err != nil {
		return Sprint{}, fmt.Errorf("create sprint: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) List(ctx context.Context, organizationID, projectID string) ([]Sprint, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text, organization_id::text, project_id::text, name, goal, starts_at, ends_at, status, created_at, updated_at FROM sprints WHERE organization_id=$1 AND project_id=$2 ORDER BY created_at DESC, id DESC`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list sprints: %w", err)
	}
	defer rows.Close()
	var result []Sprint
	for rows.Next() {
		var item Sprint
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.Name, &item.Goal, &item.StartsAt, &item.EndsAt, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sprint: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) Update(ctx context.Context, organizationID, projectID, id, name, goal string, startsAt, endsAt *time.Time) (Sprint, error) {
	var result Sprint
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE sprints SET name=$1, goal=$2, starts_at=$3, ends_at=$4, updated_at=now() WHERE organization_id=$5 AND project_id=$6 AND id=$7 AND status='PLANNED' RETURNING id::text, organization_id::text, project_id::text, name, goal, starts_at, ends_at, status, created_at, updated_at`, name, goal, startsAt, endsAt, organizationID, projectID, id).Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.Name, &result.Goal, &result.StartsAt, &result.EndsAt, &result.Status, &result.CreatedAt, &result.UpdatedAt)
	if err == pgx.ErrNoRows {
		return Sprint{}, apperr.New(apperr.CodeConflict, 409, "only planned sprints can be edited", nil)
	}
	if err != nil {
		return Sprint{}, fmt.Errorf("update sprint: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) Delete(ctx context.Context, organizationID, projectID, id string) error {
	result, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM sprints WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND status='PLANNED' AND NOT EXISTS (SELECT 1 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND sprint_id=$3)`, organizationID, projectID, id)
	if err != nil {
		return fmt.Errorf("delete sprint: %w", err)
	}
	if result.RowsAffected() != 1 {
		return apperr.New(apperr.CodeConflict, 409, "only an unused planned sprint can be deleted", nil)
	}
	return nil
}

func (s *PostgresStore) Transition(ctx context.Context, organizationID, projectID, id string, status Status) (Sprint, error) {
	var result Sprint
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE sprints SET status=$1, starts_at=CASE WHEN $1='ACTIVE' AND starts_at IS NULL THEN now() ELSE starts_at END, updated_at=now() WHERE organization_id=$2 AND project_id=$3 AND id=$4 AND (($1='ACTIVE' AND status='PLANNED') OR ($1='COMPLETED' AND status='ACTIVE')) RETURNING id::text, organization_id::text, project_id::text, name, goal, starts_at, ends_at, status, created_at, updated_at`, status, organizationID, projectID, id).Scan(&result.ID, &result.OrganizationID, &result.ProjectID, &result.Name, &result.Goal, &result.StartsAt, &result.EndsAt, &result.Status, &result.CreatedAt, &result.UpdatedAt)
	if err == pgx.ErrNoRows {
		return Sprint{}, apperr.New(apperr.CodeConflict, 409, "sprint transition is invalid or sprint not found", nil)
	}
	if err != nil {
		return Sprint{}, fmt.Errorf("transition sprint: %w", err)
	}
	return result, nil
}

type MemoryStore struct {
	mu      sync.Mutex
	sprints map[string]Sprint
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{sprints: make(map[string]Sprint)} }

func (s *MemoryStore) Create(_ context.Context, organizationID, projectID, name, goal string, startsAt, endsAt *time.Time) (Sprint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := ids.New()
	if err != nil {
		return Sprint{}, err
	}
	now := time.Now().UTC()
	item := Sprint{ID: id, OrganizationID: organizationID, ProjectID: projectID, Name: strings.TrimSpace(name), Goal: goal, StartsAt: startsAt, EndsAt: endsAt, Status: Planned, CreatedAt: now, UpdatedAt: now}
	s.sprints[id] = item
	return item, nil
}

func (s *MemoryStore) List(_ context.Context, organizationID, projectID string) ([]Sprint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Sprint, 0)
	for _, item := range s.sprints {
		if item.OrganizationID == organizationID && item.ProjectID == projectID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (s *MemoryStore) Update(_ context.Context, organizationID, projectID, id, name, goal string, startsAt, endsAt *time.Time) (Sprint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sprints[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return Sprint{}, apperr.New(apperr.CodeNotFound, 404, "sprint not found", nil)
	}
	if item.Status != Planned {
		return Sprint{}, apperr.New(apperr.CodeConflict, 409, "only planned sprints can be edited", nil)
	}
	item.Name, item.Goal, item.StartsAt, item.EndsAt = strings.TrimSpace(name), strings.TrimSpace(goal), startsAt, endsAt
	item.UpdatedAt = time.Now().UTC()
	s.sprints[id] = item
	return item, nil
}

func (s *MemoryStore) Delete(_ context.Context, organizationID, projectID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sprints[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return apperr.New(apperr.CodeNotFound, 404, "sprint not found", nil)
	}
	if item.Status != Planned {
		return apperr.New(apperr.CodeConflict, 409, "only a planned sprint can be deleted", nil)
	}
	delete(s.sprints, id)
	return nil
}

func (s *MemoryStore) Transition(_ context.Context, organizationID, projectID, id string, status Status) (Sprint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sprints[id]
	if !ok || item.OrganizationID != organizationID || item.ProjectID != projectID {
		return Sprint{}, apperr.New(apperr.CodeNotFound, 404, "sprint not found", nil)
	}
	if (status == Active && item.Status != Planned) || (status == Completed && item.Status != Active) {
		return Sprint{}, apperr.New(apperr.CodeConflict, 409, "sprint transition is invalid", nil)
	}
	if status == Active {
		for _, other := range s.sprints {
			if other.ID != id && other.OrganizationID == organizationID && other.ProjectID == projectID && other.Status == Active {
				return Sprint{}, apperr.New(apperr.CodeConflict, 409, "project already has an active sprint", nil)
			}
		}
	}
	item.Status = status
	item.UpdatedAt = time.Now().UTC()
	if status == Active {
		now := item.UpdatedAt
		item.StartsAt = &now
	}
	s.sprints[id] = item
	return item, nil
}

var _ Store = (*PostgresStore)(nil)
var _ Store = (*MemoryStore)(nil)
