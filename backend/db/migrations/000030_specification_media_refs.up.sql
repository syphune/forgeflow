ALTER TABLE specifications
    ADD COLUMN media_refs jsonb NOT NULL DEFAULT '{}'::jsonb;
