CREATE TABLE custom_field_definitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    key text NOT NULL CHECK (key ~ '^[A-Z][A-Z0-9_]{0,31}$'),
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    value_type text NOT NULL CHECK (value_type IN ('TEXT', 'NUMBER', 'BOOLEAN', 'DATE', 'SELECT')),
    options jsonb NOT NULL DEFAULT '[]'::jsonb,
    is_required boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id, key),
    UNIQUE (organization_id, project_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id) ON DELETE CASCADE
);

CREATE TABLE work_item_custom_values (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    work_item_id uuid NOT NULL,
    field_id uuid NOT NULL,
    text_value text,
    number_value numeric,
    boolean_value boolean,
    date_value date,
    option_value text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, work_item_id, field_id),
    FOREIGN KEY (organization_id, work_item_id) REFERENCES work_items(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, project_id, field_id) REFERENCES custom_field_definitions(organization_id, project_id, id) ON DELETE CASCADE,
    CHECK (((text_value IS NOT NULL)::int + (number_value IS NOT NULL)::int + (boolean_value IS NOT NULL)::int + (date_value IS NOT NULL)::int + (option_value IS NOT NULL)::int) = 1)
);

CREATE INDEX custom_field_definitions_project_idx ON custom_field_definitions (organization_id, project_id, display_name);
CREATE INDEX work_item_custom_values_item_idx ON work_item_custom_values (organization_id, project_id, work_item_id);
