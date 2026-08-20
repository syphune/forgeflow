ALTER TABLE specification_reproduction_steps
    ADD COLUMN verified_by uuid REFERENCES users(id),
    ADD COLUMN verified_at timestamptz;

ALTER TABLE specification_acceptance_criteria
    ADD COLUMN verified_by uuid REFERENCES users(id),
    ADD COLUMN verified_at timestamptz;
