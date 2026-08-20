package workitem

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const workItemSelect = `
	SELECT wi.id::text, wi.organization_id::text, wi.workspace_id::text, wi.project_id::text,
	       wi.number, p.key || '-' || wi.number, wit.key, wi.title, wi.description, ws.key,
	       COALESCE(wi.parent_id::text, ''), wi.priority, COALESCE(wi.assignee_id::text, ''),
	       COALESCE(wi.reporter_id::text, ''), wi.due_at, wi.estimate_points, wi.backlog_rank, COALESCE(wi.repository_id::text, ''), COALESCE(wi.sprint_id::text, ''),
	       wi.version, wi.archived_at, COALESCE(wi.archived_by, ''), wi.created_at, wi.updated_at
FROM work_items wi
JOIN projects p ON p.organization_id = wi.organization_id AND p.id = wi.project_id
JOIN work_item_types wit ON wit.id = wi.work_item_type_id
JOIN workflow_statuses ws ON ws.id = wi.status_id
`

func (r *PostgresRepository) Create(ctx context.Context, scope Scope, input CreateInput) (*WorkItem, error) {
	if platformdb.InTransaction(ctx) {
		return r.create(ctx, platformdb.ExecutorFrom(ctx, r.pool), scope, input)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin work item create: %w", err)
	}
	defer tx.Rollback(ctx)
	item, err := r.create(ctx, tx, scope, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit work item create: %w", err)
	}
	return item, nil
}

func (r *PostgresRepository) create(ctx context.Context, exec platformdb.Executor, scope Scope, input CreateInput) (*WorkItem, error) {
	var workspaceID string
	if err := exec.QueryRow(ctx, `SELECT workspace_id::text FROM projects WHERE organization_id=$1 AND id=$2 FOR UPDATE`, scope.OrganizationID, scope.ProjectID).Scan(&workspaceID); err != nil {
		return nil, mapDatabaseError(err, "project not found")
	}
	if input.ParentID != "" {
		var exists bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND id=$3)`, scope.OrganizationID, scope.ProjectID, input.ParentID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("validate parent work item: %w", err)
		}
		if !exists {
			return nil, apperr.New(apperr.CodeNotFound, 404, "parent work item not found", nil)
		}
	}
	if repositoryID := strings.TrimSpace(input.RepositoryID); repositoryID != "" {
		var linked bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repository_links WHERE organization_id=$1 AND project_id=$2 AND repository_id=$3)`, scope.OrganizationID, scope.ProjectID, repositoryID).Scan(&linked); err != nil {
			return nil, fmt.Errorf("validate work item repository: %w", err)
		}
		if !linked {
			return nil, apperr.New(apperr.CodeNotFound, 404, "repository is not linked to this project", nil)
		}
	}
	if assigneeID := strings.TrimSpace(input.AssigneeID); assigneeID != "" {
		var member bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_memberships WHERE organization_id=$1 AND user_id=$2)`, scope.OrganizationID, assigneeID).Scan(&member); err != nil {
			return nil, fmt.Errorf("validate work item assignee: %w", err)
		}
		if !member {
			return nil, apperr.New(apperr.CodeNotFound, 404, "assignee is not a member of this project", nil)
		}
	}
	if _, err := exec.Exec(ctx, `INSERT INTO work_item_types (key, display_name, is_sub_task) VALUES ($1,$1,$2) ON CONFLICT (key) DO NOTHING`, input.Type, input.Type == SubTask); err != nil {
		return nil, fmt.Errorf("ensure work item type: %w", err)
	}
	var typeID string
	if err := exec.QueryRow(ctx, `SELECT id FROM work_item_types WHERE key=$1`, input.Type).Scan(&typeID); err != nil {
		return nil, fmt.Errorf("load work item type: %w", err)
	}
	workflowID, err := ensureWorkflow(ctx, exec, scope.OrganizationID, scope.ProjectID)
	if err != nil {
		return nil, err
	}
	var rawStatusID string
	if err := exec.QueryRow(ctx, `SELECT id FROM workflow_statuses WHERE workflow_id=$1 AND key=$2`, workflowID, workflow.Raw).Scan(&rawStatusID); err != nil {
		return nil, fmt.Errorf("load raw status: %w", err)
	}
	var number int64
	if err := exec.QueryRow(ctx, `SELECT COALESCE(MAX(number), 0)+1 FROM work_items WHERE project_id=$1`, scope.ProjectID).Scan(&number); err != nil {
		return nil, fmt.Errorf("allocate work item number: %w", err)
	}
	var backlogRank int64
	if err := exec.QueryRow(ctx, `SELECT COALESCE(MAX(backlog_rank), 0)+1000 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND sprint_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid`, scope.OrganizationID, scope.ProjectID, input.SprintID).Scan(&backlogRank); err != nil {
		return nil, fmt.Errorf("allocate backlog rank: %w", err)
	}
	var id string
	if err := exec.QueryRow(ctx, `
	INSERT INTO work_items (organization_id, workspace_id, project_id, number, work_item_type_id, parent_id, title, description, status_id, priority, reporter_id, assignee_id, due_at, estimate_points, backlog_rank, repository_id, sprint_id)
	VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,$8,$9,COALESCE(NULLIF($10,''),'MEDIUM'),NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,NULLIF($13,'')::timestamptz,$14,$15,NULLIF($16,'')::uuid,NULLIF($17,'')::uuid)
	RETURNING id::text`, scope.OrganizationID, workspaceID, scope.ProjectID, number, typeID, input.ParentID, strings.TrimSpace(input.Title), input.Description, rawStatusID, strings.ToUpper(strings.TrimSpace(input.Priority)), input.ReporterID, input.AssigneeID, formatDueAt(input.DueAt), input.EstimatePoints, backlogRank, input.RepositoryID, input.SprintID).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert work item: %w", err)
	}
	return scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, id))
}

func (r *PostgresRepository) Get(ctx context.Context, scope Scope, id string) (*WorkItem, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	row := exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, id)
	return scanWorkItem(row)
}

func (r *PostgresRepository) List(ctx context.Context, scope Scope, filter ListFilter) ([]*WorkItem, error) {
	page, err := r.ListPage(ctx, scope, filter)
	return page.Items, err
}

func (r *PostgresRepository) ListPage(ctx context.Context, scope Scope, filter ListFilter) (ListResult, error) {
	cursor, err := decodeWorkItemCursor(filter.Cursor)
	if err != nil {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "cursor is invalid", nil)
	}
	sortOrder := strings.ToLower(strings.TrimSpace(filter.Sort))
	if sortOrder == "" {
		sortOrder = "updated"
	}
	if sortOrder != "updated" && sortOrder != "backlog" {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported work item sort", nil)
	}
	if cursor.Sort != "" && cursor.Sort != sortOrder {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "cursor does not match work item sort", nil)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	query := workItemSelect + ` WHERE wi.organization_id=$1 AND wi.project_id=$2`
	args := []any{scope.OrganizationID, scope.ProjectID}
	if !filter.IncludeArchived {
		query += ` AND wi.archived_at IS NULL`
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query += fmt.Sprintf(" AND ws.key=$%d", len(args)+1)
		args = append(args, status)
	}
	if itemType := strings.TrimSpace(filter.Type); itemType != "" {
		query += fmt.Sprintf(" AND wit.key=$%d", len(args)+1)
		args = append(args, strings.ToUpper(itemType))
	}
	if priority := strings.TrimSpace(filter.Priority); priority != "" {
		query += fmt.Sprintf(" AND wi.priority=$%d", len(args)+1)
		args = append(args, strings.ToUpper(priority))
	}
	if assigneeID := strings.TrimSpace(filter.AssigneeID); assigneeID != "" {
		query += fmt.Sprintf(" AND wi.assignee_id=$%d", len(args)+1)
		args = append(args, assigneeID)
	}
	if sprintID := strings.TrimSpace(filter.SprintID); sprintID != "" {
		query += fmt.Sprintf(" AND wi.sprint_id=$%d", len(args)+1)
		args = append(args, sprintID)
	}
	if repositoryID := strings.TrimSpace(filter.RepositoryID); repositoryID != "" {
		query += fmt.Sprintf(" AND wi.repository_id=$%d", len(args)+1)
		args = append(args, repositoryID)
	}
	if search := strings.TrimSpace(filter.Query); search != "" {
		query += fmt.Sprintf(" AND to_tsvector('simple', wi.title || ' ' || wi.description) @@ plainto_tsquery('simple', $%d)", len(args)+1)
		args = append(args, search)
	}
	if sortOrder == "backlog" && cursor.Sort == "backlog" {
		position := len(args) + 1
		query += fmt.Sprintf(" AND (wi.backlog_rank > $%d OR (wi.backlog_rank = $%d AND wi.id > $%d))", position, position, position+1)
		args = append(args, cursor.BacklogRank, cursor.ID)
	} else if !cursor.UpdatedAt.IsZero() {
		position := len(args) + 1
		query += fmt.Sprintf(" AND (wi.updated_at < $%d OR (wi.updated_at = $%d AND wi.id < $%d))", position, position, position+1)
		args = append(args, cursor.UpdatedAt, cursor.ID)
	}
	if sortOrder == "backlog" {
		query += fmt.Sprintf(" ORDER BY wi.backlog_rank ASC, wi.id ASC LIMIT $%d", len(args)+1)
	} else {
		query += fmt.Sprintf(" ORDER BY wi.updated_at DESC, wi.id DESC LIMIT $%d", len(args)+1)
	}
	args = append(args, limit+1)
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list work items: %w", err)
	}
	defer rows.Close()
	items := make([]*WorkItem, 0, limit)
	var nextCursor string
	for rows.Next() {
		item, scanErr := scanWorkItem(rows)
		if scanErr != nil {
			return ListResult{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate work items: %w", err)
	}
	if len(items) > limit {
		nextCursor, err = encodeWorkItemCursor(items[limit-1], sortOrder)
		if err != nil {
			return ListResult{}, err
		}
		items = items[:limit]
	}
	return ListResult{Items: items, NextCursor: nextCursor}, nil
}

func (r *PostgresRepository) Update(ctx context.Context, scope Scope, id string, expectedVersion int64, mutate func(*WorkItem) error) (*WorkItem, error) {
	if platformdb.InTransaction(ctx) {
		return r.update(ctx, platformdb.ExecutorFrom(ctx, r.pool), scope, id, expectedVersion, mutate)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin work item update: %w", err)
	}
	defer tx.Rollback(ctx)
	item, err := r.update(ctx, tx, scope, id, expectedVersion, mutate)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit work item update: %w", err)
	}
	return item, nil
}

func (r *PostgresRepository) update(ctx context.Context, exec platformdb.Executor, scope Scope, id string, expectedVersion int64, mutate func(*WorkItem) error) (*WorkItem, error) {
	current, err := scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 FOR UPDATE`, scope.OrganizationID, scope.ProjectID, id))
	if err != nil {
		return nil, err
	}
	if current.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": current.Version})
	}
	updated := *current
	if err := mutate(&updated); err != nil {
		return nil, err
	}
	statusKey := ""
	if updated.Status != current.Status {
		statusKey = updated.Status
	}
	assignee := "__KEEP__"
	if updated.AssigneeID != current.AssigneeID {
		assignee = updated.AssigneeID
	}
	priority := "__KEEP__"
	if updated.Priority != current.Priority {
		priority = strings.ToUpper(strings.TrimSpace(updated.Priority))
	}
	dueAt := "__KEEP__"
	if updated.DueAt == nil && current.DueAt != nil {
		dueAt = ""
	} else if updated.DueAt != nil && (current.DueAt == nil || !updated.DueAt.Equal(*current.DueAt)) {
		dueAt = formatDueAt(updated.DueAt)
	}
	estimate := "__KEEP__"
	if updated.EstimatePoints == nil && current.EstimatePoints != nil {
		estimate = ""
	} else if updated.EstimatePoints != nil && (current.EstimatePoints == nil || *updated.EstimatePoints != *current.EstimatePoints) {
		estimate = strconv.Itoa(*updated.EstimatePoints)
	}
	parent := "__KEEP__"
	if updated.ParentID != current.ParentID {
		parent = updated.ParentID
	}
	repository := "__KEEP__"
	if updated.RepositoryID != current.RepositoryID {
		repository = updated.RepositoryID
	}
	sprint := "__KEEP__"
	if updated.SprintID != current.SprintID {
		sprint = updated.SprintID
	}
	commandTag, err := exec.Exec(ctx, `
UPDATE work_items wi
SET title=$1,
    description=$2,
    parent_id=CASE WHEN $3='__KEEP__' THEN wi.parent_id ELSE NULLIF($3,'')::uuid END,
    repository_id=CASE WHEN $4='__KEEP__' THEN wi.repository_id ELSE NULLIF($4,'')::uuid END,
    status_id=CASE WHEN $5='' THEN wi.status_id ELSE (SELECT ws.id FROM workflow_statuses ws JOIN workflows w ON w.id=ws.workflow_id WHERE w.organization_id=wi.organization_id AND w.project_id=wi.project_id AND ws.key=$5) END,
    assignee_id=CASE WHEN $6='__KEEP__' THEN wi.assignee_id ELSE NULLIF($6,'')::uuid END,
    priority=CASE WHEN $7='__KEEP__' THEN wi.priority ELSE $7 END,
    due_at=CASE WHEN $8='__KEEP__' THEN wi.due_at ELSE NULLIF($8,'')::timestamptz END,
    estimate_points=CASE WHEN $9='__KEEP__' THEN wi.estimate_points ELSE NULLIF($9,'')::integer END,
    sprint_id=CASE WHEN $10='__KEEP__' THEN wi.sprint_id ELSE NULLIF($10,'')::uuid END,
    version=wi.version+1,
    updated_at=now()
WHERE wi.organization_id=$11 AND wi.project_id=$12 AND wi.id=$13 AND wi.version=$14
	  AND ($3='__KEEP__' OR $3='' OR EXISTS (SELECT 1 FROM work_items parent WHERE parent.organization_id=wi.organization_id AND parent.project_id=wi.project_id AND parent.id=NULLIF($3,'')::uuid))
	  AND ($4='__KEEP__' OR $4='' OR EXISTS (SELECT 1 FROM repository_links rl WHERE rl.organization_id=wi.organization_id AND rl.project_id=wi.project_id AND rl.repository_id=NULLIF($4,'')::uuid))
	  AND ($5='' OR EXISTS (SELECT 1 FROM workflow_statuses ws JOIN workflows w ON w.id=ws.workflow_id WHERE w.organization_id=wi.organization_id AND w.project_id=wi.project_id AND ws.key=$5))
	  AND ($6='__KEEP__' OR $6='' OR EXISTS (SELECT 1 FROM organization_memberships om WHERE om.organization_id=wi.organization_id AND om.user_id=NULLIF($6,'')::uuid))
  AND ($10='__KEEP__' OR $10='' OR EXISTS (SELECT 1 FROM sprints s WHERE s.organization_id=wi.organization_id AND s.project_id=wi.project_id AND s.id=NULLIF($10,'')::uuid))`,
		updated.Title, updated.Description, parent, repository, statusKey, assignee, priority, dueAt, estimate, sprint, scope.OrganizationID, scope.ProjectID, id, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("update work item: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale or transition is invalid", nil)
	}
	return scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, id))
}

func (r *PostgresRepository) MoveRank(ctx context.Context, scope Scope, id, direction string, expectedVersion int64) (*WorkItem, error) {
	if platformdb.InTransaction(ctx) {
		return r.moveRank(ctx, platformdb.ExecutorFrom(ctx, r.pool), scope, id, direction, expectedVersion)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin work item rank update: %w", err)
	}
	defer tx.Rollback(ctx)
	item, err := r.moveRank(ctx, tx, scope, id, direction, expectedVersion)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit work item rank update: %w", err)
	}
	return item, nil
}

func (r *PostgresRepository) moveRank(ctx context.Context, exec platformdb.Executor, scope Scope, id, direction string, expectedVersion int64) (*WorkItem, error) {
	// ponytail: serialize ranking within a project; fractional ranks are unnecessary until this lock is measurable.
	var projectKey string
	if err := exec.QueryRow(ctx, `SELECT id::text FROM projects WHERE organization_id=$1 AND id=$2 FOR UPDATE`, scope.OrganizationID, scope.ProjectID).Scan(&projectKey); err != nil {
		return nil, mapDatabaseError(err, "project not found")
	}
	current, err := scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 FOR UPDATE`, scope.OrganizationID, scope.ProjectID, id))
	if err != nil {
		return nil, err
	}
	if current.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": current.Version})
	}
	if direction != "up" && direction != "down" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "rank direction must be up or down", nil)
	}
	comparison, order := "<", "DESC"
	if direction == "down" {
		comparison, order = ">", "ASC"
	}
	var neighborID string
	var neighborRank int64
	neighborQuery := fmt.Sprintf(`
		SELECT wi.id::text, wi.backlog_rank
		FROM work_items wi
		WHERE wi.organization_id=$1 AND wi.project_id=$2
		  AND wi.sprint_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid
		  AND wi.archived_at IS NULL
		  AND (wi.backlog_rank %s $4 OR (wi.backlog_rank = $4 AND wi.id %s $5))
		ORDER BY wi.backlog_rank %s, wi.id %s
		LIMIT 1
		FOR UPDATE`, comparison, comparison, order, order)
	err = exec.QueryRow(ctx, neighborQuery, scope.OrganizationID, scope.ProjectID, current.SprintID, current.BacklogRank, current.ID).Scan(&neighborID, &neighborRank)
	if err == pgx.ErrNoRows {
		return current, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find neighboring work item: %w", err)
	}
	if _, err := exec.Exec(ctx, `UPDATE work_items SET backlog_rank=$1, version=version+1, updated_at=now() WHERE organization_id=$2 AND project_id=$3 AND id=$4`, current.BacklogRank, scope.OrganizationID, scope.ProjectID, neighborID); err != nil {
		return nil, fmt.Errorf("move neighboring work item: %w", err)
	}
	if tag, err := exec.Exec(ctx, `UPDATE work_items SET backlog_rank=$1, version=version+1, updated_at=now() WHERE organization_id=$2 AND project_id=$3 AND id=$4 AND version=$5`, neighborRank, scope.OrganizationID, scope.ProjectID, id, expectedVersion); err != nil {
		return nil, fmt.Errorf("move work item: %w", err)
	} else if tag.RowsAffected() != 1 {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", nil)
	}
	return scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, id))
}

type rowScanner interface {
	Scan(...any) error
}

func scanWorkItem(row rowScanner) (*WorkItem, error) {
	var item WorkItem
	var itemType, status string
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.ProjectID, &item.Number, &item.Key, &itemType, &item.Title, &item.Description, &status, &item.ParentID, &item.Priority, &item.AssigneeID, &item.ReporterID, &item.DueAt, &item.EstimatePoints, &item.BacklogRank, &item.RepositoryID, &item.SprintID, &item.Version, &item.ArchivedAt, &item.ArchivedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, mapDatabaseError(err, "work item not found")
	}
	item.Type = Type(itemType)
	item.Status = status
	return &item, nil
}

func (r *PostgresRepository) Archive(ctx context.Context, scope Scope, id string, expectedVersion int64, actorID string) (*WorkItem, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	current, err := scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 FOR UPDATE`, scope.OrganizationID, scope.ProjectID, id))
	if err != nil {
		return nil, err
	}
	if current.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": current.Version})
	}
	if current.ArchivedAt != nil {
		return current, nil
	}
	commandTag, err := exec.Exec(ctx, `UPDATE work_items SET archived_at=now(), archived_by=NULLIF($1,''), version=version+1, updated_at=now() WHERE organization_id=$2 AND project_id=$3 AND id=$4 AND version=$5`, actorID, scope.OrganizationID, scope.ProjectID, id, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("archive work item: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", nil)
	}
	return scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, id))
}

func (r *PostgresRepository) Restore(ctx context.Context, scope Scope, id string, expectedVersion int64) (*WorkItem, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	current, err := scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 FOR UPDATE`, scope.OrganizationID, scope.ProjectID, id))
	if err != nil {
		return nil, err
	}
	if current.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": current.Version})
	}
	if current.ArchivedAt == nil {
		return current, nil
	}
	commandTag, err := exec.Exec(ctx, `UPDATE work_items SET archived_at=NULL, archived_by=NULL, version=version+1, updated_at=now() WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND version=$4`, scope.OrganizationID, scope.ProjectID, id, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("restore work item: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", nil)
	}
	return scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, id))
}

func formatDueAt(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (r *PostgresRepository) AddComment(ctx context.Context, scope Scope, workItemID, authorID, body string) (Comment, error) {
	if strings.TrimSpace(body) == "" {
		return Comment{}, apperr.New(apperr.CodeInvalidArgument, 422, "comment body is required", nil)
	}
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	var comment Comment
	err := exec.QueryRow(ctx, `INSERT INTO comments (organization_id, work_item_id, author_id, body) SELECT $1, wi.id, $3, $4 FROM work_items wi WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$5 RETURNING id::text, work_item_id::text, author_id::text, body, created_at, updated_at, deleted_at, COALESCE(deleted_by::text,'')`, scope.OrganizationID, scope.ProjectID, authorID, body, workItemID).Scan(&comment.ID, &comment.WorkItemID, &comment.AuthorID, &comment.Body, &comment.CreatedAt, &comment.UpdatedAt, &comment.DeletedAt, &comment.DeletedBy)
	if err == pgx.ErrNoRows {
		return Comment{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if err != nil {
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	return comment, nil
}

func (r *PostgresRepository) ListComments(ctx context.Context, scope Scope, workItemID string) ([]Comment, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	rows, err := exec.Query(ctx, `SELECT c.id::text, c.work_item_id::text, c.author_id::text, c.body, c.created_at, c.updated_at, c.deleted_at, COALESCE(c.deleted_by::text,'') FROM comments c JOIN work_items wi ON wi.organization_id=c.organization_id AND wi.id=c.work_item_id WHERE c.organization_id=$1 AND wi.project_id=$2 AND c.work_item_id=$3 ORDER BY c.created_at, c.id`, scope.OrganizationID, scope.ProjectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()
	var result []Comment
	for rows.Next() {
		var item Comment
		if err := rows.Scan(&item.ID, &item.WorkItemID, &item.AuthorID, &item.Body, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt, &item.DeletedBy); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}
	if _, err := r.Get(ctx, scope, workItemID); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *PostgresRepository) UpdateComment(ctx context.Context, scope Scope, commentID, authorID, body string) (Comment, error) {
	if strings.TrimSpace(body) == "" {
		return Comment{}, apperr.New(apperr.CodeInvalidArgument, 422, "comment body is required", nil)
	}
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	var comment Comment
	err := exec.QueryRow(ctx, `UPDATE comments c SET body=$1, updated_at=now() FROM work_items wi WHERE c.organization_id=$2 AND c.id=$3 AND c.author_id=$4 AND c.work_item_id=wi.id AND wi.organization_id=$2 AND wi.project_id=$5 AND c.deleted_at IS NULL RETURNING c.id::text, c.work_item_id::text, c.author_id::text, c.body, c.created_at, c.updated_at, c.deleted_at, COALESCE(c.deleted_by::text,'')`, strings.TrimSpace(body), scope.OrganizationID, commentID, authorID, scope.ProjectID).Scan(&comment.ID, &comment.WorkItemID, &comment.AuthorID, &comment.Body, &comment.CreatedAt, &comment.UpdatedAt, &comment.DeletedAt, &comment.DeletedBy)
	if err == pgx.ErrNoRows {
		return Comment{}, apperr.New(apperr.CodeNotFound, 404, "comment not found", nil)
	}
	if err != nil {
		return Comment{}, fmt.Errorf("update comment: %w", err)
	}
	return comment, nil
}

func (r *PostgresRepository) DeleteComment(ctx context.Context, scope Scope, commentID, authorID string) (Comment, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	var comment Comment
	err := exec.QueryRow(ctx, `UPDATE comments c SET body='[deleted]', deleted_at=now(), deleted_by=$1, updated_at=now() FROM work_items wi WHERE c.organization_id=$2 AND c.id=$3 AND c.author_id=$1 AND c.work_item_id=wi.id AND wi.organization_id=$2 AND wi.project_id=$4 AND c.deleted_at IS NULL RETURNING c.id::text, c.work_item_id::text, c.author_id::text, c.body, c.created_at, c.updated_at, c.deleted_at, COALESCE(c.deleted_by::text,'')`, authorID, scope.OrganizationID, commentID, scope.ProjectID).Scan(&comment.ID, &comment.WorkItemID, &comment.AuthorID, &comment.Body, &comment.CreatedAt, &comment.UpdatedAt, &comment.DeletedAt, &comment.DeletedBy)
	if err == pgx.ErrNoRows {
		return Comment{}, apperr.New(apperr.CodeNotFound, 404, "comment not found", nil)
	}
	if err != nil {
		return Comment{}, fmt.Errorf("delete comment: %w", err)
	}
	return comment, nil
}

func (r *PostgresRepository) AddLink(ctx context.Context, scope Scope, sourceID, targetID, relationType string) (Link, error) {
	if sourceID == targetID || strings.TrimSpace(relationType) == "" {
		return Link{}, apperr.New(apperr.CodeInvalidArgument, 422, "distinct work items and relation_type are required", nil)
	}
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	var link Link
	err := exec.QueryRow(ctx, `INSERT INTO work_item_links (organization_id, source_id, target_id, relation_type) SELECT $1, source.id, target.id, $4 FROM work_items source JOIN work_items target ON target.organization_id=source.organization_id WHERE source.organization_id=$1 AND source.project_id=$2 AND target.project_id=$2 AND source.id=$3 AND target.id=$5 RETURNING id::text, source_id::text, target_id::text, relation_type, created_at`, scope.OrganizationID, scope.ProjectID, sourceID, strings.TrimSpace(relationType), targetID).Scan(&link.ID, &link.SourceID, &link.TargetID, &link.RelationType, &link.CreatedAt)
	if err == pgx.ErrNoRows {
		return Link{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return Link{}, apperr.New(apperr.CodeConflict, 409, "work item link already exists", nil)
		}
		return Link{}, fmt.Errorf("create work item link: %w", err)
	}
	return link, nil
}

func (r *PostgresRepository) ListLinks(ctx context.Context, scope Scope, workItemID string) ([]Link, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	rows, err := exec.Query(ctx, `SELECT l.id::text, l.source_id::text, l.target_id::text, l.relation_type, l.created_at FROM work_item_links l JOIN work_items wi ON wi.organization_id=l.organization_id AND wi.id=l.source_id WHERE l.organization_id=$1 AND wi.project_id=$2 AND l.source_id=$3 ORDER BY l.created_at, l.id`, scope.OrganizationID, scope.ProjectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list work item links: %w", err)
	}
	defer rows.Close()
	var result []Link
	for rows.Next() {
		var item Link
		if err := rows.Scan(&item.ID, &item.SourceID, &item.TargetID, &item.RelationType, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan work item link: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work item links: %w", err)
	}
	if _, err := r.Get(ctx, scope, workItemID); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *PostgresRepository) RemoveLink(ctx context.Context, scope Scope, workItemID, linkID string) error {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	tag, err := exec.Exec(ctx, `DELETE FROM work_item_links l USING work_items wi WHERE l.organization_id=$1 AND l.id=$2 AND l.source_id=wi.id AND wi.project_id=$3 AND l.source_id=$4`, scope.OrganizationID, linkID, scope.ProjectID, workItemID)
	if err != nil {
		return fmt.Errorf("remove work item link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.Get(ctx, scope, workItemID); err != nil {
			return err
		}
		return apperr.New(apperr.CodeNotFound, 404, "work item link not found", nil)
	}
	return nil
}

func (r *PostgresRepository) AddLabel(ctx context.Context, scope Scope, workItemID, name, color string) (Label, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	var exists bool
	if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND id=$3)`, scope.OrganizationID, scope.ProjectID, workItemID).Scan(&exists); err != nil {
		return Label{}, fmt.Errorf("validate work item for label: %w", err)
	}
	if !exists {
		return Label{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	var label Label
	if err := exec.QueryRow(ctx, `
INSERT INTO labels (organization_id, name, color)
VALUES ($1, $2, $3)
ON CONFLICT (organization_id, name) DO UPDATE SET color=EXCLUDED.color
RETURNING id::text, organization_id::text, name, color`, scope.OrganizationID, strings.TrimSpace(name), strings.TrimSpace(color)).Scan(&label.ID, &label.OrganizationID, &label.Name, &label.Color); err != nil {
		return Label{}, fmt.Errorf("upsert label: %w", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO work_item_labels (organization_id, work_item_id, label_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, scope.OrganizationID, workItemID, label.ID); err != nil {
		return Label{}, fmt.Errorf("link label: %w", err)
	}
	return label, nil
}

func (r *PostgresRepository) RemoveLabel(ctx context.Context, scope Scope, workItemID, labelID string) error {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	commandTag, err := exec.Exec(ctx, `DELETE FROM work_item_labels wl USING work_items wi WHERE wl.organization_id=$1 AND wl.work_item_id=wi.id AND wi.project_id=$2 AND wl.work_item_id=$3 AND wl.label_id=$4`, scope.OrganizationID, scope.ProjectID, workItemID, labelID)
	if err != nil {
		return fmt.Errorf("remove label: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		if _, err := r.Get(ctx, scope, workItemID); err != nil {
			return err
		}
		return apperr.New(apperr.CodeNotFound, 404, "label is not attached to work item", nil)
	}
	return nil
}

func (r *PostgresRepository) ListLabels(ctx context.Context, scope Scope, workItemID string) ([]Label, error) {
	exec := platformdb.ExecutorFrom(ctx, r.pool)
	rows, err := exec.Query(ctx, `SELECT l.id::text, l.organization_id::text, l.name, l.color FROM labels l JOIN work_item_labels wl ON wl.organization_id=l.organization_id AND wl.label_id=l.id JOIN work_items wi ON wi.organization_id=wl.organization_id AND wi.id=wl.work_item_id WHERE l.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 ORDER BY l.name`, scope.OrganizationID, scope.ProjectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	defer rows.Close()
	result := make([]Label, 0)
	for rows.Next() {
		var label Label
		if err := rows.Scan(&label.ID, &label.OrganizationID, &label.Name, &label.Color); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		result = append(result, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labels: %w", err)
	}
	if _, err := r.Get(ctx, scope, workItemID); err != nil {
		return nil, err
	}
	return result, nil
}

func ensureWorkflow(ctx context.Context, exec platformdb.Executor, organizationID, projectID string) (string, error) {
	defaultWorkflow := workflow.Default()
	inserted, err := exec.Exec(ctx, `INSERT INTO workflows (organization_id, project_id, name) VALUES ($1,$2,'Default') ON CONFLICT (organization_id, project_id) DO NOTHING`, organizationID, projectID)
	if err != nil {
		return "", fmt.Errorf("ensure workflow: %w", err)
	}
	var workflowID string
	if err := exec.QueryRow(ctx, `SELECT id FROM workflows WHERE organization_id=$1 AND project_id=$2`, organizationID, projectID).Scan(&workflowID); err != nil {
		return "", fmt.Errorf("load workflow: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var workflowName string
		if err := exec.QueryRow(ctx, `SELECT name FROM workflows WHERE id=$1`, workflowID).Scan(&workflowName); err != nil {
			return "", fmt.Errorf("load existing workflow name: %w", err)
		}
		if workflowName != defaultWorkflow.Name {
			return workflowID, nil
		}
		statusKeys := make([]string, 0, len(defaultWorkflow.Statuses))
		for key := range defaultWorkflow.Statuses {
			statusKeys = append(statusKeys, key)
		}
		var unexpectedStatusCount, transitionCount int
		if err := exec.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE key <> ALL($2::text[])),
			       (SELECT COUNT(*) FROM workflow_transitions WHERE workflow_id=$1)
			FROM workflow_statuses
			WHERE workflow_id=$1`, workflowID, statusKeys).Scan(&unexpectedStatusCount, &transitionCount); err != nil {
			return "", fmt.Errorf("inspect existing workflow definition: %w", err)
		}
		// Existing custom workflows, including intentionally empty ones, must not
		// receive the default graph implicitly.
		if unexpectedStatusCount > 0 || transitionCount > 0 {
			return workflowID, nil
		}
	}
	for _, status := range defaultWorkflow.Statuses {
		if _, err := exec.Exec(ctx, `INSERT INTO workflow_statuses (workflow_id, key, display_name, category, position, is_terminal) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (workflow_id,key) DO NOTHING`, workflowID, status.Key, status.Name, status.Category, status.Position, status.IsTerminal); err != nil {
			return "", fmt.Errorf("ensure workflow status: %w", err)
		}
	}
	for _, transition := range defaultWorkflow.Transitions {
		var fromID, toID string
		if err := exec.QueryRow(ctx, `SELECT id FROM workflow_statuses WHERE workflow_id=$1 AND key=$2`, workflowID, transition.From).Scan(&fromID); err != nil {
			return "", fmt.Errorf("load workflow transition source: %w", err)
		}
		if err := exec.QueryRow(ctx, `SELECT id FROM workflow_statuses WHERE workflow_id=$1 AND key=$2`, workflowID, transition.To).Scan(&toID); err != nil {
			return "", fmt.Errorf("load workflow transition target: %w", err)
		}
		if _, err := exec.Exec(ctx, `INSERT INTO workflow_transitions (workflow_id, from_status_id, to_status_id, key, display_name) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (workflow_id,key) DO NOTHING`, workflowID, fromID, toID, transition.Key, transition.Name); err != nil {
			return "", fmt.Errorf("ensure workflow transition: %w", err)
		}
		var transitionID string
		if err := exec.QueryRow(ctx, `SELECT id FROM workflow_transitions WHERE workflow_id=$1 AND key=$2`, workflowID, transition.Key).Scan(&transitionID); err != nil {
			return "", fmt.Errorf("load workflow transition: %w", err)
		}
		for _, rule := range transition.Required {
			if _, err := exec.Exec(ctx, `INSERT INTO transition_rules (transition_id, rule_type) VALUES ($1,$2) ON CONFLICT (transition_id,rule_type) DO NOTHING`, transitionID, rule); err != nil {
				return "", fmt.Errorf("ensure workflow transition rule: %w", err)
			}
		}
	}
	return workflowID, nil
}

func mapDatabaseError(err error, notFound string) error {
	if err == pgx.ErrNoRows {
		return apperr.New(apperr.CodeNotFound, 404, notFound, nil)
	}
	return fmt.Errorf("database query: %w", err)
}

var _ Repository = (*PostgresRepository)(nil)
