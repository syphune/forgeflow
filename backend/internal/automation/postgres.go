package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

const ruleSelect = `SELECT id::text, organization_id::text, project_id::text, name, event_type, action_type, config, enabled, created_at, updated_at FROM automation_rules`

func scanRule(row interface{ Scan(...any) error }) (Rule, error) {
	var item Rule
	var config []byte
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.Name, &item.EventType, &item.ActionType, &config, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Rule{}, err
	}
	if err := json.Unmarshal(config, &item.Config); err != nil {
		return Rule{}, fmt.Errorf("decode automation config: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) Create(ctx context.Context, organizationID string, input CreateInput) (Rule, error) {
	id, err := ids.New()
	if err != nil {
		return Rule{}, err
	}
	config, _ := json.Marshal(input.Config)
	item, err := scanRule(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO automation_rules (id,organization_id,project_id,name,event_type,action_type,config) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id::text, organization_id::text, project_id::text, name, event_type, action_type, config, enabled, created_at, updated_at`, id, organizationID, input.ProjectID, input.Name, input.EventType, input.ActionType, config))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Rule{}, apperr.New(apperr.CodeConflict, 409, "automation rule name is already used", nil)
		}
		return Rule{}, fmt.Errorf("create automation rule: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) List(ctx context.Context, organizationID, projectID string) ([]Rule, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, ruleSelect+` WHERE organization_id=$1 AND project_id=$2 ORDER BY created_at,id`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list automation rules: %w", err)
	}
	defer rows.Close()
	result := make([]Rule, 0)
	for rows.Next() {
		item, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan automation rule: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) SetEnabled(ctx context.Context, organizationID, projectID, id string, enabled bool) (Rule, error) {
	item, err := scanRule(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, ruleSelect+` WHERE organization_id=$1 AND project_id=$2 AND id=$3`, organizationID, projectID, id))
	if err == pgx.ErrNoRows {
		return Rule{}, apperr.New(apperr.CodeNotFound, 404, "automation rule not found", nil)
	}
	if err != nil {
		return Rule{}, fmt.Errorf("load automation rule: %w", err)
	}
	_, err = platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE automation_rules SET enabled=$1, updated_at=now() WHERE organization_id=$2 AND project_id=$3 AND id=$4`, enabled, organizationID, projectID, id)
	if err != nil {
		return Rule{}, fmt.Errorf("update automation rule: %w", err)
	}
	item.Enabled = enabled
	return item, nil
}

func (s *PostgresStore) Delete(ctx context.Context, organizationID, projectID, id string) error {
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM automation_rules WHERE organization_id=$1 AND project_id=$2 AND id=$3`, organizationID, projectID, id)
	if err != nil {
		return fmt.Errorf("delete automation rule: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperr.New(apperr.CodeNotFound, 404, "automation rule not found", nil)
	}
	return nil
}

func (s *PostgresStore) Matching(ctx context.Context, event outbox.Event) ([]Rule, error) {
	projectID := eventProjectID(event)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, ruleSelect+` WHERE organization_id=$1 AND enabled AND event_type=$2 AND (
		(project_id=$5 AND $5<>'' AND $4 IN ('work_item','repository','github'))
		OR (project_id=(SELECT project_id FROM work_items WHERE organization_id=$1 AND id=$3) AND $4='work_item')
		OR (project_id IN (SELECT project_id FROM repository_links WHERE organization_id=$1 AND repository_id=$3) AND $4='github' AND $5='')
		OR event_type='github.installation.connected'
	) ORDER BY created_at,id`, event.OrganizationID, event.EventType, event.AggregateID, event.AggregateType, projectID)
	if err != nil {
		return nil, fmt.Errorf("match automation rules: %w", err)
	}
	defer rows.Close()
	result := make([]Rule, 0)
	for rows.Next() {
		item, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan matched automation rule: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) ClaimExecution(ctx context.Context, organizationID, ruleID, eventID string) (bool, error) {
	id, err := ids.New()
	if err != nil {
		return false, err
	}
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `INSERT INTO automation_executions (id,organization_id,rule_id,event_id,status) VALUES ($1,$2,$3,$4,'PROCESSING') ON CONFLICT (organization_id,rule_id,event_id) DO UPDATE SET status='PROCESSING', error='' WHERE automation_executions.status='FAILED'`, id, organizationID, ruleID, eventID)
	if err != nil {
		return false, fmt.Errorf("claim automation execution: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PostgresStore) FinishExecution(ctx context.Context, organizationID, ruleID, eventID string, cause error) error {
	status := "COMPLETED"
	message := ""
	if cause != nil {
		status = "FAILED"
		message = cause.Error()
		if len(message) > 4096 {
			message = message[:4096]
		}
	}
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE automation_executions SET status=$1,error=$2,completed_at=CASE WHEN $1='COMPLETED' THEN now() ELSE NULL END WHERE organization_id=$3 AND rule_id=$4 AND event_id=$5`, status, message, organizationID, ruleID, eventID)
	if err != nil {
		return fmt.Errorf("finish automation execution: %w", err)
	}
	return nil
}

var _ Store = (*PostgresStore)(nil)
