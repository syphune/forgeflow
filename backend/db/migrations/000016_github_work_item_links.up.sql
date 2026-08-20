ALTER TABLE pull_requests
    ADD COLUMN work_item_id uuid,
    ADD COLUMN head_ref text NOT NULL DEFAULT '',
    ADD COLUMN body text NOT NULL DEFAULT '',
    ADD CONSTRAINT pull_requests_work_item_fk
        FOREIGN KEY (organization_id, work_item_id)
        REFERENCES work_items(organization_id, id);

CREATE INDEX pull_requests_work_item_idx
    ON pull_requests (organization_id, work_item_id)
    WHERE work_item_id IS NOT NULL;
