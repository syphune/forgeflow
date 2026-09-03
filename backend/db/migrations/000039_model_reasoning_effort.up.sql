ALTER TABLE agent_runs
    ADD COLUMN reasoning_effort text NOT NULL DEFAULT '';

ALTER TABLE autonomous_runs
    ADD COLUMN reasoning_effort text NOT NULL DEFAULT '';
