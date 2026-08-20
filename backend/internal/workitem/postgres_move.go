package workitem

import (
	"context"
	"fmt"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) Move(ctx context.Context, scope Scope, input MoveInput) (MoveResult, error) {
	if platformdb.InTransaction(ctx) {
		return r.move(ctx, platformdb.ExecutorFrom(ctx, r.pool), scope, input)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return MoveResult{}, fmt.Errorf("begin atomic work item move: %w", err)
	}
	defer tx.Rollback(ctx)
	result, err := r.move(ctx, tx, scope, input)
	if err != nil {
		return MoveResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MoveResult{}, fmt.Errorf("commit atomic work item move: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) move(ctx context.Context, exec platformdb.Executor, scope Scope, input MoveInput) (MoveResult, error) {
	if input.ExpectedVersion < 1 || input.ExpectedSourceOrderingVersion < 1 || input.ExpectedDestinationOrderingVersion < 1 {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "item and column ordering versions are required", nil)
	}
	targetStatus := strings.ToUpper(strings.TrimSpace(input.TargetStatus))
	if targetStatus == "" {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "target_status is required", nil)
	}
	current, err := scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 FOR UPDATE`, scope.OrganizationID, scope.ProjectID, input.ItemID))
	if err != nil {
		return MoveResult{}, err
	}
	if current.Version != input.ExpectedVersion {
		return MoveResult{}, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": input.ExpectedVersion, "current_version": current.Version})
	}
	if input.BeforeID != "" && input.BeforeID == input.AfterID {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "before_id and after_id must be different", nil)
	}
	sourceKey := orderingKey(scope, current.Status, current.SprintID)
	destinationKey := orderingKey(scope, targetStatus, current.SprintID)
	ordering := []struct {
		key     string
		status  string
		version *int64
	}{
		{key: sourceKey, status: current.Status},
	}
	if destinationKey != sourceKey {
		ordering = append(ordering, struct {
			key     string
			status  string
			version *int64
		}{key: destinationKey, status: targetStatus})
	}
	if ordering[0].key > ordering[len(ordering)-1].key {
		ordering[0], ordering[len(ordering)-1] = ordering[len(ordering)-1], ordering[0]
	}
	for index := range ordering {
		version, lockErr := r.lockColumnOrdering(ctx, exec, scope, ordering[index].status, current.SprintID)
		if lockErr != nil {
			return MoveResult{}, lockErr
		}
		ordering[index].version = &version
	}
	sourceVersion, destinationVersion := *ordering[0].version, *ordering[0].version
	if destinationKey != sourceKey {
		if ordering[1].key == sourceKey {
			sourceVersion, destinationVersion = *ordering[1].version, *ordering[0].version
		} else {
			sourceVersion, destinationVersion = *ordering[0].version, *ordering[1].version
		}
	}
	if input.ExpectedSourceOrderingVersion != sourceVersion || input.ExpectedDestinationOrderingVersion != destinationVersion {
		return MoveResult{}, staleOrderingError(input.ExpectedSourceOrderingVersion, sourceVersion, input.ExpectedDestinationOrderingVersion, destinationVersion)
	}
	beforeRank, afterRank, err := r.moveNeighborRanks(ctx, exec, scope, current, targetStatus, input.BeforeID, input.AfterID)
	if err != nil {
		return MoveResult{}, err
	}
	rank, needsRebalance, err := r.destinationRank(ctx, exec, scope, current, targetStatus, input, beforeRank, afterRank)
	if err != nil {
		return MoveResult{}, err
	}
	if needsRebalance {
		if err := r.rebalanceColumn(ctx, exec, scope, targetStatus, current.SprintID, current.ID); err != nil {
			return MoveResult{}, err
		}
		beforeRank, afterRank, err = r.moveNeighborRanks(ctx, exec, scope, current, targetStatus, input.BeforeID, input.AfterID)
		if err != nil {
			return MoveResult{}, err
		}
		rank, _, err = r.destinationRank(ctx, exec, scope, current, targetStatus, input, beforeRank, afterRank)
		if err != nil {
			return MoveResult{}, err
		}
	}
	statusChanged := current.Status != targetStatus
	rankChanged := current.BacklogRank != rank
	if !statusChanged && !rankChanged {
		return MoveResult{Item: current, SourceOrderingVersion: sourceVersion, DestinationOrderingVersion: destinationVersion}, nil
	}
	var statusID string
	if err := exec.QueryRow(ctx, `SELECT ws.id::text FROM workflow_statuses ws JOIN workflows w ON w.id=ws.workflow_id WHERE w.organization_id=$1 AND w.project_id=$2 AND ws.key=$3`, scope.OrganizationID, scope.ProjectID, targetStatus).Scan(&statusID); err != nil {
		if err == pgx.ErrNoRows {
			return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "target status is not configured for this project", map[string]any{"status": targetStatus})
		}
		return MoveResult{}, fmt.Errorf("load target status: %w", err)
	}
	// Preserve the existing gap-rank semantics: ordinary moves touch one item;
	// only exhausted gaps take the server-side rebalance path above.
	tag, err := exec.Exec(ctx, `UPDATE work_items SET status_id=$1, backlog_rank=$2, version=version+1, updated_at=now() WHERE organization_id=$3 AND project_id=$4 AND id=$5 AND version=$6`, statusID, rank, scope.OrganizationID, scope.ProjectID, current.ID, input.ExpectedVersion)
	if err != nil {
		return MoveResult{}, fmt.Errorf("move work item: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return MoveResult{}, apperr.New(apperr.CodeConflict, 409, "work item move is stale", nil)
	}
	updated, err := scanWorkItem(exec.QueryRow(ctx, workItemSelect+` WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3`, scope.OrganizationID, scope.ProjectID, current.ID))
	if err != nil {
		return MoveResult{}, err
	}
	if _, err := r.bumpColumnOrdering(ctx, exec, scope, current.Status, current.SprintID); err != nil {
		return MoveResult{}, err
	}
	sourceVersion++
	if destinationKey == sourceKey {
		destinationVersion = sourceVersion
	} else {
		if _, err := r.bumpColumnOrdering(ctx, exec, scope, targetStatus, current.SprintID); err != nil {
			return MoveResult{}, err
		}
		destinationVersion++
	}
	return MoveResult{Item: updated, SourceOrderingVersion: sourceVersion, DestinationOrderingVersion: destinationVersion, Reordered: rankChanged || statusChanged}, nil
}

func (r *PostgresRepository) lockColumnOrdering(ctx context.Context, exec platformdb.Executor, scope Scope, status, sprintID string) (int64, error) {
	if _, err := exec.Exec(ctx, `INSERT INTO work_item_column_orderings (organization_id, project_id, status_key, sprint_id) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, scope.OrganizationID, scope.ProjectID, status, sprintID); err != nil {
		return 0, fmt.Errorf("ensure column ordering: %w", err)
	}
	var version int64
	if err := exec.QueryRow(ctx, `SELECT ordering_version FROM work_item_column_orderings WHERE organization_id=$1 AND project_id=$2 AND status_key=$3 AND sprint_id=$4 FOR UPDATE`, scope.OrganizationID, scope.ProjectID, status, sprintID).Scan(&version); err != nil {
		return 0, fmt.Errorf("lock column ordering: %w", err)
	}
	return version, nil
}

func (r *PostgresRepository) bumpColumnOrdering(ctx context.Context, exec platformdb.Executor, scope Scope, status, sprintID string) (int64, error) {
	var version int64
	if err := exec.QueryRow(ctx, `UPDATE work_item_column_orderings SET ordering_version=ordering_version+1, updated_at=now() WHERE organization_id=$1 AND project_id=$2 AND status_key=$3 AND sprint_id=$4 RETURNING ordering_version`, scope.OrganizationID, scope.ProjectID, status, sprintID).Scan(&version); err != nil {
		return 0, fmt.Errorf("bump column ordering: %w", err)
	}
	return version, nil
}

func (r *PostgresRepository) moveNeighborRanks(ctx context.Context, exec platformdb.Executor, scope Scope, current *WorkItem, targetStatus, beforeID, afterID string) (int64, int64, error) {
	if beforeID == "" && afterID == "" {
		return 0, 0, nil
	}
	if beforeID != "" && beforeID == current.ID || afterID != "" && afterID == current.ID {
		return 0, 0, apperr.New(apperr.CodeInvalidArgument, 422, "a work item cannot be its own move neighbor", nil)
	}
	load := func(id string) (int64, error) {
		var rank int64
		err := exec.QueryRow(ctx, `SELECT wi.backlog_rank FROM work_items wi JOIN workflow_statuses ws ON ws.id=wi.status_id WHERE wi.organization_id=$1 AND wi.project_id=$2 AND wi.id=$3 AND ws.key=$4 AND wi.sprint_id IS NOT DISTINCT FROM NULLIF($5,'')::uuid AND wi.archived_at IS NULL`, scope.OrganizationID, scope.ProjectID, id, targetStatus, current.SprintID).Scan(&rank)
		if err == pgx.ErrNoRows {
			return 0, apperr.New(apperr.CodeNotFound, 404, "move neighbor not found in destination column", map[string]any{"id": id})
		}
		if err != nil {
			return 0, fmt.Errorf("load move neighbor: %w", err)
		}
		return rank, nil
	}
	var beforeRank, afterRank int64
	var err error
	if beforeID != "" {
		beforeRank, err = load(beforeID)
		if err != nil {
			return 0, 0, err
		}
	}
	if afterID != "" {
		afterRank, err = load(afterID)
		if err != nil {
			return 0, 0, err
		}
	}
	return beforeRank, afterRank, nil
}

func (r *PostgresRepository) destinationRank(ctx context.Context, exec platformdb.Executor, scope Scope, current *WorkItem, targetStatus string, input MoveInput, beforeRank, afterRank int64) (int64, bool, error) {
	if input.BeforeID != "" && input.AfterID != "" {
		if afterRank-beforeRank <= 1 {
			return 0, true, nil
		}
		return beforeRank + (afterRank-beforeRank)/2, false, nil
	}
	if input.BeforeID != "" {
		candidate := beforeRank + 1000
		var occupied bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_items wi JOIN workflow_statuses ws ON ws.id=wi.status_id WHERE wi.organization_id=$1 AND wi.project_id=$2 AND ws.key=$3 AND wi.sprint_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND wi.archived_at IS NULL AND wi.id<>$5 AND wi.backlog_rank >= $6)`, scope.OrganizationID, scope.ProjectID, targetStatus, current.SprintID, current.ID, candidate).Scan(&occupied); err != nil {
			return 0, false, fmt.Errorf("check destination rank: %w", err)
		}
		if occupied {
			return 0, true, nil
		}
		return candidate, false, nil
	}
	if input.AfterID != "" {
		candidate := afterRank - 1000
		var occupied bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_items wi JOIN workflow_statuses ws ON ws.id=wi.status_id WHERE wi.organization_id=$1 AND wi.project_id=$2 AND ws.key=$3 AND wi.sprint_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND wi.archived_at IS NULL AND wi.id<>$5 AND wi.backlog_rank <= $6)`, scope.OrganizationID, scope.ProjectID, targetStatus, current.SprintID, current.ID, candidate).Scan(&occupied); err != nil {
			return 0, false, fmt.Errorf("check destination rank: %w", err)
		}
		if occupied || candidate <= 0 {
			return 0, true, nil
		}
		return candidate, false, nil
	}
	var maxRank int64
	if err := exec.QueryRow(ctx, `SELECT COALESCE(MAX(wi.backlog_rank),0) FROM work_items wi JOIN workflow_statuses ws ON ws.id=wi.status_id WHERE wi.organization_id=$1 AND wi.project_id=$2 AND ws.key=$3 AND wi.sprint_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND wi.archived_at IS NULL AND wi.id<>$5`, scope.OrganizationID, scope.ProjectID, targetStatus, current.SprintID, current.ID).Scan(&maxRank); err != nil {
		return 0, false, fmt.Errorf("load destination rank: %w", err)
	}
	return maxRank + 1000, false, nil
}

func (r *PostgresRepository) rebalanceColumn(ctx context.Context, exec platformdb.Executor, scope Scope, status, sprintID, excludeID string) error {
	rows, err := exec.Query(ctx, `SELECT wi.id::text FROM work_items wi JOIN workflow_statuses ws ON ws.id=wi.status_id WHERE wi.organization_id=$1 AND wi.project_id=$2 AND ws.key=$3 AND wi.sprint_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND wi.archived_at IS NULL AND wi.id<>$5 ORDER BY wi.backlog_rank, wi.id FOR UPDATE`, scope.OrganizationID, scope.ProjectID, status, sprintID, excludeID)
	if err != nil {
		return fmt.Errorf("load column for rebalance: %w", err)
	}
	defer rows.Close()
	index := int64(1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan column item for rebalance: %w", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE work_items SET backlog_rank=$1, version=version+1, updated_at=now() WHERE organization_id=$2 AND project_id=$3 AND id=$4`, index*1000, scope.OrganizationID, scope.ProjectID, id); err != nil {
			return fmt.Errorf("rebalance column item: %w", err)
		}
		index++
	}
	return rows.Err()
}

func (r *PostgresRepository) ColumnOrderingVersions(ctx context.Context, scope Scope, sprintID string) (map[string]int64, error) {
	rows, err := platformdb.ExecutorFrom(ctx, r.pool).Query(ctx, `SELECT status_key, ordering_version FROM work_item_column_orderings WHERE organization_id=$1 AND project_id=$2 AND sprint_id=$3`, scope.OrganizationID, scope.ProjectID, sprintID)
	if err != nil {
		return nil, fmt.Errorf("list column ordering versions: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var status string
		var version int64
		if err := rows.Scan(&status, &version); err != nil {
			return nil, fmt.Errorf("scan column ordering version: %w", err)
		}
		result[status] = version
	}
	return result, rows.Err()
}
