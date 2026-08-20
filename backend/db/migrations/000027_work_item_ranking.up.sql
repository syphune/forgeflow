ALTER TABLE work_items
    ADD COLUMN backlog_rank bigint NOT NULL DEFAULT 0;

UPDATE work_items
SET backlog_rank = number * 1000
WHERE backlog_rank = 0;

CREATE INDEX work_items_project_sprint_rank_idx
    ON work_items (project_id, sprint_id, backlog_rank, id);
