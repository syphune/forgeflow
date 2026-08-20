CREATE TABLE transition_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transition_id uuid NOT NULL REFERENCES workflow_transitions(id),
    rule_type text NOT NULL,
    config jsonb NOT NULL DEFAULT '{}',
    UNIQUE (transition_id, rule_type)
);
