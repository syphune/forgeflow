ALTER TABLE pull_requests
    ADD COLUMN draft boolean NOT NULL DEFAULT false;
