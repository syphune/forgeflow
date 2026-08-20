package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) SaveWorkflow(ctx context.Context, organizationID, projectID string, input SaveInput) (Workflow, error) {
	exec := db.ExecutorFrom(ctx, s.pool)
	var workflowID string
	if err := exec.QueryRow(ctx, `
		INSERT INTO workflows (organization_id, project_id, name)
		SELECT $1, $2, $3
		WHERE EXISTS (SELECT 1 FROM projects WHERE organization_id=$1 AND id=$2)
		ON CONFLICT (organization_id, project_id) DO UPDATE SET name=EXCLUDED.name
		RETURNING id::text`, organizationID, projectID, input.Name).Scan(&workflowID); err != nil {
		if err == pgx.ErrNoRows {
			return Workflow{}, apperr.New(apperr.CodeNotFound, 404, "project not found", nil)
		}
		return Workflow{}, fmt.Errorf("save workflow: %w", err)
	}

	statusIDs := make(map[string]string, len(input.Statuses))
	statusKeys := make([]string, 0, len(input.Statuses))
	for _, status := range input.Statuses {
		statusID, err := ids.New()
		if err != nil {
			return Workflow{}, err
		}
		if err := exec.QueryRow(ctx, `
			INSERT INTO workflow_statuses (id, workflow_id, key, display_name, category, position, is_terminal)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (workflow_id, key) DO UPDATE SET display_name=EXCLUDED.display_name, category=EXCLUDED.category, position=EXCLUDED.position, is_terminal=EXCLUDED.is_terminal
			RETURNING id::text`, statusID, workflowID, status.Key, status.Name, status.Category, status.Position, status.IsTerminal).Scan(&statusID); err != nil {
			return Workflow{}, fmt.Errorf("save workflow status %s: %w", status.Key, err)
		}
		statusIDs[status.Key] = statusID
		statusKeys = append(statusKeys, status.Key)
	}

	if _, err := exec.Exec(ctx, `DELETE FROM transition_rules tr USING workflow_transitions wt WHERE tr.transition_id=wt.id AND wt.workflow_id=$1`, workflowID); err != nil {
		return Workflow{}, fmt.Errorf("remove workflow transition rules: %w", err)
	}
	// Transition IDs are implementation details; replacing the project's
	// definition keeps pair uniqueness safe when two edges are swapped.
	if _, err := exec.Exec(ctx, `DELETE FROM workflow_transitions WHERE workflow_id=$1`, workflowID); err != nil {
		return Workflow{}, fmt.Errorf("remove workflow transitions: %w", err)
	}

	rows, err := exec.Query(ctx, `
		SELECT ws.key
		FROM workflow_statuses ws
		WHERE ws.workflow_id=$1 AND ws.key <> ALL($2::text[])
		  AND EXISTS (SELECT 1 FROM work_items wi WHERE wi.status_id=ws.id)
		ORDER BY ws.key`, workflowID, statusKeys)
	if err != nil {
		return Workflow{}, fmt.Errorf("check workflow status usage: %w", err)
	}
	var inUse []string
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			rows.Close()
			return Workflow{}, fmt.Errorf("scan workflow status usage: %w", scanErr)
		}
		inUse = append(inUse, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Workflow{}, fmt.Errorf("iterate workflow status usage: %w", err)
	}
	rows.Close()
	if len(inUse) > 0 {
		return Workflow{}, apperr.New(apperr.CodeConflict, 409, "cannot remove a workflow status used by work items", map[string]any{"statuses": inUse})
	}
	if _, err := exec.Exec(ctx, `DELETE FROM workflow_statuses WHERE workflow_id=$1 AND key <> ALL($2::text[])`, workflowID, statusKeys); err != nil {
		return Workflow{}, fmt.Errorf("remove workflow statuses: %w", err)
	}

	result := workflowFromInput(input)
	result.ID = workflowID
	for _, transition := range input.Transitions {
		fromID := statusIDs[transition.From]
		toID := statusIDs[transition.To]
		transitionID, err := ids.New()
		if err != nil {
			return Workflow{}, err
		}
		if err := exec.QueryRow(ctx, `
			INSERT INTO workflow_transitions (id, workflow_id, from_status_id, to_status_id, key, display_name)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (workflow_id, key) DO UPDATE SET from_status_id=EXCLUDED.from_status_id, to_status_id=EXCLUDED.to_status_id, display_name=EXCLUDED.display_name
			RETURNING id::text`, transitionID, workflowID, fromID, toID, transition.Key, transition.Name).Scan(&transitionID); err != nil {
			return Workflow{}, fmt.Errorf("save workflow transition %s: %w", transition.Key, err)
		}
		for _, rule := range transition.Required {
			config := []byte(`{}`)
			if rule == RequirePermission {
				var marshalErr error
				config, marshalErr = json.Marshal(map[string]any{"permissions": transition.RequiredPermissions})
				if marshalErr != nil {
					return Workflow{}, fmt.Errorf("encode workflow transition rule %s: %w", transition.Key, marshalErr)
				}
			}
			if _, err := exec.Exec(ctx, `INSERT INTO transition_rules (transition_id, rule_type, config) VALUES ($1,$2,$3)`, transitionID, rule, config); err != nil {
				return Workflow{}, fmt.Errorf("save workflow transition rule %s: %w", transition.Key, err)
			}
		}
	}
	return result, nil
}

func (s *PostgresStore) LoadWorkflow(ctx context.Context, organizationID, projectID string) (Workflow, error) {
	exec := db.ExecutorFrom(ctx, s.pool)
	var result Workflow
	if err := exec.QueryRow(ctx, `SELECT id::text, name FROM workflows WHERE organization_id=$1 AND project_id=$2`, organizationID, projectID).Scan(&result.ID, &result.Name); err != nil {
		if err == pgx.ErrNoRows {
			return Default(), nil
		}
		return Workflow{}, fmt.Errorf("load project workflow: %w", err)
	}
	result.Statuses = make(map[string]Status)
	rows, err := exec.Query(ctx, `SELECT key, display_name, category, position, is_terminal FROM workflow_statuses WHERE workflow_id=$1 ORDER BY position, key`, result.ID)
	if err != nil {
		return Workflow{}, fmt.Errorf("load workflow statuses: %w", err)
	}
	for rows.Next() {
		var item Status
		if err := rows.Scan(&item.Key, &item.Name, &item.Category, &item.Position, &item.IsTerminal); err != nil {
			rows.Close()
			return Workflow{}, fmt.Errorf("scan workflow status: %w", err)
		}
		result.Statuses[item.Key] = item
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Workflow{}, fmt.Errorf("iterate workflow statuses: %w", err)
	}
	rows.Close()
	if len(result.Statuses) == 0 {
		return Default(), nil
	}

	result.Transitions = make(map[string]Transition)
	transitionRows, err := exec.Query(ctx, `
		SELECT wt.id::text, wt.key, fs.key, ts.key, wt.display_name
		FROM workflow_transitions wt
		JOIN workflow_statuses fs ON fs.id=wt.from_status_id
		JOIN workflow_statuses ts ON ts.id=wt.to_status_id
		WHERE wt.workflow_id=$1
		ORDER BY wt.key`, result.ID)
	if err != nil {
		return Workflow{}, fmt.Errorf("load workflow transitions: %w", err)
	}
	type transitionRecord struct {
		id   string
		item Transition
	}
	records := make([]transitionRecord, 0)
	for transitionRows.Next() {
		var id string
		var item Transition
		if err := transitionRows.Scan(&id, &item.Key, &item.From, &item.To, &item.Name); err != nil {
			transitionRows.Close()
			return Workflow{}, fmt.Errorf("scan workflow transition: %w", err)
		}
		records = append(records, transitionRecord{id: id, item: item})
	}
	if err := transitionRows.Err(); err != nil {
		transitionRows.Close()
		return Workflow{}, fmt.Errorf("iterate workflow transitions: %w", err)
	}
	transitionRows.Close()
	for _, record := range records {
		ruleRows, err := exec.Query(ctx, `SELECT rule_type, config FROM transition_rules WHERE transition_id=$1 ORDER BY rule_type`, record.id)
		if err != nil {
			return Workflow{}, fmt.Errorf("load transition rules: %w", err)
		}
		for ruleRows.Next() {
			var rule string
			var rawConfig []byte
			if err := ruleRows.Scan(&rule, &rawConfig); err != nil {
				ruleRows.Close()
				return Workflow{}, fmt.Errorf("scan transition rule: %w", err)
			}
			record.item.Required = append(record.item.Required, RuleType(rule))
			if RuleType(rule) == RequirePermission {
				var config struct {
					Permissions []string `json:"permissions"`
				}
				if err := json.Unmarshal(rawConfig, &config); err != nil {
					ruleRows.Close()
					return Workflow{}, fmt.Errorf("decode transition permission rule: %w", err)
				}
				record.item.RequiredPermissions = append(record.item.RequiredPermissions, normalizePermissions(config.Permissions)...)
			}
		}
		if err := ruleRows.Err(); err != nil {
			ruleRows.Close()
			return Workflow{}, fmt.Errorf("iterate transition rules: %w", err)
		}
		ruleRows.Close()
		result.Transitions[record.item.Key] = record.item
	}
	return result, nil
}
