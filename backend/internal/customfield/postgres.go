package customfield

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

const definitionSelect = `SELECT id::text, organization_id::text, project_id::text, key, display_name, value_type, options, is_required, created_at, updated_at FROM custom_field_definitions`

func scanDefinition(row interface{ Scan(...any) error }) (Definition, error) {
	var item Definition
	var rawOptions []byte
	if err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.Key, &item.DisplayName, &item.ValueType, &rawOptions, &item.Required, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Definition{}, err
	}
	if err := json.Unmarshal(rawOptions, &item.Options); err != nil {
		return Definition{}, fmt.Errorf("decode custom field options: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) ListDefinitions(ctx context.Context, organizationID, projectID string) ([]Definition, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, definitionSelect+` WHERE organization_id=$1 AND project_id=$2 ORDER BY display_name,id`, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list custom fields: %w", err)
	}
	defer rows.Close()
	result := make([]Definition, 0)
	for rows.Next() {
		item, scanErr := scanDefinition(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan custom field: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetDefinition(ctx context.Context, organizationID, projectID, id string) (Definition, error) {
	item, err := scanDefinition(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, definitionSelect+` WHERE organization_id=$1 AND project_id=$2 AND id=$3`, organizationID, projectID, id))
	if err == pgx.ErrNoRows {
		return Definition{}, apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	if err != nil {
		return Definition{}, fmt.Errorf("get custom field: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) CreateDefinition(ctx context.Context, organizationID string, input CreateInput) (Definition, error) {
	id, err := ids.New()
	if err != nil {
		return Definition{}, err
	}
	options, _ := json.Marshal(input.Options)
	item, err := scanDefinition(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `INSERT INTO custom_field_definitions (id,organization_id,project_id,key,display_name,value_type,options,is_required) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text,organization_id::text,project_id::text,key,display_name,value_type,options,is_required,created_at,updated_at`, id, organizationID, input.ProjectID, input.Key, input.DisplayName, input.ValueType, options, input.Required))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Definition{}, apperr.New(apperr.CodeConflict, 409, "custom field key is already used", nil)
		}
		return Definition{}, fmt.Errorf("create custom field: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) UpdateDefinition(ctx context.Context, organizationID, projectID, id string, input UpdateInput) (Definition, error) {
	current, err := s.GetDefinition(ctx, organizationID, projectID, id)
	if err != nil {
		return Definition{}, err
	}
	name := current.DisplayName
	if input.DisplayName != nil {
		name = *input.DisplayName
	}
	options := current.Options
	if input.Options != nil {
		options = *input.Options
	}
	required := current.Required
	if input.Required != nil {
		required = *input.Required
	}
	rawOptions, _ := json.Marshal(options)
	item, err := scanDefinition(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `UPDATE custom_field_definitions SET display_name=$1,options=$2,is_required=$3,updated_at=now() WHERE organization_id=$4 AND project_id=$5 AND id=$6 RETURNING id::text,organization_id::text,project_id::text,key,display_name,value_type,options,is_required,created_at,updated_at`, name, rawOptions, required, organizationID, projectID, id))
	if err == pgx.ErrNoRows {
		return Definition{}, apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	if err != nil {
		return Definition{}, fmt.Errorf("update custom field: %w", err)
	}
	return item, nil
}

func (s *PostgresStore) DeleteDefinition(ctx context.Context, organizationID, projectID, id string) error {
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM custom_field_definitions WHERE organization_id=$1 AND project_id=$2 AND id=$3`, organizationID, projectID, id)
	if err != nil {
		return fmt.Errorf("delete custom field: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperr.New(apperr.CodeNotFound, 404, "custom field not found", nil)
	}
	return nil
}

func (s *PostgresStore) ListValues(ctx context.Context, organizationID, projectID, workItemID string) ([]Value, error) {
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT d.id::text, w.id::text, d.key, d.display_name, d.value_type, d.options,
       COALESCE(v.text_value, v.number_value::text, v.boolean_value::text, v.date_value::text, v.option_value, ''),
       COALESCE(v.updated_at, d.updated_at)
FROM custom_field_definitions d
JOIN work_items w ON w.organization_id=d.organization_id AND w.project_id=d.project_id
LEFT JOIN work_item_custom_values v ON v.organization_id=d.organization_id AND v.project_id=d.project_id AND v.work_item_id=w.id AND v.field_id=d.id
WHERE d.organization_id=$1 AND d.project_id=$2 AND w.id=$3
ORDER BY d.display_name,d.id`, organizationID, projectID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list custom field values: %w", err)
	}
	defer rows.Close()
	result := make([]Value, 0)
	for rows.Next() {
		var item Value
		var rawOptions []byte
		if err := rows.Scan(&item.DefinitionID, &item.WorkItemID, &item.Key, &item.DisplayName, &item.ValueType, &rawOptions, &item.Value, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan custom field value: %w", err)
		}
		if err := json.Unmarshal(rawOptions, &item.Options); err != nil {
			return nil, fmt.Errorf("decode custom field value options: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		var exists bool
		if err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND id=$3)`, organizationID, projectID, workItemID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
		}
	}
	return result, nil
}

func (s *PostgresStore) SetValue(ctx context.Context, organizationID, projectID, workItemID, fieldID string, value TypedValue) (Value, error) {
	definition, err := s.GetDefinition(ctx, organizationID, projectID, fieldID)
	if err != nil {
		return Value{}, err
	}
	var textValue any
	var numberValue any
	var booleanValue any
	var dateValue any
	var optionValue any
	switch definition.ValueType {
	case Text:
		textValue = value.Text
	case Number:
		numberValue = value.Number
	case Boolean:
		booleanValue = value.Boolean
	case Date:
		dateValue = value.Date
	case Select:
		optionValue = value.Option
	}
	var item Value
	err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
INSERT INTO work_item_custom_values (organization_id,project_id,work_item_id,field_id,text_value,number_value,boolean_value,date_value,option_value)
SELECT $1,$2,w.id,$4,$5,$6,$7,NULLIF($8,'')::date,$9 FROM work_items w WHERE w.organization_id=$1 AND w.project_id=$2 AND w.id=$3
ON CONFLICT (organization_id,work_item_id,field_id) DO UPDATE SET text_value=EXCLUDED.text_value,number_value=EXCLUDED.number_value,boolean_value=EXCLUDED.boolean_value,date_value=EXCLUDED.date_value,option_value=EXCLUDED.option_value,updated_at=now()
RETURNING work_item_id::text,updated_at`, organizationID, projectID, workItemID, fieldID, derefString(textValue), derefFloat(numberValue), derefBool(booleanValue), derefDate(dateValue), derefString(optionValue)).Scan(&item.WorkItemID, &item.UpdatedAt)
	if err == pgx.ErrNoRows {
		return Value{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if err != nil {
		return Value{}, fmt.Errorf("set custom field value: %w", err)
	}
	item.DefinitionID, item.Key, item.DisplayName, item.ValueType, item.Options = definition.ID, definition.Key, definition.DisplayName, definition.ValueType, definition.Options
	return item, nil
}

func (s *PostgresStore) ClearValue(ctx context.Context, organizationID, projectID, workItemID, fieldID string) error {
	tag, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM work_item_custom_values v USING work_items w WHERE v.organization_id=$1 AND v.project_id=$2 AND v.work_item_id=$3 AND v.field_id=$4 AND w.organization_id=v.organization_id AND w.project_id=v.project_id AND w.id=v.work_item_id`, organizationID, projectID, workItemID, fieldID)
	if err != nil {
		return fmt.Errorf("clear custom field value: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_items WHERE organization_id=$1 AND project_id=$2 AND id=$3)`, organizationID, projectID, workItemID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
		}
	}
	return nil
}

func derefString(value any) any {
	if pointer, ok := value.(*string); ok && pointer != nil {
		return *pointer
	}
	return nil
}
func derefFloat(value any) any {
	if pointer, ok := value.(*float64); ok && pointer != nil {
		return *pointer
	}
	return nil
}
func derefBool(value any) any {
	if pointer, ok := value.(*bool); ok && pointer != nil {
		return *pointer
	}
	return nil
}
func derefDate(value any) string {
	if pointer, ok := value.(*time.Time); ok && pointer != nil {
		return pointer.Format("2006-01-02")
	}
	return ""
}

var _ Store = (*PostgresStore)(nil)
