ALTER TABLE comments
    ADD COLUMN updated_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by uuid REFERENCES users(id);

UPDATE comments SET updated_at = created_at WHERE updated_at IS NULL;
ALTER TABLE comments ALTER COLUMN updated_at SET DEFAULT now(), ALTER COLUMN updated_at SET NOT NULL;

CREATE INDEX comments_work_item_updated_idx ON comments (work_item_id, updated_at, id);
