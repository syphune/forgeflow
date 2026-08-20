CREATE TEMP TABLE default_workflow_transition_backfill (
    workflow_id uuid PRIMARY KEY
) ON COMMIT DROP;

INSERT INTO default_workflow_transition_backfill (workflow_id)
SELECT w.id
FROM workflows w
WHERE w.name = 'Default'
  AND NOT EXISTS (
      SELECT 1
      FROM workflow_transitions wt
      WHERE wt.workflow_id = w.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM workflow_statuses ws
      WHERE ws.workflow_id = w.id
        AND ws.key NOT IN (
            'RAW', 'REFINING', 'REVIEW_REQUIRED', 'READY',
            'IN_PROGRESS', 'CODE_REVIEW', 'QA', 'DONE', 'CANCELLED'
        )
  );

INSERT INTO workflow_statuses (workflow_id, key, display_name, category, position, is_terminal)
SELECT b.workflow_id, d.key, d.display_name, d.category, d.position, d.is_terminal
FROM default_workflow_transition_backfill b
CROSS JOIN (
    VALUES
        ('RAW', 'Raw', 'TODO', 10, false),
        ('REFINING', 'Refining', 'TODO', 20, false),
        ('REVIEW_REQUIRED', 'Review required', 'TODO', 30, false),
        ('READY', 'Ready', 'TODO', 40, false),
        ('IN_PROGRESS', 'In progress', 'IN_PROGRESS', 50, false),
        ('CODE_REVIEW', 'Code review', 'IN_PROGRESS', 60, false),
        ('QA', 'QA', 'IN_PROGRESS', 70, false),
        ('DONE', 'Done', 'DONE', 80, true),
        ('CANCELLED', 'Cancelled', 'CANCELLED', 90, true)
) AS d(key, display_name, category, position, is_terminal)
ON CONFLICT (workflow_id, key) DO NOTHING;

INSERT INTO workflow_transitions (workflow_id, from_status_id, to_status_id, key, display_name)
SELECT b.workflow_id, from_status.id, to_status.id, d.transition_key, d.display_name
FROM default_workflow_transition_backfill b
CROSS JOIN (
    VALUES
        ('RAW', 'REFINING', 'start_refining', 'Start refining'),
        ('RAW', 'CANCELLED', 'cancel', 'Cancel'),
        ('REFINING', 'REVIEW_REQUIRED', 'request_review', 'Request review'),
        ('REFINING', 'CANCELLED', 'cancel_from_refining', 'Cancel'),
        ('REVIEW_REQUIRED', 'READY', 'mark_ready', 'Mark ready'),
        ('REVIEW_REQUIRED', 'CANCELLED', 'cancel_from_review', 'Cancel'),
        ('READY', 'IN_PROGRESS', 'start_work', 'Start work'),
        ('READY', 'CANCELLED', 'cancel_from_ready', 'Cancel'),
        ('IN_PROGRESS', 'CODE_REVIEW', 'submit_code_review', 'Submit code review'),
        ('IN_PROGRESS', 'CANCELLED', 'cancel_from_progress', 'Cancel'),
        ('CODE_REVIEW', 'QA', 'move_to_qa', 'Move to QA'),
        ('QA', 'DONE', 'complete', 'Complete')
) AS d(from_key, to_key, transition_key, display_name)
JOIN workflow_statuses from_status
  ON from_status.workflow_id = b.workflow_id
 AND from_status.key = d.from_key
JOIN workflow_statuses to_status
  ON to_status.workflow_id = b.workflow_id
 AND to_status.key = d.to_key
ON CONFLICT (workflow_id, key) DO NOTHING;

INSERT INTO transition_rules (transition_id, rule_type)
SELECT wt.id, d.rule_type
FROM default_workflow_transition_backfill b
JOIN workflow_transitions wt ON wt.workflow_id = b.workflow_id
CROSS JOIN (
    VALUES
        ('mark_ready', 'require_specification_ready'),
        ('start_work', 'require_assignee'),
        ('start_work', 'require_repository'),
        ('submit_code_review', 'require_pull_request'),
        ('move_to_qa', 'require_ci_success'),
        ('complete', 'require_human_verification')
) AS d(transition_key, rule_type)
WHERE wt.key = d.transition_key
ON CONFLICT (transition_id, rule_type) DO NOTHING;
